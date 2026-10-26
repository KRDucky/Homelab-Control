// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"testing"
	"time"
)

// D-3: a re-entry is valid for five minutes, for one session and one user,
// and is cleared on logout, revocation and account disable.
func TestStepUpWindowAndBinding(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := NewStepUps()
	s.SetClock(func() time.Time { return now })
	if _, ok := s.At("k1", "alice"); ok {
		t.Fatal("step-up without a re-entry")
	}
	until := s.Record("k1", "alice")
	if !until.Equal(now.Add(StepUpWindow)) {
		t.Fatalf("until %v", until)
	}
	if at, ok := s.At("k1", "alice"); !ok || !at.Equal(now) {
		t.Fatal("fresh step-up not found")
	}
	if _, ok := s.At("k2", "alice"); ok {
		t.Fatal("another session of the same user is authorized")
	}
	if _, ok := s.At("k1", "bob"); ok {
		t.Fatal("same session key, different user is authorized")
	}
	now = now.Add(StepUpWindow)
	if _, ok := s.At("k1", "alice"); !ok {
		t.Fatal("expired exactly at the window edge")
	}
	now = now.Add(time.Second)
	if _, ok := s.At("k1", "alice"); ok {
		t.Fatal("stale step-up accepted")
	}
	// A clock that moved backwards (re-entry in the future) is not accepted.
	s.Record("k3", "alice")
	now = now.Add(-time.Hour)
	if _, ok := s.At("k3", "alice"); ok {
		t.Fatal("future-dated step-up accepted")
	}
}

func TestStepUpClear(t *testing.T) {
	s := NewStepUps()
	s.Record("k1", "alice")
	s.Record("k2", "alice")
	s.Record("k3", "bob")
	s.Clear("k1")
	if _, ok := s.At("k1", "alice"); ok {
		t.Fatal("Clear (logout/revoke) left the step-up")
	}
	s.ClearUser("alice")
	if _, ok := s.At("k2", "alice"); ok {
		t.Fatal("ClearUser (account disabled) left the step-up")
	}
	if _, ok := s.At("k3", "bob"); !ok {
		t.Fatal("ClearUser removed another user's step-up")
	}
}
