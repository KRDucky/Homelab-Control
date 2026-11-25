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
	"context"
	"homelab-control/backend/internal/mountinfo"
	"io"
	"os"
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
	zfsMaxMounts        = 256
	datasetMaxBytes     = 256
	reasonTooManyMounts = "too many mounts to list"
)

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

// ParseMountInfo returns the zfs mounts (dataset → mountpoint) in bounded
// kernel mountinfo (shared parser: internal/mountinfo). Records whose source is
// not a valid dataset name are omitted. Skipped lines and more than
// zfsMaxMounts rows mark the list truncated.
func ParseMountInfo(r io.Reader) ([]Mount, bool, error) {
	recs, info, err := mountinfo.Parse(r)
	if err != nil {
		return nil, false, err
	}
	mounts := []Mount{}
	truncated := info.Truncated
	for _, rec := range recs {
		if rec.FSType != "zfs" || !validDataset(rec.Source) {
			continue
		}
		if len(mounts) >= zfsMaxMounts {
			truncated = true
			continue
		}
		mounts = append(mounts, Mount{Mountpoint: rec.Mountpoint, Dataset: rec.Source})
	}
	return mounts, truncated, nil
}
