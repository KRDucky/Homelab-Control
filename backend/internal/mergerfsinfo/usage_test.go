// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func writeMountinfo(t *testing.T, dir, source, mountpoint string) {
	t.Helper()
	writeMountinfoSuper(t, dir, source, mountpoint, "rw,allow_other,user_id=0")
}

func writeMountinfoSuper(t *testing.T, dir, source, mountpoint, super string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	line := "36 29 0:28 / " + mountpoint + " rw - fuse.mergerfs " + source + " " + super + "\n"
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMeasurePoolUsageLiveDirs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"hc-cap-a", "hc-cap-b"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeMountinfo(t, dir, "/hc-cap-a:/hc-cap-b", "/hc-cap-pool")
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.MergerFS.Pools) != 1 {
		t.Fatalf("pools: %+v", snap.MergerFS.Pools)
	}
	pool := snap.MergerFS.Pools[0]
	if pool.UsageTruncated || len(pool.Usage) != 2 {
		t.Fatalf("usage: %+v trunc=%v", pool.Usage, pool.UsageTruncated)
	}
	for i, want := range []string{"/hc-cap-a", "/hc-cap-b"} {
		u := pool.Usage[i]
		if u.Path != want || !u.Measured || u.TotalBytes == nil || *u.TotalBytes == 0 || u.Reason != "" {
			t.Fatalf("branch %d: %+v", i, u)
		}
	}
}

func TestMeasurePoolUsageMissingIsUnread(t *testing.T) {
	dir := t.TempDir()
	writeMountinfo(t, dir, "/hc-cap-missing:/hc-cap-also-missing", "/mnt/media")
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	pool := snap.MergerFS.Pools[0]
	if len(pool.Usage) != 2 {
		t.Fatalf("usage: %+v", pool.Usage)
	}
	for _, u := range pool.Usage {
		if u.Measured || u.Reason != reasonUsageUnread {
			t.Fatalf("missing path must not invent numbers: %+v", u)
		}
	}
}

func TestMeasurePoolUsageRefusesEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "hc-cap-link")); err != nil {
		t.Fatal(err)
	}
	writeMountinfo(t, dir, "/hc-cap-link", "/hc-cap-pool")
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	u := snap.MergerFS.Pools[0].Usage[0]
	if u.Measured || u.Reason != reasonUsageUnread {
		t.Fatalf("symlink that leaves the collect root must not be measured: %+v", u)
	}
}

func TestMeasurePoolUsageRegularFileUnread(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hc-cap-file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMountinfo(t, dir, "/hc-cap-file", "/hc-cap-pool")
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	u := snap.MergerFS.Pools[0].Usage[0]
	if u.Measured || u.Reason != reasonUsageUnread {
		t.Fatalf("regular file must not be measured: %+v", u)
	}
}

func TestMeasurePoolUsageSkipsFusedMount(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "mnt", "pool"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMountinfo(t, dir, "/mnt/pool:/mnt/pool/disk", "/mnt/pool")
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	pool := snap.MergerFS.Pools[0]
	if len(pool.Usage) != 2 {
		t.Fatalf("usage: %+v", pool.Usage)
	}
	for _, u := range pool.Usage {
		if u.Measured || u.Reason != reasonUsageFused {
			t.Fatalf("fused path must not be statfs'd: %+v", u)
		}
	}
}

func TestMeasurePoolUsageStripsModeSuffix(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hc-cap-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMountinfo(t, dir, "/hc-cap-a=RW", "/hc-cap-pool")
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	u := snap.MergerFS.Pools[0].Usage[0]
	if u.Path != "/hc-cap-a=RW" || !u.Measured {
		t.Fatalf("mode suffix must be stripped only for open: %+v", u)
	}
}

func TestMeasurePoolUsageCapsBranches(t *testing.T) {
	dir := t.TempDir()
	n := usageBranchesMaxPerPool + 2
	src := make([]string, n)
	for i := 0; i < n; i++ {
		rel := "n/" + strconv.Itoa(i)
		if err := os.MkdirAll(filepath.Join(dir, rel), 0o755); err != nil {
			t.Fatal(err)
		}
		src[i] = "/" + rel
	}
	writeMountinfo(t, dir, strings.Join(src, ":"), "/hc-cap-pool")
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	pool := snap.MergerFS.Pools[0]
	if !pool.UsageTruncated || len(pool.Usage) != usageBranchesMaxPerPool || len(pool.Branches) != n {
		t.Fatalf("cap: usage=%d trunc=%v branches=%d", len(pool.Usage), pool.UsageTruncated, len(pool.Branches))
	}
}

func TestMeasurePoolUsageTimeout(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hc-cap-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMountinfo(t, dir, "/hc-cap-a", "/hc-cap-pool")
	old := collectMeasure
	t.Cleanup(func() { collectMeasure = old })
	collectMeasure = func(osRoot string, pools []Pool) {
		measurePoolUsageWith(usageSettings{
			Timeout:   20 * time.Millisecond,
			Budget:    100 * time.Millisecond,
			ParkedMax: 8,
			Workers:   8,
			Parked:    &parkedGate{},
			Statfs: func(string, string) (uint64, uint64, uint64, error) {
				time.Sleep(200 * time.Millisecond)
				return 1, 0, 1, nil
			},
		}, osRoot, pools)
	}
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	u := snap.MergerFS.Pools[0].Usage[0]
	if u.Measured || u.Reason != reasonUsageTimeout {
		t.Fatalf("timeout: %+v", u)
	}
}

func TestClipUsageFieldUTF8(t *testing.T) {
	// 255 bytes of prefix plus a 2-byte rune that would be split at 256.
	s := strings.Repeat("a", 255) + "é"
	got := clipUsageField(s)
	if len(got) != 255 || !strings.HasSuffix(got, "a") {
		t.Fatalf("split rune: len=%d %q", len(got), got[len(got)-2:])
	}
}

func TestConfiguredMountsAreNotMeasured(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "fstab"), []byte(
		"/hc-cap-a:/hc-cap-b /mnt/unmounted fuse.mergerfs defaults 0 0\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "hc-cap-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.MergerFS.Pools) != 0 {
		t.Fatalf("live pools: %+v", snap.MergerFS.Pools)
	}
	if len(snap.MergerFS.Configured) != 1 || snap.MergerFS.Configured[0].Mountpoint != "/mnt/unmounted" {
		t.Fatalf("configured: %+v", snap.MergerFS.Configured)
	}
}

func TestMeasurePoolUsageSkipsNonRootMount(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hc-cap-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMountinfoSuper(t, dir, "/hc-cap-a", "/hc-cap-pool", "rw,allow_other,user_id=1000")
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.MergerFS.Pools) != 1 || len(snap.MergerFS.Pools[0].Branches) != 1 {
		t.Fatalf("pool must still be listed: %+v", snap.MergerFS.Pools)
	}
	u := snap.MergerFS.Pools[0].Usage[0]
	if u.Measured || u.Reason != reasonUsageNotRoot {
		t.Fatalf("non-root mount must not be measured: %+v", u)
	}
}

func TestMeasurePoolUsageRefusesProcSysDev(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"proc", "sys", "dev"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeMountinfo(t, dir, "/proc:/sys:/dev/foo", "/hc-cap-pool")
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.MergerFS.Pools[0].Usage) != 3 {
		t.Fatalf("usage: %+v", snap.MergerFS.Pools[0].Usage)
	}
	for _, u := range snap.MergerFS.Pools[0].Usage {
		if u.Measured || u.Reason != reasonUsageUnread {
			t.Fatalf("kernel virtual path must not be measured: %+v", u)
		}
	}
}

func TestMeasurePoolUsageSkipsNestedPoolMount(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "mnt", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	text := "36 29 0:28 / /mnt/inner rw - fuse.mergerfs /hc-cap-a rw,allow_other,user_id=0\n" +
		"37 29 0:29 / /mnt/outer rw - fuse.mergerfs /mnt/inner rw,allow_other,user_id=0\n"
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.MergerFS.Pools) != 2 {
		t.Fatalf("pools: %+v", snap.MergerFS.Pools)
	}
	var nested BranchUsage
	for _, p := range snap.MergerFS.Pools {
		if p.Mountpoint == "/mnt/outer" && len(p.Usage) == 1 {
			nested = p.Usage[0]
		}
	}
	if nested.Measured || nested.Reason != reasonUsageFused {
		t.Fatalf("nested pool mount must not be statfs'd: %+v", nested)
	}
}

func TestMeasurePoolUsageParkedGoroutineCap(t *testing.T) {
	var n atomic.Int32
	set := usageSettings{
		Timeout:   40 * time.Millisecond,
		Budget:    150 * time.Millisecond,
		ParkedMax: 8,
		Workers:   8,
		Parked:    &parkedGate{},
		Statfs: func(string, string) (uint64, uint64, uint64, error) {
			n.Add(1)
			time.Sleep(300 * time.Millisecond)
			return 1, 0, 1, nil
		},
	}
	branches := make([]string, 9)
	for i := range branches {
		branches[i] = "/n/" + strconv.Itoa(i)
	}
	pools := []Pool{{Mountpoint: "/p", Branches: branches, Options: []string{"user_id=0"}}}
	measurePoolUsageWith(set, t.TempDir(), pools)
	if got := n.Load(); got > 8 {
		t.Fatalf("parked cap: started %d statfs", got)
	}
	if len(pools[0].Usage) != 9 {
		t.Fatalf("usage: %+v", pools[0].Usage)
	}
	for _, u := range pools[0].Usage {
		if u.Measured {
			t.Fatalf("blocking statfs must not invent numbers: %+v", u)
		}
	}
}

func TestCollectBlockingBranchesDoesNotError(t *testing.T) {
	dir := t.TempDir()
	src := make([]string, 20)
	for i := range src {
		rel := "n/" + strconv.Itoa(i)
		if err := os.MkdirAll(filepath.Join(dir, rel), 0o755); err != nil {
			t.Fatal(err)
		}
		src[i] = "/" + rel
	}
	writeMountinfo(t, dir, strings.Join(src, ":"), "/hc-cap-pool")
	old := collectMeasure
	t.Cleanup(func() { collectMeasure = old })
	collectMeasure = func(osRoot string, pools []Pool) {
		measurePoolUsageWith(usageSettings{
			Timeout:   20 * time.Millisecond,
			Budget:    80 * time.Millisecond,
			ParkedMax: 8,
			Workers:   8,
			Parked:    &parkedGate{},
			Statfs: func(string, string) (uint64, uint64, uint64, error) {
				time.Sleep(400 * time.Millisecond)
				return 1, 0, 1, nil
			},
		}, osRoot, pools)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	snap, err := Collect(ctx, dir, filepath.Join(dir, "missing.conf"), "debian")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("capacity must not 503 Collect: %v", err)
	}
	if len(snap.MergerFS.Pools) != 1 || len(snap.MergerFS.Pools[0].Branches) != 20 {
		t.Fatalf("pools hidden: %+v", snap.MergerFS.Pools)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("budget: %s", elapsed)
	}
	for _, u := range snap.MergerFS.Pools[0].Usage {
		if u.Measured {
			t.Fatalf("blocked branch measured: %+v", u)
		}
	}
}

func TestMeasurePoolUsageKeepsZeroBytes(t *testing.T) {
	pools := []Pool{{Mountpoint: "/p", Branches: []string{"/a"}, Options: []string{"user_id=0"}}}
	measurePoolUsageWith(usageSettings{
		Timeout:   time.Second,
		Budget:    time.Second,
		ParkedMax: 8,
		Workers:   1,
		Parked:    &parkedGate{},
		Statfs:    func(string, string) (uint64, uint64, uint64, error) { return 0, 0, 0, nil },
	}, t.TempDir(), pools)
	u := pools[0].Usage[0]
	if !u.Measured || u.TotalBytes == nil || *u.TotalBytes != 0 || u.UsedBytes == nil || u.AvailBytes == nil {
		t.Fatalf("zero measured values must be present: %+v", u)
	}
}

func TestParseOptionsKeepsRootUserID(t *testing.T) {
	opts := parseOptions("allow_other,user_id=0,user_id=1000,user=secret")
	joined := strings.Join(opts, ",")
	if !strings.Contains(joined, "user_id=0") || strings.Contains(joined, "1000") || strings.Contains(joined, "secret") {
		t.Fatalf("options: %q", joined)
	}
}
