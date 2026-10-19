// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"sync"
	"time"
)

// Throttle limits failed login attempts per (username, remote IP) and per
// remote IP. It is in-memory and resets on restart (documented limitation).
type Throttle struct {
	mu         sync.Mutex
	failures   map[string][]time.Time
	Window     time.Duration
	MaxPerKey  int
	MaxPerIP   int
	LockoutFor time.Duration
	locked     map[string]time.Time
	now        func() time.Time
}

// NewThrottle returns a throttle with conservative defaults.
func NewThrottle() *Throttle {
	return &Throttle{
		failures:   map[string][]time.Time{},
		locked:     map[string]time.Time{},
		Window:     15 * time.Minute,
		MaxPerKey:  5,
		MaxPerIP:   20,
		LockoutFor: 15 * time.Minute,
		now:        time.Now,
	}
}

func userKey(user, ip string) string { return "u:" + user + "|" + ip }
func ipKey(ip string) string         { return "ip:" + ip }

// Allowed reports whether a login attempt may proceed.
func (t *Throttle) Allowed(user, ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for _, k := range []string{userKey(user, ip), ipKey(ip)} {
		if until, ok := t.locked[k]; ok {
			if now.Before(until) {
				return false
			}
			delete(t.locked, k)
		}
	}
	return true
}

// maxTracked bounds memory use; beyond it, stale entries are pruned and, if
// still full, the table is reset (failing open on counting, never on auth).
const maxTracked = 50_000

func (t *Throttle) pruneLocked(now time.Time) {
	for k, ts := range t.failures {
		if len(ts) == 0 || now.Sub(ts[len(ts)-1]) >= t.Window {
			delete(t.failures, k)
		}
	}
	for k, until := range t.locked {
		if !now.Before(until) {
			delete(t.locked, k)
		}
	}
	if len(t.failures)+len(t.locked) > maxTracked {
		t.failures = map[string][]time.Time{}
	}
}

// Tracked returns the number of tracked keys (tests/metrics).
func (t *Throttle) Tracked() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.failures) + len(t.locked)
}

// Fail records a failed attempt and applies lockouts.
func (t *Throttle) Fail(user, ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if len(t.failures)+len(t.locked) >= maxTracked {
		t.pruneLocked(now)
	}
	for k, limit := range map[string]int{userKey(user, ip): t.MaxPerKey, ipKey(ip): t.MaxPerIP} {
		recent := t.failures[k][:0]
		for _, ts := range t.failures[k] {
			if now.Sub(ts) < t.Window {
				recent = append(recent, ts)
			}
		}
		recent = append(recent, now)
		t.failures[k] = recent
		if len(recent) >= limit {
			t.locked[k] = now.Add(t.LockoutFor)
			delete(t.failures, k)
		}
	}
}

// Succeed clears failures for a user/IP pair.
func (t *Throttle) Succeed(user, ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, userKey(user, ip))
}
