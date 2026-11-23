// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"net/http"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/brokerclient"
)

func TestExt4FormatRequiresItsOwnScope(t *testing.T) {
	fb := &fakeBroker{errs: map[string]error{}, results: map[string]any{}}
	h := harnessWith(t, fb, []string{auth.ScopeExt4Read})
	body := `{"by_id":"/dev/disk/by-id/virtio-mmvtestdisk01","identity_kind":"wwn","identity_value":"naa.5000cca252c8d2a1","confirm":["format"]}`
	rr := h.post("/api/v1/ext4/format/plan", body)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "ext4.format") {
		t.Fatalf("read scope: %d %s", rr.Code, rr.Body)
	}
	if len(fb.args) != 0 {
		t.Fatal("broker called without ext4.format")
	}
}

func TestExt4FormatPlanAuditsKindOnly(t *testing.T) {
	h := harnessWith(t, &fakeBroker{errs: map[string]error{}, results: map[string]any{
		"ext4.format.plan": map[string]any{"action": "format", "risk": "destructive", "step_up_required": true, "confirmation_required": true, "token": "hcp1.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "expires_at": "2026-11-23T00:00:00Z", "identity_kind": "wwn", "rdev": "254:0", "effect": "format", "consequences": []string{}, "warnings": []string{}},
	}}, []string{auth.ScopeExt4Format})
	body := `{"by_id":"/dev/disk/by-id/virtio-mmvtestdisk01","identity_kind":"wwn","identity_value":"naa.5000cca252c8d2a1","confirm":["format"]}`
	rr := h.post("/api/v1/ext4/format/plan", body)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"destructive"`) {
		t.Fatalf("plan: %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(h.audit.String(), "virtio") || strings.Contains(h.audit.String(), "naa.") {
		t.Fatalf("audit leaked a path or identity: %s", h.audit.String())
	}
	if !strings.Contains(h.audit.String(), "kind=wwn") {
		t.Fatalf("audit: %s", h.audit.String())
	}
}

func TestExt4FormatApplyUnknownOutcome(t *testing.T) {
	h := harnessWith(t, &fakeBroker{errs: map[string]error{"ext4.format.apply": brokerclient.ErrUnavailable}, results: map[string]any{}}, []string{auth.ScopeExt4Format})
	body := `{"by_id":"/dev/disk/by-id/virtio-mmvtestdisk01","identity_kind":"wwn","identity_value":"naa.5000cca252c8d2a1","confirm":["format"],"token":"hcp1.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`
	rr := h.post("/api/v1/ext4/format/apply", body)
	if rr.Code != http.StatusBadGateway || !strings.Contains(rr.Body.String(), "outcome_unknown") {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(h.audit.String(), "virtio") {
		t.Fatalf("audit leaked a path: %s", h.audit.String())
	}
}

func TestExt4FormatApplyMapsStepUp(t *testing.T) {
	h := harnessWith(t, &fakeBroker{errs: map[string]error{
		"ext4.format.apply": &proto.Error{Code: proto.CodeStepUpRequired, Message: "re-enter your password to confirm this format"},
	}, results: map[string]any{}}, []string{auth.ScopeExt4Format})
	body := `{"by_id":"/dev/disk/by-id/virtio-mmvtestdisk01","identity_kind":"wwn","identity_value":"naa.5000cca252c8d2a1","confirm":["format"],"token":"hcp1.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`
	rr := h.post("/api/v1/ext4/format/apply", body)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "step_up_required") {
		t.Fatalf("step-up: %d %s", rr.Code, rr.Body)
	}
}
