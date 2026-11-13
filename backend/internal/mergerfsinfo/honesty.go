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
	WarnDuplicateBranch = "duplicate-branch"
	WarnParityInPool    = "parity-in-pool"

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

// HonestyWarnings reports duplicate pool branches and SnapRAID parity paths
// that sit on a mergerfs branch or on the fused mount itself. Data disks as
// branches are expected and are not warnings. Comparison is string prefix
// only (exact or inner path under a cleaned prefix); mergerfs branch mode
// suffixes =RW/=NC/=RO are stripped; nothing is stat'ed. Output is capped
// (64 total, 8 per pool); truncated is true when the cap or ctx deadline hits.
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
