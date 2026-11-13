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
	"testing"
	"time"
)

func writeMountinfo(t *testing.T, dir, source, mountpoint string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	line := "36 29 0:28 / " + mountpoint + " rw - fuse.mergerfs " + source + " rw,allow_other\n"
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
		if u.Path != want || !u.Measured || u.TotalBytes == 0 || u.Reason != "" {
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
	oldFn, oldTO := openAndStatfs, usageStatfsTimeout
	t.Cleanup(func() {
		openAndStatfs = oldFn
		usageStatfsTimeout = oldTO
	})
	openAndStatfs = func(string, string) (uint64, uint64, uint64, error) {
		time.Sleep(200 * time.Millisecond)
		return 1, 0, 1, nil
	}
	usageStatfsTimeout = 20 * time.Millisecond
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	u := snap.MergerFS.Pools[0].Usage[0]
	if u.Measured || u.Reason != reasonUsageTimeout {
		t.Fatalf("timeout: %+v", u)
	}
}

func TestMeasurePoolUsageContextCancel(t *testing.T) {
	pools := []Pool{{Mountpoint: "/p", Branches: []string{"/a", "/b"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	measurePoolUsage(ctx, t.TempDir(), pools)
	if !pools[0].UsageTruncated || len(pools[0].Usage) != 0 {
		t.Fatalf("cancelled: %+v", pools[0])
	}
	for _, u := range pools[0].Usage {
		if u.Measured {
			t.Fatalf("cancelled must not invent numbers: %+v", u)
		}
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
