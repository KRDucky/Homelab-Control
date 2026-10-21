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
