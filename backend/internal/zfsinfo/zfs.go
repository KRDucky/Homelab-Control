// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package zfsinfo is the unprivileged, read-only OpenZFS inventory (M7 first
// checkpoint). It Stats well-known zfs/zpool paths, reads bounded
// /proc/filesystems for the zfs type, and lists fstype=zfs mounts from
// bounded /proc/self/mountinfo. It never executes zfs or zpool, never
// imports, exports, creates, or destroys a pool, never loads a kernel
// module, and never opens a device node.
package zfsinfo

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Mount is one observed zfs filesystem mount (dataset → mountpoint).
type Mount struct {
	Mountpoint string `json:"mountpoint"`
	Dataset    string `json:"dataset"`
}

// Inventory is the zfs half of the response.
type Inventory struct {
	Present         bool    `json:"present"`
	Reason          string  `json:"reason"`
	Mounts          []Mount `json:"mounts"`
	MountsTruncated bool    `json:"mountsTruncated"`
}

// Snapshot is the full read-only OpenZFS inventory.
type Snapshot struct {
	Tooling   Tooling   `json:"tooling"`
	Zfs       Inventory `json:"zfs"`
	KernelZfs KernelZfs `json:"kernelZfs"`
}

// mountinfoReadLimits match mergerfsinfo so a hostile or oversized kernel
// file cannot grow memory unbounded. zfsMaxMounts caps returned rows; overflow
// sets mountsTruncated instead of failing the inventory.
const (
	mountinfoMaxBytes   = 4 * 1024 * 1024
	mountinfoMaxMounts  = 8192
	mountinfoMaxLine    = 64 << 10
	zfsMaxMounts        = 256
	datasetMaxBytes     = 256
	reasonTooManyMounts = "too many mounts to list"
)

// ParseMountInfo parses bounded kernel mountinfo and retains fstype zfs
// mounts only. Dataset names are the mount source (not opened). Every other
// filesystem is dropped. Byte and record limits count every mountinfo line;
// zfs rows above zfsMaxMounts set truncated and keep the first N. A line that
// is not zfs is skipped even when the rest of the record is malformed. A line
// over mountinfoMaxLine is skipped and marks the inventory truncated. An
// unclassifiable line, or a zfs line with a bad mountpoint or source, still
// fails the parse.
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
		if fstype != "zfs" {
			if fstype == "" {
				return nil, false, fmt.Errorf("invalid mountinfo record")
			}
			continue
		}
		if sep < 6 || len(fields) < sep+3 {
			return nil, false, fmt.Errorf("invalid mountinfo record")
		}
		if _, e := strconv.ParseUint(fields[0], 10, 64); e != nil {
			return nil, false, fmt.Errorf("invalid mount identifier")
		}
		point, err := unescape(fields[4])
		if err != nil || !strings.HasPrefix(point, "/") {
			return nil, false, fmt.Errorf("invalid zfs mountpoint")
		}
		source, err := unescape(fields[sep+2])
		if err != nil {
			return nil, false, fmt.Errorf("invalid zfs source")
		}
		if !validDataset(source) {
			continue
		}
		if len(mounts) >= zfsMaxMounts {
			truncated = true
			continue
		}
		mounts = append(mounts, Mount{Mountpoint: point, Dataset: source})
	}
	return mounts, truncated, nil
}

// nextMountLine reads one mountinfo line. n includes the newline when present.
// tooLong is set when the line exceeds mountinfoMaxLine; the returned line is
// then empty and the rest of that line is discarded.
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

func validDataset(name string) bool {
	if name == "" || len(name) > datasetMaxBytes || strings.HasPrefix(name, "/") {
		return false
	}
	for _, c := range strings.Split(name, "/") {
		if c == "" || c == ".." {
			return false
		}
	}
	return true
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

// Collect reads observed zfs mounts and probes the required (zfs, zpool)
// binaries plus kernel ZFS from /proc/filesystems. zfs and zpool are never
// executed. Pools are not imported. Missing or unreadable inputs become
// present=false with a reason. Only context cancellation returns an error
// (the caller maps it to 503).
func Collect(ctx context.Context, osRoot, family string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	snap.Tooling = ProbeTooling(family)
	snap.KernelZfs = ProbeKernelZfs(osRoot)
	mounts, truncated, err := collectMounts(osRoot)
	if mounts == nil {
		mounts = []Mount{}
	}
	base := Inventory{Mounts: mounts, MountsTruncated: truncated}
	if err != nil {
		base.Present = false
		base.Reason = "mount inventory could not be read"
		base.Mounts = []Mount{}
		base.MountsTruncated = false
		snap.Zfs = base
	} else if snap.Tooling.RequiredInstalled("zfs") || snap.Tooling.RequiredInstalled("zpool") || len(mounts) > 0 {
		base.Present = true
		snap.Zfs = base
	} else {
		base.Present = false
		base.Reason = "zfs and zpool binaries were not found"
		snap.Zfs = base
	}
	return snap, nil
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
