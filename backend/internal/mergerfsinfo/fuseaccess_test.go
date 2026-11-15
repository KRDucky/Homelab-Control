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

func writeFuseConf(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "fuse.conf"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProbeFuseAccessMissing(t *testing.T) {
	dir := t.TempDir()
	got := ProbeFuseAccess(dir)
	if got.Present || got.UserAllowOther || got.Reason != reasonFuseConfMissing {
		t.Fatalf("missing: %+v", got)
	}
}

func TestProbeFuseAccessFlag(t *testing.T) {
	dir := t.TempDir()
	writeFuseConf(t, dir, "# user_allow_other\n\n  user_allow_other  \n")
	got := ProbeFuseAccess(dir)
	if !got.Present || !got.UserAllowOther || got.Reason != "" {
		t.Fatalf("set: %+v", got)
	}
	writeFuseConf(t, dir, "# user_allow_other\nuser_allow_other extra\n")
	got = ProbeFuseAccess(dir)
	if !got.Present || got.UserAllowOther || got.Reason != "" {
		t.Fatalf("comment and extra token must not set the flag: %+v", got)
	}
}

func TestProbeFuseAccessOversized(t *testing.T) {
	dir := t.TempDir()
	writeFuseConf(t, dir, strings.Repeat("a", fuseConfMax+1))
	got := ProbeFuseAccess(dir)
	if got.Present || got.UserAllowOther || got.Reason != reasonFuseConfUnread {
		t.Fatalf("oversized: %+v", got)
	}
}

func TestProbeFuseAccessDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc", "fuse.conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ProbeFuseAccess(dir)
	if got.Present || got.Reason != reasonFuseConfUnread {
		t.Fatalf("directory: %+v", got)
	}
}

func TestProbeFuseAccessEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	secret := "fuse-conf-secret-do-not-echo"
	if err := os.WriteFile(filepath.Join(outside, "fuse.conf"), []byte(secret+"\nuser_allow_other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "fuse.conf"), filepath.Join(dir, "etc", "fuse.conf")); err != nil {
		t.Skip("filesystem does not support symlinks")
	}
	got := ProbeFuseAccess(dir)
	if got.Present || got.UserAllowOther || got.Reason != reasonFuseConfUnread {
		t.Fatalf("escaping symlink must fail closed: %+v", got)
	}
	if strings.Contains(got.Reason, secret) || strings.Contains(got.Reason, outside) {
		t.Fatalf("reason echoed the target: %+v", got)
	}
}

func TestProbeCapabilityIgnoresFuseConf(t *testing.T) {
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
		t.Fatalf("missing fuse.conf must not degrade: ok=%v reason=%q", ok, reason)
	}
}
