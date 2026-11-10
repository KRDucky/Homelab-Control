// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"syscall"
)

// Scope names are enforced server-side on every endpoint. The full catalogue
// is defined in docs/project/03-SECURITY-AND-OPERATIONS.md; M0 only grants and
// uses system.read.
const (
	// ScopeNetworkingProxyRead allows Traefik inspection and pure label previews, never apply. Not granted by default.
	ScopeNetworkingProxyRead = "networking.proxy.read"
	ScopeComposeRead         = "docker.compose.read"
	// ScopePlatformsRead lists administrator-registered container platforms
	// (no connection details). Each platform additionally requires its own
	// family scope: docker.compose.read, podman.read or kubernetes.read. No
	// platform scope implies another. Not granted by default.
	ScopePlatformsRead  = "platforms.read"
	ScopePodmanRead     = "podman.read"
	ScopeKubernetesRead = "kubernetes.read"
	// ScopeStorageRead allows storage metadata and identifying labels. Never granted by default.
	ScopeStorageRead  = "storage.read"
	ScopeSystemRead   = "system.read"   // overview, legacy Webmin status
	ScopeServicesRead = "services.read" // systemd unit list (M1)
	ScopeHardwareRead = "hardware.read" // hardware inventory incl. DMI serials (M1)
	// ScopeServicesLogs allows reading service journal entries, which can
	// contain sensitive data. Not in DefaultScopes: grant it explicitly (D-7).
	ScopeServicesLogs = "services.logs"
	// ScopeAuditRead allows reading the API audit trail (who signed in, what
	// was denied, which logs were read). Not in DefaultScopes (D-7).
	ScopeAuditRead = "audit.read"
	// ScopeServicesOperate allows start/stop/restart of systemd services
	// through plan → confirm → execute (M2-2, ADR 0011). Not in DefaultScopes:
	// grant it explicitly.
	ScopeServicesOperate = "services.operate"
	// ScopeServicesConfigure allows persistent changes to systemd services:
	// boot-time enable/disable through plan → confirm → execute (M2-3, ADR
	// 0011). Not in DefaultScopes, never implied by services.operate.
	ScopeServicesConfigure = "services.configure"
	// ScopeServicesFiles allows reading unit file and drop-in contents
	// (M2-3, owner authorization 2026-11-05). Unit files can contain
	// credentials and other sensitive configuration; redaction is heuristic.
	// Not in DefaultScopes and not implied by services.logs, services.read,
	// services.operate or services.configure (no scope implies another).
	ScopeServicesFiles = "services.files"
	// ScopeHostSettingsPreview allows previewing hostname and timezone
	// changes (M2-6). A preview never changes the host, and this scope never
	// authorizes plan or apply. Not in DefaultScopes.
	ScopeHostSettingsPreview = "host.settings.preview"
	// ScopeHostSettingsApply allows plan → confirm → execute of hostname and
	// timezone changes (M2-6, ADR 0011). Not in DefaultScopes and never
	// implied by host.settings.preview or system.read.
	ScopeHostSettingsApply = "host.settings.apply"
	// ScopeLayoutManage allows editing the application-owned manual drive-bay
	// layout in state.db (M2-5 Stage A, D-7). It changes no host state. Not in
	// DefaultScopes; storage.read does not imply it, and it does not imply
	// storage.read (assigning a drive additionally requires storage.read).
	ScopeLayoutManage = "layout.manage"
	// ScopeFilesRead allows listing and statting entries under administrator
	// allow-listed confined roots (file-manager first checkpoint). It never
	// grants content download or writes. Not in DefaultScopes and not implied
	// by system.read, layout.manage, host.settings.preview, host.settings.apply,
	// storage.read or services.files (services.files stays unit-file contents).
	ScopeFilesRead = "files.read"
	// ScopePackagesRead allows read-only Software Center queries (M4-1b):
	// search, installed list, pending-update preview and configured sources,
	// all from the local cache with no network and no writes. Not in
	// DefaultScopes and implied by no other scope. packages.manage stays
	// reserved for the separately reviewed write ADR and is not granted here.
	ScopePackagesRead = "packages.read"
)

// KnownScopes lists every scope the server understands. Unknown scopes in the
// users file are rejected at load time so typos cannot silently grant nothing
// (or, in later milestones, the wrong thing). All M1 scopes are read-only.
var KnownScopes = map[string]bool{
	ScopeNetworkingProxyRead: true,
	ScopeComposeRead:         true,
	ScopePlatformsRead:       true,
	ScopePodmanRead:          true,
	ScopeKubernetesRead:      true,
	ScopeStorageRead:         true,
	ScopeSystemRead:          true,
	ScopeServicesRead:        true,
	ScopeHardwareRead:        true,
	ScopeServicesLogs:        true,
	ScopeAuditRead:           true,
	ScopeServicesOperate:     true,
	ScopeServicesConfigure:   true,
	ScopeServicesFiles:       true,
	ScopeHostSettingsPreview: true,
	ScopeHostSettingsApply:   true,
	ScopeLayoutManage:        true,
	ScopeFilesRead:           true,
	ScopePackagesRead:        true,
}

// DefaultScopes are granted by `hlctl user add` when --scopes is not given.
const DefaultScopes = ScopeSystemRead + "," + ScopeServicesRead + "," + ScopeHardwareRead

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
	// O_NOFOLLOW + fstat on the opened descriptor avoids a check/use race
	// with a symlink swapped in after a separate stat.
	fh, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%s: refusing to follow a symlink", path)
		}
		return nil, err
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s: %w (got %#o)", path, ErrInsecurePermissions, fi.Mode().Perm())
	}
	data, err := io.ReadAll(io.LimitReader(fh, 1<<20))
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
	// Preserve the owner of an existing file (e.g. homelab-control) when an
	// administrator edits it as root, so the service can still read it.
	if fi, err := os.Lstat(path); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
			if err := tmp.Chown(int(st.Uid), int(st.Gid)); err != nil {
				tmp.Close()
				return err
			}
		}
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
