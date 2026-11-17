// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/storageinfo"
)

// probeHarness wires a fake broker and a fake sysfs collector so the probe
// handler is exercised without host access.
func probeHarness(t *testing.T, scopes ...string) (*harness, *fakeBroker) {
	fb := newFake()
	h := harnessWith(t, fb, scopes)
	h.srv.storageCollect = func(ctx context.Context) (storageinfo.Graph, error) {
		return storageinfo.Graph{
			Nodes:  []storageinfo.Node{{ID: "block:0800", Kind: "block", Name: "sda", DeviceNumber: "8:0"}},
			Edges:  []storageinfo.Edge{},
			Claims: []storageinfo.Claim{},
			Status: "ok",
			Notes:  []string{},
		}, nil
	}
	return h, fb
}

func TestStorageProbeRejectsNonEmptyBody(t *testing.T) {
	h, fb := probeHarness(t, auth.ScopeStorageProbe)
	rr := h.do("POST", "/api/v1/storage/probe", "x", map[string]string{"X-CSRF-Token": h.csrf, "Origin": testOrigin})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	if len(fb.actors) != 0 {
		t.Fatalf("broker called on non-empty body: %d", len(fb.actors))
	}
}

func TestStorageProbeRequiresScope(t *testing.T) {
	fb := newFake()
	h := harnessWith(t, fb, []string{auth.ScopeSystemRead}) // no storage.probe
	h.srv.storageCollect = func(ctx context.Context) (storageinfo.Graph, error) { return storageinfo.Graph{}, nil }
	rr := h.do("POST", "/api/v1/storage/probe", "", map[string]string{"X-CSRF-Token": h.csrf, "Origin": testOrigin})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	if len(fb.actors) != 0 {
		t.Fatal("broker called without storage.probe scope")
	}
}

func TestStorageProbeRequiresCSRF(t *testing.T) {
	h, fb := probeHarness(t, auth.ScopeStorageProbe)
	rr := h.do("POST", "/api/v1/storage/probe", "", map[string]string{"X-CSRF-Token": "bad", "Origin": testOrigin})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	if len(fb.actors) != 0 {
		t.Fatal("broker called with bad CSRF")
	}
}

func TestStorageGETDoesNotProbe(t *testing.T) {
	h, fb := probeHarness(t, auth.ScopeStorageProbe, auth.ScopeStorageRead)
	rr := h.do("GET", "/api/v1/storage", "", map[string]string{"X-CSRF-Token": h.csrf, "Origin": testOrigin})
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	if len(fb.actors) != 0 {
		t.Fatalf("GET /storage invoked the probe broker op (%d calls)", len(fb.actors))
	}
}

func TestStorageProbeSuccess(t *testing.T) {
	h, fb := probeHarness(t, auth.ScopeStorageProbe)
	fb.results["storage.probe"] = storageProbeResult{
		Version: 1,
		Blkid:   map[string]string{"sda": "TYPE=ext4\nUUID=abc123"},
	}
	rr := h.do("POST", "/api/v1/storage/probe", "", map[string]string{"X-CSRF-Token": h.csrf, "Origin": testOrigin})
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"graph"`) || !strings.Contains(body, `"signature"`) {
		t.Fatalf("probe response did not include the graph/signature: %s", body)
	}
}

func TestApplyProbeOutcomePreservesWorseStatus(t *testing.T) {
	// A successful probe must not erase a pre-existing partial base graph.
	g := storageinfo.Graph{Status: "partial", Notes: []string{}}
	applyProbeOutcome(&g, storageProbeResult{Version: 1, Code: "", Partial: false})
	if g.Status != "partial" {
		t.Fatalf("ok probe erased partial base status: %q", g.Status)
	}

	// A partial probe on an ok base becomes partial and appends notes.
	g2 := storageinfo.Graph{Status: "ok"}
	applyProbeOutcome(&g2, storageProbeResult{Version: 1, Code: "", Partial: true, Errors: []string{"e1"}})
	if g2.Status != "partial" {
		t.Fatalf("partial probe did not set partial: %q", g2.Status)
	}
	if len(g2.Notes) != 1 || g2.Notes[0] != "e1" {
		t.Fatalf("notes not appended: %v", g2.Notes)
	}

	// device_policy_unresolved is unavailable regardless of the base.
	g3 := storageinfo.Graph{Status: "ok"}
	applyProbeOutcome(&g3, storageProbeResult{Version: 1, Code: "device_policy_unresolved", Partial: true})
	if g3.Status != "unavailable" {
		t.Fatalf("refusal did not set unavailable: %q", g3.Status)
	}
}
