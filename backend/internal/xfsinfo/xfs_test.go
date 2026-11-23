// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package xfsinfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func xfsLine(id int) string {
	return strings.Join([]string{
		itoa(id), "1", "0:36", "/", "/mnt/data", "rw,relatime", "-", "xfs", "/dev/sda1", "rw,relatime",
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

func TestParseMountInfoKeepsOnlyXfs(t *testing.T) {
	in := "39 29 0:31 / /mnt/other rw - vfat /dev/sdb1 rw\n" + xfsLine(40)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || truncated || len(mounts) != 1 {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
	if mounts[0].Mountpoint != "/mnt/data" || mounts[0].Device != "/dev/sda1" {
		t.Fatalf("%+v", mounts[0])
	}
}

func TestParseMountInfoSkipsUnsafeDevice(t *testing.T) {
	in := "41 1 0:36 / /mnt/skip rw - xfs /dev/../sda rw\n" + xfsLine(43)
	mounts, _, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || len(mounts) != 1 || mounts[0].Device != "/dev/sda1" {
		t.Fatalf("mounts=%v err=%v", mounts, err)
	}
}

func TestParseMountInfoMissingOptionsDoesNotPanic(t *testing.T) {
	in := "40 1 0:36 / /mnt/data rw - xfs /dev/sda1\n"
	_, _, err := ParseMountInfo(strings.NewReader(in))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseMountInfoKeepsDottedNamesAndRejectsDotDot(t *testing.T) {
	in := "44 1 0:36 / /mnt/foo..bar rw - xfs /dev/disk..1 rw\n" +
		"45 1 0:36 / /../bad rw - xfs /dev/sdb1 rw\n" +
		xfsLine(46)
	_, _, err := ParseMountInfo(strings.NewReader(in))
	if err == nil {
		t.Fatal("expected error for a dotdot mountpoint")
	}
	in = "44 1 0:36 / /mnt/foo..bar rw - xfs /dev/disk..1 rw\n" + xfsLine(45)
	mounts, _, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || len(mounts) != 2 {
		t.Fatalf("mounts=%v err=%v", mounts, err)
	}
	if mounts[0].Mountpoint != "/mnt/foo..bar" || mounts[0].Device != "/dev/disk..1" {
		t.Fatalf("%+v", mounts[0])
	}
}

func TestParseMountInfoRejectsTypelessLine(t *testing.T) {
	if _, _, err := ParseMountInfo(strings.NewReader("not-a-mountinfo-line\n")); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseKernelXfs(t *testing.T) {
	if !parseKernelXfs("nodev\txfs\n\text4\n") {
		t.Fatal("missing xfs")
	}
	if parseKernelXfs("nodev\tnotxfs\n") {
		t.Fatal("false match")
	}
}

func TestProbeToolingPackageNames(t *testing.T) {
	stat := func(string) bool { return false }
	debian := probeTooling("debian", stat)
	if debian.Required[0].Package != "xfsprogs" || debian.Required[0].Installed {
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
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\txfs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason := probeCapability("debian", stat, dir)
	if ok || !strings.Contains(reason, "xfs_info") {
		t.Fatalf("missing binary: ok=%v reason=%q", ok, reason)
	}
	present := func(path string) bool { return filepath.Base(path) == "xfs_info" }
	ok, reason = probeCapability("debian", present, dir)
	if !ok || reason != "" {
		t.Fatalf("ready: ok=%v reason=%q", ok, reason)
	}
	empty := t.TempDir()
	ok, reason = probeCapability("debian", present, empty)
	if ok || reason != reasonXfsUnread {
		t.Fatalf("unread: ok=%v reason=%q", ok, reason)
	}
}

func TestCollectFixture(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\txfs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), []byte(xfsLine(7)), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.KernelXfs.Present || !snap.Xfs.Present || len(snap.Xfs.Mounts) != 1 {
		t.Fatalf("%+v", snap)
	}
	if snap.Xfs.Mounts[0].Mountpoint != "/mnt/data" {
		t.Fatalf("%+v", snap.Xfs.Mounts[0])
	}
}
