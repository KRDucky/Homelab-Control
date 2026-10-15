package audit

import (
	"bytes"
	"encoding/json"
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
