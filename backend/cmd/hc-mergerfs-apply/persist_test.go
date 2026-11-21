// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"strings"
	"testing"

	"homelab-control/backend/internal/mergerfsapply"
)

func TestPersistWritesBackupAndRefusesForeignMount(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root+"/etc", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/etc/fstab", []byte("/dev/sda1 / ext4 defaults 0 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec, err := mergerfsapply.Validate(mergerfsapply.Request{
		Action: mergerfsapply.ActionPersist, Mountpoint: "/mnt/pool",
		Branches: []string{"/mnt/disk1", "/mnt/disk2"}, Policy: "mfs",
		Parity:  []string{"/mnt/parity1/snapraid.parity"},
		Content: []string{"/var/snapraid/content", "/mnt/disk1/snapraid.content"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := persist(root, spec); err != nil {
		t.Fatal(err)
	}
	fstab, err := os.ReadFile(root + "/etc/fstab")
	if err != nil || !strings.Contains(string(fstab), "fuse.mergerfs") || !strings.Contains(string(fstab), "/dev/sda1 / ext4") {
		t.Fatalf("fstab: %v %s", err, fstab)
	}
	bak, err := os.ReadFile(root + mergerfsapply.FstabBackup)
	if err != nil || strings.Contains(string(bak), "fuse.mergerfs") {
		t.Fatalf("backup should be the previous file: %v %s", err, bak)
	}
	conf, err := os.ReadFile(root + mergerfsapply.SnapraidPath)
	if err != nil || strings.Contains(string(conf), "include") || !strings.Contains(string(conf), "data d1 /mnt/disk1\n") {
		t.Fatalf("conf: %v %s", err, conf)
	}
	if err := os.WriteFile(root+"/etc/fstab", []byte("/dev/sda1 /mnt/pool ext4 defaults 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := persist(root, spec); err == nil {
		t.Fatal("replaced a non-mergerfs fstab line")
	}
}

func TestSummaryOmitsPaths(t *testing.T) {
	res := done(true, 0, "wrote /mnt/pool", "")
	if res.OK || strings.Contains(res.Summary, "/") {
		t.Fatalf("%+v", res)
	}
}
