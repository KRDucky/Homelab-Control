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
	// ScopeNetworkingProxyRead allows Traefik inspection and pure label previews.
	// It never authorizes writing dynamic YAML. Not granted by default.
	ScopeNetworkingProxyRead = "networking.proxy.read"
	// ScopeNetworkingProxyApply writes CORE-generated file-provider YAML into an
	// administrator-configured Traefik dynamic directory. It never enables the
	// Docker provider, ACME, or Compose label apply. Not granted by default and
	// never implied by networking.proxy.read.
	ScopeNetworkingProxyApply = "networking.proxy.apply"
	ScopeComposeRead          = "docker.compose.read"
	// ScopePlatformsRead lists administrator-registered container platforms
	// (no connection details). Each platform additionally requires its own
	// family scope: docker.compose.read, podman.read or kubernetes.read. No
	// platform scope implies another. Not granted by default.
	ScopePlatformsRead  = "platforms.read"
	ScopePodmanRead     = "podman.read"
	ScopeKubernetesRead = "kubernetes.read"
	// ScopeStorageRead allows storage metadata and identifying labels. Never granted by default.
	ScopeStorageRead = "storage.read"
	// ScopeStorageProbe triggers the on-demand native storage probe (O-1), which
	// wakes disks to read blkid/LVM signatures. Never granted by default; it is
	// a separate, explicit action scope.
	ScopeStorageProbe = "storage.probe"
	ScopeSystemRead   = "system.read"   // overview, legacy Webmin status
	ScopeServicesRead = "services.read" // systemd unit list (M1)
	ScopeHardwareRead = "hardware.read" // hardware inventory incl. DMI serials (M1)
	// ScopeServicesLogs allows reading service journal entries, which can
	// contain sensitive data. In DefaultScopes (owner 2026-11-20, issue #90):
	// new accounts from `hlctl user add` get it. An existing account does not
	// until `hlctl user scopes` replaces its list. audit.read stays out.
	ScopeServicesLogs = "services.logs"
	// ScopeAuditRead allows reading the API audit trail (who signed in, what
	// was denied, which logs were read). Not in DefaultScopes (D-7).
	ScopeAuditRead = "audit.read"
	// ScopeServicesOperate allows start/stop/restart of systemd services
	// through plan → confirm → execute (M2-2, ADR 0011). In DefaultScopes
	// (owner 2026-11-20): new accounts from `hlctl user add` get it, while an
	// existing account does not until `hlctl user scopes` replaces its list.
	ScopeServicesOperate = "services.operate"
	// ScopeServicesConfigure allows persistent changes to systemd services:
	// boot-time enable/disable through plan → confirm → execute (M2-3, ADR
	// 0011). In DefaultScopes (owner 2026-11-20) and never implied by
	// services.operate (no scope implies another).
	ScopeServicesConfigure = "services.configure"
	// ScopeServicesFiles allows reading unit file and drop-in contents
	// (M2-3, owner authorization 2026-11-05). Unit files can contain
	// credentials and other sensitive configuration; redaction is heuristic.
	// In DefaultScopes (owner 2026-11-20, issue #90) and not implied by
	// services.logs, services.read, services.operate or services.configure
	// (no scope implies another).
	ScopeServicesFiles = "services.files"
	// ScopeServicesSystemctl allows the closed systemctl verbs (mask, drop-in,
	// daemon-reload, host power, and the other templates on
	// services.systemctl). Owner 2026-11-21: these are in the production
	// admin. Not implied by services.operate or services.configure. Loading
	// an existing users file never adds it.
	ScopeServicesSystemctl = "services.systemctl"
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
	// allow-listed confined roots (file-manager first checkpoint). It also
	// grants content read and download in the write slice (2026-11-20). It
	// never grants writes. Not in DefaultScopes and not implied by system.read,
	// layout.manage, host.settings.preview, host.settings.apply, storage.read
	// or services.files (services.files stays unit-file contents).
	ScopeFilesRead = "files.read"
	// ScopeFilesWrite allows content write/replace, upload, create and rename
	// inside administrator allow-listed confined roots (2026-11-20, ADR 0012
	// amendment). Not in DefaultScopes and implied by no other scope,
	// including files.read (reading never implies writing) and services.files.
	ScopeFilesWrite = "files.write"
	// ScopeFilesDestroy allows deleting a file or empty directory inside an
	// allow-listed confined root through plan → confirm (2026-11-20, ADR 0012
	// amendment, ADR 0011 pattern). Not in DefaultScopes and implied by no
	// other scope, including files.write.
	ScopeFilesDestroy = "files.destroy"
	// ScopePackagesRead allows read-only Software Center queries (M4-1b):
	// search, installed list, pending-update preview and configured sources,
	// all from the local cache with no network and no writes. Not in
	// DefaultScopes and implied by no other scope. packages.manage stays
	// reserved for the separately reviewed write ADR and is not granted here.
	ScopePackagesRead = "packages.read"
	// ScopeMergerFSRead allows the read-only MergerFS inventory and
	// mergerfs.snapraid.status. It does not grant pool, sync, scrub, or
	// recovery operations. Not in DefaultScopes and implied by nothing,
	// including storage.read, mergerfs.configure, and mergerfs.maintain.
	ScopeMergerFSRead = "mergerfs.read"
	// ScopeMergerFSConfigure allows pool mount and saving fstab plus
	// snapraid.conf. It does not grant sync, scrub, or fix, and it is not
	// implied by mergerfs.read. Not in DefaultScopes.
	ScopeMergerFSConfigure = "mergerfs.configure"
	// ScopeMergerFSMaintain allows snapraid sync, scrub, and filtered
	// recovery (fix -m -f). It does not grant mount or configuration writes,
	// and it is not implied by mergerfs.read. Not in DefaultScopes.
	ScopeMergerFSMaintain = "mergerfs.maintain"
	// ScopeZFSRead allows the read-only OpenZFS inventory (M7 first
	// checkpoint): observed zfs mounts and zfs/zpool binary presence, with
	// no writes, no broker ops, and no zpool/zfs execution. Not in
	// DefaultScopes and implied by nothing, including storage.read.
	ScopeZFSRead = "zfs.read"
	// ScopeLVMRead allows the read-only LVM inventory (M7 first checkpoint):
	// observed device-mapper names whose uuid starts with LVM- and lvm
	// binary presence, with no writes, no broker ops, and no lvm execution.
	// Not in DefaultScopes and implied by nothing, including storage.read.
	ScopeLVMRead = "lvm.read"
	// ScopeBtrfsRead allows the read-only Btrfs inventory (M7 first
	// checkpoint): observed btrfs mounts and btrfs binary presence, with
	// no writes, no broker ops, and no btrfs execution. Not in
	// DefaultScopes and implied by nothing, including storage.read.
	// RAID5/6 is not offered.
	ScopeBtrfsRead = "btrfs.read"
	// ScopeExt4Read allows the read-only ext4 inventory (M7 first
	// checkpoint): observed ext4 mounts and dumpe2fs binary presence.
	// It does not format a disk and does not imply ScopeExt4Format. Not in
	// DefaultScopes and implied by nothing, including storage.read.
	ScopeExt4Read = "ext4.read"
	// ScopeExt4Format allows ext4.format plan/apply (one quiet ext4 format of
	// one by-id disk). Not in DefaultScopes and implied by nothing, including
	// ext4.read and storage.read.
	ScopeExt4Format = "ext4.format"
	// ScopeStoragePartition allows storage.partition plan/apply (one GPT
	// label on one whole by-id disk). It does not format. Not in
	// DefaultScopes and implied by nothing, including storage.read and
	// ext4.format.
	ScopeStoragePartition = "storage.partition"
	// ScopeMdadmRead allows the read-only Linux software RAID inventory:
	// /proc/mdstat array lines and mdadm binary presence, with no writes, no
	// broker ops, and no mdadm execution. Not in DefaultScopes and implied by
	// nothing, including storage.read.
	ScopeMdadmRead = "mdadm.read"
	// ScopeF2fsRead allows the read-only f2fs inventory (M7 first
	// checkpoint): observed f2fs mounts and dump.f2fs binary presence, with
	// no writes, no broker ops, and no dump.f2fs execution. Not in
	// DefaultScopes and implied by nothing, including storage.read.
	ScopeF2fsRead = "f2fs.read"
	// ScopeXFSRead allows the read-only XFS inventory (M7 first
	// checkpoint): observed xfs mounts and xfs_info binary presence, with
	// no writes, no broker ops, and no xfs_info execution. Not in
	// DefaultScopes and implied by nothing, including storage.read.
	ScopeXFSRead = "xfs.read"
	// M11 read-only networking (ADR 0008 Amendment 9, Proposed). None is in
	// DefaultScopes and none implies another or is implied by any scope.
	//
	// ScopeNetworkingPortsRead allows the host port-use map (listening
	// sockets with owning account, process, unit and container).
	ScopeNetworkingPortsRead = "networking.ports.read"
	// ScopeNetworkingBondsRead allows the kernel bonding / LACP inventory.
	ScopeNetworkingBondsRead = "networking.bonds.read"
	// ScopeNetworkingFirewallRead allows the active firewall backend and its
	// saved configuration. It grants no firewall change.
	ScopeNetworkingFirewallRead = "networking.firewall.read"
)

// KnownScopes lists every scope the server understands. Unknown scopes in the
// users file are rejected at load time so typos cannot silently grant nothing
// (or, in later milestones, the wrong thing). All M1 scopes are read-only.
var KnownScopes = map[string]bool{
	ScopeNetworkingProxyRead:    true,
	ScopeNetworkingProxyApply:   true,
	ScopeComposeRead:            true,
	ScopePlatformsRead:          true,
	ScopePodmanRead:             true,
	ScopeKubernetesRead:         true,
	ScopeStorageRead:            true,
	ScopeStorageProbe:           true,
	ScopeStoragePartition:       true,
	ScopeSystemRead:             true,
	ScopeServicesRead:           true,
	ScopeHardwareRead:           true,
	ScopeServicesLogs:           true,
	ScopeAuditRead:              true,
	ScopeServicesOperate:        true,
	ScopeServicesConfigure:      true,
	ScopeServicesFiles:          true,
	ScopeServicesSystemctl:      true,
	ScopeHostSettingsPreview:    true,
	ScopeHostSettingsApply:      true,
	ScopeLayoutManage:           true,
	ScopeFilesRead:              true,
	ScopeFilesWrite:             true,
	ScopeFilesDestroy:           true,
	ScopePackagesRead:           true,
	ScopeMergerFSRead:           true,
	ScopeMergerFSConfigure:      true,
	ScopeMergerFSMaintain:       true,
	ScopeZFSRead:                true,
	ScopeLVMRead:                true,
	ScopeBtrfsRead:              true,
	ScopeExt4Read:               true,
	ScopeExt4Format:             true,
	ScopeMdadmRead:              true,
	ScopeF2fsRead:               true,
	ScopeXFSRead:                true,
	ScopeNetworkingPortsRead:    true,
	ScopeNetworkingBondsRead:    true,
	ScopeNetworkingFirewallRead: true,
}

// DefaultScopes are granted by `hlctl user add` when --scopes is not given.
// Owner 2026-11-20: the services module scopes from issue #90 are included
// (read, operate, configure, logs, files). Owner 2026-11-21: services.systemctl
// is included too. audit.read stays out. Loading or upgrading an existing
// users file never adds scopes.
const DefaultScopes = ScopeSystemRead + "," + ScopeServicesRead + "," + ScopeHardwareRead +
	"," + ScopeServicesOperate + "," + ScopeServicesConfigure +
	"," + ScopeServicesLogs + "," + ScopeServicesFiles +
	"," + ScopeServicesSystemctl

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
