// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"strings"
	"testing"
)

// Owner 2026-11-20, issue #90: service journal access is granted to new
// accounts. audit.read stays out. An upgrade does not add scopes.
func TestServicesLogsGrantedByDefault(t *testing.T) {
	if !KnownScopes[ScopeServicesLogs] || ScopeServicesLogs != "services.logs" {
		t.Fatal("services.logs must be a known scope")
	}
	seen := map[string]int{}
	for _, s := range strings.Split(DefaultScopes, ",") {
		seen[s]++
		if strings.HasPrefix(s, "audit.") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	if seen[ScopeServicesLogs] != 1 {
		t.Fatalf("services.logs must appear exactly once in DefaultScopes, got %d", seen[ScopeServicesLogs])
	}
}

// Owner 2026-11-20: the issue #90 service scopes are granted to new accounts
// by `hlctl user add` with no --scopes. No scope implies another. audit.read
// stays out of DefaultScopes.
func TestServicesOperateAndConfigureGrantedByDefault(t *testing.T) {
	if !KnownScopes[ScopeServicesOperate] || ScopeServicesOperate != "services.operate" {
		t.Fatal("services.operate must be a known scope")
	}
	if !KnownScopes[ScopeServicesConfigure] || ScopeServicesConfigure != "services.configure" {
		t.Fatal("services.configure must be a known scope")
	}

	seen := map[string]int{}
	for _, s := range strings.Split(DefaultScopes, ",") {
		seen[s]++
		if strings.HasPrefix(s, "audit.") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	for _, sc := range []string{ScopeServicesOperate, ScopeServicesConfigure, ScopeServicesLogs, ScopeServicesFiles} {
		if seen[sc] != 1 {
			t.Fatalf("%s must appear exactly once in DefaultScopes, got %d", sc, seen[sc])
		}
	}

	if (&Session{Scopes: []string{ScopeServicesOperate}}).HasScope(ScopeServicesConfigure) {
		t.Fatal("services.operate must not imply services.configure")
	}
	if (&Session{Scopes: []string{ScopeServicesConfigure}}).HasScope(ScopeServicesOperate) {
		t.Fatal("services.configure must not imply services.operate")
	}
}

// M2-3: unit-file contents need services.files. Owner 2026-11-20 grants it
// to new accounts. No other scope implies it. files.read stays out.
func TestServicesFilesGrantedByDefaultNotImplied(t *testing.T) {
	if !KnownScopes[ScopeServicesFiles] || ScopeServicesFiles != "services.files" {
		t.Fatal("services.files must be a known scope")
	}
	seen := 0
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeServicesFiles {
			seen++
		}
		if s == ScopeFilesRead || strings.HasPrefix(s, "files.") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	if seen != 1 {
		t.Fatalf("services.files must appear exactly once in DefaultScopes, got %d", seen)
	}
	var others []string
	for s := range KnownScopes {
		if s != ScopeServicesFiles {
			others = append(others, s)
		}
	}
	if (&Session{Scopes: others}).HasScope(ScopeServicesFiles) {
		t.Fatal("every other scope together must not imply services.files")
	}
}

// Owner 2026-11-21: new accounts get the closed systemctl verbs. operate and
// configure do not imply them, and the file-manager scopes stay out.
func TestServicesSystemctlGrantedByDefaultNotImplied(t *testing.T) {
	if !KnownScopes[ScopeServicesSystemctl] || ScopeServicesSystemctl != "services.systemctl" {
		t.Fatal("services.systemctl must be a known scope")
	}
	seen := 0
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeServicesSystemctl {
			seen++
		}
		if strings.HasPrefix(s, "audit.") || strings.HasPrefix(s, "files.") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	if seen != 1 {
		t.Fatalf("services.systemctl must appear exactly once in DefaultScopes, got %d", seen)
	}
	var others []string
	for s := range KnownScopes {
		if s != ScopeServicesSystemctl {
			others = append(others, s)
		}
	}
	if (&Session{Scopes: others}).HasScope(ScopeServicesSystemctl) {
		t.Fatal("every other scope together must not imply services.systemctl")
	}
	if (&Session{Scopes: []string{ScopeServicesOperate, ScopeServicesConfigure}}).HasScope(ScopeServicesSystemctl) {
		t.Fatal("operate and configure must not imply services.systemctl")
	}
}

// M2-6: hostname/timezone apply is never granted by default and is not
// implied by host.settings.preview or any other scope.
func TestHostSettingsApplyNotGrantedByDefaultOrImplied(t *testing.T) {
	if !KnownScopes[ScopeHostSettingsApply] || ScopeHostSettingsApply != "host.settings.apply" {
		t.Fatal("host.settings.apply must be a known scope")
	}
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeHostSettingsApply || s == ScopeHostSettingsPreview {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	var others []string
	for s := range KnownScopes {
		if s != ScopeHostSettingsApply {
			others = append(others, s)
		}
	}
	if (&Session{Scopes: others}).HasScope(ScopeHostSettingsApply) {
		t.Fatal("every other scope together must not imply host.settings.apply")
	}
}

// File-manager first checkpoint: files.read is never default and is implied
// by no other scope, including services.files.
func TestFilesReadNotGrantedByDefaultOrImplied(t *testing.T) {
	if !KnownScopes[ScopeFilesRead] || ScopeFilesRead != "files.read" {
		t.Fatal("files.read must be a known scope")
	}
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeFilesRead || strings.HasPrefix(s, "files.") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	var others []string
	for s := range KnownScopes {
		if s != ScopeFilesRead {
			others = append(others, s)
		}
	}
	if (&Session{Scopes: others}).HasScope(ScopeFilesRead) {
		t.Fatal("every other scope together must not imply files.read")
	}
}

// File-manager write slice: files.write and files.destroy are never default
// and are implied by no other scope, including files.read.
func TestFilesWriteAndDestroyNotGrantedByDefaultOrImplied(t *testing.T) {
	for _, sc := range []string{ScopeFilesWrite, ScopeFilesDestroy} {
		if !KnownScopes[sc] {
			t.Fatalf("%s must be a known scope", sc)
		}
		for _, d := range strings.Split(DefaultScopes, ",") {
			if d == sc {
				t.Fatalf("default scopes grant %s", d)
			}
		}
	}
	if ScopeFilesWrite != "files.write" || ScopeFilesDestroy != "files.destroy" {
		t.Fatalf("scope names changed: %q %q", ScopeFilesWrite, ScopeFilesDestroy)
	}
	var others []string
	for s := range KnownScopes {
		if s != ScopeFilesWrite && s != ScopeFilesDestroy {
			others = append(others, s)
		}
	}
	sess := &Session{Scopes: others}
	if sess.HasScope(ScopeFilesWrite) || sess.HasScope(ScopeFilesDestroy) {
		t.Fatal("every other scope together must not imply files.write/files.destroy")
	}
	// files.read alone must not imply a write or destroy.
	if (&Session{Scopes: []string{ScopeFilesRead}}).HasScope(ScopeFilesWrite) {
		t.Fatal("files.read must not imply files.write")
	}
	if (&Session{Scopes: []string{ScopeFilesRead}}).HasScope(ScopeFilesDestroy) {
		t.Fatal("files.read must not imply files.destroy")
	}
}

// Traefik file-provider write is never default and is not implied by read.
func TestNetworkingProxyApplyNotGrantedByDefaultOrImplied(t *testing.T) {
	if !KnownScopes[ScopeNetworkingProxyApply] || ScopeNetworkingProxyApply != "networking.proxy.apply" {
		t.Fatal("networking.proxy.apply must be a known scope")
	}
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeNetworkingProxyApply || s == ScopeNetworkingProxyRead {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	var others []string
	for s := range KnownScopes {
		if s != ScopeNetworkingProxyApply {
			others = append(others, s)
		}
	}
	if (&Session{Scopes: others}).HasScope(ScopeNetworkingProxyApply) {
		t.Fatal("every other scope together must not imply networking.proxy.apply")
	}
}

// M4-1b: read-only Software Center needs packages.read, never granted by
// default and implied by no other scope. packages.manage stays reserved.
func TestPackagesReadNotGrantedByDefaultOrImplied(t *testing.T) {
	if !KnownScopes[ScopePackagesRead] || ScopePackagesRead != "packages.read" {
		t.Fatal("packages.read must be a known scope")
	}
	if KnownScopes["packages.manage"] {
		t.Fatal("packages.manage must stay unreserved")
	}
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopePackagesRead || strings.HasPrefix(s, "packages.") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	var others []string
	for s := range KnownScopes {
		if s != ScopePackagesRead {
			others = append(others, s)
		}
	}
	if (&Session{Scopes: others}).HasScope(ScopePackagesRead) {
		t.Fatal("every other scope together must not imply packages.read")
	}
}

// M8 first checkpoint: read-only MergerFS + optional SnapRAID needs
// mergerfs.read, never granted by default and implied by no other scope
// (including storage.read).
func TestMergerFSReadNotGrantedByDefaultOrImplied(t *testing.T) {
	if !KnownScopes[ScopeMergerFSRead] || ScopeMergerFSRead != "mergerfs.read" {
		t.Fatal("mergerfs.read must be a known scope")
	}
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeMergerFSRead || strings.HasPrefix(s, "mergerfs.") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	var others []string
	for s := range KnownScopes {
		if s != ScopeMergerFSRead {
			others = append(others, s)
		}
	}
	if (&Session{Scopes: others}).HasScope(ScopeMergerFSRead) {
		t.Fatal("every other scope together must not imply mergerfs.read")
	}
}

func TestMergerFSWriteScopesAreSeparate(t *testing.T) {
	for _, sc := range []string{ScopeMergerFSConfigure, ScopeMergerFSMaintain} {
		if !KnownScopes[sc] {
			t.Fatalf("%s must be known", sc)
		}
		for _, s := range strings.Split(DefaultScopes, ",") {
			if s == sc {
				t.Fatalf("default scopes grant %s", sc)
			}
		}
	}
	if (&Session{Scopes: []string{ScopeMergerFSRead}}).HasScope(ScopeMergerFSConfigure) ||
		(&Session{Scopes: []string{ScopeMergerFSRead}}).HasScope(ScopeMergerFSMaintain) ||
		(&Session{Scopes: []string{ScopeMergerFSConfigure}}).HasScope(ScopeMergerFSMaintain) ||
		(&Session{Scopes: []string{ScopeMergerFSMaintain}}).HasScope(ScopeMergerFSConfigure) ||
		(&Session{Scopes: []string{ScopeMergerFSConfigure, ScopeMergerFSMaintain}}).HasScope(ScopeMergerFSRead) {
		t.Fatal("mergerfs scopes must not imply each other")
	}
}

// M7 first checkpoint: read-only OpenZFS needs zfs.read, never granted by
// default and implied by no other scope (including storage.read).
func TestZFSReadNotGrantedByDefaultOrImplied(t *testing.T) {
	if !KnownScopes[ScopeZFSRead] || ScopeZFSRead != "zfs.read" {
		t.Fatal("zfs.read must be a known scope")
	}
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeZFSRead || strings.HasPrefix(s, "zfs.") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	var others []string
	for s := range KnownScopes {
		if s != ScopeZFSRead {
			others = append(others, s)
		}
	}
	if (&Session{Scopes: others}).HasScope(ScopeZFSRead) {
		t.Fatal("every other scope together must not imply zfs.read")
	}
}

// M7 first checkpoint: read-only LVM needs lvm.read, never granted by
// default and implied by no other scope (including storage.read).
func TestLVMReadNotGrantedByDefaultOrImplied(t *testing.T) {
	if !KnownScopes[ScopeLVMRead] || ScopeLVMRead != "lvm.read" {
		t.Fatal("lvm.read must be a known scope")
	}
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeLVMRead || strings.HasPrefix(s, "lvm.") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	var others []string
	for s := range KnownScopes {
		if s != ScopeLVMRead {
			others = append(others, s)
		}
	}
	if (&Session{Scopes: others}).HasScope(ScopeLVMRead) {
		t.Fatal("every other scope together must not imply lvm.read")
	}
}

// M7 first checkpoint: read-only Btrfs needs btrfs.read, never granted by
// default and implied by no other scope (including storage.read).
func TestBtrfsReadNotGrantedByDefaultOrImplied(t *testing.T) {
	if !KnownScopes[ScopeBtrfsRead] || ScopeBtrfsRead != "btrfs.read" {
		t.Fatal("btrfs.read must be a known scope")
	}
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeBtrfsRead || strings.HasPrefix(s, "btrfs.") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	var others []string
	for s := range KnownScopes {
		if s != ScopeBtrfsRead {
			others = append(others, s)
		}
	}
	if (&Session{Scopes: others}).HasScope(ScopeBtrfsRead) {
		t.Fatal("every other scope together must not imply btrfs.read")
	}
}
