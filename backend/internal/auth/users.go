package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
)

// Scope names are enforced server-side on every endpoint. The full catalogue
// is defined in docs/project/03-SECURITY-AND-OPERATIONS.md; M0 only grants and
// uses system.read.
const (
	ScopeSystemRead = "system.read"
)

// KnownScopes lists every scope the M0 server understands. Unknown scopes in the
// users file are rejected at load time so typos cannot silently grant nothing
// (or, in later milestones, the wrong thing).
var KnownScopes = map[string]bool{
	ScopeSystemRead: true,
}

var usernameRE = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// User is a local Homelab Control account. It is deliberately separate from
// Unix and Webmin accounts in M0 (see ADR 0002).
type User struct {
	Username     string   `json:"username"`
	PasswordHash string   `json:"password_hash"`
	Scopes       []string `json:"scopes"`
	Disabled     bool     `json:"disabled,omitempty"`
}

// UsersFile is the on-disk format of the local account store.
type UsersFile struct {
	Version int    `json:"version"`
	Users   []User `json:"users"`
}

// ValidateUsername applies the account name policy.
func ValidateUsername(name string) error {
	if !usernameRE.MatchString(name) {
		return fmt.Errorf("invalid username %q: must match %s", name, usernameRE.String())
	}
	return nil
}

// Validate checks structural and policy constraints of a users file.
func (f *UsersFile) Validate() error {
	if f.Version != 1 {
		return fmt.Errorf("unsupported users file version %d", f.Version)
	}
	seen := map[string]bool{}
	for _, u := range f.Users {
		if err := ValidateUsername(u.Username); err != nil {
			return err
		}
		if seen[u.Username] {
			return fmt.Errorf("duplicate user %q", u.Username)
		}
		seen[u.Username] = true
		if err := ValidateHash(u.PasswordHash); err != nil {
			return fmt.Errorf("user %q: %w", u.Username, err)
		}
		for _, s := range u.Scopes {
			if !KnownScopes[s] {
				return fmt.Errorf("user %q: unknown scope %q", u.Username, s)
			}
		}
	}
	return nil
}

// ErrInsecurePermissions is returned when the users file is accessible to
// group or other.
var ErrInsecurePermissions = errors.New("users file must not be accessible by group or other (expected mode 0600)")

// LoadUsersFile reads and validates a users file, refusing insecure modes.
func LoadUsersFile(path string) (*UsersFile, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s: %w (got %#o)", path, ErrInsecurePermissions, fi.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f UsersFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &f, nil
}

// SaveUsersFile writes the file atomically (temp file, fsync, rename) with
// mode 0600. The containing directory must already exist.
func SaveUsersFile(path string, f *UsersFile) error {
	if err := f.Validate(); err != nil {
		return err
	}
	sort.Slice(f.Users, func(i, j int) bool { return f.Users[i].Username < f.Users[j].Username })
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".users-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Store is an in-memory, read-only view of the users file used by the server.
type Store struct {
	mu    sync.RWMutex
	users map[string]User
}

// NewStore builds a Store from a validated users file.
func NewStore(f *UsersFile) *Store {
	s := &Store{users: map[string]User{}}
	for _, u := range f.Users {
		s.users[u.Username] = u
	}
	return s
}

// Authenticate verifies credentials. It always performs one hash
// verification so that timing does not reveal account existence.
func (s *Store) Authenticate(username, password string) (User, bool) {
	s.mu.RLock()
	u, ok := s.users[username]
	s.mu.RUnlock()
	hash := dummyHash
	if ok && !u.Disabled {
		hash = u.PasswordHash
	}
	match, err := VerifyPassword(password, hash)
	if err != nil || !match || !ok || u.Disabled {
		return User{}, false
	}
	return u, true
}

// Lookup returns the current record for a user (used to re-check that a
// session's account is still enabled).
func (s *Store) Lookup(username string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[username]
	return u, ok
}
