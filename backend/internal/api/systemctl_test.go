// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"net/http"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
)

func TestSystemctlScopeIsNotImplied(t *testing.T) {
	fb := opsFake()
	h := harnessWith(t, fb, []string{auth.ScopeServicesRead, auth.ScopeServicesOperate, auth.ScopeServicesConfigure, auth.ScopeServicesFiles})
	body := `{"action":"reboot","token":"` + validToken + `"}`
	for _, p := range []string{"/api/v1/services/systemctl/plan", "/api/v1/services/systemctl/apply"} {
		if rr := h.post(p, body); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "services.systemctl") {
			t.Errorf("%s: %d %s", p, rr.Code, rr.Body)
		}
	}
	if len(fb.args) != 0 {
		t.Fatalf("broker called without services.systemctl: %v", fb.args)
	}
	h = harnessWith(t, fb, []string{auth.ScopeServicesSystemctl})
	if rr := h.post("/api/v1/services/systemctl/plan", `{"action":"mask","unit":"ssh.service"}`); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "protected") {
		t.Fatalf("ssh: %d %s", rr.Code, rr.Body)
	}
	if len(fb.args) != 0 {
		t.Fatal("broker called for a protected unit")
	}
	if rr := h.post("/api/v1/services/systemctl/apply", `{"action":"daemon-reload","token":"`+validToken+`"}`); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "step_up_required") {
		t.Fatalf("step-up: %d %s", rr.Code, rr.Body)
	}
	if len(fb.args) != 0 {
		t.Fatal("broker called without step-up")
	}
}
