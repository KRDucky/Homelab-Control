// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package audit writes append-only JSON Lines audit records.
//
// M0 records authentication events only. Secrets (passwords, session IDs,
// CSRF tokens) must never be passed to this package.
package audit

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Event is one audit record.
type Event struct {
	Time    time.Time `json:"time"`
	Event   string    `json:"event"`
	User    string    `json:"user,omitempty"`
	Remote  string    `json:"remote,omitempty"`
	Outcome string    `json:"outcome"`
	Detail  string    `json:"detail,omitempty"`
}

// Logger serialises events to a writer.
type Logger struct {
	mu sync.Mutex
	w  io.Writer
	c  io.Closer
}

// Open opens (or creates with mode 0600) an append-only audit file.
func Open(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	return &Logger{w: f, c: f}, nil
}

// New wraps an arbitrary writer (used in tests).
func New(w io.Writer) *Logger { return &Logger{w: w} }

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

// Log writes an event. Errors are returned so callers can fail closed when
// auditing is mandatory.
func (l *Logger) Log(e Event) error {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	e.User, e.Remote, e.Detail = sanitize(e.User), sanitize(e.Remote), sanitize(e.Detail)
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(append(b, '\n'))
	return err
}

// Close closes the underlying file if any.
func (l *Logger) Close() error {
	if l.c != nil {
		return l.c.Close()
	}
	return nil
}
