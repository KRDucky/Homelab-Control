// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
	writeFuseConf(t, dir, "# user_allow_other\n\nuser_allow_other\n")
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

func TestProbeFuseAccessLineShapes(t *testing.T) {
	dir := t.TempDir()
	// strip_line: text after '#' is a comment; the directive still matches.
	writeFuseConf(t, dir, "user_allow_other # comment\n")
	got := ProbeFuseAccess(dir)
	if !got.Present || !got.UserAllowOther {
		t.Fatalf("same-line comment must still set the flag: %+v", got)
	}
	// ASCII surrounding whitespace (C isspace) is trimmed.
	writeFuseConf(t, dir, " user_allow_other \t\n")
	got = ProbeFuseAccess(dir)
	if !got.Present || !got.UserAllowOther {
		t.Fatalf("ASCII surrounding whitespace must set the flag: %+v", got)
	}
	// CRLF is trimmed as C isspace.
	writeFuseConf(t, dir, "user_allow_other\r\n")
	got = ProbeFuseAccess(dir)
	if !got.Present || !got.UserAllowOther {
		t.Fatalf("CRLF must set the flag: %+v", got)
	}
	// Unicode whitespace is not C isspace and must not match.
	writeFuseConf(t, dir, "\u00a0user_allow_other\n")
	got = ProbeFuseAccess(dir)
	if !got.Present || got.UserAllowOther {
		t.Fatalf("unicode whitespace must not set the flag: %+v", got)
	}
}

func TestProbeFuseAccessNoFinalNewline(t *testing.T) {
	dir := t.TempDir()
	writeFuseConf(t, dir, "user_allow_other")
	got := ProbeFuseAccess(dir)
	if !got.Present || got.UserAllowOther {
		t.Fatalf("unterminated final line must not set the flag: %+v", got)
	}
}

func TestProbeFuseAccessOverlongLine(t *testing.T) {
	dir := t.TempDir()
	// fgets(line, 256) cannot fit the newline, so fusermount discards the
	// whole line even though the trimmed content would be user_allow_other.
	writeFuseConf(t, dir, "user_allow_other"+strings.Repeat(" ", 250)+"\n")
	got := ProbeFuseAccess(dir)
	if !got.Present || got.UserAllowOther {
		t.Fatalf("overlong line must not set the flag: %+v", got)
	}
}

func TestProbeFuseAccessEmbeddedNUL(t *testing.T) {
	dir := t.TempDir()
	// C strlen stops at the NUL, so fusermount never sees the newline and
	// discards the line; we fail closed the same way.
	writeFuseConf(t, dir, "user_allow_other\x00x\n")
	got := ProbeFuseAccess(dir)
	if !got.Present || got.UserAllowOther {
		t.Fatalf("embedded NUL must not set the flag: %+v", got)
	}
}

func TestProbeFuseAccessInRootSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	secret := "fuse-conf-secret-do-not-echo"
	if err := os.WriteFile(filepath.Join(dir, "etc", "other.conf"), []byte(secret+"\nuser_allow_other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other.conf", filepath.Join(dir, "etc", "fuse.conf")); err != nil {
		t.Skip("filesystem does not support symlinks")
	}
	got := ProbeFuseAccess(dir)
	if got.Present || got.UserAllowOther || got.Reason != reasonFuseConfUnread {
		t.Fatalf("in-root symlink must fail closed: %+v", got)
	}
	if strings.Contains(got.Reason, secret) {
		t.Fatalf("reason echoed the target: %+v", got)
	}
}

func TestProbeFuseAccessEtcSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "realdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realdir", "fuse.conf"), []byte("user_allow_other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("realdir", filepath.Join(dir, "etc")); err != nil {
		t.Skip("filesystem does not support symlinks")
	}
	got := ProbeFuseAccess(dir)
	if got.Present || got.UserAllowOther || got.Reason != reasonFuseConfUnread {
		t.Fatalf("etc symlink must fail closed: %+v", got)
	}
}

func TestProbeFuseAccessRetriesEINTR(t *testing.T) {
	dir := t.TempDir()
	writeFuseConf(t, dir, "user_allow_other\n")
	old := rawOpenat
	t.Cleanup(func() { rawOpenat = old })
	calls := 0
	eintrSent := false
	rawOpenat = func(dirfd uintptr, name string, flags int) (int, error) {
		calls++
		if name == "fuse.conf" && !eintrSent {
			eintrSent = true
			return -1, syscall.EINTR
		}
		return syscall.Openat(int(dirfd), name, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	}
	got := ProbeFuseAccess(dir)
	if !got.Present || !got.UserAllowOther || !eintrSent || calls != 3 {
		t.Fatalf("EINTR retry: %+v calls=%d eintr=%v", got, calls, eintrSent)
	}
}

func TestProbeFuseAccessPersistentEINTR(t *testing.T) {
	dir := t.TempDir()
	writeFuseConf(t, dir, "user_allow_other\n")
	old := rawOpenat
	t.Cleanup(func() { rawOpenat = old })
	calls := 0
	rawOpenat = func(dirfd uintptr, name string, flags int) (int, error) {
		calls++
		return -1, syscall.EINTR
	}
	got := ProbeFuseAccess(dir)
	if got.Present || got.UserAllowOther || got.Reason != reasonFuseConfUnread || calls != 8 {
		t.Fatalf("persistent EINTR must fail closed: %+v calls=%d", got, calls)
	}
}

func TestProbeFuseAccessFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "etc", "fuse.conf"), 0o644); err != nil {
		t.Skip("filesystem does not support fifos")
	}
	got := ProbeFuseAccess(dir)
	if got.Present || got.UserAllowOther || got.Reason != reasonFuseConfUnread {
		t.Fatalf("fifo must fail closed: %+v", got)
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
