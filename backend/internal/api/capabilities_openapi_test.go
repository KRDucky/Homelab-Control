// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The Capability schema in OpenAPI enumerates the capability IDs. Every ID the
// server returns must be in that enum, and the enum must not list removed IDs.
func TestOpenAPICapabilityEnumMatchesServer(t *testing.T) {
	b, err := os.ReadFile("../../../schemas/openapi/homelab-control.v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^    Capability:\n(?:      .*\n)*?        id: \{ type: string, enum: \[([^\]]*)\]`).FindSubmatch(b)
	if m == nil {
		t.Fatal("Capability.id enum not found in OpenAPI")
	}
	var spec []string
	for _, s := range strings.Split(string(m[1]), ",") {
		spec = append(spec, strings.TrimSpace(s))
	}
	got := caps(t, harnessWith(t, newFake(), allScopes))
	for id := range got {
		if !slices.Contains(spec, id) {
			t.Errorf("capability %q is returned by the server but missing from the OpenAPI enum", id)
		}
	}
	for _, id := range spec {
		if _, ok := got[id]; !ok {
			t.Errorf("OpenAPI enum lists capability %q, which the server does not return", id)
		}
	}
}
