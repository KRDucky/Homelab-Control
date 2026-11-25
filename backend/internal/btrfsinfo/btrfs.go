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
	"context"
	"homelab-control/backend/internal/mountinfo"
	"io"
	"os"
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
	btrfsMaxMounts      = 256
	subvolMaxBytes      = 256
	reasonTooManyMounts = "too many mounts to list"
)

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
			if len(val) == 0 || len(val) > subvolMaxBytes || !strings.HasPrefix(val, "/") || mountinfo.HasDotDot(val) {
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

// ParseMountInfo returns the btrfs mounts in bounded kernel mountinfo (shared
// parser: internal/mountinfo), with the subvolume from the super options.
// Mounts with an unsafe device, mountpoint or subvolume option are omitted and
// mark the list truncated; so do skipped lines and rows beyond btrfsMaxMounts.
func ParseMountInfo(r io.Reader) ([]Mount, bool, error) {
	recs, info, err := mountinfo.Parse(r)
	if err != nil {
		return nil, false, err
	}
	mounts := []Mount{}
	truncated := info.Truncated
	for _, rec := range recs {
		if rec.FSType != "btrfs" {
			continue
		}
		subvol, subvolID, ok := parseSubvol(rec.SuperOptions)
		if !ok || !mountinfo.ValidDevice(rec.Source) || mountinfo.HasDotDot(rec.Mountpoint) {
			truncated = true
			continue
		}
		if len(mounts) >= btrfsMaxMounts {
			truncated = true
			continue
		}
		mounts = append(mounts, Mount{Mountpoint: rec.Mountpoint, Device: rec.Source, Subvol: subvol, SubvolID: subvolID})
	}
	return mounts, truncated, nil
}
