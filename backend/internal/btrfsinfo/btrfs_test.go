// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package btrfsinfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func btrfsLine(id int) string {
	return strings.Join([]string{
		itoa(id), "1", "0:36", "/", "/mnt/data", "rw,relatime", "-", "btrfs", "/dev/sda1",
		"rw,relatime,subvolid=5,subvol=/",
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

func TestParseMountInfoKeepsOnlyBtrfs(t *testing.T) {
	in := "39 29 0:31 / /mnt/other rw - ext4 /dev/sdb1 rw\n" + btrfsLine(40)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || truncated || len(mounts) != 1 {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
	if mounts[0].Mountpoint != "/mnt/data" || mounts[0].Device != "/dev/sda1" || mounts[0].Subvol != "/" || mounts[0].SubvolID != "5" {
		t.Fatalf("%+v", mounts[0])
	}
}

func TestParseMountInfoSkipsUnsafeDeviceAndSubvol(t *testing.T) {
	in := "41 1 0:36 / /mnt/skip rw - btrfs /dev/../sda rw,subvol=/\n" +
		"42 1 0:36 / /mnt/badsub rw - btrfs /dev/sdb1 rw,subvol=/../x\n" +
		btrfsLine(43)
	mounts, _, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || len(mounts) != 1 || mounts[0].Device != "/dev/sda1" {
		t.Fatalf("mounts=%v err=%v", mounts, err)
	}
}

func TestParseMountInfoFlagsInvalidSubvolumeAsTruncated(t *testing.T) {
	in := "42 1 0:36 / /mnt/badsub rw - btrfs /dev/sdb1 rw,subvolid=invalid,subvol=/\n" + btrfsLine(43)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || !truncated || len(mounts) != 1 || mounts[0].Device != "/dev/sda1" {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
}

func TestParseMountInfoKeepsDottedNames(t *testing.T) {
	in := "44 1 0:36 / /mnt/foo..bar rw - btrfs /dev/disk..1 rw,subvol=/snap..1,subvolid=7\n" + btrfsLine(45)
	mounts, _, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || len(mounts) != 2 {
		t.Fatalf("mounts=%v err=%v", mounts, err)
	}
	if mounts[0].Mountpoint != "/mnt/foo..bar" || mounts[0].Device != "/dev/disk..1" || mounts[0].Subvol != "/snap..1" || mounts[0].SubvolID != "7" {
		t.Fatalf("%+v", mounts[0])
	}
}

func TestParseKernelBtrfs(t *testing.T) {
	if !parseKernelBtrfs("nodev\tbtrfs\n\text4\n") {
		t.Fatal("missing btrfs")
	}
	if parseKernelBtrfs("nodev\tnotbtrfs\n") {
		t.Fatal("false match")
	}
}

func TestProbeToolingPackageNames(t *testing.T) {
	stat := func(string) bool { return false }
	debian := probeTooling("debian", stat)
	if debian.Required[0].Package != "btrfs-progs" || debian.Required[0].Installed {
		t.Fatalf("debian: %+v", debian.Required[0])
	}
	if debian.Optional[0].ID != "smartctl" || debian.Optional[0].Package != "smartmontools" {
		t.Fatalf("smartctl: %+v", debian.Optional[0])
	}
	suse := probeTooling("suse", stat)
	if suse.Required[0].Package != "btrfsprogs" {
		t.Fatalf("suse: %+v", suse.Required[0])
	}
	fedora := probeTooling("fedora", stat)
	if fedora.Required[0].Package != "btrfs-progs" {
		t.Fatalf("fedora: %+v", fedora.Required[0])
	}
	unknown := probeTooling("unknown", stat)
	if unknown.Required[0].Package != "" || unknown.Optional[0].Package != "" {
		t.Fatalf("unknown: %+v %+v", unknown.Required[0], unknown.Optional[0])
	}
}

func TestProbeCapability(t *testing.T) {
	stat := func(string) bool { return false }
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\tbtrfs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason := probeCapability("debian", stat, dir)
	if ok || !strings.Contains(reason, "btrfs") {
		t.Fatalf("missing binary: ok=%v reason=%q", ok, reason)
	}
	present := func(path string) bool { return filepath.Base(path) == "btrfs" }
	ok, reason = probeCapability("debian", present, dir)
	if !ok || reason != "" {
		t.Fatalf("ready: ok=%v reason=%q", ok, reason)
	}
	empty := t.TempDir()
	ok, reason = probeCapability("debian", present, empty)
	if ok || reason != reasonBtrfsUnread {
		t.Fatalf("unread: ok=%v reason=%q", ok, reason)
	}
}

func TestCollectFixture(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "nodev\tbtrfs\n"
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), []byte(btrfsLine(7)), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.KernelBtrfs.Present || !snap.Btrfs.Present || len(snap.Btrfs.Mounts) != 1 {
		t.Fatalf("%+v", snap)
	}
	if snap.Btrfs.Mounts[0].Mountpoint != "/mnt/data" {
		t.Fatalf("%+v", snap.Btrfs.Mounts[0])
	}
}

func TestCollectReturnsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Collect(ctx, t.TempDir(), "debian"); err == nil {
		t.Fatal("expected cancellation")
	}
}

// Malformed lines are skipped and mark the list incomplete (shared
// internal/mountinfo rule); valid btrfs mounts are still listed.
func TestParseMountInfoSkipsMalformedLines(t *testing.T) {
	in := "40 1 0:36 / /mnt/data rw - btrfs /dev/sda1\n" + "not-a-mountinfo-line\n" + btrfsLine(45)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || !truncated || len(mounts) != 1 {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
}
