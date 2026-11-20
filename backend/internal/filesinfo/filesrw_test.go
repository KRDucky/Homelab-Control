// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package filesinfo

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func rwJail(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "jail")
	if err := os.MkdirAll(filepath.Join(d, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "sub", "inner.txt"), []byte("inner"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/", filepath.Join(d, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(d, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func rwSet(t *testing.T) Set {
	t.Helper()
	p := filepath.Join(t.TempDir(), "files.json")
	body := `{"version":1,"roots":[{"id":"lab","label":"Lab","path":"` + rwJail(t) + `"}]}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := LoadOrFailClosed(p, false)
	if !s.BrowseAvailable() {
		t.Fatal(s.Reason)
	}
	return s
}

func TestReadContentCapsAndRefusesNonRegular(t *testing.T) {
	s := rwSet(t)
	c, err := s.ReadContent("lab", "readme.txt")
	if err != nil || string(c.Bytes) != "hello" || c.Truncated {
		t.Fatalf("readme: %v %q truncated=%v", err, c.Bytes, c.Truncated)
	}
	if _, err := s.ReadContent("lab", "escape"); err != ErrNotRegular {
		t.Fatalf("symlink read = %v, want ErrNotRegular", err)
	}
	if _, err := s.ReadContent("lab", "sub"); err != ErrNotRegular {
		t.Fatalf("dir read = %v, want ErrNotRegular", err)
	}
	if _, err := s.ReadContent("lab", "pipe"); err != ErrNotRegular {
		t.Fatalf("fifo read = %v, want ErrNotRegular", err)
	}
	if _, err := s.ReadContent("lab", ".."); err == nil {
		t.Fatal(".. escaped the jail")
	}
	if _, err := s.ReadContent("lab", "sub//inner.txt"); err == nil {
		t.Fatal("extra slashes escaped the jail")
	}
}

func TestWriteContentRefusesSymlinkDirAndSpecial(t *testing.T) {
	s := rwSet(t)
	if _, err := s.WriteContent("lab", "escape", []byte("x")); err != ErrNotRegular {
		t.Fatalf("symlink write = %v", err)
	}
	if _, err := s.WriteContent("lab", "sub", []byte("x")); err != ErrNotRegular {
		t.Fatalf("dir write = %v", err)
	}
	if _, err := s.WriteContent("lab", "pipe", []byte("x")); err != ErrNotRegular {
		t.Fatalf("fifo write = %v", err)
	}
	e, err := s.WriteContent("lab", "readme.txt", []byte("replaced"))
	if err != nil || e.Size != 8 {
		t.Fatalf("write: %v %+v", err, e)
	}
	c, _ := s.ReadContent("lab", "readme.txt")
	if string(c.Bytes) != "replaced" {
		t.Fatalf("content = %q", c.Bytes)
	}
}

func TestCreateRenameDeleteJailed(t *testing.T) {
	s := rwSet(t)
	if _, err := s.Create("lab", "", "new.txt", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("lab", "", "newdir", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("lab", "", "../evil", false); err == nil {
		t.Fatal(".. name created outside jail")
	}
	if _, err := s.Create("lab", "", "a/b", false); err != ErrBadName {
		t.Fatalf("slash name = %v", err)
	}
	if _, err := s.Create("lab", "newdir", "inside.txt", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename("lab", "new.txt", "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename("lab", "renamed.txt", "../out"); err == nil {
		t.Fatal("rename escaped jail")
	}
	// Renaming a symlink moves the link itself (renameat never follows it), so
	// it must not touch the target outside the jail.
	if _, err := s.Rename("lab", "escape", "escaped-link"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat("/"); err != nil {
		t.Fatalf("host root was affected: %v", err)
	}
	if _, err := s.Rename("lab", "escaped-link", "escape"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("lab", "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("lab", "newdir"); err != ErrNotEmpty {
		t.Fatalf("non-empty dir delete = %v, want ErrNotEmpty", err)
	}
	if err := s.Delete("lab", "newdir/inside.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("lab", "newdir"); err != nil {
		t.Fatal(err)
	}
	// The jail's escape symlink points at /; deleting it must remove the link
	// (never its target), leaving the host root intact.
	if err := s.Delete("lab", "escape"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat("/"); err != nil {
		t.Fatalf("host root was affected: %v", err)
	}
}

func TestStatTargetAndDeleteRefusesSpecial(t *testing.T) {
	s := rwSet(t)
	tg, err := s.StatTarget("lab", "readme.txt")
	if err != nil || tg.Kind != "file" || tg.Size != 5 {
		t.Fatalf("stat: %v %+v", err, tg)
	}
	if err := s.Delete("lab", "pipe"); err != ErrNotRegular {
		t.Fatalf("fifo delete = %v, want ErrNotRegular", err)
	}
	if err := s.Delete("lab", "missing"); err == nil {
		t.Fatal("missing delete succeeded")
	}
}

func TestRenameNeverReplacesDestination(t *testing.T) {
	s := rwSet(t)
	if _, err := s.Create("lab", "", "dest.txt", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteContent("lab", "dest.txt", []byte("keep-me")); err != nil {
		t.Fatal(err)
	}
	// Renaming onto an existing file must be refused and leave both files intact.
	if _, err := s.Rename("lab", "readme.txt", "dest.txt"); err != ErrExists {
		t.Fatalf("rename onto existing = %v, want ErrExists", err)
	}
	c, err := s.ReadContent("lab", "dest.txt")
	if err != nil || string(c.Bytes) != "keep-me" {
		t.Fatalf("destination changed: %v %q", err, c.Bytes)
	}
	if _, err := s.ReadContent("lab", "readme.txt"); err != nil {
		t.Fatalf("source was moved: %v", err)
	}
	// A symlink destination must be refused (never followed).
	if _, err := s.Rename("lab", "readme.txt", "escape"); err != ErrExists {
		t.Fatalf("rename onto symlink = %v, want ErrExists", err)
	}
	// A directory destination is refused too.
	if _, err := s.Rename("lab", "readme.txt", "sub"); err != ErrExists {
		t.Fatalf("rename onto dir = %v, want ErrExists", err)
	}
}

func TestValidName(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "a\x00b", strings.Repeat("x", 256), "\xff"} {
		if err := validName(bad); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	for _, ok := range []string{"a", "a.txt", "café", "a-b_c.d", strings.Repeat("x", 255)} {
		if err := validName(ok); err != nil {
			t.Errorf("name %q rejected: %v", ok, err)
		}
	}
}
