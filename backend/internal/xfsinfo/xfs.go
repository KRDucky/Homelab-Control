// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package xfsinfo is the unprivileged, read-only XFS inventory (M7 first
// checkpoint). It Stats the well-known xfs_info path, reads bounded
// /proc/filesystems for the xfs type, and lists fstype=xfs mounts from
// bounded /proc/self/mountinfo. It never executes xfs_info, never formats,
// grows, or repairs a filesystem, never loads a kernel module, and never
// opens a device node.
package xfsinfo

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Mount is one observed xfs mount. Device is the mount source under /dev.
type Mount struct {
	Mountpoint string `json:"mountpoint"`
	Device     string `json:"device"`
}

// Inventory is the xfs half of the response.
type Inventory struct {
	Present         bool    `json:"present"`
	Reason          string  `json:"reason"`
	Mounts          []Mount `json:"mounts"`
	MountsTruncated bool    `json:"mountsTruncated"`
}

// Snapshot is the full read-only XFS inventory.
type Snapshot struct {
	Tooling   Tooling   `json:"tooling"`
	Xfs       Inventory `json:"xfs"`
	KernelXfs KernelXfs `json:"kernelXfs"`
}

// mountinfoReadLimits match the other inventory pages so a hostile or
// oversized kernel file cannot grow memory unbounded. xfsMaxMounts caps
// returned rows; overflow sets mountsTruncated instead of failing.
const (
	mountinfoMaxBytes   = 4 * 1024 * 1024
	mountinfoMaxMounts  = 8192
	mountinfoMaxLine    = 64 << 10
	xfsMaxMounts        = 256
	deviceMaxBytes      = 256
	reasonTooManyMounts = "too many mounts to list"
)

// ParseMountInfo parses bounded kernel mountinfo and retains fstype xfs
// mounts only. Every other filesystem is dropped. A non-xfs line is skipped
// even when the rest of the record is malformed. A line over mountinfoMaxLine
// is skipped and marks the inventory truncated. A line with no filesystem
// type, or a xfs line with a bad mountpoint or a record missing its options
// field, still fails the parse. A xfs source that is not a /dev path is
// skipped.
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
		if fstype != "xfs" {
			if fstype == "" {
				return nil, false, fmt.Errorf("invalid mountinfo record")
			}
			continue
		}
		// A xfs record must carry the super-options field (fields[sep+3]);
		// a shorter record is malformed. Returning an error avoids indexing
		// off the end.
		if sep < 6 || len(fields) < sep+4 {
			return nil, false, fmt.Errorf("invalid mountinfo record")
		}
		if _, e := strconv.ParseUint(fields[0], 10, 64); e != nil {
			return nil, false, fmt.Errorf("invalid mount identifier")
		}
		point, err := unescape(fields[4])
		if err != nil || !strings.HasPrefix(point, "/") || hasDotDot(point) {
			return nil, false, fmt.Errorf("invalid xfs mountpoint")
		}
		source, err := unescape(fields[sep+2])
		if err != nil {
			return nil, false, fmt.Errorf("invalid xfs source")
		}
		if !validDevice(source) {
			continue
		}
		if len(mounts) >= xfsMaxMounts {
			truncated = true
			continue
		}
		mounts = append(mounts, Mount{Mountpoint: point, Device: source})
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

// Collect reads observed xfs mounts and probes the xfs_info binary plus kernel
// XFS from /proc/filesystems. xfs_info is never executed. Missing or
// unreadable mountinfo becomes present=false with a reason. Only context
// cancellation returns an error.
func Collect(ctx context.Context, osRoot, family string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	snap.Tooling = ProbeTooling(family)
	snap.KernelXfs = ProbeKernelXfs(osRoot)
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
		snap.Xfs = base
	} else if snap.Tooling.RequiredInstalled("xfs_info") || len(mounts) > 0 {
		base.Present = true
		snap.Xfs = base
	} else {
		base.Present = false
		base.Reason = absentReason(truncated, "xfs_info binary was not found")
		snap.Xfs = base
	}
	return snap, nil
}

// absentReason explains an empty inventory. A truncated read must not be
// reported as a missing binary: the mount may have been past the cap.
func absentReason(truncated bool, missingBinary string) string {
	if truncated {
		return "mount inventory was truncated; xfs presence could not be determined"
	}
	return missingBinary
}
