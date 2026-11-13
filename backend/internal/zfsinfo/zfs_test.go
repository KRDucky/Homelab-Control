// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package zfsinfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

const mountFixture = `36 29 0:28 / /tank/media rw,relatime shared:1 - zfs tank/media rw
37 29 0:29 / / rw - ext4 /dev/sda1 rw
38 29 0:30 / /data rw - zfs pool/data rw
39 29 0:31 / /mnt/other rw - btrfs /dev/sdb1 rw
`

func TestParseMountInfoKeepsOnlyZFS(t *testing.T) {
	mounts, err := ParseMountInfo(strings.NewReader(mountFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 {
		t.Fatalf("mounts: %+v", mounts)
	}
	if mounts[0].Mountpoint != "/tank/media" || mounts[0].Dataset != "tank/media" {
		t.Fatalf("mount 0: %+v", mounts[0])
	}
	if mounts[1].Mountpoint != "/data" || mounts[1].Dataset != "pool/data" {
		t.Fatalf("mount 1: %+v", mounts[1])
	}
}

func TestParseMountInfoSkipsAbsoluteAndDotDotDatasets(t *testing.T) {
	in := "36 29 0:28 / /mnt/x rw - zfs /dev/sda1 rw\n37 29 0:29 / /mnt/y rw - zfs pool/../escape rw\n"
	mounts, err := ParseMountInfo(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 0 {
		t.Fatalf("accepted bad datasets: %+v", mounts)
	}
}

func TestParseKernelZfs(t *testing.T) {
	if !parseKernelZfs("nodev\tsysfs\nnodev\tzfs\n") {
		t.Fatal("zfs line must count")
	}
	if parseKernelZfs("nodev\tzfs_member\n        fuse\n") {
		t.Fatal("zfs_member and fuse must not count as zfs")
	}
	if parseKernelZfs("") || parseKernelZfs("nodev sysfs\n") {
		t.Fatal("empty or unrelated filesystems must be absent")
	}
}

func TestProbeKernelZfsFixture(t *testing.T) {
	dir := t.TempDir()
	if got := ProbeKernelZfs(dir); got.Present || got.Reason != reasonZfsUnread {
		t.Fatalf("missing proc: %+v", got)
	}
	if err := os.MkdirAll(filepath.Join(dir, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\tzfs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ProbeKernelZfs(dir); !got.Present || got.Reason != "" {
		t.Fatalf("listed zfs: %+v", got)
	}
}

func TestProbeToolingPresentDebian(t *testing.T) {
	stat := func(path string) bool {
		switch path {
		case "/usr/sbin/zfs", "/usr/sbin/zpool":
			return true
		default:
			return false
		}
	}
	tool := probeTooling("debian", stat)
	if tool.Family != "debian" || len(tool.Required) != 2 || len(tool.Optional) != 0 {
		t.Fatalf("%+v", tool)
	}
	if tool.Required[0].ID != "zfs" || tool.Required[0].Package != "zfsutils-linux" || !tool.Required[0].Installed || tool.Required[0].Binary != "/usr/sbin/zfs" {
		t.Fatalf("zfs: %+v", tool.Required[0])
	}
	if tool.Required[1].ID != "zpool" || tool.Required[1].Package != "zfsutils-linux" || !tool.Required[1].Installed || tool.Required[1].Binary != "/usr/sbin/zpool" {
		t.Fatalf("zpool: %+v", tool.Required[1])
	}
}

func TestProbeToolingPackageNames(t *testing.T) {
	stat := func(string) bool { return false }
	for _, family := range []string{"fedora", "suse", "arch", "unknown"} {
		tool := probeTooling(family, stat)
		for _, it := range tool.Required {
			if it.Package != "" || it.Installed {
				t.Fatalf("%s: %+v", family, it)
			}
		}
	}
	debian := probeTooling("debian", stat)
	if debian.Required[0].Package != "zfsutils-linux" || debian.Required[1].Package != "zfsutils-linux" {
		t.Fatalf("debian missing: %+v", debian)
	}
}

func TestProbeCapabilityMissingRequiredAndZfs(t *testing.T) {
	stat := func(string) bool { return false }
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\tzfs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason := probeCapability("debian", stat, dir)
	if ok || !strings.Contains(reason, "zfs") || !strings.Contains(reason, "zpool") {
		t.Fatalf("missing both: ok=%v reason=%q", ok, reason)
	}
	zfsOnly := func(path string) bool { return filepath.Base(path) == "zfs" }
	ok, reason = probeCapability("debian", zfsOnly, dir)
	if ok || !strings.Contains(reason, "zpool") || strings.Contains(reason, "zfs,") {
		t.Fatalf("missing zpool: ok=%v reason=%q", ok, reason)
	}
	both := func(path string) bool {
		base := filepath.Base(path)
		return base == "zfs" || base == "zpool"
	}
	empty := t.TempDir()
	ok, reason = probeCapability("debian", both, empty)
	if ok || reason != reasonZfsUnread {
		t.Fatalf("missing filesystems: ok=%v reason=%q", ok, reason)
	}
	ok, reason = probeCapability("debian", both, dir)
	if !ok || reason != "" {
		t.Fatalf("tools+kernel must be available: ok=%v reason=%q", ok, reason)
	}
}

func TestClipCapabilityReasonRuneSafe(t *testing.T) {
	prefix := strings.Repeat("a", capabilityReasonMax-1)
	got := clipCapabilityReason(prefix + "é")
	if !utf8.ValidString(got) {
		t.Fatalf("invalid utf8: %q", got)
	}
}

func TestCollectPresentFromMounts(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\tzfs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), []byte(mountFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Zfs.Present || len(snap.Zfs.Mounts) != 2 {
		t.Fatalf("%+v", snap.Zfs)
	}
	if !snap.KernelZfs.Present {
		t.Fatalf("kernel: %+v", snap.KernelZfs)
	}
}

func TestCollectCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Collect(ctx, t.TempDir(), "debian"); err == nil {
		t.Fatal("cancelled collect must error")
	}
}
