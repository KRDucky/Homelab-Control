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

func TestParseMountInfoSkipsUnsafeDeviceAndMarksIncomplete(t *testing.T) {
	for _, src := range []string{"/dev/../sda", "none", "/dev//sda", "/dev/bad\\999"} {
		in := "41 1 0:36 / /mnt/skip rw - xfs " + src + " rw\n" + xfsLine(43)
		mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
		if err != nil || !truncated || len(mounts) != 1 || mounts[0].Device != "/dev/sda1" {
			t.Fatalf("source %q: mounts=%v truncated=%v err=%v", src, mounts, truncated, err)
		}
	}
}

func TestParseMountInfoMalformedXfsRecordKeepsOtherMounts(t *testing.T) {
	for _, bad := range []string{
		"40 1 0:36 / /mnt/data rw - xfs /dev/sda1",      // missing super options
		"x 1 0:36 / /mnt/data rw - xfs /dev/sdb1 rw",    // bad mount ID
		"40 1 0:36 / relative rw - xfs /dev/sdb1 rw",    // relative mountpoint
		"40 1 0:36 / /mnt/bad\\9 rw - xfs /dev/sdb1 rw", // bad escape
	} {
		in := bad + "\n" + xfsLine(41)
		mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
		if err != nil || !truncated || len(mounts) != 1 || mounts[0].Mountpoint != "/mnt/data" {
			t.Fatalf("%q: mounts=%v truncated=%v err=%v", bad, mounts, truncated, err)
		}
	}
}

func TestParseMountInfoKeepsDottedNamesAndRejectsDotDot(t *testing.T) {
	in := "44 1 0:36 / /mnt/foo..bar rw - xfs /dev/disk..1 rw\n" +
		"45 1 0:36 / /../bad rw - xfs /dev/sdb1 rw\n" +
		xfsLine(46)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || !truncated || len(mounts) != 2 {
		t.Fatalf("dotdot mountpoint must be skipped, not listed: mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
	for _, m := range mounts {
		if hasDotDot(m.Mountpoint) {
			t.Fatalf("listed a dotdot mountpoint: %+v", m)
		}
	}
	in = "44 1 0:36 / /mnt/foo..bar rw - xfs /dev/disk..1 rw\n" + xfsLine(45)
	mounts, truncated, err = ParseMountInfo(strings.NewReader(in))
	if err != nil || truncated || len(mounts) != 2 {
		t.Fatalf("mounts=%v err=%v", mounts, err)
	}
	if mounts[0].Mountpoint != "/mnt/foo..bar" || mounts[0].Device != "/dev/disk..1" {
		t.Fatalf("%+v", mounts[0])
	}
}

func TestParseMountInfoTypelessLineMarksIncomplete(t *testing.T) {
	in := "not-a-mountinfo-line\n" + xfsLine(40)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || !truncated || len(mounts) != 1 {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
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
	if len(debian.Required) == 0 {
		t.Fatal("debian required tools missing")
	}
	if debian.Required[0].Package != "xfsprogs" || debian.Required[0].Installed {
		t.Fatalf("debian: %+v", debian.Required[0])
	}
	unknown := probeTooling("unknown", stat)
	if len(unknown.Required) == 0 {
		t.Fatal("unknown distro required tools missing")
	}
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

func TestAbsentReasonPrefersTruncation(t *testing.T) {
	if got := absentReason(true, "xfs_info binary was not found"); got != "mount inventory was incomplete; xfs presence could not be determined" {
		t.Fatal(got)
	}
	if got := absentReason(false, "xfs_info binary was not found"); got != "xfs_info binary was not found" {
		t.Fatal(got)
	}
}

func TestCollectReturnsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Collect(ctx, t.TempDir(), "debian"); err == nil {
		t.Fatal("expected cancellation")
	}
}

// The OpenAPI contract caps mountpoint at 256 bytes; a longer one is skipped
// and marks the list incomplete instead of breaking the response schema.
func TestParseMountInfoSkipsOverlongMountpoint(t *testing.T) {
	long := "/mnt/" + strings.Repeat("a", mountpointMaxBytes)
	in := "40 1 0:36 / " + long + " rw - xfs /dev/sdb1 rw\n" + xfsLine(41)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || !truncated || len(mounts) != 1 || mounts[0].Mountpoint != "/mnt/data" {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
	exact := "/" + strings.Repeat("b", mountpointMaxBytes-1)
	mounts, truncated, err = ParseMountInfo(strings.NewReader("40 1 0:36 / " + exact + " rw - xfs /dev/sdb1 rw\n"))
	if err != nil || truncated || len(mounts) != 1 {
		t.Fatalf("a %d-byte mountpoint must be kept: mounts=%v truncated=%v err=%v", mountpointMaxBytes, mounts, truncated, err)
	}
}
