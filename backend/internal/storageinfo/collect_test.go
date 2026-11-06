// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectFixtureNeverOpensBlockNodesOrMountpoints(t *testing.T) {
	root := t.TempDir()
	put := func(p, s string) {
		t.Helper()
		p = filepath.Join(root, p)
		if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(s), 0644); e != nil {
			t.Fatal(e)
		}
	}
	put("sys/class/block/sda/dev", "8:0")
	put("sys/class/block/sda/size", "2048")
	put("sys/class/block/sda/device/wwid", "naa.5000000000000a01")
	put("proc/self/mountinfo", "1 0 8:0 / /does-not-exist rw - ext4 /dev/sda rw\n")
	g, e := Collect(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if len(g.Nodes) != 2 || len(g.Edges) != 1 || g.Status != "partial" {
		t.Fatalf("unexpected inventory %+v", g)
	}
	found := false
	for _, n := range g.Nodes {
		if n.PersistentID == "wwn:naa.5000000000000a01" && n.SizeBytes == 1048576 {
			found = true
		}
	}
	if !found {
		t.Fatal("observed disk missing")
	}
}
func TestCollectRefusesEscapingSysfsSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if e := os.MkdirAll(filepath.Join(root, "sys/class/block"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(outside, "dev"), []byte("8:0"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(root, "sys/class/block/escape")); e != nil {
		t.Fatal(e)
	}
	g, e := Collect(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if len(g.Nodes) != 0 {
		t.Fatal("escaped selected metadata root")
	}
}
func TestCollectCancellation(t *testing.T) {
	root := t.TempDir()
	if e := os.MkdirAll(filepath.Join(root, "sys/class/block/sda"), 0755); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Collect(ctx, root); e == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestCollectRealisticPartitionAndStackedSysfsLinks(t *testing.T) {
	root := t.TempDir()
	put := func(p, s string) {
		t.Helper()
		p = filepath.Join(root, p)
		if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(s), 0644); e != nil {
			t.Fatal(e)
		}
	}
	link := func(target, path string) {
		t.Helper()
		path = filepath.Join(root, path)
		if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink(target, path); e != nil {
			t.Fatal(e)
		}
	}
	put("sys/devices/sda/dev", "8:0")
	put("sys/devices/sda/size", "4096")
	put("sys/devices/sda/device/serial", "SERIAL-A")
	put("sys/devices/sda/diskseq", "42")
	put("sys/devices/sda/sda1/dev", "8:1")
	put("sys/devices/sda/sda1/size", "2048")
	put("sys/devices/sda/sda1/partition", "1")
	put("sys/devices/dm-0/dev", "253:0")
	put("sys/devices/dm-0/size", "1024")
	put("sys/devices/dm-0/dm/uuid", "LVM-fixture")
	link("../../devices/sda", "sys/class/block/sda")
	link("../../devices/sda/sda1", "sys/class/block/sda1")
	link("../../devices/dm-0", "sys/class/block/dm-0")
	link("../../sda/sda1", "sys/devices/dm-0/slaves/sda1")
	put("proc/self/mountinfo", "1 0 253:0 / /fixture-data rw - ext4 /dev/dm-0 rw\n")
	g, e := Collect(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if len(g.Nodes) != 4 || len(g.Edges) != 3 {
		t.Fatalf("sysfs links lost topology: %+v", g)
	}
	var partition, stack bool
	for _, edge := range g.Edges {
		partition = partition || edge.Kind == "contains"
		stack = stack || edge.Kind == "used-by"
	}
	if !partition || !stack {
		t.Fatal("partition/stack relationships missing")
	}
	for _, n := range g.Nodes {
		want := map[string]string{"sda": "42", "sda1": "42", "dm-0": ""}[n.Name]
		if n.DiskSeq != want {
			t.Fatalf("%s diskseq = %q, want %q (partitions report their disk's)", n.Name, n.DiskSeq, want)
		}
	}
}

func TestCollectMDRuntimeStateWithoutDerivingHealth(t *testing.T) {
	root := t.TempDir()
	put := func(p, s string) {
		t.Helper()
		p = filepath.Join(root, p)
		if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(s), 0644); e != nil {
			t.Fatal(e)
		}
	}
	link := func(target, path string) {
		t.Helper()
		path = filepath.Join(root, path)
		if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink(target, path); e != nil {
			t.Fatal(e)
		}
	}
	// Synthetic degraded RAID1 that is recovering onto a second member.
	for _, d := range []struct{ name, dev string }{{"sda", "8:0"}, {"sdb", "8:16"}} {
		put("sys/devices/"+d.name+"/dev", d.dev)
		put("sys/devices/"+d.name+"/size", "8")
		link("../../devices/"+d.name, "sys/class/block/"+d.name)
	}
	put("sys/devices/md0/dev", "9:0")
	put("sys/devices/md0/size", "8")
	put("sys/devices/md0/md/level", "raid1")
	put("sys/devices/md0/md/uuid", "example-md-uuid")
	put("sys/devices/md0/md/array_state", "clean")
	put("sys/devices/md0/md/raid_disks", "2")
	put("sys/devices/md0/md/degraded", "1")
	put("sys/devices/md0/md/sync_action", "recover")
	put("sys/devices/md0/md/sync_completed", "1024 / 4096")
	put("sys/devices/md0/md/mismatch_cnt", "0")
	put("sys/devices/md0/md/dev-sda/state", "in_sync")
	put("sys/devices/md0/md/dev-sda/slot", "0")
	link("../../../sda", "sys/devices/md0/md/dev-sda/block")
	put("sys/devices/md0/md/dev-sdb/state", "spare")
	put("sys/devices/md0/md/dev-sdb/slot", "none")
	link("../../../sdb", "sys/devices/md0/md/dev-sdb/block")
	link("../../sda", "sys/devices/md0/slaves/sda")
	link("../../sdb", "sys/devices/md0/slaves/sdb")
	link("../../devices/md0", "sys/class/block/md0")
	g, e := Collect(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	var md *MDState
	for _, n := range g.Nodes {
		if n.Name == "md0" {
			md = n.MD
		}
	}
	if md == nil || md.Level != "raid1" || md.ArrayState != "clean" || md.Degraded == nil || *md.Degraded != 1 || md.SyncAction != "recover" || md.SyncCompleted != "1024 / 4096" || md.MismatchCount == nil || len(md.Members) != 2 {
		t.Fatalf("md runtime state lost: %+v", md)
	}
	if md.Members[0].DeviceNumber != "8:0" || md.Members[0].State != "in_sync" || md.Members[1].State != "spare" || md.Members[1].Slot != "none" {
		t.Fatalf("md members: %+v", md.Members)
	}
}

func TestMDTokenDropsUnexpectedContent(t *testing.T) {
	for _, s := range []string{"clean\x00", "<script>", "UPPER", string(make([]byte, 65))} {
		if mdToken(s) != "" {
			t.Fatalf("relayed %q", s)
		}
	}
	if mdToken(" active ") != "active" {
		t.Fatal("trimmed keyword rejected")
	}
}
