// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsapply

import (
	"strings"
	"testing"
)

func pool() Request {
	return Request{
		Version:    ProtocolVersion,
		Action:     ActionMount,
		Mountpoint: "/mnt/pool",
		Branches:   []string{"/mnt/disk1", "/mnt/disk2"},
		Policy:     "epmfs",
		MinFree:    "20G",
	}
}

func TestMountArgvIsClosed(t *testing.T) {
	spec, err := Validate(pool())
	if err != nil {
		t.Fatal(err)
	}
	argv, err := spec.Argv()
	if err != nil {
		t.Fatal(err)
	}
	want := "/usr/bin/mergerfs -o category.create=epmfs,allow_other,minfreespace=20G /mnt/disk1:/mnt/disk2 /mnt/pool"
	if strings.Join(argv, " ") != want {
		t.Fatalf("argv %q", argv)
	}
	for _, bad := range []string{"mkfs", "--force", "-F", "format"} {
		if strings.Contains(strings.Join(argv, " "), bad) {
			t.Fatalf("argv contains %s", bad)
		}
	}
	if strings.Contains(spec.Effect(), "/mnt") || strings.Contains(spec.AuditDetail(), "/mnt") {
		t.Fatal("effect or audit leaked a path")
	}
}

func TestRejectsUnsafePools(t *testing.T) {
	cases := []Request{
		func() Request { r := pool(); r.Mountpoint = "/mnt"; return r }(),
		func() Request { r := pool(); r.Mountpoint = "/etc/pool"; return r }(),
		func() Request { r := pool(); r.Mountpoint = "/mnt/pool/nested"; return r }(),
		func() Request { r := pool(); r.Branches = []string{"/mnt/disk1"}; return r }(),
		func() Request { r := pool(); r.Branches = []string{"/mnt/disk1", "/mnt/disk1"}; return r }(),
		func() Request { r := pool(); r.Branches = []string{"/mnt/pool", "/mnt/disk2"}; return r }(),
		func() Request { r := pool(); r.Branches = []string{"/mnt/disk1", "/mnt/../disk2"}; return r }(),
		func() Request { r := pool(); r.Policy = "newest"; return r }(),
		func() Request { r := pool(); r.MinFree = "0G"; return r }(),
		func() Request { r := pool(); r.Branches = []string{"/mnt/a:b", "/mnt/disk2"}; return r }(),
	}
	for i, req := range cases {
		if _, err := Validate(req); err == nil {
			t.Errorf("case %d accepted %#v", i, req)
		}
	}
}

func TestPersistRefusesParityOnBranch(t *testing.T) {
	req := pool()
	req.Action = ActionPersist
	req.Parity = []string{"/mnt/disk1/snapraid.parity"}
	req.Content = []string{"/var/snapraid/content", "/var/snapraid/content.bak"}
	if _, err := Validate(req); err == nil {
		t.Fatal("parity on a branch was accepted")
	}
	req.Parity = []string{"/mnt/parity1/snapraid.parity"}
	req.Content = []string{"/mnt/parity1/content", "/var/snapraid/content"}
	if _, err := Validate(req); err == nil {
		t.Fatal("content on parity was accepted")
	}
	req.Content = []string{"/mnt/pool/content", "/var/snapraid/content"}
	if _, err := Validate(req); err == nil {
		t.Fatal("content on the pool was accepted")
	}
	req.Content = []string{"/var/snapraid/content", "/mnt/disk1/snapraid.content"}
	spec, err := Validate(req)
	if err != nil {
		t.Fatal(err)
	}
	conf, err := spec.SnapraidConf()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(conf, "include") || !strings.Contains(conf, "data d1 /mnt/disk1\n") || !strings.Contains(conf, "parity /mnt/parity1/snapraid.parity\n") {
		t.Fatalf("conf:\n%s", conf)
	}
	if _, err := spec.Argv(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncAndScrubArgv(t *testing.T) {
	syncSpec, err := Validate(Request{Action: ActionSync})
	if err != nil {
		t.Fatal(err)
	}
	argv, err := syncSpec.Argv()
	if err != nil || strings.Join(argv, " ") != "/usr/bin/snapraid sync" {
		t.Fatalf("%v %v", argv, err)
	}
	if _, err := Validate(Request{Action: ActionSync, Files: []string{"a"}}); err == nil {
		t.Fatal("sync accepted a file")
	}
	scrub, err := Validate(Request{Action: ActionScrub, ScrubPercent: 5})
	if err != nil {
		t.Fatal(err)
	}
	argv, err = scrub.Argv()
	if err != nil || strings.Join(argv, " ") != "/usr/bin/snapraid scrub -p 5" {
		t.Fatalf("%v %v", argv, err)
	}
	if _, err := Validate(Request{Action: ActionScrub, ScrubPercent: 101}); err == nil {
		t.Fatal("percent 101 accepted")
	}
}

func TestFixIsFilteredRecoveryOnly(t *testing.T) {
	spec, err := Validate(Request{Action: ActionFix, Files: []string{"movies/a.mkv", "b.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	argv, err := spec.Argv()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(argv, " ")
	if got != "/usr/bin/snapraid fix -m -f /movies/a.mkv -f /b.txt" {
		t.Fatalf("argv %s", got)
	}
	for _, flag := range []string{" -e", "--force", " filter"} {
		if strings.Contains(" "+got, flag) {
			t.Fatalf("forbidden flag in %s", got)
		}
	}
	for _, files := range [][]string{{"/abs"}, {"../x"}, {"a/../../b"}, {""}, nil} {
		if _, err := Validate(Request{Action: ActionFix, Files: files}); err == nil {
			t.Fatalf("accepted %q", files)
		}
	}
	if spec.Target() != "recover" || strings.Contains(spec.AuditDetail(), "movies") {
		t.Fatalf("target/audit %s %s", spec.Target(), spec.AuditDetail())
	}
}

func TestBlockFromDiff(t *testing.T) {
	if got := BlockFromDiff("1 removed\n3 added\n", false); got != BlockRemovedFiles {
		t.Fatalf("1 removed: %s", got)
	}
	if got := BlockFromDiff("removed 2\n", false); got != BlockRemovedFiles {
		t.Fatalf("removed 2: %s", got)
	}
	if got := BlockFromDiff("0 removed\n2 added\n", false); got != "" {
		t.Fatalf("0 removed blocked: %s", got)
	}
	if got := BlockFromDiff("Disk 'd1' not present\n", false); got != BlockMissingDisk {
		t.Fatalf("not present: %s", got)
	}
	if got := BlockFromDiff("missing disk d2\n", false); got != BlockMissingDisk {
		t.Fatalf("missing disk: %s", got)
	}
	if got := BlockFromDiff("WARNING! Content file 'content' not found, attempting with another copy...\nNo content file found. Assuming empty.\n2 added\n0 removed\n", false); got != "" {
		t.Fatalf("new array blocked: %s", got)
	}
	if got := BlockFromDiff("Error opening directory. No such file or directory.\n", false); got != BlockMissingDisk {
		t.Fatalf("opening directory: %s", got)
	}
	if got := BlockFromDiff("This could occur when some disks are not mounted\n", false); got != BlockMissingDisk {
		t.Fatalf("not mounted: %s", got)
	}
	if got := DiffGate(2, "2 added\n0 removed\n", false); got != "" {
		t.Fatalf("diff exit 2 blocked: %s", got)
	}
	if got := DiffGate(2, "1 removed\n", false); got != BlockRemovedFiles {
		t.Fatalf("diff exit 2 removed: %s", got)
	}
	if got := DiffGate(1, "Error opening directory. No such file or directory.\n", false); got != BlockMissingDisk {
		t.Fatalf("fatal missing dir: %s", got)
	}
	if got := DiffGate(1, "unexpected failure\n", false); got != BlockDiffUnreadable {
		t.Fatalf("fatal diff: %s", got)
	}
	if got := BlockFromDiff("WARNING! All the files in a disk are removed\n", false); got != BlockRemovedFiles {
		t.Fatalf("all files: %s", got)
	}
	if got := BlockFromDiff("No error detected.\n", true); got != BlockDiffTruncated {
		t.Fatalf("truncated: %s", got)
	}
	if got := BlockFromDiff("No sync is in progress.\n", false); got != "" {
		t.Fatalf("clean diff blocked: %s", got)
	}
}

func TestMergeFstab(t *testing.T) {
	spec, err := Validate(pool())
	if err != nil {
		t.Fatal(err)
	}
	line, err := spec.FstabLine()
	if err != nil {
		t.Fatal(err)
	}
	out, err := MergeFstab("# comment\nUUID=abc / ext4 defaults 0 1\n", line, spec.Mountpoint)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "# comment\n") || !strings.Contains(out, "UUID=abc / ext4 defaults 0 1\n") || !strings.HasSuffix(out, line+"\n") {
		t.Fatalf("merged:\n%s", out)
	}
	again, err := MergeFstab(out, line, spec.Mountpoint)
	if err != nil || strings.Count(again, spec.Mountpoint) != 1 {
		t.Fatalf("replace: %v\n%s", err, again)
	}
	if _, err := MergeFstab("/dev/sda1 /mnt/pool ext4 defaults 0 0\n", line, spec.Mountpoint); err == nil {
		t.Fatal("replaced a non-mergerfs mount")
	}
}
