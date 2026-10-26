// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/brokerclient"
)

var validToken = "hcp1.eyJpZCI6IngifQ." + strings.Repeat("A", 43)

func opsFake() *fakeBroker {
	fb := newFake()
	fb.results["broker.info"] = map[string]any{"version": "t", "systemd_booted": true, "writes_available": true,
		"exec_available": []string{"systemctl"}, "ops": []map[string]string{{"name": "broker.info"},
			{"name": "services.systemd.list"}, {"name": "services.unit.plan"}, {"name": "services.unit.apply"}}}
	fb.results["services.systemd.list"] = map[string]any{"counts": map[string]int{"active": 3}, "units": []map[string]string{
		{"name": "ssh.service", "type": "service", "load": "loaded", "active": "active", "sub": "running"},
		{"name": "nginx.service", "type": "service", "load": "loaded", "active": "active", "sub": "running"},
		{"name": "dbus.socket", "type": "socket", "load": "loaded", "active": "active", "sub": "running"},
	}}
	fb.results["services.unit.plan"] = map[string]any{"unit": "nginx.service", "requested": "nginx.service", "action": "stop",
		"risk": "disruptive", "step_up_required": true, "token": validToken, "expires_at": time.Now().Add(2 * time.Minute),
		"observed": map[string]any{"id": "nginx.service", "load_state": "loaded", "active_state": "active", "sub_state": "running"},
		"effect":   "Stop nginx.service", "consequences": []string{"no automatic rollback"}}
	fb.results["services.unit.apply"] = map[string]any{"operation_id": "op1", "unit": "nginx.service", "action": "stop",
		"outcome": "succeeded", "message": "The service is stopped.", "started_at": time.Now(), "finished_at": time.Now(),
		"observed": map[string]any{"id": "nginx.service", "load_state": "loaded", "active_state": "inactive", "sub_state": "dead"}}
	fb.results["operations.journal.list"] = map[string]any{"source": "t", "operations": []map[string]any{
		{"id": "op1", "op": "services.unit", "action": "stop", "unit": "nginx.service", "actor": "full", "risk": "disruptive",
			"outcome": "interrupted", "started_at": time.Now()}}}
	return fb
}

var operateScopes = []string{auth.ScopeServicesRead, auth.ScopeServicesOperate}

// W8: services.operate is required (API side); the broker is never called without it.
func TestServiceOpsRequireOperateScope(t *testing.T) {
	fb := opsFake()
	h := harnessWith(t, fb, []string{auth.ScopeServicesRead, auth.ScopeServicesLogs, auth.ScopeAuditRead})
	for _, p := range []string{"/api/v1/services/units/nginx.service/plan", "/api/v1/services/units/nginx.service/apply"} {
		if rr := h.post(p, `{"action":"stop","token":"`+validToken+`"}`); rr.Code != http.StatusForbidden {
			t.Errorf("%s: %d %s", p, rr.Code, rr.Body)
		}
	}
	if len(fb.args) != 0 {
		t.Fatalf("broker called: %v", fb.args)
	}
	// The services list says why there are no controls.
	rr := h.do("GET", "/api/v1/services", "", nil)
	var sr servicesResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &sr)
	if sr.Operate.Available || !strings.Contains(sr.Operate.Reason, "services.operate") {
		t.Fatalf("operate: %+v", sr.Operate)
	}
}

// W7: Origin and CSRF are enforced on every new POST route.
func TestServiceOpsCSRFAndOrigin(t *testing.T) {
	fb := opsFake()
	h := harnessWith(t, fb, operateScopes)
	for _, p := range []string{"/api/v1/services/units/nginx.service/plan", "/api/v1/services/units/nginx.service/apply", "/api/v1/auth/step-up"} {
		body := `{"action":"stop","token":"` + validToken + `"}`
		if rr := h.do("POST", p, body, map[string]string{"Content-Type": "application/json", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden {
			t.Errorf("%s without Origin: %d", p, rr.Code)
		}
		if rr := h.do("POST", p, body, map[string]string{"Content-Type": "application/json", "Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden {
			t.Errorf("%s cross-origin: %d", p, rr.Code)
		}
		if rr := h.do("POST", p, body, map[string]string{"Content-Type": "application/json", "Origin": testOrigin}); rr.Code != http.StatusForbidden {
			t.Errorf("%s without CSRF token: %d", p, rr.Code)
		}
		if rr := h.do("POST", p, body, map[string]string{"Content-Type": "text/plain", "Origin": testOrigin, "X-CSRF-Token": h.csrf}); rr.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s non-JSON: %d", p, rr.Code)
		}
	}
	if len(fb.args) != 0 {
		t.Fatal("broker called")
	}
}

func TestServiceOpsValidateBeforeBroker(t *testing.T) {
	fb := opsFake()
	h := harnessWith(t, fb, operateScopes)
	for _, c := range []struct{ unit, body string }{
		{"nginx.socket", `{"action":"stop"}`}, {"-x.service", `{"action":"stop"}`}, {"foo@.service", `{"action":"stop"}`},
		{"nginx.service", `{"action":"enable"}`}, {"nginx.service", `{"action":"daemon-reload"}`},
		{"nginx.service", `{"action":"stop","extra":1}`}, {"nginx.service", `{"action":"stop"}{}`},
		{"nginx.service", `{"action":["stop"]}`}, {"a%3Bb.service", `{"action":"stop"}`},
	} {
		if rr := h.post("/api/v1/services/units/"+c.unit+"/plan", c.body); rr.Code != http.StatusBadRequest {
			t.Errorf("plan %s %s: %d", c.unit, c.body, rr.Code)
		}
	}
	for _, tok := range []string{"", "x", "hcp1.a.b", strings.Repeat("A", 3000), "hcp1.a b." + strings.Repeat("A", 43)} {
		b, _ := json.Marshal(map[string]string{"action": "start", "token": tok})
		if rr := h.post("/api/v1/services/units/nginx.service/apply", string(b)); rr.Code != http.StatusBadRequest {
			t.Errorf("token %.20q: %d", tok, rr.Code)
		}
	}
	// Protected names are refused at the API too (the broker re-checks).
	if rr := h.post("/api/v1/services/units/sshd.service/plan", `{"action":"restart"}`); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "policy_denied") {
		t.Errorf("protected: %d %s", rr.Code, rr.Body)
	}
	if len(fb.args) != 0 {
		t.Fatalf("broker reached with invalid input: %v", fb.args)
	}
}

func TestPlanBindsSessionAndAudits(t *testing.T) {
	fb := opsFake()
	h := harnessWith(t, fb, operateScopes)
	rr := h.post("/api/v1/services/units/nginx.service/plan", `{"action":"stop"}`)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"risk":"disruptive"`) {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	a := fb.actors[len(fb.actors)-1]
	if a.User != "full" || len(a.Session) != 64 || a.Session == h.cookie.Value || a.StepUpAt != 0 {
		t.Fatalf("actor %+v", a)
	}
	if !strings.Contains(h.audit.String(), `"event":"services.op.plan"`) || strings.Contains(h.audit.String(), validToken) {
		t.Fatalf("audit: %s", h.audit)
	}
}

// W6: stop/restart need a step-up for the same session within 5 minutes.
func TestApplyStepUp(t *testing.T) {
	fb := opsFake()
	h := harnessWith(t, fb, operateScopes)
	body := `{"action":"stop","token":"` + validToken + `"}`
	if rr := h.post("/api/v1/services/units/nginx.service/apply", body); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "step_up_required") {
		t.Fatalf("without step-up: %d %s", rr.Code, rr.Body)
	}
	if code := h.stepUp("wrong-password"); code != http.StatusForbidden {
		t.Fatalf("wrong password: %d", code)
	}
	if rr := h.post("/api/v1/services/units/nginx.service/apply", body); rr.Code != http.StatusForbidden {
		t.Fatalf("after failed step-up: %d", rr.Code)
	}
	if code := h.stepUp(fullPW); code != 200 {
		t.Fatalf("step-up: %d", code)
	}
	rr := h.post("/api/v1/services/units/nginx.service/apply", body)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"outcome":"succeeded"`) || !strings.Contains(rr.Body.String(), `"active_state":"inactive"`) {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body)
	}
	a := fb.actors[len(fb.actors)-1]
	if a.StepUpAt == 0 || time.Since(time.Unix(a.StepUpAt, 0)) > time.Minute {
		t.Fatalf("broker not given the step-up time: %+v", a)
	}
	// Session shows when the step-up lapses.
	srr := h.do("GET", "/api/v1/auth/session", "", nil)
	if !strings.Contains(srr.Body.String(), "step_up_until") {
		t.Fatalf("session: %s", srr.Body)
	}
	// Stale after 5 minutes.
	h.srv.stepups.SetClock(func() time.Time { return time.Now().Add(auth.StepUpWindow + time.Second) })
	if rr := h.post("/api/v1/services/units/nginx.service/apply", body); rr.Code != http.StatusForbidden {
		t.Fatalf("stale step-up: %d", rr.Code)
	}
	logs := h.audit.String()
	if strings.Contains(logs, fullPW) || strings.Contains(logs, "wrong-password") || strings.Contains(logs, validToken) {
		t.Fatal("secret in the audit log")
	}
	var events []string
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var e audit.Event
		if json.Unmarshal([]byte(line), &e) == nil {
			events = append(events, e.Event+"/"+e.Outcome)
		}
	}
	got := strings.Join(events, " ")
	for _, want := range []string{"auth.step_up/failure", "auth.step_up/success", "services.op.apply/step_up_required",
		"services.op.apply/requested", "services.op.apply/succeeded"} {
		if !strings.Contains(got, want) {
			t.Errorf("audit lacks %s: %s", want, got)
		}
	}
}

// One session's step-up does not authorize another session of the same user.
func TestStepUpIsPerSession(t *testing.T) {
	fb := opsFake()
	h := harnessWith(t, fb, operateScopes)
	if code := h.stepUp(fullPW); code != 200 {
		t.Fatal(code)
	}
	firstCookie, firstCSRF := h.cookie, h.csrf
	h.cookie = nil
	if rr := h.login("full", fullPW); rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	if rr := h.post("/api/v1/services/units/nginx.service/apply", `{"action":"restart","token":"`+validToken+`"}`); rr.Code != http.StatusForbidden {
		t.Fatalf("second session used the first session's step-up: %d", rr.Code)
	}
	// Revoking the first session clears its step-up.
	h.cookie, h.csrf = firstCookie, firstCSRF
	key := auth.KeyFor(firstCookie.Value)
	if _, ok := h.srv.stepups.At(key, "full"); !ok {
		t.Fatal("expected step-up on first session")
	}
	h.srv.sessions.Destroy(firstCookie.Value)
	if rr := h.post("/api/v1/services/units/nginx.service/apply", `{"action":"restart","token":"`+validToken+`"}`); rr.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session: %d", rr.Code)
	}
}

// Removing the scope from the account takes effect on an existing session.
func TestScopeRemovalAppliesToLiveSessions(t *testing.T) {
	fb := opsFake()
	h := harnessWith(t, fb, operateScopes)
	for i, u := range h.users.Users {
		if u.Username == "full" {
			h.users.Users[i].Scopes = []string{auth.ScopeServicesRead}
		}
	}
	h.srv.users = auth.NewStore(h.users)
	if rr := h.post("/api/v1/services/units/nginx.service/plan", `{"action":"start"}`); rr.Code != http.StatusForbidden {
		t.Fatalf("revoked scope still works: %d", rr.Code)
	}
}

func TestApplyBrokerRefusalsMapped(t *testing.T) {
	cases := map[string]int{
		proto.CodeInvalidToken: http.StatusConflict, proto.CodeConflict: http.StatusConflict, proto.CodeBusy: http.StatusConflict,
		proto.CodePolicyDenied: http.StatusForbidden, proto.CodeStepUpRequired: http.StatusForbidden,
		proto.CodeUnavailable: http.StatusServiceUnavailable,
	}
	for c, want := range cases {
		fb := opsFake()
		fb.errs["services.unit.apply"] = &proto.Error{Code: c, Message: "broker says " + c}
		h := harnessWith(t, fb, operateScopes)
		rr := h.post("/api/v1/services/units/nginx.service/apply", `{"action":"start","token":"`+validToken+`"}`)
		if rr.Code != want || !strings.Contains(rr.Body.String(), "broker says") {
			t.Errorf("%s: %d %s", c, rr.Code, rr.Body)
		}
	}
}

// A lost broker connection during apply is "outcome unknown", never "helper down".
func TestApplyTransportErrorIsOutcomeUnknown(t *testing.T) {
	for _, e := range []error{brokerclient.ErrUnavailable, errors.New("read: connection reset by peer")} {
		fb := opsFake()
		fb.errs["services.unit.apply"] = e
		h := harnessWith(t, fb, operateScopes)
		rr := h.post("/api/v1/services/units/nginx.service/apply", `{"action":"start","token":"`+validToken+`"}`)
		if rr.Code != http.StatusBadGateway || !strings.Contains(rr.Body.String(), "outcome_unknown") || !strings.Contains(rr.Body.String(), "may or may not have run") {
			t.Errorf("%v: %d %s", e, rr.Code, rr.Body)
		}
		if !strings.Contains(h.audit.String(), `"outcome":"outcome_unknown"`) {
			t.Errorf("%v: not audited as outcome_unknown", e)
		}
	}
}

// Nothing is executed when the request cannot be audited first.
func TestApplyFailClosedOnAudit(t *testing.T) {
	fb := opsFake()
	h := harnessWith(t, fb, operateScopes)
	h.srv.audit = audit.New(brokenWriter{})
	n := len(fb.args)
	if rr := h.post("/api/v1/services/units/nginx.service/apply", `{"action":"start","token":"`+validToken+`"}`); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("%d", rr.Code)
	}
	if rr := h.post("/api/v1/services/units/nginx.service/plan", `{"action":"start"}`); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), validToken) {
		t.Fatalf("plan token released without audit: %d %s", rr.Code, rr.Body)
	}
	for _, a := range fb.args[n:] {
		if m, ok := a.(map[string]string); ok && m["token"] != "" {
			t.Fatal("apply reached the broker without an audit record")
		}
	}
}

func TestServicesListMarksProtectedAndOperable(t *testing.T) {
	h := harnessWith(t, opsFake(), operateScopes)
	rr := h.do("GET", "/api/v1/services", "", nil)
	var sr servicesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &sr); err != nil {
		t.Fatal(err)
	}
	if !sr.Operate.Available || !sr.Protected["ssh.service"].Protected || sr.Protected["nginx.service"].Protected || len(sr.Units) != 3 {
		t.Fatalf("%+v", sr)
	}
	if _, ok := sr.Protected["dbus.socket"]; ok {
		t.Fatal("non-service units are not labelled")
	}
}

func TestOperationsEndpoint(t *testing.T) {
	h := harnessWith(t, opsFake(), []string{auth.ScopeAuditRead})
	rr := h.do("GET", "/api/v1/operations", "", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"outcome":"interrupted"`) {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	h2 := harnessWith(t, opsFake(), operateScopes)
	if rr := h2.do("GET", "/api/v1/operations", "", nil); rr.Code != http.StatusForbidden {
		t.Fatalf("without audit.read: %d", rr.Code)
	}
}
