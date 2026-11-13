// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package mergerfsinfo is the unprivileged, read-only MergerFS + optional
// SnapRAID inventory (M8 first checkpoint). It reads bounded kernel mountinfo,
// /etc/fstab, systemd .mount units, and a single administrator-configured
// snapraid.conf. Live pool branches are opened only as directories under the
// collect root (os.OpenRoot, directory) for fstatfs; SnapRAID data/parity/
// content and fstab/systemd paths are not opened. It never writes and never
// executes a program.
package mergerfsinfo

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Pool is one observed fuse.mergerfs mount.
type Pool struct {
	Mountpoint     string        `json:"mountpoint"`
	Branches       []string      `json:"branches"`
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
	Tooling           Tooling    `json:"tooling"`
	MergerFS          MergerFS   `json:"mergerfs"`
	SnapRAID          SnapRAID   `json:"snapraid"`
	KernelFuse        KernelFuse `json:"kernelFuse"`
	Warnings          []Warning  `json:"warnings"`
	WarningsTruncated bool       `json:"warningsTruncated"`
}

// mountinfoReadLimits match storageinfo.ParseMountInfo so a hostile or
// oversized kernel file cannot grow memory unbounded.
const (
	mountinfoMaxBytes  = 4 * 1024 * 1024
	mountinfoMaxMounts = 8192
)

// ParseMountInfo parses bounded kernel mountinfo and retains source and
// options only for fuse.mergerfs mounts (and fuseblk whose source clearly
// looks like colon-separated mergerfs branches). Every other filesystem's
// source and options are dropped; unlike storageinfo, only mergerfs pools are
// returned.
func ParseMountInfo(r io.Reader) ([]Pool, error) {
	s := bufio.NewScanner(io.LimitReader(r, mountinfoMaxBytes+1))
	s.Buffer(make([]byte, 4096), 65536)
	pools := []Pool{}
	size := 0
	for s.Scan() {
		size += len(s.Bytes()) + 1
		if size > mountinfoMaxBytes {
			return nil, fmt.Errorf("mount inventory exceeds byte limit")
		}
		if len(pools) >= mountinfoMaxMounts {
			return nil, fmt.Errorf("mount inventory exceeds entry limit")
		}
		fields := strings.Fields(s.Text())
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || len(fields) < sep+4 {
			return nil, fmt.Errorf("invalid mountinfo record")
		}
		if _, e := strconv.ParseUint(fields[0], 10, 64); e != nil {
			return nil, fmt.Errorf("invalid mount identifier")
		}
		fstype := fields[sep+1]
		if fstype != "fuse.mergerfs" && fstype != "fuseblk" {
			continue
		}
		point, err := unescape(fields[4])
		if err != nil || !strings.HasPrefix(point, "/") {
			return nil, fmt.Errorf("invalid mergerfs mountpoint")
		}
		source, err := unescape(fields[sep+2])
		if err != nil {
			return nil, fmt.Errorf("invalid mergerfs source")
		}
		branches, ok := parseBranches(source)
		if !ok || (fstype == "fuseblk" && len(branches) < 2) {
			// fuseblk is only accepted when the source clearly looks like
			// multiple colon-separated absolute mergerfs branches.
			continue
		}
		options := parseOptions(fields[sep+3])
		pools = append(pools, Pool{Mountpoint: point, Branches: branches, Options: options})
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("cannot read bounded mount inventory: %w", err)
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
		tok, err := unescape(raw)
		if err != nil || tok == "" {
			continue
		}
		low := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(low, "user="), strings.HasPrefix(low, "pass="), strings.HasPrefix(low, "password="):
			continue
		case tok == "allow_other" || tok == "use_ino" || tok == "moveonenospc":
			out = append(out, tok)
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
// optional (snapraid) binaries plus kernel FUSE from /proc/filesystems. It
// never fails on a missing or unreadable input: those become present=false
// with a reason. Only context cancellation returns an error (the caller maps
// it to 503).
func Collect(ctx context.Context, osRoot, snapraidPath, family string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	snap.Tooling = ProbeTooling(family)
	snap.KernelFuse = ProbeKernelFuse(osRoot)
	pools, err := collectMounts(osRoot)
	if pools == nil {
		pools = []Pool{}
	}
	if err == nil {
		measurePoolUsage(ctx, osRoot, pools)
	}
	configured, truncated := collectConfigured(ctx, osRoot, pools)
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
	return snap, nil
}

// unescape decodes the mountinfo escape sequences (\040 space, \011 tab,
// \012 newline, \134 backslash); anything else is refused.
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
