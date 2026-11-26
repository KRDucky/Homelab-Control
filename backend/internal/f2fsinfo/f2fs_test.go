// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package f2fsinfo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func f2fsLine(id int) string {
	return strings.Join([]string{
		itoa(id), "1", "0:36", "/", "/mnt/data", "rw,relatime", "-", "f2fs", "/dev/sda1", "rw,relatime",
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

func TestParseMountInfoKeepsOnlyF2fs(t *testing.T) {
	in := "39 29 0:31 / /mnt/other rw - vfat /dev/sdb1 rw\n" + f2fsLine(40)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || truncated || len(mounts) != 1 {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
	if mounts[0].Mountpoint != "/mnt/data" || mounts[0].Device != "/dev/sda1" {
		t.Fatalf("%+v", mounts[0])
	}
}

func TestParseMountInfoSkipsUnsafeDevice(t *testing.T) {
	in := "41 1 0:36 / /mnt/skip rw - f2fs /dev/../sda rw\n" + f2fsLine(43)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || !truncated || len(mounts) != 1 || mounts[0].Device != "/dev/sda1" {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
}

func TestParseMountInfoIgnoresInvalidNonDeviceSource(t *testing.T) {
	in := "41 1 0:36 / /mnt/skip rw - f2fs remote\\999path rw\n" + f2fsLine(43)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || !truncated || len(mounts) != 1 || mounts[0].Device != "/dev/sda1" {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
}

func TestParseKernelF2fs(t *testing.T) {
	if !parseKernelF2fs("nodev\tf2fs\n\text4\n") {
		t.Fatal("missing f2fs")
	}
	if parseKernelF2fs("nodev\tnotf2fs\n") {
		t.Fatal("false match")
	}
}

func TestProbeToolingPackageNames(t *testing.T) {
	stat := func(string) bool { return false }
	debian := probeTooling("debian", stat)
	if debian.Required[0].Package != "f2fs-tools" || debian.Required[0].Installed {
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
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\tf2fs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason := probeCapability("debian", stat, dir)
	if ok || !strings.Contains(reason, "dump.f2fs") {
		t.Fatalf("missing binary: ok=%v reason=%q", ok, reason)
	}
	present := func(path string) bool { return filepath.Base(path) == "dump.f2fs" }
	ok, reason = probeCapability("debian", present, dir)
	if !ok || reason != "" {
		t.Fatalf("ready: ok=%v reason=%q", ok, reason)
	}
	empty := t.TempDir()
	ok, reason = probeCapability("debian", present, empty)
	if ok || reason != reasonF2fsUnread {
		t.Fatalf("unread: ok=%v reason=%q", ok, reason)
	}
}

func TestCollectFixture(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\tf2fs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), []byte(f2fsLine(7)), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.KernelF2fs.Present || !snap.F2fs.Present || len(snap.F2fs.Mounts) != 1 {
		t.Fatalf("%+v", snap)
	}
	if snap.F2fs.Mounts[0].Mountpoint != "/mnt/data" {
		t.Fatalf("%+v", snap.F2fs.Mounts[0])
	}
}

func TestAbsentReasonPrefersTruncation(t *testing.T) {
	if got := absentReason(true, "dump.f2fs binary was not found"); got != "mount inventory was truncated; f2fs presence could not be determined" {
		t.Fatal(got)
	}
	if got := absentReason(false, "dump.f2fs binary was not found"); got != "dump.f2fs binary was not found" {
		t.Fatal(got)
	}
}

func TestCollectReturnsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Collect(ctx, t.TempDir(), "debian"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// Malformed lines and unsafe f2fs records are skipped and mark the list
// incomplete (shared internal/mountinfo rule); valid mounts are still listed.
func TestParseMountInfoSkipsMalformedAndUnsafeRecords(t *testing.T) {
	in := "40 1 0:36 / /mnt/data rw - f2fs /dev/sda1\n" + // no super-options
		"41 1 0:36 / /mnt/bad rw f2fs /dev/sdb1 rw\n" + // no separator
		"not-a-mountinfo-line\n" +
		"45 1 0:36 / /../bad rw - f2fs /dev/sdb1 rw\n" + // ".." mountpoint
		"44 1 0:36 / /mnt/foo..bar rw - f2fs /dev/disk..1 rw\n" + // dots inside a name are fine
		f2fsLine(46)
	mounts, truncated, err := ParseMountInfo(strings.NewReader(in))
	if err != nil || !truncated || len(mounts) != 2 {
		t.Fatalf("mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
	if mounts[0].Mountpoint != "/mnt/foo..bar" || mounts[0].Device != "/dev/disk..1" {
		t.Fatalf("%+v", mounts[0])
	}
	// A clean inventory is not marked truncated.
	if mounts, truncated, err := ParseMountInfo(strings.NewReader(f2fsLine(46))); err != nil || truncated || len(mounts) != 1 {
		t.Fatalf("clean: mounts=%v truncated=%v err=%v", mounts, truncated, err)
	}
}

// Review finding on #178: when mountinfo cannot be read, the status must say
// it is unavailable, not report an empty (complete) list of filesystems.
func TestCollectMarksStatusUnavailableWhenMountinfoUnread(t *testing.T) {
	snap, err := Collect(context.Background(), t.TempDir(), "debian")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status.Available || snap.Status.Reason != reasonStatusUnread {
		t.Fatalf("status = %+v, want unavailable with reason", snap.Status)
	}
	if snap.Status.Filesystems == nil || len(snap.Status.Filesystems) != 0 {
		t.Fatalf("filesystems = %#v, want empty non-nil", snap.Status.Filesystems)
	}
	b, err := json.Marshal(snap.Status)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"available":false`) {
		t.Fatalf("json = %s", b)
	}
}

func TestCollectMarksStatusAvailableWhenMountinfoRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Status.Available || snap.Status.Reason != "" {
		t.Fatalf("status = %+v, want available", snap.Status)
	}
}
