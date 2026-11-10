// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package kubernetes

import (
	"context"
	"embed"
	"slices"
	"strings"

	"homelab-control/backend/internal/platform"
)

//go:embed testdata/fixture/*.json
var fixtureFS embed.FS

// FixtureNamespaces is the namespace set of the embedded K3s demonstration.
var FixtureNamespaces = []string{"demo"}

// MapSource serves fixed responses for an explicit path allowlist. It is
// used for the labeled fixture and tests; it never performs network access.
type MapSource struct {
	Allowed   []string
	Responses map[string][]byte
}

func (m MapSource) Get(_ context.Context, path string) ([]byte, error) {
	b, ok := m.Responses[path]
	if !slices.Contains(m.Allowed, path) || !ok {
		return nil, platform.ErrUnavailable
	}
	return b, nil
}

// NewFixture returns the labeled K3s demonstration adapter, reading the
// embedded data through the same parser and path allowlist as a live source.
func NewFixture() (*Adapter, error) {
	src := MapSource{Responses: map[string][]byte{}}
	a, err := New(&src, Options{Namespaces: FixtureNamespaces, Entrypoints: []string{"web", "websecure"}, Resolvers: []string{}, Auth: platform.AuthFixture, Transport: "fixture", EndpointLabel: "Demonstration K3s cluster"})
	if err != nil {
		return nil, err
	}
	src.Allowed = a.Paths()
	for _, p := range src.Allowed {
		if b, err := fixtureFS.ReadFile("testdata/fixture/" + strings.ReplaceAll(strings.TrimPrefix(p, "/"), "/", "_") + ".json"); err == nil {
			src.Responses[p] = b
		}
	}
	return a, nil
}
