// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"sync"
	"time"
)

// Session is server-side session state. The raw session ID is only ever held
// by the client cookie; the server indexes sessions by SHA-256(ID) (Key), so
// a dump of the session table or the state database does not yield usable
// cookies. Key is not secret and identifies a session in the session list.
type Session struct {
	Key       string
	Username  string
	Scopes    []string
	CSRFToken string
	Created   time.Time
	LastSeen  time.Time
	RemoteIP  string
	UserAgent string
}

// HasScope reports whether the session grants a scope.
func (s *Session) HasScope(scope string) bool { return slices.Contains(s.Scopes, scope) }

// SessionStore persists sessions by key. Implementations: the in-memory
// store (default, tests) and the SQLite state store (internal/state, D-6).
type SessionStore interface {
	Insert(s Session) error
	Get(key string) (Session, bool, error)
	Touch(key string, t time.Time) error
	Delete(key string) error
	DeleteUser(username string) error
	ListUser(username string) ([]Session, error)
	// Expire removes sessions idle since before idleBefore or created before
	// createdBefore.
	Expire(idleBefore, createdBefore time.Time) error
	Count() (int, error)
	DeleteOldest() error
}

// SessionManager enforces the session policy (idle and absolute timeouts, a
// global cap) over a SessionStore. Store errors fail closed: a session that
// cannot be read is treated as absent.
type SessionManager struct {
	store       SessionStore
	IdleTimeout time.Duration
	MaxLifetime time.Duration
	MaxSessions int
	// TouchEvery limits last-seen writes: the idle timer is refreshed at most
	// this often per session (so idle expiry is accurate to this interval).
	TouchEvery time.Duration
	mu         sync.Mutex // serialises create (cap check) and touches
	now        func() time.Time
}

// NewSessionManager returns an in-memory manager with conservative defaults.
func NewSessionManager() *SessionManager { return NewSessionManagerWith(NewMemoryStore()) }

// NewSessionManagerWith uses the given store (e.g. the persistent state DB).
func NewSessionManagerWith(st SessionStore) *SessionManager {
	return &SessionManager{
		store:       st,
		IdleTimeout: 30 * time.Minute,
		MaxLifetime: 12 * time.Hour,
		MaxSessions: 1024,
		TouchEvery:  30 * time.Second,
		now:         time.Now,
	}
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// KeyFor returns the storage key (SHA-256 hex) of a raw session ID.
func KeyFor(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func (m *SessionManager) expired(s Session, now time.Time) bool {
	return now.Sub(s.LastSeen) > m.IdleTimeout || now.Sub(s.Created) > m.MaxLifetime
}

// Create issues a new session and returns the raw ID (for the cookie).
func (m *SessionManager) Create(u User, remoteIP, userAgent string) (string, *Session, error) {
	id, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	if len(userAgent) > 200 {
		userAgent = userAgent[:200]
	}
	now := m.now()
	s := Session{Key: KeyFor(id), Username: u.Username, Scopes: slices.Clone(u.Scopes), CSRFToken: csrf,
		Created: now, LastSeen: now, RemoteIP: remoteIP, UserAgent: userAgent}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.store.Expire(now.Add(-m.IdleTimeout), now.Add(-m.MaxLifetime)); err != nil {
		return "", nil, err
	}
	n, err := m.store.Count()
	if err != nil {
		return "", nil, err
	}
	for ; n >= m.MaxSessions && n > 0; n-- {
		if err := m.store.DeleteOldest(); err != nil {
			return "", nil, err
		}
	}
	if err := m.store.Insert(s); err != nil {
		return "", nil, err
	}
	return id, &s, nil
}

// Get returns a live session for a raw ID and refreshes its idle timer.
func (m *SessionManager) Get(id string) (*Session, bool) {
	if id == "" || len(id) > 128 {
		return nil, false
	}
	now := m.now()
	k := KeyFor(id)
	s, ok, err := m.store.Get(k)
	if err != nil || !ok {
		return nil, false
	}
	if m.expired(s, now) {
		_ = m.store.Delete(k)
		return nil, false
	}
	if now.Sub(s.LastSeen) >= m.TouchEvery {
		if err := m.store.Touch(k, now); err != nil {
			return nil, false
		}
		s.LastSeen = now
	}
	return &s, true
}

// Destroy removes the session with this raw ID.
func (m *SessionManager) Destroy(id string) { _ = m.store.Delete(KeyFor(id)) }

// DestroyUser removes every session for a user (e.g. account disabled).
func (m *SessionManager) DestroyUser(username string) { _ = m.store.DeleteUser(username) }

// List returns a user's live sessions, newest activity first.
func (m *SessionManager) List(username string) ([]Session, error) {
	all, err := m.store.ListUser(username)
	if err != nil {
		return nil, err
	}
	now := m.now()
	out := all[:0]
	for _, s := range all {
		if !m.expired(s, now) {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b Session) int { return b.LastSeen.Compare(a.LastSeen) })
	return out, nil
}

// Revoke deletes one of username's sessions by key. It reports false if the
// key does not belong to that user (other users' sessions are never touched).
func (m *SessionManager) Revoke(username, key string) (bool, error) {
	s, ok, err := m.store.Get(key)
	if err != nil {
		return false, err
	}
	if !ok || s.Username != username {
		return false, nil
	}
	return true, m.store.Delete(key)
}

// memStore is the in-memory SessionStore.
type memStore struct {
	mu sync.Mutex
	m  map[string]Session
}

// NewMemoryStore returns a non-persistent SessionStore.
func NewMemoryStore() SessionStore { return &memStore{m: map[string]Session{}} }

func (s *memStore) Insert(x Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	x.Scopes = slices.Clone(x.Scopes)
	s.m[x.Key] = x
	return nil
}

func (s *memStore) Get(k string) (Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.m[k]
	x.Scopes = slices.Clone(x.Scopes)
	return x, ok, nil
}

func (s *memStore) Touch(k string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, ok := s.m[k]; ok {
		x.LastSeen = t
		s.m[k] = x
	}
	return nil
}

func (s *memStore) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
	return nil
}

func (s *memStore) DeleteUser(u string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, x := range s.m {
		if x.Username == u {
			delete(s.m, k)
		}
	}
	return nil
}

func (s *memStore) ListUser(u string) ([]Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Session
	for _, x := range s.m {
		if x.Username == u {
			x.Scopes = slices.Clone(x.Scopes)
			out = append(out, x)
		}
	}
	return out, nil
}

func (s *memStore) Expire(idleBefore, createdBefore time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, x := range s.m {
		if x.LastSeen.Before(idleBefore) || x.Created.Before(createdBefore) {
			delete(s.m, k)
		}
	}
	return nil
}

func (s *memStore) Count() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m), nil
}

func (s *memStore) DeleteOldest() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ok string
	var oldest time.Time
	for k, x := range s.m {
		if ok == "" || x.LastSeen.Before(oldest) {
			ok, oldest = k, x.LastSeen
		}
	}
	delete(s.m, ok)
	return nil
}

// CheckCSRF compares a presented token with the session's token in constant time.
func CheckCSRF(s *Session, presented string) bool {
	if presented == "" || s.CSRFToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(s.CSRFToken)) == 1
}
