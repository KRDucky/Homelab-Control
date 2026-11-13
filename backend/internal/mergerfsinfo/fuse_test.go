// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseKernelFuse(t *testing.T) {
	if !parseKernelFuse("nodev\tsysfs\nnodev\tfuse\nnodev\tfusectl\n") {
		t.Fatal("fuse line must count")
	}
	if parseKernelFuse("nodev\tfusectl\n        fuseblk\n") {
		t.Fatal("fusectl and fuseblk must not count as fuse")
	}
	if parseKernelFuse("") || parseKernelFuse("nodev sysfs\n") {
		t.Fatal("empty or unrelated filesystems must be absent")
	}
}

func TestProbeKernelFuseFixture(t *testing.T) {
	dir := t.TempDir()
	if got := ProbeKernelFuse(dir); got.Present || got.Reason != reasonFuseUnread {
		t.Fatalf("missing proc: %+v", got)
	}
	if err := os.MkdirAll(filepath.Join(dir, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\tfuse\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ProbeKernelFuse(dir); !got.Present || got.Reason != "" {
		t.Fatalf("listed fuse: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\tfusectl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ProbeKernelFuse(dir); got.Present || got.Reason != reasonFuseMissing {
		t.Fatalf("fusectl only: %+v", got)
	}
}

func TestProbeCapabilityMissingRequiredAndFuse(t *testing.T) {
	stat := func(string) bool { return false }
	dir := t.TempDir()
	ok, reason := probeCapability("debian", stat, dir)
	if ok || !strings.Contains(reason, "mergerfs") || !strings.Contains(reason, "fusermount3") {
		t.Fatalf("missing binaries: ok=%v reason=%q", ok, reason)
	}
	if strings.Contains(reason, "snapraid") {
		t.Fatalf("optional snapraid must not degrade: %q", reason)
	}

	present := func(path string) bool {
		base := filepath.Base(path)
		return base == "mergerfs" || base == "fusermount3"
	}
	ok, reason = probeCapability("debian", present, dir)
	if ok || reason != reasonFuseUnread {
		t.Fatalf("binaries present, no proc: ok=%v reason=%q", ok, reason)
	}
	if err := os.MkdirAll(filepath.Join(dir, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\tfuse\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason = probeCapability("debian", present, dir)
	if !ok || reason != "" {
		t.Fatalf("ready: ok=%v reason=%q", ok, reason)
	}
}
