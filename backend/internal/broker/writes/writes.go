// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package writes is the broker's plan → confirm → execute framework for
// privileged write operations (ADR 0011, owner decision D-2 option A):
//
//   - Plan tokens are minted here, inside the broker: HMAC-SHA256 under a
//     random key generated when the broker starts and never written anywhere.
//     A token is bound to op, action, canonical target, actor, session, the
//     observed-state digest and the risk class, expires after 120 s and is
//     single-use: the broker keeps every outstanding token ID in memory and
//     deletes it on use, so a replayed token is unknown. A broker restart
//     (new key, empty set) invalidates all earlier tokens.
//   - Locks are exclusive per target domain (for example "service:foo.service").
//   - The journal is an append-only, hash-chained, fsync'ed JSON Lines file
//     (internal/audit format). The intent record is durable before anything
//     executes; the outcome record follows. Intents without an outcome found
//     at startup are reported as interrupted, never retried.
//
// Limit (documented in ADR 0011): hc-api authenticates to the broker by peer
// uid and asserts the actor. A compromised hc-api can therefore plan and apply
// any operation the broker's own policy allows, for any user name it chooses.
// Tokens prevent replay, substitution and stale-state execution; the narrow op
// set and the broker-side policy carry the real safety.
package writes

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/broker/types"
)

// TokenTTL is the plan-token lifetime (D-2).
const TokenTTL = 120 * time.Second

// StepUpWindow is how long a password re-entry authorizes disruptive ops (D-3).
const StepUpWindow = 5 * time.Minute

// maxOutstanding bounds the in-memory token set; when full, the oldest
// tokens are dropped (they simply become invalid). maxPerActor bounds one
// actor's share first, so one user requesting many reviews only invalidates
// their own older reviews, not other users'.
const (
	maxOutstanding = 256
	maxPerActor    = 16
)

// Binding is everything a plan token authorizes.
type Binding struct {
	ID          string    `json:"id"`
	Op          string    `json:"op"`
	Action      string    `json:"action"`
	Target      string    `json:"target"`
	Actor       string    `json:"actor"`
	Session     string    `json:"session"`
	StateDigest string    `json:"state"`
	Risk        string    `json:"risk"`
	Expires     time.Time `json:"exp"`
}

// Errors returned by Consume. Callers map them to protocol codes.
var (
	ErrTokenInvalid = errors.New("plan token is invalid, expired, already used or not bound to this request")
	ErrStepUp       = errors.New("a recent password re-entry is required")
	ErrBusy         = errors.New("another operation on this target is in progress")
	ErrNoJournal    = errors.New("the operation journal is not available")
)

// Framework holds the per-process key, outstanding tokens, locks and journal.
type Framework struct {
	key   []byte
	now   func() time.Time
	mu    sync.Mutex
	out   map[string]Binding // outstanding tokens by ID
	order []string           // issue order, for bounding
	locks map[string]bool

	jmu     sync.Mutex
	journal *audit.Logger
	jpath   string
}

// New creates a framework with a fresh random key. journalPath may be "" in
// tests that do not exercise the journal (writes then fail closed).
func New(journal *audit.Logger, journalPath string) (*Framework, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return &Framework{key: key, now: time.Now, out: map[string]Binding{}, locks: map[string]bool{}, journal: journal, jpath: journalPath}, nil
}

// SetClock replaces the clock (tests only).
func (f *Framework) SetClock(now func() time.Time) { f.now = now }

// Now returns the framework clock.
func (f *Framework) Now() time.Time { return f.now() }

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

const tokenPrefix = "hcp1"

func (f *Framework) sign(payload []byte) []byte {
	m := hmac.New(sha256.New, f.key)
	m.Write([]byte(tokenPrefix + "."))
	m.Write(payload)
	return m.Sum(nil)
}

// Mint records b (ID and expiry are set here) and returns its token.
func (f *Framework) Mint(b Binding) (string, Binding, error) {
	b.ID = randomID()
	b.Expires = f.now().Add(TokenTTL).UTC().Truncate(time.Second)
	payload, err := json.Marshal(b)
	if err != nil {
		return "", b, err
	}
	tok := tokenPrefix + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(f.sign(payload))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruneLocked()
	mine := 0
	for _, id := range f.order {
		if f.out[id].Actor == b.Actor {
			mine++
		}
	}
	for i := 0; mine >= maxPerActor && i < len(f.order); {
		if id := f.order[i]; f.out[id].Actor == b.Actor {
			delete(f.out, id)
			f.order = append(f.order[:i], f.order[i+1:]...)
			mine--
			continue
		}
		i++
	}
	for len(f.order) >= maxOutstanding {
		delete(f.out, f.order[0])
		f.order = f.order[1:]
	}
	f.out[b.ID] = b
	f.order = append(f.order, b.ID)
	return tok, b, nil
}

func (f *Framework) pruneLocked() {
	now := f.now()
	keep := f.order[:0]
	for _, id := range f.order {
		if b, ok := f.out[id]; ok && now.Before(b.Expires) {
			keep = append(keep, id)
		} else {
			delete(f.out, id)
		}
	}
	f.order = keep
}

// parse verifies the token's MAC and returns its payload binding.
func (f *Framework) parse(tok string) (Binding, bool) {
	var b Binding
	if len(tok) > 2048 {
		return b, false
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || parts[0] != tokenPrefix {
		return b, false
	}
	payload, err1 := base64.RawURLEncoding.DecodeString(parts[1])
	mac, err2 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || !hmac.Equal(mac, f.sign(payload)) {
		return b, false
	}
	if json.Unmarshal(payload, &b) != nil {
		return b, false
	}
	return b, true
}

// Request is what the apply call claims; it must match the token exactly.
type Request struct {
	Op, Action, Target, Actor, Session string
	StepUpAt                           int64 // Unix seconds, 0 = none
}

func eq(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func same(a, b Binding) bool {
	return a.ID == b.ID && a.Op == b.Op && a.Action == b.Action && a.Target == b.Target &&
		a.Actor == b.Actor && a.Session == b.Session && a.StateDigest == b.StateDigest &&
		a.Risk == b.Risk && a.Expires.Equal(b.Expires)
}

// Consume verifies tok against req and, if it is valid, removes it from the
// outstanding set and returns its binding. Verification, the step-up check
// and removal happen under one lock, so a token is used at most once even
// under concurrent applies. A missing step-up does not consume the token: the
// user may re-enter the password and confirm again before it expires.
func (f *Framework) Consume(tok string, req Request) (Binding, error) {
	b, ok := f.parse(tok)
	if !ok {
		return Binding{}, ErrTokenInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.out[b.ID]
	now := f.now()
	switch {
	case !ok,
		!same(stored, b), // the MAC'd payload must equal what was minted
		!now.Before(b.Expires),
		!eq(b.Op, req.Op), !eq(b.Action, req.Action), !eq(b.Target, req.Target),
		!eq(b.Actor, req.Actor), b.Session == "" || !eq(b.Session, req.Session):
		return Binding{}, ErrTokenInvalid
	}
	if b.Risk != types.RiskReversible {
		at := time.Unix(req.StepUpAt, 0)
		if req.StepUpAt <= 0 || at.After(now.Add(5*time.Second)) || now.Sub(at) > StepUpWindow {
			return Binding{}, ErrStepUp
		}
	}
	delete(f.out, b.ID)
	return b, nil
}

// Outstanding reports the number of unused, unexpired tokens (tests, info).
func (f *Framework) Outstanding() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruneLocked()
	return len(f.out)
}

// Lock takes the exclusive lock for domain. It returns a release function, or
// ErrBusy when the domain is locked.
func (f *Framework) Lock(domain string) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.locks[domain] {
		return nil, ErrBusy
	}
	f.locks[domain] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			f.mu.Lock()
			delete(f.locks, domain)
			f.mu.Unlock()
		})
	}, nil
}

// Locked reports whether domain is currently locked.
func (f *Framework) Locked(domain string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.locks[domain]
}

// Journal record kinds.
const (
	kindIntent      = "op.intent"
	kindOutcome     = "op.outcome"
	kindInterrupted = "op.interrupted"
)

// Record is the journal payload (stored as JSON in the audit Data field).
// It never contains a plan token (only the token ID) or any secret.
type Record struct {
	OpID      string          `json:"op_id"`
	Op        string          `json:"op"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	Actor     string          `json:"actor"`
	RequestID string          `json:"request_id,omitempty"`
	TokenID   string          `json:"token_id,omitempty"`
	Risk      string          `json:"risk,omitempty"`
	State     string          `json:"state,omitempty"` // state digest at intent
	Outcome   string          `json:"outcome,omitempty"`
	ExitCode  *int            `json:"exit_code,omitempty"`
	Message   string          `json:"message,omitempty"`
	Observed  json.RawMessage `json:"observed,omitempty"`
}

func (f *Framework) write(kind string, r Record, outcome string) error {
	if f.journal == nil {
		return ErrNoJournal
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f.jmu.Lock()
	defer f.jmu.Unlock()
	// Detail is a short human summary (the audit logger caps it); the full
	// record goes in Data, which the hash chain covers.
	return f.journal.Log(audit.Event{Event: kind, User: r.Actor, Outcome: outcome,
		Detail: r.Action + " " + r.Target + " " + r.OpID, Data: b})
}

// Intent durably records an operation before it executes and returns its ID.
// If the record cannot be written the operation must not run.
func (f *Framework) Intent(r Record) (string, error) {
	r.OpID = randomID()
	return r.OpID, f.write(kindIntent, r, "intent")
}

// Outcome records the observed result of an operation.
func (f *Framework) Outcome(r Record) error { return f.write(kindOutcome, r, r.Outcome) }

// Entry is one operation reconstructed from the journal.
type Entry struct {
	Intent     Record
	IntentAt   time.Time
	Final      *Record
	FinishedAt time.Time
}

// maxJournalScan bounds how much of the journal is read (newest records win).
const maxJournalScan = 32 << 20

// Read reconstructs operations from the journal file, oldest first. Records
// that do not parse are skipped (hlctl audit verify reports tampering).
func (f *Framework) Read() ([]Entry, error) {
	if f.jpath == "" {
		return nil, ErrNoJournal
	}
	fh, err := os.OpenFile(f.jpath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	if fi, err := fh.Stat(); err == nil && fi.Size() > maxJournalScan {
		if _, err := fh.Seek(fi.Size()-maxJournalScan, io.SeekStart); err != nil {
			return nil, err
		}
	}
	byID := map[string]*Entry{}
	var order []string
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var ev audit.Event
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		var r Record
		if json.Unmarshal(ev.Data, &r) != nil || r.OpID == "" {
			continue
		}
		switch ev.Event {
		case kindIntent:
			if _, dup := byID[r.OpID]; !dup {
				byID[r.OpID] = &Entry{Intent: r, IntentAt: ev.Time}
				order = append(order, r.OpID)
			}
		case kindOutcome, kindInterrupted:
			if e, ok := byID[r.OpID]; ok && e.Final == nil {
				rc := r
				e.Final, e.FinishedAt = &rc, ev.Time
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// Inspector reads a target's current state after an interruption. It returns
// the observed state as JSON and a short description.
type Inspector func(target string) (json.RawMessage, string)

// Recover finds intents without an outcome (the broker stopped between intent
// and outcome) and records each as interrupted, with the target's current
// observed state. Nothing is retried. It returns the number recovered.
func (f *Framework) Recover(inspect Inspector) (int, error) {
	entries, err := f.Read()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.Final != nil {
			continue
		}
		r := e.Intent
		r.Outcome = "interrupted"
		r.Message = "The broker stopped before the outcome of this operation was recorded. It was not retried."
		if inspect != nil {
			obs, desc := inspect(r.Target)
			r.Observed = obs
			if desc != "" {
				r.Message += " State observed at broker restart: " + desc + "."
			}
		}
		if err := f.write(kindInterrupted, r, "interrupted"); err != nil {
			return n, fmt.Errorf("recording interrupted operation %s: %w", r.OpID, err)
		}
		n++
	}
	return n, nil
}
