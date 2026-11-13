// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// BranchUsage is one live pool branch's statfs. Numbers are omitted when
// Measured is false. Path is the mountinfo branch (mode suffix still present).
type BranchUsage struct {
	Path       string `json:"path"`
	Measured   bool   `json:"measured"`
	TotalBytes uint64 `json:"totalBytes,omitempty"`
	UsedBytes  uint64 `json:"usedBytes,omitempty"`
	AvailBytes uint64 `json:"availBytes,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

const (
	usageBranchesMaxPerPool = 64
	usageBranchesMaxTotal   = 64
	usageFieldMax           = 256

	reasonUsageUnread  = "path could not be measured"
	reasonUsageFused   = "fused mount is not a per-branch filesystem"
	reasonUsageTimeout = "filesystem did not respond"
)

// usageStatfsTimeout bounds each open+fstatfs. A stuck FUSE or NFS branch
// must not hold Collect for the full request deadline.
var usageStatfsTimeout = 500 * time.Millisecond

var errStatfsTimeout = errors.New("statfs timed out")

var (
	usageInflightMu sync.Mutex
	usageInflight   = map[string]bool{}
)

// openAndStatfs opens rel under osRoot as a directory and returns
// total/used/avail like hostinfo.statfs. os.OpenRoot refuses paths that
// escape; each call owns its Root so a timed-out goroutine cannot use a
// closed fd.
var openAndStatfs = openAndStatfsLive

func openAndStatfsLive(osRoot, rel string) (total, used, avail uint64, err error) {
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return 0, 0, 0, err
	}
	defer root.Close()
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return 0, 0, 0, err
	}
	defer f.Close()
	var st syscall.Statfs_t
	if err := syscall.Fstatfs(int(f.Fd()), &st); err != nil {
		return 0, 0, 0, err
	}
	bs := uint64(st.Bsize)
	if bs == 0 {
		return 0, 0, 0, errors.New("statfs zero block size")
	}
	total = st.Blocks * bs
	free := st.Bfree * bs
	return total, total - free, st.Bavail * bs, nil
}

// measurePoolUsage fills Usage on observed live pools. It never measures
// fstab/systemd configured paths, SnapRAID data/parity/content, or the fused
// mount itself. Paths are opened only under osRoot.
func measurePoolUsage(ctx context.Context, osRoot string, pools []Pool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		for i := range pools {
			pools[i].Usage = unmeasuredAll(pools[i].Branches, reasonUsageUnread)
			if len(pools[i].Branches) > usageBranchesMaxPerPool {
				pools[i].UsageTruncated = true
			}
		}
		return
	}
	root.Close()

	total := 0
	for i := range pools {
		if ctx.Err() != nil {
			pools[i].UsageTruncated = true
			if pools[i].Usage == nil {
				pools[i].Usage = []BranchUsage{}
			}
			continue
		}
		branches := pools[i].Branches
		capAt := len(branches)
		if capAt > usageBranchesMaxPerPool {
			capAt = usageBranchesMaxPerPool
			pools[i].UsageTruncated = true
		}
		if total+capAt > usageBranchesMaxTotal {
			capAt = usageBranchesMaxTotal - total
			if capAt < 0 {
				capAt = 0
			}
			pools[i].UsageTruncated = true
		}
		out := make([]BranchUsage, 0, capAt)
		for _, b := range branches[:capAt] {
			if ctx.Err() != nil {
				pools[i].UsageTruncated = true
				break
			}
			out = append(out, measureBranch(ctx, osRoot, pools[i].Mountpoint, b))
			total++
		}
		if out == nil {
			out = []BranchUsage{}
		}
		pools[i].Usage = out
	}
}

func unmeasuredAll(branches []string, reason string) []BranchUsage {
	n := len(branches)
	if n > usageBranchesMaxPerPool {
		n = usageBranchesMaxPerPool
	}
	out := make([]BranchUsage, 0, n)
	for _, b := range branches[:n] {
		out = append(out, BranchUsage{Path: clipUsageField(b), Reason: reason})
	}
	return out
}

func measureBranch(ctx context.Context, osRoot, mountpoint, branch string) BranchUsage {
	u := BranchUsage{Path: clipUsageField(branch)}
	key := branchKey(branch)
	if key == "" || key == "/" {
		u.Reason = reasonUsageUnread
		return u
	}
	if pathCovers(mountpoint, key) {
		u.Reason = reasonUsageFused
		return u
	}
	rel := strings.TrimPrefix(key, "/")
	if rel == "" || strings.Contains(rel, "..") {
		u.Reason = reasonUsageUnread
		return u
	}
	total, used, avail, err := fstatfsBounded(ctx, osRoot, rel)
	if err != nil {
		if errors.Is(err, errStatfsTimeout) {
			u.Reason = reasonUsageTimeout
		} else {
			u.Reason = reasonUsageUnread
		}
		return u
	}
	u.Measured = true
	u.TotalBytes = total
	u.UsedBytes = used
	u.AvailBytes = avail
	return u
}

func fstatfsBounded(ctx context.Context, osRoot, rel string) (uint64, uint64, uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, 0, errStatfsTimeout
	}
	key := osRoot + "\x00" + rel
	usageInflightMu.Lock()
	if usageInflight[key] {
		usageInflightMu.Unlock()
		return 0, 0, 0, errStatfsTimeout
	}
	usageInflight[key] = true
	usageInflightMu.Unlock()

	type res struct {
		t, u, a uint64
		err     error
	}
	ch := make(chan res, 1)
	go func() {
		t, u, a, err := openAndStatfs(osRoot, rel)
		usageInflightMu.Lock()
		delete(usageInflight, key)
		usageInflightMu.Unlock()
		ch <- res{t, u, a, err}
	}()

	timeout := usageStatfsTimeout
	if dl, ok := ctx.Deadline(); ok {
		remain := time.Until(dl)
		if remain < timeout {
			timeout = remain
		}
	}
	if timeout <= 0 {
		return 0, 0, 0, errStatfsTimeout
	}
	select {
	case r := <-ch:
		return r.t, r.u, r.a, r.err
	case <-ctx.Done():
		return 0, 0, 0, errStatfsTimeout
	case <-time.After(timeout):
		return 0, 0, 0, errStatfsTimeout
	}
}

func clipUsageField(s string) string {
	if len(s) <= usageFieldMax {
		return s
	}
	s = s[:usageFieldMax]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
