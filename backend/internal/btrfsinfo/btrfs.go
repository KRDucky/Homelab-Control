// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package btrfsinfo is the unprivileged, read-only Btrfs inventory (M7 first
// checkpoint). It Stats the well-known btrfs path, reads bounded
// /proc/filesystems for the btrfs type, and lists fstype=btrfs mounts from
// bounded /proc/self/mountinfo. It never executes btrfs, never formats,
// balances, scrubs, or replaces a device, never loads a kernel module, and
// never opens a device node. RAID5/6 is not offered.
package btrfsinfo

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Mount is one observed btrfs mount. Device is the mount source under /dev.
// Subvol and SubvolID come from the mount options when those keys are present.
type Mount struct {
	Mountpoint string `json:"mountpoint"`
	Device     string `json:"device"`
	Subvol     string `json:"subvol"`
	SubvolID   string `json:"subvolId"`
}

// Inventory is the btrfs half of the response.
type Inventory struct {
	Present         bool    `json:"present"`
	Reason          string  `json:"reason"`
	Mounts          []Mount `json:"mounts"`
	MountsTruncated bool    `json:"mountsTruncated"`
}

// Snapshot is the full read-only Btrfs inventory.
type Snapshot struct {
	Tooling     Tooling     `json:"tooling"`
	Btrfs       Inventory   `json:"btrfs"`
	KernelBtrfs KernelBtrfs `json:"kernelBtrfs"`
}

const (
	mountinfoMaxBytes   = 4 * 1024 * 1024
	mountinfoMaxMounts  = 8192
	mountinfoMaxLine    = 64 << 10
	btrfsMaxMounts      = 256
	deviceMaxBytes      = 256
	subvolMaxBytes      = 256
	reasonTooManyMounts = "too many mounts to list"
)

// ParseMountInfo parses bounded kernel mountinfo and retains fstype btrfs
// mounts only. Every other filesystem is dropped. A non-btrfs line is skipped
// even when the rest of the record is malformed. A line over mountinfoMaxLine
// is skipped and marks the inventory truncated. A line with no filesystem
// type, or a btrfs line with a bad mountpoint, still fails the parse. A btrfs
// source that is not a /dev path, or a subvolume option that is not a safe
// absolute path, is skipped.
func ParseMountInfo(r io.Reader) ([]Mount, bool, error) {
	br := bufio.NewReader(io.LimitReader(r, mountinfoMaxBytes+1))
	mounts := []Mount{}
	size := 0
	records := 0
	truncated := false
	for {
		line, n, tooLong, err := nextMountLine(br)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("cannot read bounded mount inventory: %w", err)
		}
		size += n
		if size > mountinfoMaxBytes {
			truncated = true
			break
		}
		if records >= mountinfoMaxMounts {
			truncated = true
			break
		}
		records++
		if tooLong {
			truncated = true
			continue
		}
		fields := strings.Fields(line)
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		fstype := ""
		if sep >= 0 && len(fields) > sep+1 {
			fstype = fields[sep+1]
		}
		if fstype != "btrfs" {
			if fstype == "" {
				return nil, false, fmt.Errorf("invalid mountinfo record")
			}
			continue
		}
		// super-options is fields[sep+3]. A shorter btrfs record is malformed.
		// Returning an error avoids indexing off the end.
		if sep < 6 || len(fields) < sep+4 {
			return nil, false, fmt.Errorf("invalid mountinfo record")
		}
		if _, e := strconv.ParseUint(fields[0], 10, 64); e != nil {
			return nil, false, fmt.Errorf("invalid mount identifier")
		}
		point, err := unescape(fields[4])
		if err != nil || !strings.HasPrefix(point, "/") || hasDotDot(point) {
			return nil, false, fmt.Errorf("invalid btrfs mountpoint")
		}
		source, err := unescape(fields[sep+2])
		if err != nil {
			return nil, false, fmt.Errorf("invalid btrfs source")
		}
		if !validDevice(source) {
			continue
		}
		subvol, subvolID, ok := parseSubvol(fields[sep+3])
		if !ok {
			truncated = true // A btrfs record was omitted because its options were unsafe or invalid.
			continue
		}
		if len(mounts) >= btrfsMaxMounts {
			truncated = true
			continue
		}
		mounts = append(mounts, Mount{Mountpoint: point, Device: source, Subvol: subvol, SubvolID: subvolID})
	}
	return mounts, truncated, nil
}

func nextMountLine(r *bufio.Reader) (line string, n int, tooLong bool, err error) {
	var b strings.Builder
	for {
		frag, readErr := r.ReadSlice('\n')
		n += len(frag)
		if !tooLong && len(frag) > 0 {
			if b.Len()+len(frag) > mountinfoMaxLine {
				tooLong = true
				b.Reset()
			} else {
				b.Write(frag)
			}
		}
		if readErr == bufio.ErrBufferFull {
			continue
		}
		if readErr == io.EOF {
			if n == 0 {
				return "", 0, false, io.EOF
			}
			return strings.TrimRight(b.String(), "\n"), n, tooLong, nil
		}
		if readErr != nil {
			return "", n, tooLong, readErr
		}
		return strings.TrimRight(b.String(), "\n"), n, tooLong, nil
	}
}

func validDevice(name string) bool {
	if len(name) < len("/dev/x") || len(name) > deviceMaxBytes || !strings.HasPrefix(name, "/dev/") {
		return false
	}
	if hasDotDot(name) || strings.Contains(name, "//") {
		return false
	}
	return true
}

// hasDotDot reports a path component that is exactly "..". A name such as
// foo..bar is kept. A component ".." is not.
func hasDotDot(path string) bool {
	for _, c := range strings.Split(path, "/") {
		if c == ".." {
			return true
		}
	}
	return false
}

// parseSubvol reads subvol= and subvolid= from a mount-options field.
// A missing key is empty. An unsafe subvol path returns ok=false.
func parseSubvol(options string) (subvol, id string, ok bool) {
	ok = true
	for _, part := range strings.Split(options, ",") {
		key, val, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		switch key {
		case "subvol":
			if len(val) == 0 || len(val) > subvolMaxBytes || !strings.HasPrefix(val, "/") || hasDotDot(val) {
				return "", "", false
			}
			subvol = val
		case "subvolid":
			if len(val) == 0 || len(val) > 20 {
				return "", "", false
			}
			for _, c := range val {
				if c < '0' || c > '9' {
					return "", "", false
				}
			}
			id = val
		}
	}
	return subvol, id, ok
}

func unescape(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+3 >= len(s) {
			return "", fmt.Errorf("invalid mount escape")
		}
		switch esc := s[i+1 : i+4]; esc {
		case "040":
			b.WriteByte(' ')
		case "011":
			b.WriteByte('\t')
		case "012":
			b.WriteByte('\n')
		case "134":
			b.WriteByte('\\')
		default:
			return "", fmt.Errorf("invalid mount escape")
		}
		i += 3
	}
	return b.String(), nil
}

func collectMounts(osRoot string) ([]Mount, bool, error) {
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	f, err := root.Open("proc/self/mountinfo")
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	return ParseMountInfo(f)
}

// Collect reads observed btrfs mounts and probes the btrfs binary plus kernel
// Btrfs from /proc/filesystems. btrfs is never executed. Missing or unreadable
// mountinfo becomes present=false with a reason. Only context cancellation
// returns an error.
func Collect(ctx context.Context, osRoot, family string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	snap.Tooling = ProbeTooling(family)
	snap.KernelBtrfs = ProbeKernelBtrfs(osRoot)
	mounts, truncated, err := collectMounts(osRoot)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Snapshot{}, ctxErr
	}
	if mounts == nil {
		mounts = []Mount{}
	}
	base := Inventory{Mounts: mounts, MountsTruncated: truncated}
	if err != nil {
		base.Present = false
		base.Reason = "mount inventory could not be read"
		base.Mounts = []Mount{}
		base.MountsTruncated = false
		snap.Btrfs = base
	} else if snap.Tooling.RequiredInstalled("btrfs") || len(mounts) > 0 {
		base.Present = true
		snap.Btrfs = base
	} else {
		base.Present = false
		if truncated {
			base.Reason = "mount inventory was truncated; btrfs presence could not be determined"
		} else {
			base.Reason = "btrfs binary was not found"
		}
		snap.Btrfs = base
	}
	return snap, nil
}
