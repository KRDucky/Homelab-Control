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
// by the client cookie; the server indexes sessions by SHA-256(ID) so a memory
// or debug dump of the table does not yield usable cookies.
type Session struct {
	Username  string
	Scopes    []string
	CSRFToken string
	Created   time.Time
	LastSeen  time.Time
	RemoteIP  string
}

// HasScope reports whether the session grants a scope.
func (s *Session) HasScope(scope string) bool { return slices.Contains(s.Scopes, scope) }

// SessionManager stores sessions in memory. M0 intentionally does not persist
// sessions: a restart logs everyone out (documented in ADR 0002).
type SessionManager struct {
	mu          sync.Mutex
	sessions    map[string]*Session
	IdleTimeout time.Duration
	MaxLifetime time.Duration
	MaxSessions int
	now         func() time.Time
}

// NewSessionManager returns a manager with conservative defaults.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions:    map[string]*Session{},
		IdleTimeout: 30 * time.Minute,
		MaxLifetime: 12 * time.Hour,
		MaxSessions: 1024,
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

func keyFor(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// Create issues a new session and returns the raw ID (for the cookie).
func (m *SessionManager) Create(u User, remoteIP string) (string, *Session, error) {
	id, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	now := m.now()
	s := &Session{Username: u.Username, Scopes: slices.Clone(u.Scopes), CSRFToken: csrf, Created: now, LastSeen: now, RemoteIP: remoteIP}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gcLocked(now)
	if len(m.sessions) >= m.MaxSessions {
		m.evictOldestLocked()
	}
	m.sessions[keyFor(id)] = s
	return id, s, nil
}

// Get returns a live session for a raw ID and refreshes its idle timer.
func (m *SessionManager) Get(id string) (*Session, bool) {
	if id == "" || len(id) > 128 {
		return nil, false
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	k := keyFor(id)
	s, ok := m.sessions[k]
	if !ok {
		return nil, false
	}
	if now.Sub(s.LastSeen) > m.IdleTimeout || now.Sub(s.Created) > m.MaxLifetime {
		delete(m.sessions, k)
		return nil, false
	}
	s.LastSeen = now
	cp := *s
	return &cp, true
}

// Destroy removes a session.
func (m *SessionManager) Destroy(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, keyFor(id))
}

// DestroyUser removes every session for a user (e.g. account disabled).
func (m *SessionManager) DestroyUser(username string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.sessions {
		if s.Username == username {
			delete(m.sessions, k)
		}
	}
}

func (m *SessionManager) gcLocked(now time.Time) {
	for k, s := range m.sessions {
		if now.Sub(s.LastSeen) > m.IdleTimeout || now.Sub(s.Created) > m.MaxLifetime {
			delete(m.sessions, k)
		}
	}
}

func (m *SessionManager) evictOldestLocked() {
	var oldestK string
	var oldest time.Time
	for k, s := range m.sessions {
		if oldestK == "" || s.LastSeen.Before(oldest) {
			oldestK, oldest = k, s.LastSeen
		}
	}
	delete(m.sessions, oldestK)
}

// CheckCSRF compares a presented token with the session's token in constant time.
func CheckCSRF(s *Session, presented string) bool {
	if presented == "" || s.CSRFToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(s.CSRFToken)) == 1
}
