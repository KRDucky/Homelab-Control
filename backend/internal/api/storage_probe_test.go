// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/storageinfo"
	"homelab-control/backend/internal/storagetools"
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
			Notes:  []string{storageinfo.NoteSignaturesNotProbed},
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

func TestStorageProbeBusyClosesAudit(t *testing.T) {
	h, fb := probeHarness(t, auth.ScopeStorageProbe)
	h.srv.storageSem <- struct{}{}
	t.Cleanup(func() { <-h.srv.storageSem })
	rr := h.do("POST", "/api/v1/storage/probe", "", map[string]string{"X-CSRF-Token": h.csrf, "Origin": testOrigin})
	if rr.Code != http.StatusConflict {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	if len(fb.actors) != 0 {
		t.Fatal("broker called while the storage slot was busy")
	}
	log := h.audit.String()
	requested := strings.Index(log, `"event":"storage.probe"`)
	if requested < 0 || !strings.Contains(log[requested:], `"outcome":"requested"`) {
		t.Fatalf("busy probe missing requested audit: %s", log)
	}
	conflict := strings.Index(log[requested:], `"outcome":"conflict"`)
	if conflict < 0 {
		t.Fatalf("busy probe left the audit open: %s", log)
	}
}

// auditWriteFailSecond simulates losing the audit sink between the requested
// entry and the closing conflict entry.
type auditWriteFailSecond struct{ writes int }

func (w *auditWriteFailSecond) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 2 {
		return 0, errors.New("audit sink unavailable")
	}
	return len(p), nil
}

func TestStorageProbeBusyAuditFailureFailsClosed(t *testing.T) {
	h, fb := probeHarness(t, auth.ScopeStorageProbe)
	h.srv.audit = audit.New(&auditWriteFailSecond{})
	h.srv.storageSem <- struct{}{}
	t.Cleanup(func() { <-h.srv.storageSem })
	rr := h.do("POST", "/api/v1/storage/probe", "", map[string]string{"X-CSRF-Token": h.csrf, "Origin": testOrigin})
	if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "audit_unavailable") {
		t.Fatalf("got %d %s; want audited failure", rr.Code, rr.Body)
	}
	if len(fb.actors) != 0 {
		t.Fatal("broker called while the storage slot was busy")
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
	passed := true
	fb.results["storage.probe"] = storageProbeResult{
		Version: 2,
		Blocks:  []storagetools.BlockDevice{{Name: "sda", KName: "sda", Type: "disk", FSType: "ext4", UUID: "abc123"}},
		SMART:   map[string]storagetools.SMART{"sda": {Protocol: "ATA", Passed: &passed}},
	}
	rr := h.do("POST", "/api/v1/storage/probe", "", map[string]string{"X-CSRF-Token": h.csrf, "Origin": testOrigin})
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	body := rr.Body.String()
	for _, want := range []string{`"graph"`, `"signature"`, `"source":"lsblk"`, `"smart_health":"ok"`, `"protocol":"ATA"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("probe response missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, storageinfo.NoteSignaturesNotProbed) {
		t.Fatalf("stale 'signatures not probed' note kept after lsblk supplied them: %s", body)
	}
}

func TestApplyProbeOutcomePreservesWorseStatus(t *testing.T) {
	// A successful probe must not erase a pre-existing partial base graph.
	g := storageinfo.Graph{Status: "partial", Notes: []string{}}
	applyProbeOutcome(&g, storageProbeResult{Version: 2, Code: "", Partial: false})
	if g.Status != "partial" {
		t.Fatalf("ok probe erased partial base status: %q", g.Status)
	}

	// A partial probe on an ok base becomes partial and appends notes.
	g2 := storageinfo.Graph{Status: "ok"}
	applyProbeOutcome(&g2, storageProbeResult{Version: 2, Code: "", Partial: true, Errors: []string{"e1"}})
	if g2.Status != "partial" {
		t.Fatalf("partial probe did not set partial: %q", g2.Status)
	}
	if len(g2.Notes) != 1 || g2.Notes[0] != "e1" {
		t.Fatalf("notes not appended: %v", g2.Notes)
	}

	// device_policy_unresolved is unavailable regardless of the base.
	g3 := storageinfo.Graph{Status: "ok"}
	applyProbeOutcome(&g3, storageProbeResult{Version: 2, Code: "device_policy_unresolved", Partial: true})
	if g3.Status != "unavailable" {
		t.Fatalf("refusal did not set unavailable: %q", g3.Status)
	}
}

func TestStorageProbeRefusalIsUnavailable(t *testing.T) {
	h, fb := probeHarness(t, auth.ScopeStorageProbe)
	fb.results["storage.probe"] = storageProbeResult{Version: 2, Code: "device_policy_unresolved", Partial: true, Errors: []string{"probe refused: block device classes not available: md"}}
	rr := h.do("POST", "/api/v1/storage/probe", "", map[string]string{"X-CSRF-Token": h.csrf, "Origin": testOrigin})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "storage_probe_unavailable") {
		t.Fatalf("missing storage_probe_unavailable code: %s", rr.Body)
	}
}

func TestStorageProbeRateLimited(t *testing.T) {
	h, fb := probeHarness(t, auth.ScopeStorageProbe)
	fb.errs["storage.probe"] = &proto.Error{Code: proto.CodeRateLimited, Message: "storage probe rate-limited; retry shortly"}
	rr := h.do("POST", "/api/v1/storage/probe", "", map[string]string{"X-CSRF-Token": h.csrf, "Origin": testOrigin})
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	if rr.Header().Get("Retry-After") != "30" {
		t.Fatalf("Retry-After = %q, want 30", rr.Header().Get("Retry-After"))
	}
}

const (
	probeVG = "aaaaaa-bbbb-cccc-dddd-eeee-ffff-000001"
	probePV = "abcdef-1234-5678-90ab-cdef-1234-567890"
	probeLV = "999999-9988-8877-7766-6655-5544-444444"
)

const probeLVS = `{"report":[{"seg":[{"lv_uuid":"` + probeLV + `","lv_name":"lv1","vg_uuid":"` + probeVG + `","vg_name":"vg1","segtype":"linear","seg_start":"0","seg_size":"536870912","devices":"/dev/sda(0)"}]}]}`

const probePVS = `{"report":[{"pvseg":[{"pv_uuid":"` + probePV + `","pv_name":"/dev/sda","vg_uuid":"` + probeVG + `","vg_name":"vg1","pv_size":"1073741824","dev_size":"1073741824","pv_major":"8","pv_minor":"0","pv_attr":"a--","pv_missing":"","pvseg_start":"0","pvseg_size":"128","lv_uuid":"` + probeLV + `"}]}]}`

func TestStorageProbeLVMGraph(t *testing.T) {
	h, fb := probeHarness(t, auth.ScopeStorageProbe)
	fb.results["storage.probe"] = storageProbeResult{Version: 2, PVS: json.RawMessage(probePVS), LVS: json.RawMessage(probeLVS)}
	rr := h.do("POST", "/api/v1/storage/probe", "", map[string]string{"X-CSRF-Token": h.csrf, "Origin": testOrigin})
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	body := rr.Body.String()
	for _, want := range []string{"lvm-vg", "lvm-lv", "lvm-pv", `"mode":"metadata-only"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("LVM graph missing %q: %s", want, body)
		}
	}
}

func TestStorageProbeBadLVMReportIsPartial(t *testing.T) {
	h, fb := probeHarness(t, auth.ScopeStorageProbe)
	fb.results["storage.probe"] = storageProbeResult{Version: 2, PVS: json.RawMessage(probePVS), LVS: json.RawMessage(`{"report":[{"seg":[{"lv_name":"no-uuid"}]}]}`)}
	rr := h.do("POST", "/api/v1/storage/probe", "", map[string]string{"X-CSRF-Token": h.csrf, "Origin": testOrigin})
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"status":"partial"`) || !strings.Contains(body, "LVM report could not be read") {
		t.Fatalf("bad LVM report not reported: %s", body)
	}
}
