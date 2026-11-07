// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package filesinfo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSetting(t *testing.T, roots []fileRoot) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "files.json")
	b, err := json.Marshal(file{Version: 1, Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func jail(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "jail")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(d, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "sub", "inner.txt"), []byte("in"), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLoadMissingEmptyUnsafe(t *testing.T) {
	dir := t.TempDir()
	if s := LoadOrFailClosed(filepath.Join(dir, "missing.json"), false); s.BrowseAvailable() || s.Reason == "" {
		t.Fatal("missing file must fail closed")
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(`{"version":1,"roots":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := LoadOrFailClosed(empty, false); s.BrowseAvailable() || s.Reason == "" {
		t.Fatal("empty roots must fail closed")
	}
	p := filepath.Join(dir, "world.json")
	if err := os.WriteFile(p, []byte(`{"version":1,"roots":[{"id":"a","label":"A","path":"`+jail(t)+`"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p, false); err == nil {
		t.Fatal("world-writable setting accepted")
	}
	if s := LoadOrFailClosed(p, false); s.BrowseAvailable() {
		t.Fatal("world-writable setting must fail closed")
	}
	good := writeSetting(t, []fileRoot{{ID: "lab", Label: "Lab", Path: jail(t)}})
	ln := filepath.Join(dir, "link.json")
	if err := os.Symlink(good, ln); err != nil {
		t.Fatal(err)
	}
	if s := LoadOrFailClosed(ln, false); s.BrowseAvailable() {
		t.Fatal("symlinked setting must fail closed")
	}
	if os.Getuid() != 0 {
		if s := LoadOrFailClosed(good, true); s.BrowseAvailable() || s.Reason == "" {
			t.Fatal("non-root-owned setting must fail closed in production mode")
		}
	}
}

func TestParseRefusesForbiddenAndNonCanonical(t *testing.T) {
	d := jail(t)
	ok, err := Parse([]byte(`{"version":1,"roots":[{"id":"lab","label":"Lab files","path":"` + d + `"}]}`))
	if err != nil || !ok.BrowseAvailable() || len(ok.PublicRoots()) != 1 || ok.PublicRoots()[0].ID != "lab" {
		t.Fatalf("%+v %v", ok, err)
	}
	for _, body := range []string{
		``, `{}`, `{"version":2,"roots":[]}`, `{"version":1,"roots":[{"id":"x","label":"X","path":"/"}]}`,
		`{"version":1,"roots":[{"id":"x","label":"X","path":"/etc"}]}`,
		`{"version":1,"roots":[{"id":"x","label":"X","path":"/etc/passwd"}]}`,
		`{"version":1,"roots":[{"id":"x","label":"X","path":"/root"}]}`,
		`{"version":1,"roots":[{"id":"x","label":"X","path":"/proc"}]}`,
		`{"version":1,"roots":[{"id":"x","label":"X","path":"/sys"}]}`,
		`{"version":1,"roots":[{"id":"x","label":"X","path":"/home"}]}`,
		`{"version":1,"roots":[{"id":"x","label":"X","path":"/home/alice"}]}`,
		`{"version":1,"roots":[{"id":"X","label":"X","path":"` + d + `"}]}`,
		`{"version":1,"roots":[{"id":"lab","label":"X","path":"relative"}]}`,
		`{"version":1,"roots":[{"id":"lab","label":"X","path":"` + d + `/../` + filepath.Base(d) + `"}]}`,
		`{"version":1,"roots":[],"extra":true}`,
		`{"version":1,"roots":[{"id":"a","label":"A","path":"` + d + `"},{"id":"a","label":"B","path":"` + d + `"}]}`,
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestJailEscapeAndONoFollow(t *testing.T) {
	d := jail(t)
	if err := os.Symlink("/", filepath.Join(d, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub", filepath.Join(d, "inside-link")); err != nil {
		t.Fatal(err)
	}
	s, err := Parse([]byte(`{"version":1,"roots":[{"id":"lab","label":"Lab","path":"` + d + `"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ents, err := s.List("lab", "")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]Entry{}
	for _, e := range ents {
		names[e.Name] = e
		if strings.Contains(e.Name, "/") || strings.Contains(e.Owner, d) {
			t.Fatalf("entry leaked a path: %+v", e)
		}
	}
	if !names["sub"].Dir || names["readme.txt"].Dir || names["escape"].Dir || names["inside-link"].Dir {
		t.Fatalf("%+v", names)
	}
	if _, err := s.List("lab", ".."); !errors.Is(err, ErrEscape) {
		t.Fatalf(".. : %v", err)
	}
	if _, err := s.List("lab", "../"+filepath.Base(d)); !errors.Is(err, ErrEscape) && !errors.Is(err, ErrBadPath) {
		t.Fatalf("prefixed .. : %v", err)
	}
	if _, err := s.List("lab", "sub//inner.txt"); !errors.Is(err, ErrBadPath) {
		t.Fatalf("extra slashes: %v", err)
	}
	if _, err := s.List("lab", "/etc"); !errors.Is(err, ErrBadPath) {
		t.Fatalf("absolute: %v", err)
	}
	if _, err := s.List("lab", "escape"); !errors.Is(err, ErrNotDir) && !errors.Is(err, ErrEscape) {
		t.Fatalf("symlink out as dir: %v", err)
	}
	if _, err := s.List("lab", "escape/etc"); !errors.Is(err, ErrEscape) && !errors.Is(err, ErrBadPath) && !errors.Is(err, ErrNotDir) {
		t.Fatalf("symlink out walk: %v", err)
	}
	if _, err := s.List("lab", "inside-link"); !errors.Is(err, ErrNotDir) {
		t.Fatalf("O_NOFOLLOW inside symlink: %v", err)
	}
	got, err := s.List("lab", "sub")
	if err != nil || len(got) != 1 || got[0].Name != "inner.txt" || got[0].Dir {
		t.Fatalf("sub: %+v %v", got, err)
	}
	if _, err := s.List("nope", ""); !errors.Is(err, ErrUnknownRoot) {
		t.Fatalf("unknown root: %v", err)
	}
}

func TestListDoesNotReadContents(t *testing.T) {
	d := jail(t)
	s, err := Parse([]byte(`{"version":1,"roots":[{"id":"lab","label":"Lab","path":"` + d + `"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ents, err := s.List("lab", ".")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(ents)
	if strings.Contains(string(b), "hello") || strings.Contains(string(b), d) {
		t.Fatalf("listing leaked contents or jail path: %s", b)
	}
}

func TestOversizeDirectory(t *testing.T) {
	d := filepath.Join(t.TempDir(), "many")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxEntries+1; i++ {
		if err := os.WriteFile(filepath.Join(d, fmt.Sprintf("f-%04d", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Parse([]byte(`{"version":1,"roots":[{"id":"lab","label":"Lab","path":"` + d + `"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.List("lab", ""); !errors.Is(err, ErrTooMany) {
		t.Fatalf("oversize: %v", err)
	}
}

func TestRelPath(t *testing.T) {
	if p, err := RelPath(""); err != nil || p != "." {
		t.Fatal(p, err)
	}
	if p, err := RelPath("a/b"); err != nil || p != "a/b" {
		t.Fatal(p, err)
	}
	if _, err := RelPath("a/../b"); !errors.Is(err, ErrEscape) {
		t.Fatal(err)
	}
	if _, err := RelPath("a//b"); !errors.Is(err, ErrBadPath) {
		t.Fatal(err)
	}
}
