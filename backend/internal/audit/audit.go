// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package audit writes append-only, hash-chained JSON Lines audit records.
//
// Every record carries a sequence number, the SHA-256 hash of the previous
// record ("prev") and its own hash, and is fsync'ed before Log returns.
// Verify detects modified, reordered, inserted or deleted records. It cannot,
// on its own, detect a truncated tail or a whole-file rewrite by someone who
// can write the file: for that, the logger reports checkpoints (sequence and
// hash) to a separate store (the systemd journal, via the caller), and Verify
// compares the file against such a checkpoint. See docs/security/AUDIT-LOG.md.
// This is tamper-evident, not tamper-proof.
//
// Secrets (passwords, session IDs, CSRF tokens) must never be passed to this
// package.
package audit

import (
	"bufio"
	"bytes"
	"crypto/sha256"
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
	"unicode"
)

// ZeroHash is the "prev" value of the first record of a chain segment.
var ZeroHash = strings.Repeat("0", 64)

// Event is one audit record. Seq, Prev and Hash are set by the Logger.
type Event struct {
	Seq     uint64    `json:"seq,omitempty"`
	Time    time.Time `json:"time"`
	Event   string    `json:"event"`
	User    string    `json:"user,omitempty"`
	Remote  string    `json:"remote,omitempty"`
	Outcome string    `json:"outcome"`
	Detail  string    `json:"detail,omitempty"`
	// Data is optional structured detail (compact JSON, at most MaxData
	// bytes), used by the broker operation journal. It is covered by the
	// hash; when empty it is omitted, so older chains verify unchanged. The
	// SQLite store does not hold it and refuses events that carry it.
	Data json.RawMessage `json:"data,omitempty"`
	Prev string          `json:"prev,omitempty"`
	Hash string          `json:"hash,omitempty"`
}

// digest is the hash of everything except Hash itself.
func digest(e Event) string {
	e.Hash = ""
	b, _ := json.Marshal(e)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Logger serialises events to a writer and chains them.
type Logger struct {
	mu   sync.Mutex
	w    io.Writer
	f    *os.File
	st   Store
	seq  uint64
	head string
	// Checkpoint, if set, is called with the chain head after opening and
	// every CheckpointEvery records, so the caller can record it elsewhere.
	Checkpoint      func(seq uint64, hash string)
	CheckpointEvery uint64
}

// Open opens (or creates with mode 0600) an append-only audit file and
// continues its chain. A symlink or non-regular file is refused.
func Open(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	l := &Logger{w: f, f: f, CheckpointEvery: 100}
	if fi.Size() > 0 {
		// A torn final write (crash) leaves no newline; start on a fresh line
		// so the torn record stays visible to Verify instead of absorbing ours.
		var lastByte [1]byte
		if _, err := f.ReadAt(lastByte[:], fi.Size()-1); err == nil && lastByte[0] != '\n' {
			if _, err := f.Write([]byte{'\n'}); err != nil {
				f.Close()
				return nil, err
			}
		}
	}
	last, err := lastLine(f, fi.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	detail := "audit log opened"
	switch {
	case len(last) == 0:
		detail = "new chain"
	default:
		var e Event
		if json.Unmarshal(last, &e) == nil && e.Seq > 0 && e.Hash != "" && digest(e) == e.Hash {
			l.seq, l.head = e.Seq, e.Hash
		} else if json.Unmarshal(last, &e) == nil && e.Seq == 0 {
			detail = "new chain; earlier records are unchained (written before chaining existed)"
		} else {
			detail = "new chain; the previous last record is unreadable or does not verify (crash or tampering)"
		}
	}
	if l.seq == 0 {
		l.head = ZeroHash
		if err := l.Log(Event{Event: "audit.chain", Outcome: "ok", Detail: detail}); err != nil {
			f.Close()
			return nil, err
		}
	} else if err := l.Log(Event{Event: "audit.open", Outcome: "ok", Detail: detail}); err != nil {
		f.Close()
		return nil, err
	}
	return l, nil
}

// lastLine returns the last non-empty line of f (reads at most 64 KiB).
func lastLine(f *os.File, size int64) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}
	n := int64(64 << 10)
	if size < n {
		n = size
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, size-n); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	buf = bytes.TrimRight(buf, "\n")
	if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
		buf = buf[i+1:]
	}
	return buf, nil
}

// Store is a durable, ordered record store (the hc-api state database).
// Append must be durable when it returns.
type Store interface {
	Last() (*Event, error) // the last record, or nil when empty
	Append(e Event) error
}

// OpenStore continues (or starts) the chain held in a Store. As with Open, a
// last record that does not verify starts a new segment instead of extending
// a chain that may have been tampered with.
func OpenStore(st Store) (*Logger, error) {
	l := &Logger{st: st, CheckpointEvery: 100, head: ZeroHash}
	last, err := st.Last()
	if err != nil {
		return nil, err
	}
	detail := "new chain"
	if last != nil {
		if last.Seq > 0 && last.Hash != "" && digest(*last) == last.Hash {
			l.seq, l.head = last.Seq, last.Hash
		} else {
			detail = "new chain; the previous last record does not verify (crash or tampering)"
		}
	}
	if l.seq == 0 {
		if err := l.Log(Event{Event: "audit.chain", Outcome: "ok", Detail: detail}); err != nil {
			return nil, err
		}
	} else if err := l.Log(Event{Event: "audit.open", Outcome: "ok", Detail: "audit log opened"}); err != nil {
		return nil, err
	}
	return l, nil
}

// New wraps an arbitrary writer (tests); the chain starts at sequence 1.
func New(w io.Writer) *Logger { return &Logger{w: w, head: ZeroHash} }

// Head returns the current chain head.
func (l *Logger) Head() (uint64, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq, l.head
}

// sanitize strips control characters so attacker-controlled values such as a
// submitted username cannot forge additional log lines, and bounds length.
func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if len(s) > 128 {
		s = s[:128]
	}
	return s
}

// MaxData bounds Event.Data.
const MaxData = 8 << 10

// ErrBadData is returned for Data that is not compact, valid JSON within MaxData.
var ErrBadData = errors.New("audit data must be compact JSON of at most 8 KiB")

// Log writes an event, durably (fsync) when backed by a file. Errors are
// returned so callers can fail closed when auditing is mandatory.
func (l *Logger) Log(e Event) error {
	if len(e.Data) > 0 {
		var c bytes.Buffer
		if len(e.Data) > MaxData || json.Compact(&c, e.Data) != nil || c.Len() != len(e.Data) {
			return ErrBadData
		}
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	e.Time = e.Time.UTC()
	e.Event, e.Outcome = sanitize(e.Event), sanitize(e.Outcome)
	e.User, e.Remote, e.Detail = sanitize(e.User), sanitize(e.Remote), sanitize(e.Detail)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.head == "" {
		l.head = ZeroHash
	}
	e.Seq, e.Prev = l.seq+1, l.head
	e.Hash = digest(e)
	// A failed or partial write (for example ENOSPC) must not leave a
	// fragment that the next record is appended to: cut the file back to its
	// size before this record. The record then counts as not written.
	before := int64(-1)
	if l.f != nil {
		if fi, err := l.f.Stat(); err == nil {
			before = fi.Size()
		}
	}
	undo := func() {
		if before >= 0 {
			_ = l.f.Truncate(before)
		}
	}
	if l.st != nil {
		if err := l.st.Append(e); err != nil {
			return err
		}
	} else if b, err := json.Marshal(e); err != nil {
		return err
	} else if _, err := l.w.Write(append(b, '\n')); err != nil {
		undo()
		return err
	}
	if l.f != nil {
		if err := l.f.Sync(); err != nil {
			undo()
			return err
		}
	}
	l.seq, l.head = e.Seq, e.Hash
	if l.Checkpoint != nil && (e.Event == "audit.chain" || e.Event == "audit.open" || (l.CheckpointEvery > 0 && e.Seq%l.CheckpointEvery == 0)) {
		l.Checkpoint(l.seq, l.head)
	}
	return nil
}

// EmitCheckpoint reports the current head to the Checkpoint callback.
func (l *Logger) EmitCheckpoint() {
	l.mu.Lock()
	seq, head, cb := l.seq, l.head, l.Checkpoint
	l.mu.Unlock()
	if cb != nil && seq > 0 {
		cb(seq, head)
	}
}

// Close reports a final checkpoint and closes the underlying file if any.
func (l *Logger) Close() error {
	l.EmitCheckpoint()
	if l.f != nil {
		return l.f.Close()
	}
	if c, ok := l.w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// Report is the result of Verify.
type Report struct {
	Records  int      `json:"records"`  // chained records
	Legacy   int      `json:"legacy"`   // unchained records before the first chain
	Segments int      `json:"segments"` // chain segments (each starts with audit.chain)
	HeadSeq  uint64   `json:"head_seq"`
	HeadHash string   `json:"head_hash"`
	Problems []string `json:"problems,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// OK reports whether no problems were found.
func (r Report) OK() bool { return len(r.Problems) == 0 }

// Checkpoint is an externally recorded chain position.
type Checkpoint struct {
	Seq  uint64
	Hash string
}

// ParseCheckpoint parses "SEQ:HASH".
func ParseCheckpoint(s string) (Checkpoint, error) {
	a, b, ok := strings.Cut(strings.TrimSpace(s), ":")
	var c Checkpoint
	if !ok {
		return c, errors.New("checkpoint must be SEQ:HASH")
	}
	if _, err := fmt.Sscanf(a, "%d", &c.Seq); err != nil || c.Seq == 0 {
		return c, errors.New("checkpoint sequence must be a positive integer")
	}
	if len(b) != 64 {
		return c, errors.New("checkpoint hash must be 64 hex characters")
	}
	if _, err := hex.DecodeString(b); err != nil {
		return c, errors.New("checkpoint hash must be hex")
	}
	c.Hash = strings.ToLower(b)
	return c, nil
}

// Verify checks a log. With checkpoints, it also detects a truncated tail or a
// rollback to an older copy: every checkpoint must be present in the last
// segment with the same hash.
func Verify(r io.Reader, cps ...Checkpoint) Report {
	var rep Report
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var seq uint64
	head := ""
	inChain := false
	seen := map[uint64]string{}
	line := 0
	for sc.Scan() {
		line++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(raw, &e); err != nil {
			rep.Problems = append(rep.Problems, fmt.Sprintf("line %d: not a JSON record", line))
			continue
		}
		if e.Seq == 0 {
			if inChain {
				rep.Problems = append(rep.Problems, fmt.Sprintf("line %d: unchained record after the chain started", line))
			} else {
				rep.Legacy++
			}
			continue
		}
		if e.Hash == "" || digest(e) != e.Hash {
			rep.Problems = append(rep.Problems, fmt.Sprintf("line %d (seq %d): record hash does not match its content (modified)", line, e.Seq))
		}
		if e.Event == "audit.chain" && e.Seq == 1 && e.Prev == ZeroHash {
			if inChain {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("line %d: new chain segment started (%s)", line, e.Detail))
			}
			rep.Segments++
			seen = map[uint64]string{}
		} else {
			if !inChain {
				rep.Problems = append(rep.Problems, fmt.Sprintf("line %d (seq %d): chain does not start with an audit.chain record", line, e.Seq))
			} else {
				if e.Seq != seq+1 {
					rep.Problems = append(rep.Problems, fmt.Sprintf("line %d: sequence %d follows %d (records missing, inserted or reordered)", line, e.Seq, seq))
				}
				if e.Prev != head {
					rep.Problems = append(rep.Problems, fmt.Sprintf("line %d (seq %d): previous-hash link broken", line, e.Seq))
				}
			}
		}
		inChain = true
		seq, head = e.Seq, e.Hash
		seen[e.Seq] = e.Hash
		rep.Records++
	}
	if err := sc.Err(); err != nil {
		rep.Problems = append(rep.Problems, "read error: "+err.Error())
	}
	rep.HeadSeq, rep.HeadHash = seq, head
	for _, c := range cps {
		h, ok := seen[c.Seq]
		switch {
		case !ok && c.Seq > seq:
			rep.Problems = append(rep.Problems, fmt.Sprintf("checkpoint seq %d is beyond the log head %d: records were truncated or the file was rolled back", c.Seq, seq))
		case !ok:
			rep.Problems = append(rep.Problems, fmt.Sprintf("checkpoint seq %d is not in the current chain segment", c.Seq))
		case h != c.Hash:
			rep.Problems = append(rep.Problems, fmt.Sprintf("checkpoint seq %d: hash differs (the log was rewritten)", c.Seq))
		}
	}
	if rep.Records == 0 && rep.Legacy == 0 {
		rep.Warnings = append(rep.Warnings, "log is empty")
	}
	return rep
}
