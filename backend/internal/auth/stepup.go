// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"sync"
	"time"
)

// StepUpWindow is how long a password re-entry authorizes disruptive
// operations for one session (owner decision D-3).
const StepUpWindow = 5 * time.Minute

// StepUps records password re-entries per session (D-3). Entries are keyed by
// session key (SHA-256 of the session ID) and bound to the username, so one
// session's re-entry never authorizes another session, and a lookup with a
// different user fails. The table is in memory only: an hc-api restart clears
// every step-up (the safe direction). A revoked or expired session cannot
// make requests, and Clear is also called on logout and revocation.
type StepUps struct {
	mu  sync.Mutex
	m   map[string]stepUp
	now func() time.Time
}

type stepUp struct {
	user string
	at   time.Time
}

// NewStepUps returns an empty table.
func NewStepUps() *StepUps { return &StepUps{m: map[string]stepUp{}, now: time.Now} }

// SetClock replaces the clock (tests only).
func (s *StepUps) SetClock(now func() time.Time) { s.now = now }

// Record notes a successful re-entry for the session and returns its expiry.
func (s *StepUps) Record(sessionKey, user string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.pruneLocked(now)
	s.m[sessionKey] = stepUp{user: user, at: now}
	return now.Add(StepUpWindow)
}

// At returns the time of the session's last valid re-entry, if it is still
// within the window and belongs to user.
func (s *StepUps) At(sessionKey, user string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[sessionKey]
	now := s.now()
	if !ok || e.user != user || now.Sub(e.at) > StepUpWindow || e.at.After(now) {
		if ok && now.Sub(e.at) > StepUpWindow {
			delete(s.m, sessionKey)
		}
		return time.Time{}, false
	}
	return e.at, true
}

// Clear removes a session's step-up (logout, revocation).
func (s *StepUps) Clear(sessionKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, sessionKey)
}

// ClearUser removes every step-up of a user (account disabled).
func (s *StepUps) ClearUser(user string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.m {
		if e.user == user {
			delete(s.m, k)
		}
	}
}

func (s *StepUps) pruneLocked(now time.Time) {
	if len(s.m) < 4096 {
		return
	}
	for k, e := range s.m {
		if now.Sub(e.at) > StepUpWindow {
			delete(s.m, k)
		}
	}
}
