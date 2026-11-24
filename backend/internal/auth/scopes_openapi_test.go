// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The Session schema in OpenAPI lists every scope a session can carry. A scope
// added to KnownScopes but not to that enum makes valid sessions fail schema
// validation in API clients.
func TestOpenAPISessionScopeEnumMatchesKnownScopes(t *testing.T) {
	b, err := os.ReadFile("../../../schemas/openapi/homelab-control.v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^    Session:\n(?:      .*\n)*?        scopes: \{[^\n]*enum: \[([^\]]*)\]`).FindSubmatch(b)
	if m == nil {
		t.Fatal("Session.scopes enum not found in OpenAPI")
	}
	var spec []string
	for _, s := range strings.Split(string(m[1]), ",") {
		spec = append(spec, strings.TrimSpace(s))
	}
	for s := range KnownScopes {
		if !slices.Contains(spec, s) {
			t.Errorf("scope %q is in KnownScopes but not in the OpenAPI Session.scopes enum", s)
		}
	}
	for _, s := range spec {
		if !KnownScopes[s] {
			t.Errorf("OpenAPI Session.scopes enum lists %q, which is not a known scope", s)
		}
	}
}
