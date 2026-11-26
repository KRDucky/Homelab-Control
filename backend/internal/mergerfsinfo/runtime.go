// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// mergerfs publishes its live configuration as extended attributes of the
// control file <mountpoint>/.mergerfs ("Runtime Config" in the mergerfs
// manual): user.mergerfs.branches is the branch list as mergerfs uses it now,
// with each branch's mode (RW, RO, NC) and optional per-branch minfreespace.
// The kernel mount table carries none of this (it shows only the generic FUSE
// options), and fstab shows only what the mount was started with. Reading the
// attributes needs no privilege on a mount with allow_other; nothing is set.

// Runtime is the live mergerfs configuration of one pool.
type Runtime struct {
	Version      string          `json:"version,omitempty"`
	Branches     []RuntimeBranch `json:"branches"`
	CreatePolicy string          `json:"createPolicy,omitempty"`
	ActionPolicy string          `json:"actionPolicy,omitempty"`
	SearchPolicy string          `json:"searchPolicy,omitempty"`
	MinFreeSpace string          `json:"minFreeSpace,omitempty"`
	MoveOnENOSPC string          `json:"moveOnENOSPC,omitempty"`
}

// RuntimeBranch is one entry of user.mergerfs.branches.
type RuntimeBranch struct {
	Path    string `json:"path"`
	Mode    string `json:"mode,omitempty"`
	MinFree string `json:"minFree,omitempty"`
}

const (
	runtimeMaxPools    = 16
	runtimeMaxValue    = 64 << 10
	runtimeMaxBranches = 64
	runtimeParkedMax   = 4

	reasonRuntimeUnread  = "mergerfs's control file could not be read (the mount may lack allow_other)"
	reasonRuntimeTimeout = "mergerfs did not answer in time"
	reasonRuntimeInvalid = "mergerfs's branch list could not be parsed"
	reasonRuntimeSkipped = "too many pools; live configuration not read"
)

var (
	runtimeTimeout = 500 * time.Millisecond
	runtimeBudget  = 1500 * time.Millisecond
	runtimeParked  parkedGate
)

// runtimeXattrs are the attributes read, in this order. Every other one is
// left alone.
var runtimeXattrs = []string{
	"user.mergerfs.branches", "user.mergerfs.version", "user.mergerfs.category.create",
	"user.mergerfs.category.action", "user.mergerfs.category.search",
	"user.mergerfs.minfreespace", "user.mergerfs.moveonenospc",
}

// getxattrFunc reads one attribute; tests replace it (no FUSE in unit tests).
var getxattrFunc = getxattrLive

func getxattrLive(path, name string) (string, error) {
	buf := make([]byte, 4096)
	for {
		n, err := syscall.Getxattr(path, name, buf)
		if errors.Is(err, syscall.ERANGE) && len(buf) < runtimeMaxValue {
			buf = make([]byte, runtimeMaxValue)
			continue
		}
		if err != nil {
			return "", err
		}
		return string(buf[:n]), nil
	}
}

// readRuntimes fills Runtime (or RuntimeReason) on each observed pool. A pool
// whose branch list mergerfs reports takes it as Branches (BranchesFrom
// "runtime"). Each pool is read in its own goroutine with a timeout; a stuck
// FUSE daemon parks at most runtimeParkedMax goroutines, and the whole read
// has its own budget, so a hung mount cannot hold the page.
func readRuntimes(osRoot string, pools []Pool) {
	if osRoot == "" {
		osRoot = "/"
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeBudget)
	defer cancel()
	for i := range pools {
		if i >= runtimeMaxPools {
			pools[i].RuntimeReason = reasonRuntimeSkipped
			continue
		}
		rt, reason := readRuntime(ctx, filepath.Join(osRoot, pools[i].Mountpoint, ".mergerfs"))
		if rt == nil {
			pools[i].RuntimeReason = reason
			continue
		}
		pools[i].Runtime = rt
		paths := make([]string, len(rt.Branches))
		for j, b := range rt.Branches {
			paths[j] = b.Path
		}
		pools[i].Branches, pools[i].BranchesFrom = paths, "runtime"
	}
}

func readRuntime(ctx context.Context, control string) (*Runtime, string) {
	if !runtimeParked.try(runtimeParkedMax) {
		return nil, reasonRuntimeTimeout
	}
	type result struct {
		vals map[string]string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		defer runtimeParked.done()
		vals := map[string]string{}
		for _, name := range runtimeXattrs {
			v, err := getxattrFunc(control, name)
			if err != nil {
				if name == runtimeXattrs[0] {
					ch <- result{err: err}
					return
				}
				continue
			}
			vals[name] = v
		}
		ch <- result{vals: vals}
	}()
	timer := time.NewTimer(runtimeTimeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, reasonRuntimeUnread
		}
		branches, ok := ParseRuntimeBranches(r.vals["user.mergerfs.branches"])
		if !ok {
			return nil, reasonRuntimeInvalid
		}
		return &Runtime{
			Version: knownVersion(r.vals["user.mergerfs.version"]), Branches: branches,
			CreatePolicy: clean(r.vals["user.mergerfs.category.create"]),
			ActionPolicy: clean(r.vals["user.mergerfs.category.action"]),
			SearchPolicy: clean(r.vals["user.mergerfs.category.search"]),
			MinFreeSpace: clean(r.vals["user.mergerfs.minfreespace"]),
			MoveOnENOSPC: clean(r.vals["user.mergerfs.moveonenospc"]),
		}, ""
	case <-timer.C:
		return nil, reasonRuntimeTimeout
	case <-ctx.Done():
		return nil, reasonRuntimeTimeout
	}
}

// ParseRuntimeBranches parses user.mergerfs.branches: branches separated by
// ':', each PATH[=MODE[,MINFREE]] (mergerfs manual, "branches"). Paths must be
// absolute with no ".." component.
func ParseRuntimeBranches(v string) ([]RuntimeBranch, bool) {
	v = strings.TrimSpace(v)
	if v == "" || !utf8.ValidString(v) {
		return nil, false
	}
	parts := strings.Split(v, ":")
	if len(parts) > runtimeMaxBranches {
		return nil, false
	}
	out := make([]RuntimeBranch, 0, len(parts))
	for _, p := range parts {
		path, opts, _ := strings.Cut(p, "=")
		b := RuntimeBranch{Path: path}
		if opts != "" {
			b.Mode, b.MinFree, _ = strings.Cut(opts, ",")
		}
		if !strings.HasPrefix(path, "/") || hasDotDot(path) || strings.ContainsAny(path, "\x00\n") {
			return nil, false
		}
		switch b.Mode {
		case "", "RW", "RO", "NC":
		default:
			return nil, false
		}
		out = append(out, b)
	}
	return out, true
}

func hasDotDot(p string) bool {
	for _, c := range strings.Split(p, "/") {
		if c == ".." {
			return true
		}
	}
	return false
}

// knownVersion drops the "unknown" that distribution builds without git
// metadata report (Ubuntu's mergerfs 2.40.2 does).
func knownVersion(v string) string {
	if v = clean(v); v == "unknown" {
		return ""
	}
	return v
}

// clean keeps a short printable attribute value.
func clean(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 128 || !utf8.ValidString(s) {
		return ""
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return s
}
