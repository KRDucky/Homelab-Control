// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import "strings"

// Warning codes for read-only MergerFS + SnapRAID honesty (01-FOUNDATION).
const (
	WarnDuplicateBranch = "duplicate-branch"
	WarnParityInPool    = "parity-in-pool"
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
// only (exact or inner path under a cleaned prefix); nothing is stat'ed.
func HonestyWarnings(pools []Pool, snap SnapRAID) []Warning {
	out := []Warning{}
	for _, pool := range pools {
		seen := map[string]int{}
		for _, b := range pool.Branches {
			key := cleanPath(b)
			seen[key]++
			if seen[key] == 2 {
				out = append(out, Warning{
					Code:   WarnDuplicateBranch,
					Pool:   pool.Mountpoint,
					Branch: b,
					Other:  b,
					Detail: "this pool lists the same branch more than once",
				})
			}
		}
		if !snap.Present {
			continue
		}
		for _, b := range uniqueClean(pool.Branches) {
			for _, p := range snap.Parity {
				if pathCovers(b, p) {
					out = append(out, Warning{
						Code:   WarnParityInPool,
						Pool:   pool.Mountpoint,
						Branch: b,
						Other:  p,
						Detail: "SnapRAID parity sits on a mergerfs branch; pooling is not redundancy",
					})
				}
			}
		}
		for _, p := range snap.Parity {
			if pathCovers(pool.Mountpoint, p) {
				out = append(out, Warning{
					Code:   WarnParityInPool,
					Pool:   pool.Mountpoint,
					Branch: pool.Mountpoint,
					Other:  p,
					Detail: "SnapRAID parity sits on the mergerfs mount itself; pooling is not redundancy",
				})
			}
		}
	}
	return out
}

func uniqueClean(paths []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range paths {
		key := cleanPath(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

// pathCovers reports whether container is inner, or inner is under container/.
func pathCovers(container, inner string) bool {
	c, i := cleanPath(container), cleanPath(inner)
	if c == "" || i == "" {
		return false
	}
	return c == i || strings.HasPrefix(i, c+"/")
}

func cleanPath(p string) string {
	if p == "/" {
		return "/"
	}
	return strings.TrimRight(p, "/")
}
