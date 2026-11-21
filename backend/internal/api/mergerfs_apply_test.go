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
	"homelab-control/backend/internal/modules"
)

func TestMergerFSPlanSeparatesScopesAndAuditsCounts(t *testing.T) {
	body := `{"action":"mount","mountpoint":"/mnt/pool","branches":["/mnt/disk1","/mnt/disk2"],"policy":"epmfs"}`
	h := harnessWith(t, nil, []string{auth.ScopeMergerFSRead})
	if rr := h.post("/api/v1/mergerfs/pool/plan", body); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "mergerfs.configure") {
		t.Fatalf("read is not configure: %d %s", rr.Code, rr.Body)
	}
	h = harnessWith(t, &fakeBroker{errs: map[string]error{}, results: map[string]any{
		"mergerfs.pool.plan": map[string]any{"action": "mount", "risk": "disruptive", "step_up_required": true, "token": validToken, "effect": "Mount 2 existing directories.", "argv": []string{"/usr/bin/mergerfs"}},
	}}, []string{auth.ScopeMergerFSConfigure})
	if rr := h.post("/api/v1/mergerfs/maintain/plan", `{"action":"sync"}`); rr.Code != http.StatusForbidden {
		t.Fatalf("configure is not maintain: %d %s", rr.Code, rr.Body)
	}
	rr := h.post("/api/v1/mergerfs/pool/plan", body)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), validToken) {
		t.Fatalf("plan: %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(h.audit.String(), "/mnt/pool") || strings.Contains(h.audit.String(), "/mnt/disk1") {
		t.Fatalf("audit leaked a path: %s", h.audit)
	}
	if !strings.Contains(h.audit.String(), "branches=2") {
		t.Fatalf("audit: %s", h.audit)
	}
	if rr := h.post("/api/v1/mergerfs/pool/apply", body+`}`); rr.Code == http.StatusOK {
		t.Fatal("malformed apply succeeded")
	}
	apply := `{"action":"mount","mountpoint":"/mnt/pool","branches":["/mnt/disk1","/mnt/disk2"],"policy":"epmfs","token":"` + validToken + `"}`
	if rr := h.post("/api/v1/mergerfs/pool/apply", apply); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "step_up_required") {
		t.Fatalf("apply without step-up: %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(h.audit.String(), "/mnt/disk1") {
		t.Fatalf("apply audit leaked a path: %s", h.audit)
	}
}

func TestMergerFSApplyDisabledModule(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeMergerFSMaintain})
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["mergerfs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithModules(set)
	unknown := h.do("POST", "/api/v1/no-such-route", `{}`, map[string]string{"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": h.csrf})
	rr := h.post("/api/v1/mergerfs/maintain/apply", `{"action":"sync","token":"`+validToken+`"}`)
	if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
		t.Fatalf("disabled: %d %s vs %s", rr.Code, rr.Body, unknown.Body)
	}
}

func TestMergerFSMaintainPlanRefusesFixEscape(t *testing.T) {
	h := harnessWith(t, &fakeBroker{errs: map[string]error{
		"mergerfs.maintain.apply": &proto.Error{Code: proto.CodePolicyDenied, Message: "refused"},
	}, results: map[string]any{}}, []string{auth.ScopeMergerFSMaintain})
	if rr := h.post("/api/v1/mergerfs/maintain/plan", `{"action":"fix","files":["/etc/passwd"]}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("absolute recovery path: %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(h.audit.String(), "/etc/passwd") {
		t.Fatalf("audit leaked the rejected path: %s", h.audit)
	}
}
