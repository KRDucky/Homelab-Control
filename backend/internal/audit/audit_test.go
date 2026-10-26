// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogInjectionIsNeutralised(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	if err := l.Log(Event{Event: "auth.login", User: "evil\n{\"event\":\"forged\"}", Outcome: "failure"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("newline injection produced %d lines", len(lines))
	}
	var e Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil || e.Event != "auth.login" {
		t.Fatalf("%v %+v", err, e)
	}
}

func TestFileCreatedPrivate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Log(Event{Event: "x", Outcome: "ok"})
	_ = l.Close()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("audit log mode %#o", fi.Mode().Perm())
	}
}

func writeN(t *testing.T, p string, n int) *Logger {
	t.Helper()
	l, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := l.Log(Event{Event: "auth.login", User: "u", Outcome: "success", Detail: strings.Repeat("x", i%7)}); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func readLines(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func writeLines(t *testing.T, p string, lines []string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func verifyFile(t *testing.T, p string, cps ...Checkpoint) Report {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return Verify(f, cps...)
}

func TestChainVerifiesAndContinuesAcrossReopen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	l := writeN(t, p, 5)
	_ = l.Close()
	l = writeN(t, p, 3)
	seq, head := l.Head()
	_ = l.Close()
	rep := verifyFile(t, p, Checkpoint{Seq: seq, Hash: head})
	if !rep.OK() || rep.Segments != 1 || rep.Records != 10 || rep.HeadSeq != 10 {
		t.Fatalf("%+v", rep)
	}
}

func TestDetectsModificationReorderDeletionInsertion(t *testing.T) {
	cases := map[string]func([]string) []string{
		"modify": func(l []string) []string {
			l[3] = strings.Replace(l[3], `"user":"u"`, `"user":"mallory"`, 1)
			return l
		},
		"reorder": func(l []string) []string { l[2], l[3] = l[3], l[2]; return l },
		"delete":  func(l []string) []string { return append(l[:2:2], l[3:]...) },
		"insert": func(l []string) []string {
			return append(l[:2:2], append([]string{`{"time":"2026-01-01T00:00:00Z","event":"forged","outcome":"ok"}`}, l[2:]...)...)
		},
		"garbage": func(l []string) []string { l[4] = "not json"; return l },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "a.jsonl")
			_ = writeN(t, p, 6).Close()
			writeLines(t, p, mut(readLines(t, p)))
			if rep := verifyFile(t, p); rep.OK() {
				t.Fatalf("%s not detected: %+v", name, rep)
			}
		})
	}
}

func TestTruncationAndRollbackNeedCheckpoint(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	l := writeN(t, p, 6)
	seq, head := l.Head()
	_ = l.Close()
	lines := readLines(t, p)
	writeLines(t, p, lines[:len(lines)-2]) // drop the last two records
	if rep := verifyFile(t, p); !rep.OK() {
		t.Fatalf("a clean truncation is undetectable without a checkpoint, but got %+v", rep)
	}
	rep := verifyFile(t, p, Checkpoint{Seq: seq, Hash: head})
	if rep.OK() || !strings.Contains(strings.Join(rep.Problems, " "), "truncated or the file was rolled back") {
		t.Fatalf("truncation not detected with checkpoint: %+v", rep)
	}
	// Rewritten history with a re-computed chain: detected by the checkpoint hash.
	q := filepath.Join(t.TempDir(), "b.jsonl")
	_ = writeN(t, q, 7).Close()
	if rep := verifyFile(t, q, Checkpoint{Seq: seq, Hash: head}); rep.OK() {
		t.Fatalf("rewritten chain not detected: %+v", rep)
	}
}

func TestOpenRefusesSymlinkAndStartsNewSegmentAfterBadTail(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	_ = os.WriteFile(target, nil, 0o600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(link); err == nil {
		t.Fatal("symlink accepted")
	}
	p := filepath.Join(dir, "a.jsonl")
	_ = writeN(t, p, 2).Close()
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"seq":3,"time":"2026`) // torn write
	f.Close()
	_ = writeN(t, p, 1).Close()
	rep := verifyFile(t, p)
	if rep.Segments != 2 || len(rep.Warnings) == 0 || rep.OK() {
		t.Fatalf("expected a reported torn record and a new segment: %+v", rep)
	}
}

func TestLegacyRecordsBeforeChain(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	writeLines(t, p, []string{`{"time":"2026-01-01T00:00:00Z","event":"auth.login","outcome":"success"}`})
	_ = writeN(t, p, 2).Close()
	rep := verifyFile(t, p)
	if !rep.OK() || rep.Legacy != 1 || rep.Records != 3 {
		t.Fatalf("%+v", rep)
	}
}

func TestCheckpointCallbackAndParse(t *testing.T) {
	var got []uint64
	l := New(&bytes.Buffer{})
	l.CheckpointEvery = 3
	l.Checkpoint = func(seq uint64, _ string) { got = append(got, seq) }
	for i := 0; i < 7; i++ {
		_ = l.Log(Event{Event: "x", Outcome: "ok"})
	}
	if len(got) != 2 || got[0] != 3 || got[1] != 6 {
		t.Fatalf("checkpoints %v", got)
	}
	if _, err := ParseCheckpoint("12:" + ZeroHash); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "0:" + ZeroHash, "x:" + ZeroHash, "3:abc", "3:" + strings.Repeat("g", 64)} {
		if _, err := ParseCheckpoint(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestStructuredDataIsChainedAndValidated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ops.jsonl")
	l, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Log(Event{Event: "op.intent", Outcome: "intent", Data: json.RawMessage(`{"op_id":"1","msg":"<a>"}`)}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{"a":`, `{ "a": 1 }`, "[" + strings.Repeat(`"x",`, MaxData) + `"x"]`} {
		if err := l.Log(Event{Event: "op.intent", Data: json.RawMessage(bad)}); !errors.Is(err, ErrBadData) {
			t.Errorf("data %.20q accepted: %v", bad, err)
		}
	}
	l.Close()
	raw, _ := os.ReadFile(p)
	if rep := Verify(bytes.NewReader(raw)); !rep.OK() {
		t.Fatalf("verify: %+v", rep.Problems)
	}
	tampered := strings.Replace(string(raw), `"op_id":"1"`, `"op_id":"2"`, 1)
	if rep := Verify(strings.NewReader(tampered)); rep.OK() {
		t.Fatal("tampered data not detected")
	}
}
