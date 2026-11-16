// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package audit

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"
	"time"
)

// Golden vectors for the record digest and the JSON Lines export format.
// backend/audit-retention (Rust) re-implements digest() and the export
// encoding and must reproduce these bytes exactly; its tests read the same
// file. Regenerate with: go test ./internal/audit -run TestDigestVectors -update-digest-vectors
var updateDigestVectors = flag.Bool("update-digest-vectors", false, "rewrite testdata/digest-vectors.json")

const digestVectorsFile = "testdata/digest-vectors.json"

type digestVector struct {
	Name    string          `json:"name"`
	Seq     uint64          `json:"seq"`
	Time    string          `json:"time"` // as stored in state.db: Time.UTC().Format(time.RFC3339Nano)
	Event   string          `json:"event"`
	User    string          `json:"user"`
	Remote  string          `json:"remote"`
	Outcome string          `json:"outcome"`
	Detail  string          `json:"detail"`
	Data    json.RawMessage `json:"data,omitempty"`
	Prev    string          `json:"prev"`
	Hash    string          `json:"hash"` // digest() of the record
	Line    string          `json:"line"` // json.Marshal of the record including hash (one export line, no newline)
}

func digestVectorInputs() []digestVector {
	t := func(s string) string { return s }
	return []digestVector{
		{Name: "segment start", Seq: 1, Time: t("2026-01-02T03:04:05Z"), Event: "audit.chain", Outcome: "ok", Detail: "new chain", Prev: ZeroHash},
		{Name: "fraction one digit", Seq: 2, Time: t("2026-01-02T03:04:05.1Z"), Event: "auth.login", User: "admin", Remote: "192.0.2.10", Outcome: "success"},
		{Name: "fraction nanoseconds", Seq: 3, Time: t("2026-12-31T23:59:59.123456789Z"), Event: "storage.inventory.read", User: "admin", Remote: "2001:db8::1", Outcome: "collected", Detail: "nodes=12"},
		{Name: "html characters", Seq: 4, Time: t("2026-06-01T00:00:00.5Z"), Event: "csrf.origin_rejected", Remote: "198.51.100.7", Outcome: "denied", Detail: "https://evil.example/?a=<b>&c='d'"},
		{Name: "quotes backslash", Seq: 5, Time: t("2026-06-01T00:00:01Z"), Event: "services.logs.read", User: "op", Outcome: "returned", Detail: `say "hi" \ path\to`},
		{Name: "controls and del", Seq: 6, Time: t("2026-06-01T00:00:02Z"), Event: "x.read", Outcome: "ok", Detail: "a\x01b\bc\fd\ne\rf\tg\x1fh\x7fi"},
		{Name: "unicode separators", Seq: 7, Time: t("2026-06-01T00:00:03Z"), Event: "x.read", Outcome: "ok", Detail: "line para end"},
		{Name: "non-ascii", Seq: 8, Time: t("2026-06-01T00:00:04Z"), Event: "x.read", User: "jörg", Outcome: "ok", Detail: "Büro ✓ 😀 �"},
		{Name: "empty optional fields", Seq: 9, Time: t("2026-06-01T00:00:05Z"), Event: "audit.open", Outcome: "ok"},
		{Name: "structured data", Seq: 10, Time: t("2026-06-01T00:00:06Z"), Event: "retention.prune", Outcome: "done", Data: json.RawMessage(`{"count":2,"from_id":1,"last_hash":"` + ZeroHash + `","to_id":2}`)},
		{Name: "data compacted and html-escaped", Seq: 11, Time: t("2026-06-01T00:00:06.75Z"), Event: "op.intent", Outcome: "ok", Data: json.RawMessage("{ \"note\" : \"a<b>&c\u2028\", \"n\": [1, 2] }")},
		{Name: "seq zero legacy shape", Seq: 0, Time: t("2026-06-01T00:00:07Z"), Event: "legacy", Outcome: "ok"},
	}
}

func buildDigestVectors(t *testing.T) []byte {
	t.Helper()
	vs := digestVectorInputs()
	prev := ""
	for i := range vs {
		v := &vs[i]
		tm, err := time.Parse(time.RFC3339Nano, v.Time)
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		if got := tm.UTC().Format(time.RFC3339Nano); got != v.Time {
			t.Fatalf("%s: vector time %q is not in state.db form %q", v.Name, v.Time, got)
		}
		if v.Prev == "" && v.Seq > 1 {
			v.Prev = prev
		}
		e := Event{Seq: v.Seq, Time: tm, Event: v.Event, User: v.User, Remote: v.Remote, Outcome: v.Outcome, Detail: v.Detail, Data: v.Data, Prev: v.Prev}
		e.Hash = digest(e)
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		v.Hash, v.Line = e.Hash, string(line)
		prev = e.Hash
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(vs); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDigestVectors(t *testing.T) {
	want := buildDigestVectors(t)
	if *updateDigestVectors {
		if err := os.WriteFile(digestVectorsFile, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(digestVectorsFile)
	if err != nil {
		t.Fatalf("%v (regenerate with -update-digest-vectors)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale: the Go digest or export encoding changed; regenerate it and update backend/audit-retention to match", digestVectorsFile)
	}
	// Every exported line must verify with the real verifier.
	var vs []digestVector
	if err := json.Unmarshal(got, &vs); err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		var e Event
		if err := json.Unmarshal([]byte(v.Line), &e); err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		if digest(e) != v.Hash {
			t.Fatalf("%s: exported line does not round-trip to its digest", v.Name)
		}
	}
}
