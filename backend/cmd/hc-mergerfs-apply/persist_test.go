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

// A hand-written snapraid.conf (other disk names, excludes, settings) must not
// be replaced: SnapRAID's content file is keyed on the disk names, so a
// rewrite to d1..dN breaks the existing array. Nothing, not even fstab, is
// written in that case. A file this helper wrote is replaced.
func TestPersistRefusesUnmanagedSnapraidConf(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root+"/etc", 0o755); err != nil {
		t.Fatal(err)
	}
	const fstab0 = "/dev/sda1 / ext4 defaults 0 1\n"
	const conf0 = "parity /mnt/parity/snapraid.parity\ncontent /var/snapraid.content\ndata media /mnt/disk1/\nexclude *.tmp\n"
	if err := os.WriteFile(root+"/etc/fstab", []byte(fstab0), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+mergerfsapply.SnapraidPath, []byte(conf0), 0o644); err != nil {
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
	if err := persist(root, spec); err == nil {
		t.Fatal("replaced a hand-written snapraid.conf")
	}
	if b, _ := os.ReadFile(root + mergerfsapply.SnapraidPath); string(b) != conf0 {
		t.Fatalf("snapraid.conf changed: %s", b)
	}
	if b, _ := os.ReadFile(root + "/etc/fstab"); string(b) != fstab0 {
		t.Fatalf("fstab changed: %s", b)
	}
	res, _ := execute(t.Context(), mergerfsapply.Request{Version: 1, Action: mergerfsapply.ActionPersist, Mountpoint: spec.Mountpoint,
		Branches: spec.Branches, Policy: spec.Policy, Parity: spec.Parity, Content: spec.Content}, root)
	if res.OK || !strings.Contains(res.Summary, "not written by Homelab Control") {
		t.Fatalf("%+v", res)
	}

	managed, err := spec.SnapraidConf()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+mergerfsapply.SnapraidPath, []byte(managed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := persist(root, spec); err != nil {
		t.Fatalf("a file this helper wrote must be replaceable: %v", err)
	}
}

func TestSummaryOmitsPaths(t *testing.T) {
	res := done(true, 0, "wrote /mnt/pool", "")
	if res.OK || strings.Contains(res.Summary, "/") {
		t.Fatalf("%+v", res)
	}
}

// Review finding on #181: status text cut at MaxStatusOutput must be marked,
// so the broker does not present the prefix as a complete report.
func TestStatusResultMarksCutOutput(t *testing.T) {
	res := statusResult(-1, []byte("SnapRAID status report:\n"), true)
	if res.OK || !res.OutputTruncated || res.Output == "" {
		t.Fatalf("%+v", res)
	}
	res = statusResult(0, []byte("No error detected.\n"), false)
	if !res.OK || res.OutputTruncated || res.Output != "No error detected.\n" {
		t.Fatalf("%+v", res)
	}
}
