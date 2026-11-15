// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package audit

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSummarize(t *testing.T) {
	input := strings.Join([]string{
		// A legacy (unchained) record before the chain.
		`{"time":"2025-12-31T23:00:00Z","event":"auth.login","outcome":"ok","user":"alice"}`,
		// First segment start.
		`{"seq":1,"time":"2026-01-01T00:00:00Z","event":"audit.chain","outcome":"ok","prev":"` + ZeroHash + `","hash":"a"}`,
		// Read events across families and days.
		`{"seq":2,"time":"2026-01-01T10:00:00Z","event":"files.entries.read","outcome":"ok","prev":"a","hash":"b"}`,
		`{"seq":3,"time":"2026-01-02T10:00:00Z","event":"files.entries.read","outcome":"ok","prev":"b","hash":"c"}`,
		`{"seq":4,"time":"2026-01-02T11:00:00Z","event":"services.logs.read","outcome":"ok","prev":"c","hash":"d"}`,
		`{"seq":5,"time":"2026-01-03T09:00:00Z","event":"mergerfs.inventory.read","outcome":"ok","prev":"d","hash":"e"}`,
		// A non-read event must not be counted in ReadEvents.
		`{"seq":6,"time":"2026-01-03T10:00:00Z","event":"auth.login","outcome":"ok","prev":"e","hash":"f"}`,
	}, "\n") + "\n"

	s, err := Summarize(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if s.Records != 6 || s.Legacy != 1 || s.Segments != 1 {
		t.Fatalf("records %d legacy %d segments %d", s.Records, s.Legacy, s.Segments)
	}
	if want := time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC); !s.Oldest.Equal(want) {
		t.Fatalf("oldest = %v, want %v", s.Oldest, want)
	}
	if want := time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC); !s.Newest.Equal(want) {
		t.Fatalf("newest = %v, want %v", s.Newest, want)
	}
	files := s.ReadEvents["files"]
	if files == nil || files["2026-01-01"] != 1 || files["2026-01-02"] != 1 {
		t.Fatalf("files read events = %+v", files)
	}
	if s.ReadEvents["services"]["2026-01-02"] != 1 {
		t.Fatalf("services read events = %+v", s.ReadEvents["services"])
	}
	if s.ReadEvents["mergerfs"]["2026-01-03"] != 1 {
		t.Fatalf("mergerfs read events = %+v", s.ReadEvents["mergerfs"])
	}
	if _, ok := s.ReadEvents["auth"]; ok {
		t.Fatalf("auth.login must not be counted as a read event: %+v", s.ReadEvents)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	s, err := Summarize(strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if s.Records != 0 || s.Legacy != 0 || s.Segments != 0 || len(s.ReadEvents) != 0 {
		t.Fatalf("empty: %+v", s)
	}
}

func TestSummarizeMalformedLine(t *testing.T) {
	if _, err := Summarize(strings.NewReader("not json\n")); err == nil || !strings.Contains(err.Error(), "not a JSON record") {
		t.Fatalf("malformed line not reported: %v", err)
	}
}

func TestSummarizeOversizedLine(t *testing.T) {
	big := strings.Repeat("x", 1<<20+1) + "\n"
	if _, err := Summarize(strings.NewReader(big)); err == nil || !strings.Contains(err.Error(), "read error") {
		t.Fatalf("oversized line not reported: %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestSummarizeReadError(t *testing.T) {
	if _, err := Summarize(failingReader{}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("read error not reported: %v", err)
	}
}

func TestEventFamilyAndReadEvent(t *testing.T) {
	for event, fam := range map[string]string{
		"files.entries.read":      "files",
		"services.logs.read":      "services",
		"mergerfs.inventory.read": "mergerfs",
		"auth.login":              "auth",
		"login":                   "login",
	} {
		if got := eventFamily(event); got != fam {
			t.Errorf("eventFamily(%q) = %q, want %q", event, got, fam)
		}
	}
	for _, read := range []string{"files.entries.read", "services.logs.read", "mergerfs.inventory.read"} {
		if !isReadEvent(read) {
			t.Errorf("%q should be a read event", read)
		}
	}
	for _, notRead := range []string{"auth.login", "services.op.apply", "host.settings.apply", "audit.chain"} {
		if isReadEvent(notRead) {
			t.Errorf("%q must not be a read event", notRead)
		}
	}
}
