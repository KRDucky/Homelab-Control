// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package writes

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
)

func newFW(t *testing.T) (*Framework, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "operations.jsonl")
	l, err := audit.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	f, err := New(l, p)
	if err != nil {
		t.Fatal(err)
	}
	return f, p
}

var base = Binding{Op: "services.unit", Action: "stop", Target: "demo.service", Actor: "alice",
	Session: "sess-a", StateDigest: "digest-1", Risk: "disruptive"}

func req(b Binding, stepUp time.Time) Request {
	r := Request{Op: b.Op, Action: b.Action, Target: b.Target, Actor: b.Actor, Session: b.Session}
	if !stepUp.IsZero() {
		r.StepUpAt = stepUp.Unix()
	}
	return r
}

func TestTokenHappyPathAndReplay(t *testing.T) {
	f, _ := newFW(t)
	tok, b, err := f.Mint(base)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID == "" || b.Expires.Sub(time.Now()) > TokenTTL || b.Expires.Sub(time.Now()) < TokenTTL-2*time.Second {
		t.Fatalf("binding %+v", b)
	}
	got, err := f.Consume(tok, req(base, time.Now()))
	if err != nil || got.StateDigest != "digest-1" {
		t.Fatalf("consume: %v %+v", err, got)
	}
	if _, err := f.Consume(tok, req(base, time.Now())); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("replay accepted: %v", err)
	}
	if f.Outstanding() != 0 {
		t.Fatal("token still outstanding after use")
	}
}

func TestTokenExpires(t *testing.T) {
	f, _ := newFW(t)
	now := time.Now()
	f.SetClock(func() time.Time { return now })
	tok, _, _ := f.Mint(base)
	now = now.Add(TokenTTL + time.Second)
	if _, err := f.Consume(tok, req(base, now)); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expired token accepted: %v", err)
	}
}

func TestTokenBoundToEveryField(t *testing.T) {
	f, _ := newFW(t)
	tok, _, _ := f.Mint(base)
	mut := map[string]func(*Request){
		"actor":   func(r *Request) { r.Actor = "mallory" },
		"session": func(r *Request) { r.Session = "sess-b" },
		"empty":   func(r *Request) { r.Session = "" },
		"op":      func(r *Request) { r.Op = "services.other" },
		"action":  func(r *Request) { r.Action = "start" },
		"target":  func(r *Request) { r.Target = "sshd.service" },
	}
	for name, m := range mut {
		r := req(base, time.Now())
		m(&r)
		if _, err := f.Consume(tok, r); !errors.Is(err, ErrTokenInvalid) {
			t.Errorf("%s substitution accepted: %v", name, err)
		}
	}
	// None of the rejected attempts used up the legitimate token.
	if _, err := f.Consume(tok, req(base, time.Now())); err != nil {
		t.Fatalf("legitimate use after rejected substitutions: %v", err)
	}
}

func TestTamperedAndForeignTokens(t *testing.T) {
	f, _ := newFW(t)
	other, _ := newFW(t) // "previous boot": different key, different set
	tok, _, _ := f.Mint(base)
	parts := strings.Split(tok, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var b Binding
	_ = json.Unmarshal(payload, &b)
	b.Risk = "reversible" // try to dodge the step-up
	forged, _ := json.Marshal(b)
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString(forged) + "." + parts[2]

	otherTok, _, _ := other.Mint(base)
	for name, tk := range map[string]string{
		"tampered payload": tampered, "other boot": otherTok, "empty": "", "garbage": "hcp1.x.y",
		"wrong prefix": "hcp2." + parts[1] + "." + parts[2], "two parts": parts[0] + "." + parts[1],
		"oversize": strings.Repeat("A", 4096),
	} {
		if _, err := f.Consume(tk, req(base, time.Now())); !errors.Is(err, ErrTokenInvalid) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}

func TestStepUpRequiredForDisruptive(t *testing.T) {
	f, _ := newFW(t)
	tok, _, _ := f.Mint(base)
	now := time.Now()
	for name, at := range map[string]time.Time{
		"missing": {}, "stale": now.Add(-StepUpWindow - time.Second), "future": now.Add(time.Minute),
	} {
		if _, err := f.Consume(tok, req(base, at)); !errors.Is(err, ErrStepUp) {
			t.Errorf("%s step-up: %v", name, err)
		}
	}
	// A failed step-up check does not consume the token.
	if _, err := f.Consume(tok, req(base, now.Add(-time.Minute))); err != nil {
		t.Fatalf("fresh step-up: %v", err)
	}
	rev := base
	rev.Risk = "reversible"
	tok2, _, _ := f.Mint(rev)
	if _, err := f.Consume(tok2, req(rev, time.Time{})); err != nil {
		t.Fatalf("reversible op should not need step-up: %v", err)
	}
}

func TestConcurrentConsumeIsSingleUse(t *testing.T) {
	f, _ := newFW(t)
	tok, _, _ := f.Mint(base)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.Consume(tok, req(base, time.Now())); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("token used %d times", ok.Load())
	}
}

func TestOutstandingBounded(t *testing.T) {
	f, _ := newFW(t)
	first, _, _ := f.Mint(base)
	for i := 0; i < maxOutstanding+50; i++ {
		_, _, _ = f.Mint(base)
	}
	if n := f.Outstanding(); n > maxOutstanding {
		t.Fatalf("%d outstanding", n)
	}
	if _, err := f.Consume(first, req(base, time.Now())); !errors.Is(err, ErrTokenInvalid) {
		t.Fatal("evicted token still valid")
	}
}

func TestLocks(t *testing.T) {
	f, _ := newFW(t)
	rel, err := f.Lock("service:a.service")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Lock("service:a.service"); !errors.Is(err, ErrBusy) {
		t.Fatalf("double lock: %v", err)
	}
	if _, err := f.Lock("service:b.service"); err != nil {
		t.Fatalf("independent domain: %v", err)
	}
	rel()
	rel() // idempotent
	if f.Locked("service:a.service") {
		t.Fatal("still locked")
	}
	rel2, err := f.Lock("service:a.service")
	if err != nil {
		t.Fatal(err)
	}
	// Released by a deferred call even when the holder panics.
	func() {
		defer func() { _ = recover() }()
		defer rel2()
		panic("handler panic")
	}()
	if f.Locked("service:a.service") {
		t.Fatal("lock leaked after panic")
	}
}

func TestJournalIntentOutcomeAndInterruptedRecovery(t *testing.T) {
	p := filepath.Join(t.TempDir(), "operations.jsonl")
	l, _ := audit.Open(p)
	f, _ := New(l, p)
	tok, b, _ := f.Mint(base)
	// Completed operation.
	id1, err := f.Intent(Record{Op: b.Op, Action: "stop", Target: "a.service", Actor: "alice", TokenID: b.ID, Risk: "disruptive"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Outcome(Record{OpID: id1, Op: b.Op, Action: "stop", Target: "a.service", Actor: "alice", Outcome: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	// Broker "killed" between intent and outcome.
	id2, _ := f.Intent(Record{Op: b.Op, Action: "restart", Target: "b.service", Actor: "alice", Risk: "disruptive"})
	l.Close()

	// Restart: new process, new key, same file.
	l2, err := audit.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	f2, _ := New(l2, p)
	inspected := ""
	n, err := f2.Recover(func(target string) (json.RawMessage, string) {
		inspected = target
		return json.RawMessage(`{"id":"b.service","load_state":"loaded","active_state":"failed","sub_state":"failed"}`), "failed (failed)"
	})
	if err != nil || n != 1 || inspected != "b.service" {
		t.Fatalf("recover: n=%d err=%v inspected=%q", n, err, inspected)
	}
	if n, _ := f2.Recover(nil); n != 0 {
		t.Fatalf("second recovery found %d", n)
	}
	entries, err := f2.Read()
	if err != nil || len(entries) != 2 {
		t.Fatalf("read: %v %d", err, len(entries))
	}
	if entries[0].Intent.OpID != id1 || entries[0].Final.Outcome != "succeeded" {
		t.Errorf("entry 0: %+v", entries[0])
	}
	if entries[1].Intent.OpID != id2 || entries[1].Final == nil || entries[1].Final.Outcome != "interrupted" ||
		!strings.Contains(entries[1].Final.Message, "not retried") || !strings.Contains(entries[1].Final.Message, "failed (failed)") {
		t.Errorf("entry 1: %+v", entries[1].Final)
	}
	// Tokens from before the restart are unknown to the new process.
	if _, err := f2.Consume(tok, req(base, time.Now())); !errors.Is(err, ErrTokenInvalid) {
		t.Fatal("token survived a broker restart")
	}
	// The journal is a valid hash chain and never contains a token.
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), tok) || strings.Contains(string(raw), "hcp1.") {
		t.Fatal("journal contains a plan token")
	}
	fh, _ := os.Open(p)
	defer fh.Close()
	if rep := audit.Verify(fh); !rep.OK() {
		t.Fatalf("journal chain: %+v", rep.Problems)
	}
}

func TestIntentFailsClosedWithoutJournal(t *testing.T) {
	f, err := New(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Intent(Record{OpID: "x"}); !errors.Is(err, ErrNoJournal) {
		t.Fatalf("intent without journal: %v", err)
	}
	if _, err := f.Read(); !errors.Is(err, ErrNoJournal) {
		t.Fatalf("read without journal: %v", err)
	}
}

func TestJournalRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	_ = os.WriteFile(target, nil, 0o600)
	link := filepath.Join(dir, "operations.jsonl")
	_ = os.Symlink(target, link)
	if _, err := audit.Open(link); err == nil {
		t.Fatal("journal opened through a symlink")
	}
	f, _ := New(nil, link)
	if _, err := f.Read(); err == nil {
		t.Fatal("journal read through a symlink")
	}
}

// One actor requesting many reviews evicts only their own older tokens.
func TestPerActorBoundProtectsOtherUsers(t *testing.T) {
	f, _ := newFW(t)
	other := base
	other.Actor = "bob"
	bobTok, _, _ := f.Mint(other)
	for i := 0; i < maxOutstanding; i++ {
		_, _, _ = f.Mint(base) // alice
	}
	if n := f.Outstanding(); n != maxPerActor+1 {
		t.Fatalf("%d outstanding, want %d", n, maxPerActor+1)
	}
	if _, err := f.Consume(bobTok, req(other, time.Now())); err != nil {
		t.Fatalf("another user's review was evicted: %v", err)
	}
}
