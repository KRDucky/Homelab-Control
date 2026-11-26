// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package f2fsinfo is the unprivileged, read-only f2fs view. It Stats the
// well-known dump.f2fs path, reads bounded /proc/filesystems for the f2fs
// type, lists fstype=f2fs mounts from bounded /proc/self/mountinfo, and reads
// each mounted filesystem's kernel counters from /sys/fs/f2fs (sysfs.go). It
// never executes dump.f2fs, never formats,
// grows, or repairs a filesystem, never loads a kernel module, and never
// opens a device node.
package f2fsinfo

import (
	"context"
	"homelab-control/backend/internal/mountinfo"
	"io"
	"os"
)

// Mount is one observed f2fs mount (device → mountpoint).
type Mount = mountinfo.DeviceMount

// Inventory is the f2fs half of the response.
type Inventory struct {
	Present         bool    `json:"present"`
	Reason          string  `json:"reason"`
	Mounts          []Mount `json:"mounts"`
	MountsTruncated bool    `json:"mountsTruncated"`
}

// Snapshot is the full read-only f2fs inventory.
type Snapshot struct {
	Tooling    Tooling    `json:"tooling"`
	F2fs       Inventory  `json:"f2fs"`
	KernelF2fs KernelF2fs `json:"kernelF2fs"`
	// Status is each mounted filesystem's live kernel counters (sysfs).
	Status Status `json:"status"`
}

// mountinfoReadLimits match the other inventory pages so a hostile or
// oversized kernel file cannot grow memory unbounded. f2fsMaxMounts caps
// returned rows; overflow sets mountsTruncated instead of failing.
const (
	f2fsMaxMounts       = 256
	reasonTooManyMounts = "too many mounts to list"
)

func collectMounts(osRoot string) ([]Mount, bool, Status, error) {
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return nil, false, Status{}, err
	}
	defer root.Close()
	f, err := root.Open("proc/self/mountinfo")
	if err != nil {
		return nil, false, Status{}, err
	}
	defer f.Close()
	recs, info, err := mountinfo.Parse(f)
	if err != nil {
		return nil, false, Status{}, err
	}
	mounts, truncated := mountinfo.DeviceMounts(recs, info, "f2fs", f2fsMaxMounts)
	return mounts, truncated, collectStatus(root, recs), nil
}

// Collect reads observed f2fs mounts, each mounted filesystem's sysfs
// counters, and probes the dump.f2fs binary plus kernel f2fs from
// /proc/filesystems. dump.f2fs is never executed. Missing or
// unreadable mountinfo becomes present=false with a reason. Only context
// cancellation returns an error.
func Collect(ctx context.Context, osRoot, family string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	snap.Tooling = ProbeTooling(family)
	snap.KernelF2fs = ProbeKernelF2fs(osRoot)
	mounts, truncated, status, err := collectMounts(osRoot)
	snap.Status = status
	if snap.Status.Filesystems == nil {
		snap.Status.Filesystems = []Filesystem{}
	}
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
		snap.F2fs = base
	} else if snap.Tooling.RequiredInstalled("dump.f2fs") || len(mounts) > 0 {
		base.Present = true
		snap.F2fs = base
	} else {
		base.Present = false
		base.Reason = absentReason(truncated, "dump.f2fs binary was not found")
		snap.F2fs = base
	}
	return snap, nil
}

// absentReason explains an empty inventory. A truncated read must not be
// reported as a missing binary: the mount may have been past the cap.
func absentReason(truncated bool, missingBinary string) string {
	if truncated {
		return "mount inventory was truncated; f2fs presence could not be determined"
	}
	return missingBinary
}

// ParseMountInfo returns the f2fs mounts in bounded kernel mountinfo (shared
// parser: internal/mountinfo). Mounts whose source is not a safe device path
// are omitted and mark the list truncated; so do skipped lines.
func ParseMountInfo(r io.Reader) ([]Mount, bool, error) {
	recs, info, err := mountinfo.Parse(r)
	if err != nil {
		return nil, false, err
	}
	mounts, truncated := mountinfo.DeviceMounts(recs, info, "f2fs", f2fsMaxMounts)
	return mounts, truncated, nil
}
