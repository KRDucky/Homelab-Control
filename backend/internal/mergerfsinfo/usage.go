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

// BranchUsage is one live pool branch's statfs. Byte fields are set only when
// Measured is true (including zeros). Path is the mountinfo branch (mode suffix
// still present).
type BranchUsage struct {
	Path       string  `json:"path"`
	Measured   bool    `json:"measured"`
	TotalBytes *uint64 `json:"totalBytes,omitempty"`
	UsedBytes  *uint64 `json:"usedBytes,omitempty"`
	AvailBytes *uint64 `json:"availBytes,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

const (
	usageBranchesMaxPerPool = 64
	usageBranchesMaxTotal   = 64
	usageFieldMax           = 256
	usageParkedMax          = 8
	usageWorkers            = 8

	reasonUsageUnread  = "path could not be measured"
	reasonUsageFused   = "fused mount is not a per-branch filesystem"
	reasonUsageTimeout = "filesystem did not respond"
	reasonUsageNotRoot = "pool is not a root mergerfs mount"
)

var (
	usageTimeout     = 500 * time.Millisecond
	usageBudget      = 1500 * time.Millisecond
	errStatfsTimeout = errors.New("statfs timed out")
)

type parkedGate struct {
	mu sync.Mutex
	n  int
}

func (g *parkedGate) try(max int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.n >= max {
		return false
	}
	g.n++
	return true
}

func (g *parkedGate) done() {
	g.mu.Lock()
	if g.n > 0 {
		g.n--
	}
	g.mu.Unlock()
}

var liveParked parkedGate

type usageSettings struct {
	Timeout   time.Duration
	Budget    time.Duration
	ParkedMax int
	Workers   int
	Parked    *parkedGate
	Statfs    func(osRoot, rel string) (total, used, avail uint64, err error)
}

func liveUsageSettings() usageSettings {
	return usageSettings{
		Timeout:   usageTimeout,
		Budget:    usageBudget,
		ParkedMax: usageParkedMax,
		Workers:   usageWorkers,
		Parked:    &liveParked,
		Statfs:    openAndStatfsLive,
	}
}

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
	var usedBlocks uint64
	if st.Blocks > st.Bfree {
		usedBlocks = st.Blocks - st.Bfree
	}
	return total, usedBlocks * bs, st.Bavail * bs, nil
}

func u64(v uint64) *uint64 { return &v }

func hasRootUserID(options []string) bool {
	for _, o := range options {
		if strings.ToLower(o) == "user_id=0" {
			return true
		}
	}
	return false
}

func forbiddenBranch(key string) bool {
	for _, p := range []string{"/proc", "/sys", "/dev"} {
		if pathCovers(p, key) {
			return true
		}
	}
	return false
}

func fusedMounts(pools []Pool) []string {
	out := make([]string, 0, len(pools))
	for _, p := range pools {
		if m := cleanPath(p.Mountpoint); m != "" {
			out = append(out, m)
		}
	}
	return out
}

type usageJob struct {
	pool   int
	idx    int
	osRoot string
	branch string
	fused  []string
}

var collectMeasure = measurePoolUsage

// measurePoolUsage fills Usage on observed live pools. It never measures
// fstab/systemd configured paths, listed SnapRAID paths as such, or a fused
// mount. A data disk that is also a pool branch is measured as a branch.
// Capacity uses its own budget and never cancels the caller.
func measurePoolUsage(osRoot string, pools []Pool) {
	measurePoolUsageWith(liveUsageSettings(), osRoot, pools)
}

func measurePoolUsageWith(set usageSettings, osRoot string, pools []Pool) {
	if osRoot == "" {
		osRoot = "/"
	}
	if set.Parked == nil {
		set.Parked = &parkedGate{}
	}
	if set.Statfs == nil {
		set.Statfs = openAndStatfsLive
	}
	if set.ParkedMax <= 0 {
		set.ParkedMax = usageParkedMax
	}
	if set.Workers <= 0 {
		set.Workers = usageWorkers
	}
	if set.Budget <= 0 {
		set.Budget = usageBudget
	}
	if set.Timeout <= 0 {
		set.Timeout = usageTimeout
	}

	if _, err := os.OpenRoot(osRoot); err != nil {
		for i := range pools {
			n := len(pools[i].Branches)
			trunc := false
			if n > usageBranchesMaxPerPool {
				n = usageBranchesMaxPerPool
				trunc = true
			}
			if n > usageBranchesMaxTotal {
				n = usageBranchesMaxTotal
				trunc = true
			}
			pools[i].Usage = unmeasuredAll(pools[i].Branches[:n], reasonUsageUnread)
			pools[i].UsageTruncated = trunc || len(pools[i].Branches) > n
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), set.Budget)
	defer cancel()

	fused := fusedMounts(pools)
	var jobs []usageJob
	total := 0
	for i := range pools {
		branches := pools[i].Branches
		capAt := len(branches)
		trunc := false
		if capAt > usageBranchesMaxPerPool {
			capAt = usageBranchesMaxPerPool
			trunc = true
		}
		if total+capAt > usageBranchesMaxTotal {
			capAt = usageBranchesMaxTotal - total
			if capAt < 0 {
				capAt = 0
			}
			trunc = true
		}
		pools[i].UsageTruncated = trunc
		if !hasRootUserID(pools[i].Options) {
			pools[i].Usage = unmeasuredAll(branches[:capAt], reasonUsageNotRoot)
			continue
		}
		out := make([]BranchUsage, capAt)
		for j, b := range branches[:capAt] {
			out[j] = BranchUsage{Path: clipUsageField(b), Reason: reasonUsageTimeout}
			jobs = append(jobs, usageJob{pool: i, idx: j, osRoot: osRoot, branch: b, fused: fused})
		}
		pools[i].Usage = out
		total += capAt
	}

	if len(jobs) == 0 {
		return
	}

	ch := make(chan usageJob)
	var wg sync.WaitGroup
	for w := 0; w < set.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				u := measureBranch(ctx, set, j.osRoot, j.fused, j.branch)
				pools[j.pool].Usage[j.idx] = u
			}
		}()
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			pools[j.pool].UsageTruncated = true
			continue
		}
		select {
		case ch <- j:
		case <-ctx.Done():
			pools[j.pool].UsageTruncated = true
		}
	}
	close(ch)
	wg.Wait()
}

func unmeasuredAll(branches []string, reason string) []BranchUsage {
	out := make([]BranchUsage, 0, len(branches))
	for _, b := range branches {
		out = append(out, BranchUsage{Path: clipUsageField(b), Reason: reason})
	}
	return out
}

func measureBranch(ctx context.Context, set usageSettings, osRoot string, fused []string, branch string) BranchUsage {
	u := BranchUsage{Path: clipUsageField(branch)}
	key := branchKey(branch)
	if key == "" || key == "/" || forbiddenBranch(key) {
		u.Reason = reasonUsageUnread
		return u
	}
	for _, m := range fused {
		if pathCovers(m, key) {
			u.Reason = reasonUsageFused
			return u
		}
	}
	rel := strings.TrimPrefix(key, "/")
	if rel == "" || strings.Contains(rel, "..") {
		u.Reason = reasonUsageUnread
		return u
	}
	total, used, avail, err := fstatfsBounded(ctx, set, osRoot, rel)
	if err != nil {
		if errors.Is(err, errStatfsTimeout) {
			u.Reason = reasonUsageTimeout
		} else {
			u.Reason = reasonUsageUnread
		}
		return u
	}
	u.Measured = true
	u.TotalBytes, u.UsedBytes, u.AvailBytes = u64(total), u64(used), u64(avail)
	return u
}

func fstatfsBounded(ctx context.Context, set usageSettings, osRoot, rel string) (uint64, uint64, uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, 0, errStatfsTimeout
	}
	if !set.Parked.try(set.ParkedMax) {
		return 0, 0, 0, errStatfsTimeout
	}

	type res struct {
		t, u, a uint64
		err     error
	}
	ch := make(chan res, 1)
	statfs := set.Statfs
	go func() {
		defer set.Parked.done()
		t, u, a, err := statfs(osRoot, rel)
		ch <- res{t, u, a, err}
	}()

	timeout := set.Timeout
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
