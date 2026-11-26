// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Values read with getfattr from a mergerfs 2.40.2 pool (Ubuntu 26.04 VM),
// mounted from fstab with fsname=mergerfs, category.create=mfs, minfreespace=1G.
var vmRuntime = map[string]string{
	"user.mergerfs.branches":        "/mnt/disk1=RW:/mnt/disk2=RW",
	"user.mergerfs.version":         "2.40.2",
	"user.mergerfs.category.create": "mfs",
	"user.mergerfs.category.action": "epall",
	"user.mergerfs.category.search": "ff",
	"user.mergerfs.minfreespace":    "1073741824",
	"user.mergerfs.moveonenospc":    "pfrd",
}

func fakeXattrs(t *testing.T, byControl map[string]map[string]string) {
	t.Helper()
	old := getxattrFunc
	getxattrFunc = func(path, name string) (string, error) {
		m, ok := byControl[path]
		if !ok {
			return "", syscall.EACCES
		}
		v, ok := m[name]
		if !ok {
			return "", syscall.ENODATA
		}
		return v, nil
	}
	t.Cleanup(func() { getxattrFunc = old })
}

func TestParseRuntimeBranches(t *testing.T) {
	got, ok := ParseRuntimeBranches("/mnt/disk1=RW:/mnt/disk2=NC,10G:/mnt/disk3")
	if !ok || len(got) != 3 || got[0] != (RuntimeBranch{Path: "/mnt/disk1", Mode: "RW"}) ||
		got[1] != (RuntimeBranch{Path: "/mnt/disk2", Mode: "NC", MinFree: "10G"}) || got[2].Path != "/mnt/disk3" {
		t.Fatalf("%v %+v", ok, got)
	}
	for _, bad := range []string{"", "relative=RW", "/mnt/../etc=RW", "/mnt/a=XX", "/mnt/a::/mnt/b"} {
		if _, ok := ParseRuntimeBranches(bad); ok {
			t.Fatalf("accepted %q", bad)
		}
	}
}

// The kernel mount table names neither the branches of an fsname= pool nor
// any mergerfs policy, and fstab names only what the mount started with. The
// page now takes both from mergerfs's control file. Here fstab still lists
// the old branches; the live list (a branch added at runtime) must win.
func TestCollectUsesMergerfsRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"proc/self", "etc"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mi := "36 29 0:28 / /mnt/storage rw,relatime shared:1 - fuse.mergerfs mergerfs rw,user_id=0,group_id=0,default_permissions,allow_other\n"
	if err := os.WriteFile(filepath.Join(dir, "proc/self/mountinfo"), []byte(mi), 0o644); err != nil {
		t.Fatal(err)
	}
	fstab := "/mnt/disk1 /mnt/storage fuse.mergerfs allow_other,fsname=mergerfs 0 0\n"
	if err := os.WriteFile(filepath.Join(dir, "etc/fstab"), []byte(fstab), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeXattrs(t, map[string]map[string]string{filepath.Join(dir, "/mnt/storage/.mergerfs"): vmRuntime})
	old := collectMeasure
	collectMeasure = func(string, []Pool) {}
	t.Cleanup(func() { collectMeasure = old })

	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "none.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.MergerFS.Pools) != 1 {
		t.Fatalf("%+v", snap.MergerFS)
	}
	p := snap.MergerFS.Pools[0]
	if p.BranchesFrom != "runtime" || strings.Join(p.Branches, ":") != "/mnt/disk1:/mnt/disk2" {
		t.Fatalf("branches %q from %q", p.Branches, p.BranchesFrom)
	}
	rt := p.Runtime
	if rt == nil || rt.Version != "2.40.2" || rt.CreatePolicy != "mfs" || rt.MinFreeSpace != "1073741824" ||
		len(rt.Branches) != 2 || rt.Branches[1].Mode != "RW" || p.RuntimeReason != "" {
		t.Fatalf("runtime %+v reason %q", rt, p.RuntimeReason)
	}
}

func TestReadRuntimesUnreadableKeepsMountinfoBranches(t *testing.T) {
	fakeXattrs(t, nil)
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/a", "/mnt/b"}, BranchesFrom: "mountinfo"}}
	readRuntimes("/", pools)
	if pools[0].Runtime != nil || pools[0].RuntimeReason != reasonRuntimeUnread || pools[0].BranchesFrom != "mountinfo" {
		t.Fatalf("%+v", pools[0])
	}
}

// A hung FUSE daemon must not hold the page: each pool times out, and the
// whole read stays within its budget.
func TestReadRuntimesBoundedWhenMergerfsHangs(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	old := getxattrFunc
	getxattrFunc = func(string, string) (string, error) { <-block; return "", syscall.EIO }
	t.Cleanup(func() { getxattrFunc = old })
	oldT, oldB := runtimeTimeout, runtimeBudget
	runtimeTimeout, runtimeBudget = 20*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { runtimeTimeout, runtimeBudget = oldT, oldB })

	pools := make([]Pool, 6)
	for i := range pools {
		pools[i].Mountpoint = "/mnt/p" + string(rune('a'+i))
	}
	start := time.Now()
	readRuntimes("/", pools)
	if time.Since(start) > time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	for _, p := range pools {
		if p.Runtime != nil || p.RuntimeReason != reasonRuntimeTimeout {
			t.Fatalf("%+v", p)
		}
	}
}
