// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"context"
	"strings"
)

// Warning codes for read-only MergerFS + SnapRAID honesty (01-FOUNDATION).
const (
	WarnDuplicateBranch     = "duplicate-branch"
	WarnParityInPool        = "parity-in-pool"
	WarnContentOnPool       = "content-on-pool"
	WarnContentOnParity     = "content-on-parity"
	WarnContentNotRedundant = "content-not-redundant"
	WarnFuseAllowOther      = "fuse-allow-other"

	maxWarningsTotal   = 64
	maxWarningsPerPool = 8
	warningFieldMax    = 256
)

// Warning is a path-only observation. No path is opened or mutated.
type Warning struct {
	Code   string `json:"code"`
	Pool   string `json:"pool"`
	Branch string `json:"branch"`
	Other  string `json:"other"`
	Detail string `json:"detail"`
}

// HonestyWarnings reports duplicate pool branches and SnapRAID parity or
// content paths that sit on a mergerfs branch or on the fused mount.
// Content on a data branch is expected and is not a warning. Fewer than two
// content files is a warning. A pool whose options were parsed and omit
// allow_other is a warning; nil options (older fixtures) stay silent. This
// page does not remount. Comparison is string prefix only (exact or
// inner path under a cleaned prefix); mergerfs branch mode suffixes
// =RW/=NC/=RO are stripped; nothing is stat'ed and content files are not
// read. Output is capped (64 total, 8 per pool for branch findings);
// truncated is true when the cap or ctx deadline hits.
func HonestyWarnings(ctx context.Context, pools []Pool, snap SnapRAID) (out []Warning, truncated bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	out = []Warning{}
	iters := 0
	for _, pool := range pools {
		if ctx.Err() != nil {
			return out, true
		}
		if len(out) >= maxWarningsTotal {
			return out, true
		}
		poolCount := 0
		add := func(w Warning) bool {
			if len(out) >= maxWarningsTotal || poolCount >= maxWarningsPerPool {
				truncated = true
				return false
			}
			out = append(out, clipWarning(w))
			poolCount++
			return true
		}
		if pool.Options != nil && !hasOption(pool.Options, "allow_other") {
			if !add(Warning{
				Code:   WarnFuseAllowOther,
				Pool:   pool.Mountpoint,
				Branch: pool.Mountpoint,
				Detail: "this pool is mounted without allow_other; other users may not see its files. This page does not remount",
			}) {
				continue
			}
		}
		seen := map[string]int{}
		for _, b := range pool.Branches {
			iters++
			if iters%4096 == 0 && ctx.Err() != nil {
				return out, true
			}
			key := branchKey(b)
			if key == "" {
				continue
			}
			seen[key]++
			if seen[key] == 2 {
				if !add(Warning{
					Code:   WarnDuplicateBranch,
					Pool:   pool.Mountpoint,
					Branch: b,
					Detail: "this pool lists the same branch more than once",
				}) {
					break
				}
			}
		}
		if truncated || !snap.Present {
			continue
		}
		for _, b := range uniqueBranchKeys(pool.Branches) {
			if truncated {
				break
			}
			for _, p := range snap.Parity {
				iters++
				if iters%4096 == 0 && ctx.Err() != nil {
					return out, true
				}
				if pathCovers(b, p) {
					if !add(Warning{
						Code:   WarnParityInPool,
						Pool:   pool.Mountpoint,
						Branch: b,
						Other:  p,
						Detail: "SnapRAID parity sits on a mergerfs branch; pooling is not redundancy",
					}) {
						break
					}
				}
			}
		}
		if truncated {
			continue
		}
		for _, p := range snap.Parity {
			if pathCovers(pool.Mountpoint, p) {
				if !add(Warning{
					Code:   WarnParityInPool,
					Pool:   pool.Mountpoint,
					Branch: pool.Mountpoint,
					Other:  p,
					Detail: "SnapRAID parity sits on the mergerfs mount itself; pooling is not redundancy",
				}) {
					break
				}
			}
		}
	}
	if !snap.Present {
		return out, truncated
	}
	addGlobal := func(w Warning) {
		if len(out) >= maxWarningsTotal {
			truncated = true
			return
		}
		out = append(out, clipWarning(w))
	}
	if ctx.Err() != nil {
		return out, true
	}
	if len(snap.Content) < 2 {
		branch := snap.ConfigPath
		if len(snap.Content) == 1 {
			branch = snap.Content[0]
		}
		addGlobal(Warning{
			Code:   WarnContentNotRedundant,
			Branch: branch,
			Detail: "SnapRAID lists fewer than two content files; one copy is not redundant",
		})
	}
	for _, c := range snap.Content {
		if ctx.Err() != nil {
			return out, true
		}
		for _, p := range snap.Parity {
			if pathCovers(p, c) || pathCovers(c, p) {
				addGlobal(Warning{
					Code:   WarnContentOnParity,
					Branch: p,
					Other:  c,
					Detail: "SnapRAID content sits on a parity path",
				})
			}
		}
		for _, pool := range pools {
			if pathCovers(pool.Mountpoint, c) {
				addGlobal(Warning{
					Code:   WarnContentOnPool,
					Pool:   pool.Mountpoint,
					Branch: pool.Mountpoint,
					Other:  c,
					Detail: "SnapRAID content sits on the mergerfs mount; content belongs on a data disk, not the pool",
				})
			}
		}
	}
	return out, truncated
}

func uniqueBranchKeys(paths []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range paths {
		key := branchKey(p)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

// pathCovers reports whether container is inner, or inner is under container/.
// A root container only covers root (never every path via a "//" prefix).
func pathCovers(container, inner string) bool {
	c, i := cleanPath(container), cleanPath(inner)
	if c == "" || i == "" {
		return false
	}
	if c == "/" {
		return i == "/"
	}
	return c == i || strings.HasPrefix(i, c+"/")
}

func cleanPath(p string) string {
	if p == "/" {
		return "/"
	}
	return strings.TrimRight(p, "/")
}

// branchKey is the duplicate/cover key for a mergerfs branch: trailing slashes
// stripped, then a trailing =RW/=NC/=RO mode suffix stripped. Other spellings
// (globs, bind mounts, symlinks) are not resolved.
func branchKey(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	if i := strings.LastIndexByte(p, '='); i > 0 {
		switch strings.ToUpper(p[i+1:]) {
		case "RW", "RO", "NC":
			p = strings.TrimRight(p[:i], "/")
		}
	}
	if p == "" {
		return "/"
	}
	return p
}

func clipWarning(w Warning) Warning {
	w.Code = clipField(w.Code)
	w.Pool = clipField(w.Pool)
	w.Branch = clipField(w.Branch)
	w.Other = clipField(w.Other)
	w.Detail = clipField(w.Detail)
	return w
}

func clipField(s string) string {
	if len(s) <= warningFieldMax {
		return s
	}
	return s[:warningFieldMax]
}

func hasOption(opts []string, name string) bool {
	for _, o := range opts {
		if o == name {
			return true
		}
	}
	return false
}
