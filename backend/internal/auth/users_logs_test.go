// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"strings"
	"testing"
)

// Owner decision D-7: log access is never granted automatically, neither to
// new accounts by default nor to existing accounts on upgrade.
func TestServicesLogsNotGrantedByDefault(t *testing.T) {
	if !KnownScopes[ScopeServicesLogs] {
		t.Fatal("services.logs must be a known scope")
	}
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeServicesLogs || strings.HasSuffix(s, ".logs") || strings.HasPrefix(s, "audit.") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
}

// M2-3: persistent service configuration (boot-time enable/disable) is never
// granted by default and is not implied by services.operate.
func TestServicesConfigureNotGrantedByDefault(t *testing.T) {
	if !KnownScopes[ScopeServicesConfigure] || ScopeServicesConfigure != "services.configure" {
		t.Fatal("services.configure must be a known scope")
	}
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeServicesConfigure || s == ScopeServicesOperate || strings.HasSuffix(s, ".configure") || strings.HasSuffix(s, ".operate") {
			t.Fatalf("default scopes grant %s", s)
		}
	}
	sess := Session{Scopes: []string{ScopeServicesOperate}}
	if sess.HasScope(ScopeServicesConfigure) {
		t.Fatal("services.operate must not imply services.configure")
	}
}

// M2-3 (owner authorization 2026-11-05): unit-file contents need
// services.files, never granted by default and implied by no other scope.
func TestServicesFilesNotGrantedByDefaultOrImplied(t *testing.T) {
	if !KnownScopes[ScopeServicesFiles] || ScopeServicesFiles != "services.files" {
		t.Fatal("services.files must be a known scope")
	}
	for _, s := range strings.Split(DefaultScopes, ",") {
		if s == ScopeServicesFiles || strings.HasSuffix(s, ".files") {
			t.Fatalf("default scopes grant %s", s)
		}
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
		if s == ScopeFilesRead || strings.HasSuffix(s, ".files") {
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
