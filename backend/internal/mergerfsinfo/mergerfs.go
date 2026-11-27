// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package mergerfsinfo is the unprivileged, read-only MergerFS + optional
// SnapRAID inventory (M8 first checkpoint). It reads bounded kernel mountinfo,
// /etc/fstab, systemd .mount units, and a single administrator-configured
// snapraid.conf. Each pool's live configuration is read from mergerfs's own
// control file (<mountpoint>/.mergerfs, getxattr only; runtime.go). Live pool
// branches are opened only as directories under the
// collect root (os.OpenRoot, directory) for fstatfs of root-owned live pool
// branches. Listed SnapRAID paths are not opened as such; a data disk that is
// also a branch is measured as a branch. fstab/systemd paths are not opened.
// It never writes and never executes a program.
package mergerfsinfo

import (
	"context"
	"fmt"
	"homelab-control/backend/internal/mountinfo"
	"io"
	"os"
	"strings"
)

// Pool is one observed fuse.mergerfs mount.
type Pool struct {
	Mountpoint string   `json:"mountpoint"`
	Branches   []string `json:"branches"`
	// BranchesFrom says where Branches came from: "runtime" (mergerfs's own
	// control file, the live list), "mountinfo" (the mount source), or
	// "fstab"/"systemd" when neither names them and the matching configured
	// mount does. Empty when they are unknown.
	BranchesFrom string `json:"branchesFrom,omitempty"`
	// Runtime is mergerfs's live configuration from <mountpoint>/.mergerfs;
	// RuntimeReason says why it is absent.
	Runtime        *Runtime      `json:"runtime,omitempty"`
	RuntimeReason  string        `json:"runtimeReason,omitempty"`
	Options        []string      `json:"options"`
	Usage          []BranchUsage `json:"usage"`
	UsageTruncated bool          `json:"usageTruncated,omitempty"`
}

// MergerFS is the mergerfs half of the response.
type MergerFS struct {
	Present             bool              `json:"present"`
	Reason              string            `json:"reason"`
	Pools               []Pool            `json:"pools"`
	Configured          []ConfiguredMount `json:"configured"`
	ConfiguredTruncated bool              `json:"configuredTruncated,omitempty"`
}

// Snapshot is the full read-only MergerFS + SnapRAID inventory.
type Snapshot struct {
	Tooling           Tooling       `json:"tooling"`
	MergerFS          MergerFS      `json:"mergerfs"`
	SnapRAID          SnapRAID      `json:"snapraid"`
	KernelFuse        KernelFuse    `json:"kernelFuse"`
	KernelRelease     KernelRelease `json:"kernelRelease"`
	FuseAccess        FuseAccess    `json:"fuseAccess"`
	Warnings          []Warning     `json:"warnings"`
	WarningsTruncated bool          `json:"warningsTruncated"`
}

// mountinfoReadLimits match storageinfo.ParseMountInfo so a hostile or
// oversized kernel file cannot grow memory unbounded.
const ()

// ParseMountInfo parses bounded kernel mountinfo (shared parser:
// internal/mountinfo) and retains source and options only for fuse.mergerfs
// mounts (and fuseblk whose source clearly looks like colon-separated mergerfs
// branches). Every other filesystem's source and options are dropped; unlike
// storageinfo, only mergerfs pools are returned. A skipped line or a reached
// bound fails the parse, so a broken inventory never reads as "no pools".
func ParseMountInfo(r io.Reader) ([]Pool, error) {
	recs, info, err := mountinfo.Parse(r)
	if err != nil {
		return nil, err
	}
	if info.Truncated {
		return nil, fmt.Errorf("mount inventory is malformed or exceeds its limits")
	}
	pools := []Pool{}
	for _, rec := range recs {
		if rec.FSType != "fuse.mergerfs" && rec.FSType != "fuseblk" {
			continue
		}
		branches, ok := parseBranches(rec.Source)
		if rec.FSType == "fuseblk" && (!ok || len(branches) < 2) {
			// fuseblk is only accepted when the source clearly looks like
			// multiple colon-separated absolute mergerfs branches.
			continue
		}
		from := "mountinfo"
		if !ok {
			// fuse.mergerfs with fsname= (for example fsname=mergerfs, a common
			// setting): the source is a label, not the branch list. The pool is
			// still real; Collect fills its branches from the configured mount.
			branches, from = []string{}, ""
		}
		pools = append(pools, Pool{Mountpoint: rec.Mountpoint, Branches: branches, BranchesFrom: from, Options: parseOptions(rec.SuperOptions)})
	}
	return pools, nil
}

// parseBranches splits a colon-separated branch list and rejects relative
// paths, empty branches and ".." components. No path is opened.
func parseBranches(source string) ([]string, bool) {
	parts := strings.Split(source, ":")
	if len(parts) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || !strings.HasPrefix(p, "/") {
			return nil, false
		}
		for _, c := range strings.Split(p, "/") {
			if c == ".." {
				return nil, false
			}
		}
		out = append(out, p)
	}
	return out, true
}

// parseOptions keeps mergerfs-relevant super-options and drops everything
// else, including any credential-looking token.
func parseOptions(super string) []string {
	out := []string{}
	for _, raw := range strings.Split(super, ",") {
		tok, err := mountinfo.Unescape(raw)
		if err != nil || tok == "" {
			continue
		}
		low := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(low, "user="), strings.HasPrefix(low, "pass="), strings.HasPrefix(low, "password="):
			continue
		case tok == "allow_other" || tok == "use_ino" || tok == "moveonenospc":
			out = append(out, tok)
		case low == "user_id=0":
			out = append(out, "user_id=0")
		case strings.HasPrefix(low, "moveonenospc="),
			strings.HasPrefix(low, "category.create="),
			strings.HasPrefix(low, "minfreespace="),
			strings.HasPrefix(low, "fsname="):
			out = append(out, tok)
		}
	}
	return out
}

// collectMounts reads bounded /proc/self/mountinfo beneath osRoot.
func collectMounts(osRoot string) ([]Pool, error) {
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open("proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseMountInfo(f)
}

// Collect reads mergerfs pools, optional fstab/systemd declarations, and the
// optional snapraid.conf, and probes the required (mergerfs, fusermount3) and
// optional (snapraid, smartctl, dumpe2fs, xfs_info, btrfs) binaries plus kernel
// FUSE from /proc/filesystems, kernel release from /proc/sys/kernel/osrelease,
// and user_allow_other from /etc/fuse.conf. Optional tools are never executed,
// uname is never run, and mergerfs --version is never run. Disks are not
// formatted and fuse.conf is not edited. Missing or unreadable inputs become
// present=false with a reason. Only context cancellation of the non-capacity
// work returns an error (the caller maps it to 503). Capacity uses its own
// budget. Kernel release and fuse.conf are informational and never degrade
// ProbeCapability.
func Collect(ctx context.Context, osRoot, snapraidPath, family string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	snap.Tooling = ProbeTooling(family)
	snap.KernelFuse = ProbeKernelFuse(osRoot)
	snap.KernelRelease = ProbeKernelRelease(osRoot)
	snap.FuseAccess = ProbeFuseAccess(osRoot)
	pools, err := collectMounts(osRoot)
	if pools == nil {
		pools = []Pool{}
	}
	if err == nil {
		// mergerfs's own report of its branches and policies; bounded, so a
		// hung FUSE daemon cannot hold the page.
		readRuntimes(osRoot, pools)
	}
	configured, truncated := collectConfigured(ctx, osRoot, pools)
	fillBranchesFromConfigured(pools, configured)
	base := MergerFS{Pools: pools, Configured: configured, ConfiguredTruncated: truncated}
	if err != nil {
		// A mount inventory that cannot be parsed must not read as a healthy
		// empty pool list; report it honestly with fixed text (no error string).
		// Configured fstab/systemd entries are still returned.
		base.Present = false
		base.Reason = "mount inventory could not be read"
		base.Pools = []Pool{}
		snap.MergerFS = base
	} else if snap.Tooling.RequiredInstalled("mergerfs") || len(pools) > 0 {
		// mergerfs.present prefers the binary gate; a parsed mount without the
		// binary is still listed (empty pools without a binary is not a fake page).
		base.Present = true
		snap.MergerFS = base
	} else {
		base.Present = false
		base.Reason = "mergerfs is not installed"
		snap.MergerFS = base
	}
	snap.SnapRAID = ReadSnapRAID(snapraidPath)
	snap.Warnings, snap.WarningsTruncated = HonestyWarnings(ctx, snap.MergerFS.Pools, snap.SnapRAID)
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	// Capacity has its own budget and must not 503 the inventory.
	if err == nil {
		collectMeasure(osRoot, snap.MergerFS.Pools)
	}
	return snap, nil
}

// fillBranchesFromConfigured gives a live pool whose branches neither
// mergerfs's control file nor the mount source named the branches of the
// configured fstab or systemd mount with the same mountpoint. Those are what
// the mount was started with, not necessarily what it uses now.
func fillBranchesFromConfigured(pools []Pool, configured []ConfiguredMount) {
	for i := range pools {
		if pools[i].BranchesFrom != "" {
			continue
		}
		for _, c := range configured {
			if cleanMountpoint(c.Mountpoint) == cleanMountpoint(pools[i].Mountpoint) && len(c.Branches) > 0 {
				pools[i].Branches = append([]string{}, c.Branches...)
				pools[i].BranchesFrom = c.Origin
				break
			}
		}
	}
}
