// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/state"
)

func TestUserLifecycle(t *testing.T) {
	p := filepath.Join(t.TempDir(), "users.json")
	var out bytes.Buffer
	if err := run([]string{"user", "add", "--file", p, "--username", "admin"}, strings.NewReader("short\n"), &out); err == nil {
		t.Fatal("short password accepted")
	}
	if err := run([]string{"user", "add", "--file", p, "--username", "admin"}, strings.NewReader("a-long-enough-password\n"), &out); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("users file mode %#o", fi.Mode().Perm())
	}
	if err := run([]string{"user", "add", "--file", p, "--username", "admin"}, strings.NewReader("a-long-enough-password\n"), &out); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := run([]string{"user", "add", "--file", p, "--username", "x", "--scopes", "storage.execute"}, strings.NewReader("a-long-enough-password\n"), &out); err == nil {
		t.Fatal("unknown scope accepted")
	}
	f, err := auth.LoadUsersFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := auth.NewStore(f).Authenticate("admin", "a-long-enough-password"); !ok {
		t.Fatal("created user cannot authenticate")
	}
	if err := run([]string{"user", "disable", "--file", p, "--username", "admin"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"user", "list", "--file", p}, nil, &out); err != nil || !strings.Contains(out.String(), "admin\tdisabled") {
		t.Fatalf("%v %q", err, out.String())
	}
}

func TestUserScopes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "users.json")
	var out bytes.Buffer
	if err := run([]string{"user", "add", "--file", p, "--username", "admin"}, strings.NewReader("a-long-enough-password\n"), &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"user", "scopes", "--file", p, "--username", "admin"}, nil, &out); err == nil {
		t.Fatal("scopes without --scopes accepted")
	}
	if err := run([]string{"user", "scopes", "--file", p, "--username", "admin", "--scopes", "system.read,storage.execute"}, nil, &out); err == nil {
		t.Fatal("unknown scope accepted")
	}
	if err := run([]string{"user", "scopes", "--file", p, "--username", "nobody", "--scopes", "system.read"}, nil, &out); err == nil {
		t.Fatal("unknown user accepted")
	}
	if err := run([]string{"user", "scopes", "--file", p, "--username", "admin", "--scopes", "system.read,services.read,services.logs"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"user", "list", "--file", p}, nil, &out); err != nil || !strings.Contains(out.String(), "admin\tenabled\tsystem.read,services.read,services.logs") {
		t.Fatalf("%v %q", err, out.String())
	}
}

func TestAuditVerify(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	l, err := audit.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := l.Log(audit.Event{Event: "services.unit.logs", Outcome: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	seq, hash := l.Head()
	l.Close()
	cp := fmt.Sprintf("%d:%s", seq, hash)
	var out bytes.Buffer
	if err := run([]string{"audit", "verify", "--file", p, "--checkpoint", cp}, nil, &out); err != nil || !strings.Contains(out.String(), "OK: chain verified") {
		t.Fatalf("%v\n%s", err, out.String())
	}
	b, _ := os.ReadFile(p)
	lines := strings.SplitAfter(strings.TrimSuffix(string(b), "\n"), "\n")
	if err := os.WriteFile(p, []byte(strings.Join(lines[:len(lines)-1], "")), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"audit", "verify", "--file", p}, nil, &out); err != nil || !strings.Contains(out.String(), "cannot be detected") {
		t.Fatalf("clean truncation without checkpoint should pass with a note: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := run([]string{"audit", "verify", "--file", p, "--checkpoint", cp}, nil, &out); err == nil || !strings.Contains(out.String(), "truncated") {
		t.Fatalf("truncation not detected with checkpoint: %v\n%s", err, out.String())
	}
	for _, bad := range []string{"x", "0:" + hash, "1:zz"} {
		if err := run([]string{"audit", "verify", "--file", p, "--checkpoint", bad}, nil, &out); err == nil {
			t.Errorf("checkpoint %q accepted", bad)
		}
	}
}

func TestAuditVerifyDatabase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s")
	_ = os.Mkdir(dir, 0o700)
	p := filepath.Join(dir, "state.db")
	db, err := state.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	l, err := audit.OpenStore(db.Audit())
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Log(audit.Event{Event: "auth.login", User: "admin", Outcome: "success"})
	seq, hash := l.Head()
	// The service keeps the database open while hlctl reads it.
	var out bytes.Buffer
	if err := run([]string{"audit", "verify", "--db", p, "--checkpoint", fmt.Sprintf("%d:%s", seq, hash)}, nil, &out); err != nil || !strings.Contains(out.String(), "OK: chain verified") {
		t.Fatalf("%v\n%s", err, out.String())
	}
	out.Reset()
	if err := run([]string{"audit", "export", "--db", p}, nil, &out); err != nil || strings.Count(out.String(), "\n") != 2 || !strings.Contains(out.String(), `"event":"auth.login"`) {
		t.Fatalf("%v\n%s", err, out.String())
	}
	db.Close()
	for _, bad := range [][]string{{"audit", "verify"}, {"audit", "verify", "--db", p, "--file", p}, {"audit", "export", "--file", p}} {
		if err := run(bad, nil, &out); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestAuditStatsFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	l, err := audit.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []audit.Event{
		{Event: "auth.login", User: "alice", Outcome: "ok"},
		{Event: "files.entries.read", User: "alice", Outcome: "ok", Detail: "secret-detail"},
		{Event: "services.logs.read", User: "bob", Outcome: "ok"},
	} {
		if err := l.Log(e); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()
	fi, _ := os.Stat(p)
	var out bytes.Buffer
	if err := run([]string{"audit", "stats", "--file", p}, nil, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "records 4") || !strings.Contains(s, "segments 1") || !strings.Contains(s, fmt.Sprintf("bytes %d", fi.Size())) {
		t.Fatalf("%q", s)
	}
	if !strings.Contains(s, "files:") || !strings.Contains(s, "services:") {
		t.Fatalf("missing read-event families: %q", s)
	}
	for _, leak := range []string{"alice", "bob", "secret-detail"} {
		if strings.Contains(s, leak) {
			t.Fatalf("stats leaked %q: %q", leak, s)
		}
	}
}

func TestAuditStatsDatabase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s")
	_ = os.Mkdir(dir, 0o700)
	p := filepath.Join(dir, "state.db")
	db, err := state.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	l, err := audit.OpenStore(db.Audit())
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Log(audit.Event{Event: "files.entries.read", User: "alice", Outcome: "ok"})
	_ = l.Log(audit.Event{Event: "auth.login", User: "alice", Outcome: "ok"})
	db.Close()
	var out bytes.Buffer
	if err := run([]string{"audit", "stats", "--db", p}, nil, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "records") || !strings.Contains(s, "files:") {
		t.Fatalf("%q", s)
	}
	if strings.Contains(s, "alice") {
		t.Fatalf("stats leaked user: %q", s)
	}
}

func TestAuditVerifyAnchored(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	l, err := audit.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := l.Log(audit.Event{Event: "auth.login", Outcome: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()
	// audit.chain (seq 1) + 4 auth.login (seq 2..5) = 5 lines.
	raw, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	var anchorEv audit.Event
	if err := json.Unmarshal([]byte(lines[1]), &anchorEv); err != nil {
		t.Fatal(err)
	}
	anchor := fmt.Sprintf("%d:%s", anchorEv.Seq, anchorEv.Hash)
	pruned := filepath.Join(t.TempDir(), "pruned.jsonl")
	if err := os.WriteFile(pruned, []byte(strings.Join(lines[2:], "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	// Without an anchor a partial log must fail.
	if err := run([]string{"audit", "verify", "--file", pruned}, nil, &out); err == nil {
		t.Fatalf("unanchored verify of a pruned log accepted: %q", out.String())
	}
	out.Reset()
	// With the correct anchor it must pass.
	if err := run([]string{"audit", "verify", "--file", pruned, "--anchor", anchor}, nil, &out); err != nil || !strings.Contains(out.String(), "OK: chain verified") {
		t.Fatalf("%v\n%s", err, out.String())
	}
	// A wrong anchor hash must fail.
	out.Reset()
	if err := run([]string{"audit", "verify", "--file", pruned, "--anchor", fmt.Sprintf("%d:%s", anchorEv.Seq, audit.ZeroHash)}, nil, &out); err == nil {
		t.Fatalf("wrong anchor hash accepted: %q", out.String())
	}
}

func TestAuditVerifyAnchoredUnrelatedChain(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	l, err := audit.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := l.Log(audit.Event{Event: "auth.login", Outcome: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()
	// A fresh, internally valid chain must not pass with an unrelated anchor.
	var out bytes.Buffer
	if err := run([]string{"audit", "verify", "--file", p, "--anchor", fmt.Sprintf("100:%s", audit.ZeroHash)}, nil, &out); err == nil {
		t.Fatalf("unrelated anchor accepted a fresh chain: %q", out.String())
	}
}

func TestAuditStatsMalformedFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(p, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"audit", "stats", "--file", p}, nil, &out); err == nil {
		t.Fatalf("malformed log accepted: %q", out.String())
	}
}
