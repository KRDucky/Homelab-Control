// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package safefile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteSetsExactModeDespiteUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	p := filepath.Join(t.TempDir(), "f")
	if err := Write(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", fi.Mode().Perm())
	}
	if _, err := os.Stat(p + ".hc-tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
}

func TestReadRefusesSymlinkAndLarge(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(link, 100); err == nil {
		t.Fatal("symlink followed")
	}
	if _, err := Read(real, 5); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large file: %v", err)
	}
	if _, err := Read(dir, 100); err == nil {
		t.Fatal("directory read")
	}
	if b, err := Read(real, 100); err != nil || string(b) != "0123456789" {
		t.Fatalf("%q %v", b, err)
	}
}
