// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageprobe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeScript writes an executable fake tool. It is test-only and never runs
// on the host; the tools are synthetic fixtures.
func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// mkdev creates a sysfs-like device directory with optional attributes.
func mkdev(t *testing.T, dir, name, devnode, diskseq, removable, partition string) {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if devnode != "" {
		writeFile(t, filepath.Join(d, "dev"), devnode+"\n")
	}
	if diskseq != "" {
		writeFile(t, filepath.Join(d, "diskseq"), diskseq+"\n")
	}
	if removable != "" {
		writeFile(t, filepath.Join(d, "removable"), removable+"\n")
	}
	if partition != "" {
		writeFile(t, filepath.Join(d, "partition"), partition+"\n")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// link mirrors real sysfs: /sys/class/block entries are symlinks to the device
// directory, so validDevices must follow them with Stat, not Lstat.
func link(t *testing.T, sys, devs, name string) {
	t.Helper()
	if err := os.Symlink(filepath.Join(devs, name), filepath.Join(sys, name)); err != nil {
		t.Fatal(err)
	}
}

// fixture builds a synthetic /sys/class/block tree and fake lvs/pvs/blkid, and
// points the package vars at them. It returns the fixture root and the blkid
// argv log path.
func fixture(t *testing.T) (base, blkidLog string) {
	t.Helper()
	base = t.TempDir()
	sys := filepath.Join(base, "block")
	devs := filepath.Join(base, "devs")
	if err := os.MkdirAll(sys, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(devs, 0o755); err != nil {
		t.Fatal(err)
	}

	// Whole disks and partitions, entered as symlinks like real sysfs.
	mkdev(t, devs, "sda", "8:0", "12", "0", "")
	link(t, sys, devs, "sda")
	mkdev(t, filepath.Join(devs, "sda"), "sda1", "8:1", "", "", "1")
	os.Symlink(filepath.Join(devs, "sda", "sda1"), filepath.Join(sys, "sda1"))

	mkdev(t, devs, "nvme0n1", "259:0", "99", "0", "")
	link(t, sys, devs, "nvme0n1")
	mkdev(t, filepath.Join(devs, "nvme0n1"), "nvme0n1p1", "259:1", "", "", "1")
	os.Symlink(filepath.Join(devs, "nvme0n1", "nvme0n1p1"), filepath.Join(sys, "nvme0n1p1"))

	// Devices that must be skipped by class.
	mkdev(t, devs, "loop0", "7:0", "", "0", "")
	link(t, sys, devs, "loop0")
	mkdev(t, devs, "dm-0", "253:0", "", "0", "")
	link(t, sys, devs, "dm-0")
	mkdev(t, devs, "sdb", "8:16", "", "1", "") // removable
	link(t, sys, devs, "sdb")
	mkdev(t, devs, "sdc", "8:32", "", "0", "") // usb
	if err := os.MkdirAll(filepath.Join(devs, "sdc", "usb"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("usb", filepath.Join(devs, "sdc", "device")); err != nil {
		t.Fatal(err)
	}
	link(t, sys, devs, "sdc")
	mkdev(t, devs, "sdd", "8:48", "", "0", "") // device state offline
	if err := os.MkdirAll(filepath.Join(devs, "sdd", "device"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(devs, "sdd", "device", "state"), "offline\n")
	link(t, sys, devs, "sdd")

	// Fake tools.
	bin := filepath.Join(base, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeScript(t, filepath.Join(bin, "lvs"), "#!/bin/sh\necho '{\"report\":[{\"lv\":[{\"lv_uuid\":\"u1\"}]}]}'\n")
	writeScript(t, filepath.Join(bin, "pvs"), "#!/bin/sh\necho '{\"report\":[{\"pv\":[{\"pv_uuid\":\"p1\"}]}]}'\n")
	blkidLog = filepath.Join(base, "blkid.log")
	writeScript(t, filepath.Join(bin, "blkid"), "#!/bin/sh\nfor a in \"$@\"; do last=$a; done\necho \"$last\" >> '"+blkidLog+"'\necho 'TYPE=LVM2_member'\necho 'UUID=abc'\necho 'SECRET_KEY=hunter2'\n")

	// Point the package at the fixture and restore it afterwards.
	oldSys, oldBin, oldOpen, oldMin, oldLast := sysBlockRoot, binaries, openAndVerify, minRunInterval, lastRun
	sysBlockRoot = sys
	binaries = map[string]string{
		"lvs":   filepath.Join(bin, "lvs"),
		"pvs":   filepath.Join(bin, "pvs"),
		"blkid": filepath.Join(bin, "blkid"),
	}
	openAndVerify = func(path string, maj, min uint64) (*os.File, error) {
		return os.Open(os.DevNull)
	}
	minRunInterval = 0
	lastRun = time.Time{}
	t.Cleanup(func() {
		sysBlockRoot, binaries, openAndVerify, minRunInterval, lastRun = oldSys, oldBin, oldOpen, oldMin, oldLast
	})
	return base, blkidLog
}

func TestRunCollectsAndRevalidates(t *testing.T) {
	_, blkidLog := fixture(t)

	req := Request{Version: 1, Devices: []string{
		"sda", "sda1", "nvme0n1p1", "loop0", "dm-0", "sdb", "sdc", "sdd",
		"../evil", "nonexistent", "a/b",
	}}
	res := Run(context.Background(), req)

	// Probed devices: whole disk + partition, resolved via /dev/block/MAJ:MIN.
	for _, want := range []string{"sda", "sda1", "nvme0n1p1"} {
		if _, ok := res.Blkid[want]; !ok {
			t.Fatalf("device %q not probed: %+v", want, res)
		}
		if strings.Contains(res.Blkid[want], "SECRET_KEY") {
			t.Fatalf("unfiltered blkid key leaked for %q: %q", want, res.Blkid[want])
		}
		if !strings.Contains(res.Blkid[want], "TYPE=LVM2_member") {
			t.Fatalf("allowed key missing for %q: %q", want, res.Blkid[want])
		}
	}
	for _, skipped := range []string{"loop0", "dm-0", "sdb", "sdc", "sdd"} {
		if _, ok := res.Blkid[skipped]; ok {
			t.Fatalf("excluded device %q was probed", skipped)
		}
	}

	// The fake blkid logged the resolved /dev/block paths, never /dev/<name>.
	log, err := os.ReadFile(blkidLog)
	if err != nil {
		t.Fatal(err)
	}
	got := string(log)
	for _, want := range []string{"/dev/block/8:0", "/dev/block/8:1", "/dev/block/259:1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("blkid did not get resolved path %s; log: %q", want, got)
		}
	}
	if strings.Contains(got, "/dev/sda\n") || strings.Contains(got, "/dev/sda ") {
		t.Fatalf("blkid got raw /dev/<name> instead of /dev/block/MAJ:MIN: %q", got)
	}
}

func TestFilterBlkidExport(t *testing.T) {
	raw := "DEVNAME=/dev/sda\nTYPE=LVM2_member\nUUID=abc\nSECRET_KEY=hunter2\nPART_ENTRY_NUMBER=1\nSOME_NOISE=zz\n"
	out := filterBlkidExport(raw)
	for _, want := range []string{"DEVNAME=", "TYPE=", "UUID=", "PART_ENTRY_NUMBER="} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing allowed key %s in %q", want, out)
		}
	}
	for _, bad := range []string{"SECRET_KEY", "SOME_NOISE"} {
		if strings.Contains(out, bad) {
			t.Fatalf("leaked key %s in %q", bad, out)
		}
	}
}

func TestLvmConfig(t *testing.T) {
	c := lvmConfig([]string{"sda", "nvme0n1"})
	for _, want := range []string{"hints=0", "a|^/dev/sda$|", "a|^/dev/nvme0n1$|", "r|.*|"} {
		if !strings.Contains(c, want) {
			t.Fatalf("lvmConfig missing %q in %q", want, c)
		}
	}
}

func TestMakedev(t *testing.T) {
	for _, tc := range []struct {
		maj, min, want uint64
	}{
		{8, 0, 2048},
		{8, 16, 2064},
		{259, 0, 66304},
		{8, 257, 1050625},
	} {
		if got := makedev(tc.maj, tc.min); got != tc.want {
			t.Fatalf("makedev(%d,%d)=%d want %d", tc.maj, tc.min, got, tc.want)
		}
	}
}

func TestOpenAndVerifyRejectsNonBlock(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "reg")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("x")
	f.Close()
	if _, err := openAndVerify(f.Name(), 8, 0); err == nil || !strings.Contains(err.Error(), "not a block device") {
		t.Fatalf("expected block-device rejection, got %v", err)
	}
}

func TestAdmitRateLimit(t *testing.T) {
	oldMin, oldLast := minRunInterval, lastRun
	defer func() { minRunInterval, lastRun = oldMin, oldLast }()
	minRunInterval = time.Hour
	lastRun = time.Time{}
	if !admit() {
		t.Fatal("first admit should pass")
	}
	if admit() {
		t.Fatal("second admit within window should fail")
	}
	release()
}
