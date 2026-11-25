// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ext4info

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ext4Line(id int) string {
	return strings.Join([]string{
		itoa(id), "1", "0:36", "/", "/mnt/data", "rw,relatime", "-", "ext4", "/dev/sda1", "rw,relatime",
	}, " ") + "\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestParseMountInfoKeepsOnlyExt4(t *testing.T) {
	in := "39 29 0:31 / /mnt/other rw - vfat /dev/sdb1 rw\n" + ext4Line(40)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || truncated || len(mounts) != 1 {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
	if mounts[0].Mountpoint != "/mnt/data" || mounts[0].Device != "/dev/sda1" {
		t.Fatalf("%+v", mounts[0])
	}
}

func TestParseMountInfoSkipsUnsafeDevice(t *testing.T) {
	in := "41 1 0:36 / /mnt/skip rw - ext4 /dev/../sda rw\n" + ext4Line(43)
	mounts, _, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || len(mounts) != 1 || mounts[0].Device != "/dev/sda1" {
		t.Fatalf("mounts=%v err=%v", mounts, err)
	}
}

func TestParseKernelExt4(t *testing.T) {
	if !parseKernelExt4("nodev\text4\n\text4\n") {
		t.Fatal("missing ext4")
	}
	if parseKernelExt4("nodev\tnotext4\n") {
		t.Fatal("false match")
	}
}

func TestProbeToolingPackageNames(t *testing.T) {
	stat := func(string) bool { return false }
	debian := probeTooling("debian", stat)
	if debian.Required[0].Package != "e2fsprogs" || debian.Required[0].Installed {
		t.Fatalf("debian: %+v", debian.Required[0])
	}
	unknown := probeTooling("unknown", stat)
	if unknown.Required[0].Package != "" {
		t.Fatalf("unknown: %+v", unknown.Required[0])
	}
	if len(debian.Optional) != 0 {
		t.Fatalf("optional: %+v", debian.Optional)
	}
}

func TestProbeCapability(t *testing.T) {
	stat := func(string) bool { return false }
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\text4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason := probeCapability("debian", stat, dir)
	if ok || !strings.Contains(reason, "dumpe2fs") {
		t.Fatalf("missing binary: ok=%v reason=%q", ok, reason)
	}
	present := func(path string) bool { return filepath.Base(path) == "dumpe2fs" }
	ok, reason = probeCapability("debian", present, dir)
	if !ok || reason != "" {
		t.Fatalf("ready: ok=%v reason=%q", ok, reason)
	}
	empty := t.TempDir()
	ok, reason = probeCapability("debian", present, empty)
	if ok || reason != reasonExt4Unread {
		t.Fatalf("unread: ok=%v reason=%q", ok, reason)
	}
}

func TestCollectFixture(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\text4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), []byte(ext4Line(7)), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.KernelExt4.Present || !snap.Ext4.Present || len(snap.Ext4.Mounts) != 1 {
		t.Fatalf("%+v", snap)
	}
	if snap.Ext4.Mounts[0].Mountpoint != "/mnt/data" {
		t.Fatalf("%+v", snap.Ext4.Mounts[0])
	}
}

// Malformed lines and unsafe ext4 records are skipped and mark the list
// incomplete (shared internal/mountinfo rule); valid mounts are still listed.
func TestParseMountInfoSkipsMalformedAndUnsafeRecords(t *testing.T) {
	in := "40 1 0:36 / /mnt/data rw - ext4 /dev/sda1\n" + // no super-options
		"41 1 0:36 / /mnt/bad rw ext4 /dev/sdb1 rw\n" + // no separator
		"not-a-mountinfo-line\n" +
		"45 1 0:36 / /../bad rw - ext4 /dev/sdb1 rw\n" + // ".." mountpoint
		"44 1 0:36 / /mnt/foo..bar rw - ext4 /dev/disk..1 rw\n" + // dots inside a name are fine
		ext4Line(46)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || !truncated || len(mounts) != 2 {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
	if mounts[0].Mountpoint != "/mnt/foo..bar" || mounts[0].Device != "/dev/disk..1" {
		t.Fatalf("%+v", mounts[0])
	}
	// A clean inventory is not marked truncated.
	if mounts, truncated, err := ParseMountInfo(strings.NewReader(ext4Line(46))); err != nil || truncated || len(mounts) != 1 {
		t.Fatalf("clean: mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
}
