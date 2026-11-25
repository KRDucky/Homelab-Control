// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ext4info is the unprivileged, read-only ext4 inventory (M7 first
// checkpoint). It Stats the well-known dumpe2fs path, reads bounded
// /proc/filesystems for the ext4 type, and lists fstype=ext4 mounts from
// bounded /proc/self/mountinfo. It never executes dumpe2fs, never formats,
// grows, or repairs a filesystem, never loads a kernel module, and never
// opens a device node.
package ext4info

import (
	"context"
	"homelab-control/backend/internal/mountinfo"
	"io"
	"os"
)

// Mount is one observed ext4 mount (device → mountpoint).
type Mount = mountinfo.DeviceMount

// Inventory is the ext4 half of the response.
type Inventory struct {
	Present         bool    `json:"present"`
	Reason          string  `json:"reason"`
	Mounts          []Mount `json:"mounts"`
	MountsTruncated bool    `json:"mountsTruncated"`
}

// Snapshot is the full read-only ext4 inventory.
type Snapshot struct {
	Tooling    Tooling    `json:"tooling"`
	Ext4       Inventory  `json:"ext4"`
	KernelExt4 KernelExt4 `json:"kernelExt4"`
}

// mountinfoReadLimits match the other inventory pages so a hostile or
// oversized kernel file cannot grow memory unbounded. ext4MaxMounts caps
// returned rows; overflow sets mountsTruncated instead of failing.
const (
	ext4MaxMounts       = 256
	reasonTooManyMounts = "too many mounts to list"
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

// Collect reads observed ext4 mounts and probes the dumpe2fs binary plus kernel
// ext4 from /proc/filesystems. dumpe2fs is never executed. Missing or
// unreadable mountinfo becomes present=false with a reason. Only context
// cancellation returns an error.
func Collect(ctx context.Context, osRoot, family string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	snap.Tooling = ProbeTooling(family)
	snap.KernelExt4 = ProbeKernelExt4(osRoot)
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
		snap.Ext4 = base
	} else if snap.Tooling.RequiredInstalled("dumpe2fs") || len(mounts) > 0 {
		base.Present = true
		snap.Ext4 = base
	} else {
		base.Present = false
		if truncated {
			base.Reason = "mount inventory was truncated; ext4 presence could not be determined"
		} else {
			base.Reason = "dumpe2fs binary was not found"
		}
		snap.Ext4 = base
	}
	return snap, nil
}

// ParseMountInfo returns the ext4 mounts in bounded kernel mountinfo (shared
// parser: internal/mountinfo). Mounts whose source is not a safe device path
// are omitted and mark the list truncated; so do skipped lines.
func ParseMountInfo(r io.Reader) ([]Mount, bool, error) {
	recs, info, err := mountinfo.Parse(r)
	if err != nil {
		return nil, false, err
	}
	mounts, truncated := mountinfo.DeviceMounts(recs, info, "ext4", ext4MaxMounts)
	return mounts, truncated, nil
}
