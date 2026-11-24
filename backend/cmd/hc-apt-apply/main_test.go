// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/aptapply"
)

// Fixture: shaped like Ubuntu 24.04's default ubuntu.sources (test data only).
const ubuntuSources = `Types: deb
URIs: http://us.archive.ubuntu.com/ubuntu/
Suites: noble noble-updates noble-backports
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg

Types: deb
URIs: http://security.ubuntu.com/ubuntu/
Suites: noble-security
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
`

func testRoot(t *testing.T) string {
	t.Helper()
	rootUID = uint32(os.Getuid())
	t.Cleanup(func() { rootUID = 0 })
	root := t.TempDir()
	// /etc/os-release is a symlink to ../usr/lib/os-release on Debian and Ubuntu.
	if err := os.MkdirAll(root+"/usr/lib", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/usr/lib/os-release", []byte("ID=ubuntu\nVERSION_CODENAME=noble\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+"/etc", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../usr/lib/os-release", root+aptapply.OSRelease); err != nil {
		t.Fatal(err)
	}
	for p, c := range map[string]string{
		aptapply.UbuntuSources: ubuntuSources,
	} {
		full := root + p
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(root+"/etc/apt/apt.conf.d", 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

var clock = func() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC) }

func call(t *testing.T, root string, req aptapply.Request) aptapply.Result {
	t.Helper()
	b, _ := json.Marshal(req)
	var out bytes.Buffer
	if err := run(bytes.NewReader(b), &out, root, clock); err != nil {
		t.Fatal(err)
	}
	var res aptapply.Result
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

func ip(i int) *int { return &i }

func TestConfWriteCreateReplaceAndRemove(t *testing.T) {
	root := testRoot(t)
	conf := root + aptapply.ConfPath
	res := call(t, root, aptapply.Request{Version: 1, Action: aptapply.ActionConfWrite, Tunables: aptapply.Tunables{Retries: ip(3)}})
	if !res.OK || res.Backup != "" {
		t.Fatalf("create: %+v", res)
	}
	first, _ := os.ReadFile(conf)
	if !strings.Contains(string(first), `Acquire::Retries "3";`) {
		t.Fatalf("content: %s", first)
	}
	if fi, _ := os.Stat(conf); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	// A stale digest writes nothing.
	res = call(t, root, aptapply.Request{Version: 1, Action: aptapply.ActionConfWrite, Tunables: aptapply.Tunables{Retries: ip(5)}})
	if res.OK {
		t.Fatal("write with a stale (empty) digest succeeded")
	}
	if now, _ := os.ReadFile(conf); !bytes.Equal(now, first) {
		t.Fatal("refused request changed the file")
	}
	res = call(t, root, aptapply.Request{Version: 1, Action: aptapply.ActionConfWrite, Tunables: aptapply.Tunables{Retries: ip(5)}, Digest: aptapply.Digest(first)})
	if !res.OK || res.Backup == "" {
		t.Fatalf("replace: %+v", res)
	}
	bak, err := os.ReadFile(filepath.Join(root+aptapply.BackupDir, res.Backup))
	if err != nil || !bytes.Equal(bak, first) {
		t.Fatalf("backup %q: %v", bak, err)
	}
	second, _ := os.ReadFile(conf)
	res = call(t, root, aptapply.Request{Version: 1, Action: aptapply.ActionConfRemove, Digest: aptapply.Digest(second)})
	if !res.OK {
		t.Fatalf("remove: %+v", res)
	}
	if _, err := os.Stat(conf); !os.IsNotExist(err) {
		t.Fatal("managed file still present")
	}
}

func TestMirrorSetEditsOnlyTheArchiveURI(t *testing.T) {
	root := testRoot(t)
	path := root + aptapply.UbuntuSources
	req := aptapply.Request{Version: 1, Action: aptapply.ActionMirrorSet, File: aptapply.UbuntuSources,
		From: "http://us.archive.ubuntu.com/ubuntu/", To: "http://mirror.example.edu/ubuntu/", Digest: aptapply.Digest([]byte(ubuntuSources))}
	res := call(t, root, req)
	if !res.OK || res.Changed != 1 || res.Backup == "" {
		t.Fatalf("%+v", res)
	}
	got, _ := os.ReadFile(path)
	if want := strings.Replace(ubuntuSources, req.From, req.To, 1); string(got) != want {
		t.Fatalf("file:\n%s", got)
	}
	// The same plan cannot be replayed: the digest no longer matches.
	if res := call(t, root, req); res.OK {
		t.Fatal("replayed request succeeded")
	}
	// Security entries are never switched.
	req2 := aptapply.Request{Version: 1, Action: aptapply.ActionMirrorSet, File: aptapply.UbuntuSources,
		From: "http://security.ubuntu.com/ubuntu/", To: "http://mirror.example.edu/ubuntu/", Digest: aptapply.Digest(got)}
	if res := call(t, root, req2); res.OK {
		t.Fatal("security entry switched")
	}
	if now, _ := os.ReadFile(path); !bytes.Equal(now, got) {
		t.Fatal("refused request changed the file")
	}
}

func TestRefusesSymlinkAndNonVendorTargets(t *testing.T) {
	root := testRoot(t)
	path := root + aptapply.UbuntuSources
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(elsewhere, []byte(ubuntuSources), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, path); err != nil {
		t.Fatal(err)
	}
	res := call(t, root, aptapply.Request{Version: 1, Action: aptapply.ActionMirrorSet, File: aptapply.UbuntuSources,
		From: "http://us.archive.ubuntu.com/ubuntu/", To: "http://mirror.example.edu/ubuntu/", Digest: aptapply.Digest([]byte(ubuntuSources))})
	if res.OK {
		t.Fatal("followed a symlink")
	}
	if b, _ := os.ReadFile(elsewhere); string(b) != ubuntuSources {
		t.Fatal("symlink target was changed")
	}
	b, _ := json.Marshal(map[string]any{"version": 1, "action": "mirror.set", "file": "/etc/shadow", "from": "http://a.example/", "to": "http://b.example/", "digest": strings.Repeat("a", 64)})
	var out bytes.Buffer
	if err := run(bytes.NewReader(b), &out, root, clock); err != nil || !strings.Contains(out.String(), `"ok":false`) {
		t.Fatalf("non-vendor file: %v %s", err, out.String())
	}
	out.Reset()
	if err := run(strings.NewReader(`{"version":1,"action":"conf.write","extra":1}`), &out, root, clock); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestBackupsArePruned(t *testing.T) {
	root := testRoot(t)
	for i := 0; i < aptapply.KeepBackups+3; i++ {
		ts := clock().Add(time.Duration(i) * time.Second)
		if _, err := backup(root, aptapply.ConfPath, []byte("x"), ts); err != nil {
			t.Fatal(err)
		}
	}
	ents, _ := os.ReadDir(root + aptapply.BackupDir)
	if len(ents) != aptapply.KeepBackups {
		t.Fatalf("%d backups kept", len(ents))
	}
	if !strings.Contains(ents[0].Name(), "120003") {
		t.Fatalf("oldest kept = %s; the three oldest should be gone", ents[0].Name())
	}
	if fi, _ := ents[0].Info(); fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %v", fi.Mode().Perm())
	}
}
