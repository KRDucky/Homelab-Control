// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/modules"
)

func m23Fake() *fakeBroker {
	fb := opsFake()
	fb.results["broker.info"] = map[string]any{"version": "t", "systemd_booted": true, "writes_available": true,
		"exec_available": []string{"systemctl", "journalctl"}, "ops": []map[string]string{{"name": "broker.info"},
			{"name": "services.systemd.list"}, {"name": "services.unit.logs"}, {"name": "services.unit.plan"}, {"name": "services.unit.apply"},
			{"name": "services.enablement.plan"}, {"name": "services.enablement.apply"}, {"name": "services.unit.detail"}}}
	fb.results["services.enablement.plan"] = map[string]any{"unit": "nginx.service", "requested": "nginx.service", "action": "enable",
		"risk": "disruptive", "step_up_required": true, "token": validToken, "expires_at": time.Now().Add(2 * time.Minute),
		"observed":   map[string]any{"id": "nginx.service", "load_state": "loaded", "active_state": "inactive", "sub_state": "dead", "unit_file_state": "disabled"},
		"boot_state": "disabled", "effect": "Enable nginx.service at boot.", "consequences": []string{"no automatic rollback"},
		"links": []string{"/etc/systemd/system/multi-user.target.wants/nginx.service"}}
	fb.results["services.enablement.apply"] = map[string]any{"operation_id": "op2", "unit": "nginx.service", "action": "enable",
		"outcome": "succeeded", "message": "The service is enabled at boot.", "boot_state_before": "disabled", "boot_state": "enabled",
		"links":      []map[string]any{{"path": "/etc/systemd/system/multi-user.target.wants/nginx.service", "present": true}},
		"started_at": time.Now(), "finished_at": time.Now()}
	fb.results["services.unit.detail"] = map[string]any{"requested": "sshd.service", "id": "ssh.service", "kind": "alias",
		"in_runtime": true, "names": []string{"ssh.service", "sshd.service"}, "dependencies": map[string]any{}, "source": "t"}
	fb.results["services.unit.contents"] = map[string]any{"unit": "nginx.service", "files": []map[string]string{{"path": "/etc/systemd/system/nginx.service", "text": "Environment=X=[redacted]"}},
		"redactions": 1, "truncated": false, "source": "t"}
	fb.results["services.timers.list"] = map[string]any{"timers": []map[string]any{{"unit": "a.timer", "active_state": "active"}}, "truncated": false, "source": "t"}
	fb.results["services.sockets.list"] = map[string]any{"sockets": []map[string]any{{"unit": "a.socket", "listen": []string{"/run/a"}, "active_state": "active"}}, "truncated": false, "source": "t"}
	return fb
}

var configureScopes = []string{auth.ScopeServicesRead, auth.ScopeServicesConfigure}

const enPlan = "/api/v1/services/units/nginx.service/enablement/plan"
const enApply = "/api/v1/services/units/nginx.service/enablement/apply"

// services.configure is required for enablement; services.operate does not imply it.
func TestEnablementRequiresConfigureScope(t *testing.T) {
	fb := m23Fake()
	h := harnessWith(t, fb, []string{auth.ScopeServicesRead, auth.ScopeServicesOperate, auth.ScopeServicesLogs, auth.ScopeAuditRead})
	for _, p := range []string{enPlan, enApply} {
		if rr := h.post(p, `{"action":"enable","token":"`+validToken+`"}`); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "services.configure") {
			t.Errorf("%s: %d %s", p, rr.Code, rr.Body)
		}
	}
	if len(fb.args) != 0 {
		t.Fatalf("broker called: %v", fb.args)
	}
	rr := h.do("GET", "/api/v1/services", "", nil)
	var sr servicesResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &sr)
	if sr.Configure.Available || !strings.Contains(sr.Configure.Reason, "services.configure") || !sr.Operate.Available {
		t.Fatalf("configure: %+v operate: %+v", sr.Configure, sr.Operate)
	}
	if !strings.Contains(h.audit.String(), `"event":"authz.denied"`) {
		t.Fatal("denial not audited")
	}
}

func TestEnablementCSRFOriginAndValidation(t *testing.T) {
	fb := m23Fake()
	h := harnessWith(t, fb, configureScopes)
	body := `{"action":"enable","token":"` + validToken + `"}`
	for _, p := range []string{enPlan, enApply} {
		if rr := h.do("POST", p, body, map[string]string{"Content-Type": "application/json", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden {
			t.Errorf("%s without Origin: %d", p, rr.Code)
		}
		if rr := h.do("POST", p, body, map[string]string{"Content-Type": "application/json", "Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden {
			t.Errorf("%s cross-origin: %d", p, rr.Code)
		}
		if rr := h.do("POST", p, body, map[string]string{"Content-Type": "application/json", "Origin": testOrigin}); rr.Code != http.StatusForbidden {
			t.Errorf("%s without CSRF: %d", p, rr.Code)
		}
	}
	for _, c := range []struct{ unit, body string }{
		{"nginx.socket", `{"action":"enable"}`}, {"getty@tty1.service", `{"action":"enable"}`}, {"foo@.service", `{"action":"enable"}`},
		{"-x.service", `{"action":"enable"}`}, {"nginx.service", `{"action":"mask"}`}, {"nginx.service", `{"action":"start"}`},
		{"nginx.service", `{"action":"enable","now":true}`}, {"nginx.service", `{"action":"enable"}{}`}, {"a%3Bb.service", `{"action":"enable"}`},
	} {
		if rr := h.post("/api/v1/services/units/"+c.unit+"/enablement/plan", c.body); rr.Code != http.StatusBadRequest {
			t.Errorf("plan %s %s: %d", c.unit, c.body, rr.Code)
		}
	}
	if rr := h.post("/api/v1/services/units/sshd.service/enablement/plan", `{"action":"disable"}`); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "policy_denied") {
		t.Errorf("protected: %d %s", rr.Code, rr.Body)
	}
	for _, tok := range []string{"", "x", strings.Repeat("A", 3000)} {
		b, _ := json.Marshal(map[string]string{"action": "enable", "token": tok})
		if rr := h.post(enApply, string(b)); rr.Code != http.StatusBadRequest {
			t.Errorf("token %.10q: %d", tok, rr.Code)
		}
	}
	// The start/stop endpoint still refuses enablement verbs.
	if rr := h.post("/api/v1/services/units/nginx.service/plan", `{"action":"enable"}`); rr.Code != http.StatusForbidden && rr.Code != http.StatusBadRequest {
		t.Errorf("start/stop route accepted enable: %d", rr.Code)
	}
	if len(fb.args) != 0 {
		t.Fatalf("broker reached: %v", fb.args)
	}
}

// Boot-time changes always need a step-up, audited before the broker runs.
func TestEnablementApplyAlwaysNeedsStepUp(t *testing.T) {
	fb := m23Fake()
	h := harnessWith(t, fb, configureScopes)
	if rr := h.post(enPlan, `{"action":"enable"}`); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"boot_state":"disabled"`) {
		t.Fatalf("plan: %d %s", rr.Code, rr.Body)
	}
	body := `{"action":"enable","token":"` + validToken + `"}`
	if rr := h.post(enApply, body); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "step_up_required") {
		t.Fatalf("without step-up: %d %s", rr.Code, rr.Body)
	}
	if h.stepUp(fullPW) != 200 {
		t.Fatal("step-up")
	}
	rr := h.post(enApply, body)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"boot_state":"enabled"`) {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body)
	}
	if a := fb.actors[len(fb.actors)-1]; a.StepUpAt == 0 {
		t.Fatal("step-up time not forwarded")
	}
	logs := h.audit.String()
	if strings.Contains(logs, validToken) || strings.Contains(logs, fullPW) {
		t.Fatal("secret in audit")
	}
	var events []string
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var e audit.Event
		if json.Unmarshal([]byte(line), &e) == nil {
			events = append(events, e.Event+"/"+e.Outcome)
		}
	}
	got := strings.Join(events, " ")
	for _, want := range []string{"services.enablement.plan/success", "services.enablement.apply/step_up_required",
		"services.enablement.apply/requested", "services.enablement.apply/succeeded"} {
		if !strings.Contains(got, want) {
			t.Errorf("audit lacks %s: %s", want, got)
		}
	}
	// Broker refusals map to clear statuses.
	for c, want := range map[string]int{proto.CodeInvalidToken: 409, proto.CodeConflict: 409, proto.CodeBusy: 409, proto.CodePolicyDenied: 403, proto.CodeStepUpRequired: 403} {
		fb.errs["services.enablement.apply"] = &proto.Error{Code: c, Message: "x"}
		if rr := h.post(enApply, body); rr.Code != want {
			t.Errorf("%s → %d, want %d", c, rr.Code, want)
		}
	}
}

// Nothing is executed when the request cannot be audited first.
func TestEnablementFailsClosedWithoutAudit(t *testing.T) {
	fb := m23Fake()
	h := harnessWith(t, fb, configureScopes)
	if h.stepUp(fullPW) != 200 {
		t.Fatal("step-up")
	}
	h.srv.audit = audit.New(brokenWriter{})
	if rr := h.post(enApply, `{"action":"enable","token":"`+validToken+`"}`); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("apply without audit: %d", rr.Code)
	}
	if rr := h.post(enPlan, `{"action":"enable"}`); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), validToken) {
		t.Fatalf("plan without audit released a token: %d %s", rr.Code, rr.Body)
	}
	for _, a := range fb.args {
		if m, ok := a.(map[string]string); ok && m["token"] != "" {
			t.Fatal("apply reached the broker without an audit record")
		}
	}
}

// Unit-file contents need services.files, which no other scope implies
// (owner authorization 2026-11-05): every other scope together is refused,
// before the broker is asked and without an audit record of a read.
func TestUnitContentsNeedFilesScopeAndAudit(t *testing.T) {
	fb := m23Fake()
	others := []string{auth.ScopeSystemRead, auth.ScopeServicesRead, auth.ScopeHardwareRead, auth.ScopeServicesLogs,
		auth.ScopeAuditRead, auth.ScopeServicesOperate, auth.ScopeServicesConfigure}
	for _, set := range [][]string{{auth.ScopeServicesRead}, {auth.ScopeServicesLogs}, {auth.ScopeServicesOperate},
		{auth.ScopeServicesConfigure}, others} {
		h := harnessWith(t, fb, set)
		rr := h.do("GET", "/api/v1/services/units/nginx.service/contents", "", nil)
		if rr.Code != http.StatusForbidden || strings.Contains(rr.Body.String(), "Environment") ||
			strings.Contains(h.audit.String(), `"event":"services.unit.contents.read"`) {
			t.Fatalf("contents with %v: %d %s / %s", set, rr.Code, rr.Body, h.audit)
		}
	}
	if len(fb.args) != 0 {
		t.Fatalf("broker asked for contents without services.files: %v", fb.args)
	}
	h := harnessWith(t, fb, []string{auth.ScopeServicesRead, auth.ScopeServicesFiles})
	rr := h.do("GET", "/api/v1/services/units/nginx.service/contents", "", nil)
	if rr.Code != 200 || !strings.Contains(h.audit.String(), `"event":"services.unit.contents.read"`) || strings.Contains(h.audit.String(), "redacted") {
		t.Fatalf("%d %s / %s", rr.Code, rr.Body, h.audit)
	}
	h.srv.audit = audit.New(brokenWriter{})
	if rr := h.do("GET", "/api/v1/services/units/nginx.service/contents", "", nil); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "Environment") {
		t.Fatalf("contents released without audit: %d %s", rr.Code, rr.Body)
	}
	for _, bad := range []string{"x.device", "-x.service", "a%2Fb.service", "x.service%0A"} {
		if rr := h.do("GET", "/api/v1/services/units/"+bad+"/contents", "", nil); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rr.Code)
		}
	}
}

func TestReadViewsAndDetailProtectionLabel(t *testing.T) {
	fb := m23Fake()
	h := harnessWith(t, fb, []string{auth.ScopeServicesRead})
	for _, p := range []string{"/api/v1/services/timers", "/api/v1/services/sockets", "/api/v1/services/units/sshd.service", "/api/v1/services/units/getty@.service"} {
		if rr := h.do("GET", p, "", nil); rr.Code != 200 {
			t.Errorf("%s: %d %s", p, rr.Code, rr.Body)
		}
	}
	rr := h.do("GET", "/api/v1/services/units/sshd.service", "", nil)
	if !strings.Contains(rr.Body.String(), `"protected":{"protected":true`) {
		t.Fatalf("alias of a protected unit not labelled: %s", rr.Body)
	}
	h2 := harnessWith(t, fb, []string{auth.ScopeSystemRead})
	for _, p := range []string{"/api/v1/services/timers", "/api/v1/services/sockets", "/api/v1/services/units/a.service"} {
		if rr := h2.do("GET", p, "", nil); rr.Code != http.StatusForbidden {
			t.Errorf("%s without services.read: %d", p, rr.Code)
		}
	}
	fb.errs["services.unit.detail"] = &proto.Error{Code: proto.CodeNotFound, Message: "not in inventory"}
	if rr := h.do("GET", "/api/v1/services/units/nope.service", "", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("missing unit: %d", rr.Code)
	}
}

// D-10 registry: a disabled module's routes are indistinguishable from
// unknown routes (before authentication), its capabilities disappear and the
// module is listed as disabled.
func TestDisabledModuleRoutes404(t *testing.T) {
	fb := m23Fake()
	h := harnessWith(t, fb, []string{auth.ScopeSystemRead, auth.ScopeServicesRead, auth.ScopeServicesLogs, auth.ScopeServicesOperate, auth.ScopeServicesConfigure})
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["services"]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithModules(set)
	unknown := h.do("GET", "/api/v1/no-such-route", "", nil)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/services"}, {"GET", "/api/v1/services/timers"}, {"GET", "/api/v1/services/sockets"},
		{"GET", "/api/v1/services/units/nginx.service"}, {"GET", "/api/v1/services/units/nginx.service/logs"},
		{"GET", "/api/v1/services/units/nginx.service/contents"}, {"POST", "/api/v1/services/units/nginx.service/plan"},
		{"POST", "/api/v1/services/units/nginx.service/apply"}, {"POST", enPlan}, {"POST", enApply},
	} {
		rr := h.do(c.method, c.path, `{"action":"enable"}`, map[string]string{"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": h.csrf})
		if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
			t.Errorf("%s %s: %d %s", c.method, c.path, rr.Code, rr.Body)
		}
		// Unauthenticated callers get the same answer (no existence oracle).
		saved := h.cookie
		h.cookie = nil
		if rr := h.do(c.method, c.path, "", nil); rr.Code != http.StatusNotFound {
			t.Errorf("unauthenticated %s %s: %d", c.method, c.path, rr.Code)
		}
		h.cookie = saved
	}
	if len(fb.args) != 0 {
		t.Fatalf("broker called for a disabled module: %v", fb.args)
	}
	rr := h.do("GET", "/api/v1/capabilities", "", nil)
	var cr capabilitiesResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &cr)
	for _, c := range cr.Capabilities {
		if c.ID == "services" || c.ID == "service_logs" {
			t.Errorf("capability %s of a disabled module listed", c.ID)
		}
	}
	if len(cr.Modules) != 1 || cr.Modules[0].Enabled || cr.Modules[0].Reason == "" {
		t.Fatalf("modules: %+v", cr.Modules)
	}
	// Core routes are unaffected.
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != 200 {
		t.Fatalf("core route: %d", rr.Code)
	}
	// Re-enabled: routes are back.
	h.srv.WithModules(modules.Set{})
	if rr := h.do("GET", "/api/v1/services/timers", "", nil); rr.Code != 200 {
		t.Fatalf("enabled again: %d", rr.Code)
	}
	if rr := h.do("GET", "/api/v1/capabilities", "", nil); !strings.Contains(rr.Body.String(), `"enabled":true`) {
		t.Fatalf("capabilities: %s", rr.Body)
	}
}

// A broker that refuses a disabled module's op is reported as not found.
func TestBrokerModuleDisabledMapsTo404(t *testing.T) {
	fb := m23Fake()
	fb.errs["services.timers.list"] = &proto.Error{Code: proto.CodeModuleDisabled, Message: "the services module is disabled on this host"}
	h := harnessWith(t, fb, []string{auth.ScopeServicesRead})
	if rr := h.do("GET", "/api/v1/services/timers", "", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("%d", rr.Code)
	}
}
