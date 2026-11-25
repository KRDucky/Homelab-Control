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
	"context"
	"homelab-control/backend/internal/mountinfo"
	"io"
	"os"
)

// Mount is one observed xfs mount (device → mountpoint).
type Mount = mountinfo.DeviceMount

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
// returned rows; overflow or a skipped record sets mountsTruncated
// instead of failing.
const (
	xfsMaxMounts = 256
)

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

// absentReason explains an empty inventory. An incomplete read must not be
// reported as a missing binary: an xfs mount may have been past a cap or in
// a skipped record.
func absentReason(truncated bool, missingBinary string) string {
	if truncated {
		return "mount inventory was incomplete; xfs presence could not be determined"
	}
	return missingBinary
}

// ParseMountInfo returns the xfs mounts in bounded kernel mountinfo (shared
// parser: internal/mountinfo). Mounts whose source is not a safe device path
// are omitted and mark the list truncated; so do skipped lines.
func ParseMountInfo(r io.Reader) ([]Mount, bool, error) {
	recs, info, err := mountinfo.Parse(r)
	if err != nil {
		return nil, false, err
	}
	mounts, truncated := mountinfo.DeviceMounts(recs, info, "xfs", xfsMaxMounts)
	return mounts, truncated, nil
}
