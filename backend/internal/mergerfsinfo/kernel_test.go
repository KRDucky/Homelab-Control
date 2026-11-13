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

func TestParseKernelRelease(t *testing.T) {
	ok := []string{"6.8.0-47-generic", "6.12.1-arch1-1", "6.1.0-32-amd64", "6.8.0+"}
	for _, s := range ok {
		got, good := parseKernelRelease(s + "\n")
		if !good || got != s {
			t.Fatalf("%q: got %q ok=%v", s, got, good)
		}
	}
	bad := []string{"", "   ", "6.8.0 /tmp", "6.8.0;id", "../6.8", strings.Repeat("a", kernelReleaseMax+1)}
	for _, s := range bad {
		if rel, good := parseKernelRelease(s); good {
			t.Fatalf("must reject %q got %q", s, rel)
		}
	}
}

func writeOSRelease(t *testing.T, dir, body string) {
	t.Helper()
	p := filepath.Join(dir, "proc", "sys", "kernel")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "osrelease"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProbeKernelReleaseFixture(t *testing.T) {
	dir := t.TempDir()
	if got := ProbeKernelRelease(dir); got.Present || got.Reason != reasonReleaseUnread || got.Release != "" {
		t.Fatalf("missing proc: %+v", got)
	}
	writeOSRelease(t, dir, "6.8.0-hc-test\n")
	got := ProbeKernelRelease(dir)
	if !got.Present || got.Release != "6.8.0-hc-test" || got.Reason != "" {
		t.Fatalf("listed: %+v", got)
	}
	writeOSRelease(t, dir, "not a version!")
	got = ProbeKernelRelease(dir)
	if got.Present || got.Release != "" || got.Reason != reasonReleaseUnread {
		t.Fatalf("invalid: %+v", got)
	}
}

func TestProbeKernelReleaseOversized(t *testing.T) {
	dir := t.TempDir()
	writeOSRelease(t, dir, strings.Repeat("x", kernelReleaseMaxBytes+1))
	got := ProbeKernelRelease(dir)
	if got.Present || got.Reason != reasonReleaseUnread {
		t.Fatalf("oversized: %+v", got)
	}
}

func TestProbeKernelReleaseDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc", "sys", "kernel", "osrelease"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ProbeKernelRelease(dir)
	if got.Present || got.Reason != reasonReleaseUnread {
		t.Fatalf("directory: %+v", got)
	}
}

func TestProbeKernelReleaseEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "osrelease"), []byte("6.8.0-escape\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "proc", "sys", "kernel"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "osrelease"), filepath.Join(dir, "proc", "sys", "kernel", "osrelease")); err != nil {
		t.Skip("filesystem does not support symlinks")
	}
	got := ProbeKernelRelease(dir)
	if got.Present || got.Release != "" || got.Reason != reasonReleaseUnread {
		t.Fatalf("escaping symlink must fail closed: %+v", got)
	}
}

func TestProbeCapabilityIgnoresKernelRelease(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "filesystems"), []byte("nodev\tfuse\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	both := func(path string) bool {
		base := filepath.Base(path)
		return base == "mergerfs" || base == "fusermount3"
	}
	ok, reason := probeCapability("debian", both, dir)
	if !ok || reason != "" {
		t.Fatalf("missing osrelease must not degrade: ok=%v reason=%q", ok, reason)
	}
}
