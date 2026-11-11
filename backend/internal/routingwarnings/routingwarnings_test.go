// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package routingwarnings

import (
	"context"
	"strings"
	"testing"

	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/traefikclient"
)

func build(t *testing.T, routers []traefikclient.Resource) []composediscovery.Issue {
	t.Helper()
	s, _ := composediscovery.Fixture{}.Snapshot(context.Background())
	p, err := composediscovery.Build(s, traefikclient.Inventory{Routers: routers}, composediscovery.Request{ServiceID: "fixture-notes-web", Network: "hc-traefik-test",
		Model: traefikclient.Model{Name: "notes", Host: "notes.example.com", Port: 8080, Entrypoint: "web", Protocol: "http", UpstreamScheme: "http"}})
	if err != nil {
		t.Fatal(err)
	}
	return p.Warnings
}

func byCode(ws []composediscovery.Issue) map[string][]string {
	m := map[string][]string{}
	for _, w := range ws {
		m[w.Code] = append(m[w.Code], w.Message)
	}
	return m
}

// The live Phase 7 test environment has three rule-less routers; Build reports
// the same warning three times. Consolidation keeps one, naming the routers.
func TestConsolidateRepeatedRouterWarnings(t *testing.T) {
	routers := []traefikclient.Resource{
		{Name: "ping@internal", Provider: "internal"},
		{Name: "fixture-router@file", Provider: "file"},
		{Name: "hc-api@file", Provider: "file"},
		{Name: "complex@file", Provider: "file", Rule: "PathPrefix(`/x`)"},
		{Name: "api@file", Provider: "file", Rule: "PathPrefix(`/api`)"},
		{Name: "other@file", Provider: "file", Rule: "Host(`a.example.com`) || Host(`b.example.com`)"},
		{Name: "plain@file", Provider: "file", Rule: "Host(`plain.example.com`)"},
		{Name: "app@file", Provider: "file", Rule: "Host(`app.isolated.test`) && PathPrefix(`/app`)"},
	}
	raw := build(t, routers)
	if n := len(byCode(raw)[CodeHostUnverified]); n != 3 {
		t.Fatalf("precondition: Build should repeat host_unverified per router, got %d", n)
	}
	if n := len(byCode(raw)[CodeComplexRule]); n != 2 {
		t.Fatalf("precondition: Build should repeat complex_rule per router, got %d", n)
	}
	before := strings.Join(byCode(raw)[CodeHostUnverified], "|")
	got := Consolidate(raw, traefikclient.Inventory{Routers: routers})
	if strings.Join(byCode(raw)[CodeHostUnverified], "|") != before {
		t.Fatal("input modified")
	}
	m := byCode(got)
	if len(m[CodeHostUnverified]) != 1 || m[CodeHostUnverified][0] != "3 live routers (fixture-router@file, hc-api@file, ping@internal) lack routing rules; hostname conflicts cannot be fully verified." {
		t.Fatalf("%q", m[CodeHostUnverified])
	}
	if len(m[CodeComplexRule]) != 1 || !strings.HasPrefix(m[CodeComplexRule][0], "2 live routers (api@file, complex@file) have routing rules that this single-host model cannot compare") {
		t.Fatalf("%q", m[CodeComplexRule])
	}
	// Every other warning is kept, in order.
	var rawOther, gotOther []string
	for _, w := range raw {
		if w.Code != CodeHostUnverified && w.Code != CodeComplexRule {
			rawOther = append(rawOther, w.Code)
		}
	}
	for _, w := range got {
		if w.Code != CodeHostUnverified && w.Code != CodeComplexRule {
			gotOther = append(gotOther, w.Code)
		}
	}
	if strings.Join(rawOther, ",") != strings.Join(gotOther, ",") {
		t.Fatal(rawOther, gotOther)
	}
}

func TestConsolidateEdgeCases(t *testing.T) {
	// No affected routers: warnings are unchanged.
	plain := []traefikclient.Resource{{Name: "x@file", Provider: "file", Rule: "Host(`x.example.com`)"}}
	raw := build(t, plain)
	if got := Consolidate(raw, traefikclient.Inventory{Routers: plain}); len(got) != len(raw) {
		t.Fatal(raw, got)
	}
	// One router: singular form.
	one := []traefikclient.Resource{{Name: "ping@internal", Provider: "internal"}}
	got := byCode(Consolidate(build(t, one), traefikclient.Inventory{Routers: one}))
	if got[CodeHostUnverified][0] != "1 live router (ping@internal) lacks a routing rule; hostname conflicts cannot be fully verified." {
		t.Fatal(got)
	}
	// Bounded list.
	if s := List([]string{"g", "f", "e", "d", "c", "b", "a", "a"}, "x", "y"); s != "7 live routers (a, b, c, d, e and 2 more) y" {
		t.Fatal(s)
	}
	// Exact duplicates of other warnings are dropped; inventory mismatch keeps one original message.
	in := []composediscovery.Issue{{Code: "a", Message: "m"}, {Code: "a", Message: "m"}, {Code: CodeHostUnverified, Message: "orig"}, {Code: CodeHostUnverified, Message: "orig"}}
	out := Consolidate(in, traefikclient.Inventory{})
	if len(out) != 2 || out[1].Message != "orig" {
		t.Fatal(out)
	}
	if Consolidate(nil, traefikclient.Inventory{}) == nil {
		t.Fatal("nil result")
	}
}
