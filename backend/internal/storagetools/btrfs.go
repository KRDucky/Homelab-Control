// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagetools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Btrfs: the helper runs four btrfs-progs commands per mounted filesystem,
// on a mountpoint it picked from its own mountinfo, and these parsers decode
// their reports: `filesystem usage -b` (allocation by profile and device),
// `device stats` (per-device error counters), `scrub status` and
// `subvolume list`. btrfs-progs has no machine-readable mode for these on
// Ubuntu 22.04 (5.16), so the stable text forms are parsed.

// Bounds on Btrfs output.
const (
	MaxBtrfsBytes       = 256 << 10 // per command, except subvolume list
	MaxBtrfsSubvolBytes = 1 << 20   // subvolume list (snapshot-heavy hosts)
	MaxBtrfsFilesystems = 16
	MaxBtrfsSubvolumes  = 1024 // per filesystem
	MaxBtrfsDevices     = 64   // per filesystem
)

// BtrfsUsageArgs, BtrfsDeviceStatsArgs, BtrfsScrubStatusArgs and
// BtrfsSubvolumeListArgs are the fixed templates. mnt is an absolute
// mountpoint chosen by the helper, never by a caller.
func BtrfsUsageArgs(mnt string) []string       { return []string{"filesystem", "usage", "-b", mnt} }
func BtrfsDeviceStatsArgs(mnt string) []string { return []string{"device", "stats", mnt} }
func BtrfsScrubStatusArgs(mnt string) []string { return []string{"scrub", "status", mnt} }
func BtrfsSubvolumeListArgs(mnt string) []string {
	return []string{"subvolume", "list", mnt}
}

// BtrfsReport lists the mounted Btrfs filesystems the helper could inspect.
type BtrfsReport struct {
	Filesystems []BtrfsFilesystem `json:"filesystems"`
	// Skipped names filesystems the helper cannot open (only mounted under a
	// path its sandbox hides) and filesystems over the limit.
	Skipped   []SkippedMount `json:"skipped,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
}

// SkippedMount is a mounted filesystem the helper did not inspect, and why
// (Btrfs, ext4 and XFS reports).
type SkippedMount struct {
	Mountpoint string `json:"mountpoint"`
	Reason     string `json:"reason"`
}

// BtrfsFilesystem is one filesystem (all mounts of one superblock).
type BtrfsFilesystem struct {
	UUID string `json:"uuid,omitempty"`
	// Mountpoint is the path the reports were taken from; Mounts lists every
	// mount of this filesystem (subvolumes included).
	Mountpoint string         `json:"mountpoint"`
	Mounts     []string       `json:"mounts"`
	Overall    BtrfsOverall   `json:"overall"`
	Profiles   []BtrfsProfile `json:"profiles"`
	Devices    []BtrfsDevice  `json:"devices"`
	Scrub      BtrfsScrub     `json:"scrub"`
	Subvolumes []BtrfsSubvol  `json:"subvolumes"`
	// SubvolumesTruncated: more than MaxBtrfsSubvolumes were listed.
	SubvolumesTruncated bool `json:"subvolumes_truncated,omitempty"`
	// Errors are the commands that failed for this filesystem.
	Errors []string `json:"errors,omitempty"`
}

// BtrfsOverall is the "Overall:" block of `btrfs filesystem usage -b`.
type BtrfsOverall struct {
	DeviceSize    *uint64  `json:"device_size,omitempty"`
	Allocated     *uint64  `json:"allocated,omitempty"`
	Unallocated   *uint64  `json:"unallocated,omitempty"`
	Missing       *uint64  `json:"missing,omitempty"`
	Used          *uint64  `json:"used,omitempty"`
	FreeEstimated *uint64  `json:"free_estimated,omitempty"`
	DataRatio     *float64 `json:"data_ratio,omitempty"`
	MetadataRatio *float64 `json:"metadata_ratio,omitempty"`
}

// BtrfsProfile is one block-group type with its RAID profile, e.g.
// Data,RAID1. Type "Unallocated" carries the per-device unallocated space.
type BtrfsProfile struct {
	Type    string            `json:"type"`
	Profile string            `json:"profile,omitempty"`
	Size    *uint64           `json:"size,omitempty"`
	Used    *uint64           `json:"used,omitempty"`
	Devices []BtrfsDeviceUsed `json:"devices"`
}

// BtrfsDeviceUsed is a device's share of one profile.
type BtrfsDeviceUsed struct {
	Path  string `json:"path"`
	Bytes uint64 `json:"bytes"`
}

// BtrfsDevice is one device with its `btrfs device stats` error counters.
type BtrfsDevice struct {
	Path           string  `json:"path"`
	WriteIOErrs    *uint64 `json:"write_io_errs,omitempty"`
	ReadIOErrs     *uint64 `json:"read_io_errs,omitempty"`
	FlushIOErrs    *uint64 `json:"flush_io_errs,omitempty"`
	CorruptionErrs *uint64 `json:"corruption_errs,omitempty"`
	GenerationErrs *uint64 `json:"generation_errs,omitempty"`
}

// BtrfsScrub is the `btrfs scrub status` report, as the tool words it.
type BtrfsScrub struct {
	Started      string `json:"started,omitempty"`
	Status       string `json:"status,omitempty"`
	Duration     string `json:"duration,omitempty"`
	ErrorSummary string `json:"error_summary,omitempty"`
	// NeverRun: the tool said "no stats available".
	NeverRun bool `json:"never_run,omitempty"`
}

// BtrfsSubvol is one `btrfs subvolume list` row.
type BtrfsSubvol struct {
	ID       uint64 `json:"id"`
	TopLevel uint64 `json:"top_level"`
	Path     string `json:"path"`
}

var (
	btrfsProfileRe = regexp.MustCompile(`^([A-Za-z+]+),([A-Za-z0-9]+): Size:(\d+), Used:(\d+)`)
	btrfsDevLineRe = regexp.MustCompile(`^\s+(/\S+)\s+(\d+)$`)
	btrfsStatRe    = regexp.MustCompile(`^\[(/[^\]]+)\]\.([a-z_]+)\s+(\d+)$`)
	btrfsSubvolRe  = regexp.MustCompile(`^ID (\d+) gen \d+ top level (\d+) path (.+)$`)
)

// ParseBtrfsUsage reads `btrfs filesystem usage -b`.
func ParseBtrfsUsage(b []byte) (BtrfsOverall, []BtrfsProfile, error) {
	if len(b) > MaxBtrfsBytes {
		return BtrfsOverall{}, nil, fmt.Errorf("btrfs usage exceeds %d bytes", MaxBtrfsBytes)
	}
	var o BtrfsOverall
	profiles := []BtrfsProfile{}
	var cur *BtrfsProfile
	sawOverall := false
	for _, line := range lines(b) {
		if strings.TrimSpace(line) == "Overall:" {
			sawOverall, cur = true, nil
			continue
		}
		if m := btrfsProfileRe.FindStringSubmatch(line); m != nil {
			profiles = append(profiles, BtrfsProfile{Type: m[1], Profile: m[2], Size: num(m[3]), Used: num(m[4]), Devices: []BtrfsDeviceUsed{}})
			cur = &profiles[len(profiles)-1]
			continue
		}
		if strings.TrimSpace(line) == "Unallocated:" {
			profiles = append(profiles, BtrfsProfile{Type: "Unallocated", Devices: []BtrfsDeviceUsed{}})
			cur = &profiles[len(profiles)-1]
			continue
		}
		if m := btrfsDevLineRe.FindStringSubmatch(line); m != nil && cur != nil {
			if len(cur.Devices) < MaxBtrfsDevices {
				v, _ := strconv.ParseUint(m[2], 10, 64)
				cur.Devices = append(cur.Devices, BtrfsDeviceUsed{Path: clipText(m[1]), Bytes: v})
			}
			continue
		}
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || cur != nil {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		switch k {
		case "Device size":
			o.DeviceSize = num(f[0])
		case "Device allocated":
			o.Allocated = num(f[0])
		case "Device unallocated":
			o.Unallocated = num(f[0])
		case "Device missing":
			o.Missing = num(f[0])
		case "Used":
			o.Used = num(f[0])
		case "Free (estimated)":
			o.FreeEstimated = num(f[0])
		case "Data ratio":
			o.DataRatio = ratio(f[0])
		case "Metadata ratio":
			o.MetadataRatio = ratio(f[0])
		}
	}
	if !sawOverall {
		return BtrfsOverall{}, nil, fmt.Errorf("unexpected btrfs usage report")
	}
	return o, profiles, nil
}

// ParseBtrfsDeviceStats reads `btrfs device stats`, one row per counter.
func ParseBtrfsDeviceStats(b []byte) ([]BtrfsDevice, error) {
	if len(b) > MaxBtrfsBytes {
		return nil, fmt.Errorf("btrfs device stats exceeds %d bytes", MaxBtrfsBytes)
	}
	out := []BtrfsDevice{}
	index := map[string]int{}
	for _, line := range lines(b) {
		m := btrfsStatRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			return nil, fmt.Errorf("unexpected btrfs device stats row")
		}
		i, ok := index[m[1]]
		if !ok {
			if len(out) >= MaxBtrfsDevices {
				continue
			}
			i = len(out)
			index[m[1]] = i
			out = append(out, BtrfsDevice{Path: clipText(m[1])})
		}
		d, v := &out[i], num(m[3])
		switch m[2] {
		case "write_io_errs":
			d.WriteIOErrs = v
		case "read_io_errs":
			d.ReadIOErrs = v
		case "flush_io_errs":
			d.FlushIOErrs = v
		case "corruption_errs":
			d.CorruptionErrs = v
		case "generation_errs":
			d.GenerationErrs = v
		}
	}
	return out, nil
}

// ParseBtrfsScrub reads `btrfs scrub status`. It returns the filesystem
// UUID, which the report names on its first line.
func ParseBtrfsScrub(b []byte) (string, BtrfsScrub, error) {
	if len(b) > MaxBtrfsBytes {
		return "", BtrfsScrub{}, fmt.Errorf("btrfs scrub status exceeds %d bytes", MaxBtrfsBytes)
	}
	var uuid string
	var s BtrfsScrub
	for _, line := range lines(b) {
		t := strings.TrimSpace(line)
		if t == "no stats available" {
			s.NeverRun = true
			continue
		}
		k, v, ok := strings.Cut(t, ":")
		if !ok {
			continue
		}
		v = clipText(v)
		switch k {
		case "UUID":
			uuid = v
		case "Scrub started", "Scrub resumed":
			s.Started = v
		case "Status":
			s.Status = v
		case "Duration":
			s.Duration = v
		case "Error summary":
			s.ErrorSummary = v
		}
	}
	if uuid == "" {
		return "", BtrfsScrub{}, fmt.Errorf("unexpected btrfs scrub status report")
	}
	return uuid, s, nil
}

// ParseBtrfsSubvolumes reads `btrfs subvolume list`; truncated is set over
// MaxBtrfsSubvolumes rows.
func ParseBtrfsSubvolumes(b []byte) ([]BtrfsSubvol, bool, error) {
	if len(b) > MaxBtrfsSubvolBytes {
		return nil, false, fmt.Errorf("btrfs subvolume list exceeds %d bytes", MaxBtrfsSubvolBytes)
	}
	out := []BtrfsSubvol{}
	for _, line := range lines(b) {
		m := btrfsSubvolRe.FindStringSubmatch(line)
		if m == nil {
			return nil, false, fmt.Errorf("unexpected btrfs subvolume list row")
		}
		if len(out) >= MaxBtrfsSubvolumes {
			return out, true, nil
		}
		id, _ := strconv.ParseUint(m[1], 10, 64)
		top, _ := strconv.ParseUint(m[2], 10, 64)
		out = append(out, BtrfsSubvol{ID: id, TopLevel: top, Path: clipText(m[3])})
	}
	return out, false, nil
}

func ratio(s string) *float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}
