// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
)

const fullPW = "full-password-1234"

func (h *harness) post(path, body string) *httptest.ResponseRecorder {
	return h.do("POST", path, body, map[string]string{"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": h.csrf})
}

func (h *harness) stepUp(pw string) int {
	b, _ := json.Marshal(map[string]string{"password": pw})
	return h.post("/api/v1/auth/step-up", string(b)).Code
}

// D-3 / W6: the endpoint re-verifies the password, records the re-entry for
// this session only, reports its expiry, and never logs the password.
func TestStepUpEndpoint(t *testing.T) {
	h := harnessWith(t, newFake(), []string{auth.ScopeServicesRead})
	key := auth.KeyFor(h.cookie.Value)
	if code := h.stepUp("wrong-password"); code != http.StatusForbidden {
		t.Fatalf("wrong password: %d", code)
	}
	if _, ok := h.srv.stepups.At(key, "full"); ok {
		t.Fatal("failed re-entry recorded")
	}
	for _, body := range []string{``, `{}`, `{"password":""}`, `{"password":"x","extra":1}`, `{"password":"` + strings.Repeat("a", 1025) + `"}`} {
		if rr := h.post("/api/v1/auth/step-up", body); rr.Code != http.StatusBadRequest && rr.Code != http.StatusForbidden {
			t.Errorf("malformed %q: %d", body, rr.Code)
		}
	}
	rr := h.post("/api/v1/auth/step-up", `{"password":"`+fullPW+`"}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "step_up_until") {
		t.Fatalf("step-up: %d %s", rr.Code, rr.Body)
	}
	if _, ok := h.srv.stepups.At(key, "full"); !ok {
		t.Fatal("step-up not recorded for this session")
	}
	if srr := h.do("GET", "/api/v1/auth/session", "", nil); !strings.Contains(srr.Body.String(), "step_up_until") {
		t.Fatalf("session does not show the step-up: %s", srr.Body)
	}
	logs := h.audit.String()
	if strings.Contains(logs, fullPW) || strings.Contains(logs, "wrong-password") {
		t.Fatal("password in the audit log")
	}
	if !strings.Contains(logs, `"auth.step_up"`) || !strings.Contains(logs, `"outcome":"failure"`) || !strings.Contains(logs, `"outcome":"success"`) {
		t.Fatalf("step-up attempts not audited: %s", logs)
	}
}

// W7: Origin, CSRF token and JSON are required, like every other POST.
func TestStepUpRequiresOriginCSRFAndJSON(t *testing.T) {
	h := harnessWith(t, newFake(), []string{auth.ScopeServicesRead})
	body := `{"password":"` + fullPW + `"}`
	for name, hdr := range map[string]map[string]string{
		"no CSRF":        {"Content-Type": "application/json", "Origin": testOrigin},
		"foreign Origin": {"Content-Type": "application/json", "Origin": "http://evil.example", "X-CSRF-Token": h.csrf},
		"form body":      {"Content-Type": "application/x-www-form-urlencoded", "Origin": testOrigin, "X-CSRF-Token": h.csrf},
	} {
		if rr := h.do("POST", "/api/v1/auth/step-up", body, hdr); rr.Code < 400 {
			t.Errorf("%s accepted: %d", name, rr.Code)
		}
	}
	if _, ok := h.srv.stepups.At(auth.KeyFor(h.cookie.Value), "full"); ok {
		t.Fatal("rejected request recorded a step-up")
	}
	h.cookie = nil
	if rr := h.post("/api/v1/auth/step-up", body); rr.Code != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", rr.Code)
	}
}

// Failures share the login throttle.
func TestStepUpThrottled(t *testing.T) {
	h := harnessWith(t, newFake(), []string{auth.ScopeServicesRead})
	for i := 0; i < 5; i++ {
		_ = h.stepUp("wrong-password")
	}
	if code := h.stepUp(fullPW); code != http.StatusTooManyRequests {
		t.Fatalf("throttle not shared with login: %d", code)
	}
}

// One session's re-entry never authorizes another; logout and revocation clear it.
func TestStepUpScopedToSessionAndCleared(t *testing.T) {
	h := harnessWith(t, newFake(), []string{auth.ScopeServicesRead})
	first := h.cookie
	if code := h.stepUp(fullPW); code != 200 {
		t.Fatal(code)
	}
	h.cookie = nil
	if rr := h.login("full", fullPW); rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	second := h.cookie
	if _, ok := h.srv.stepups.At(auth.KeyFor(second.Value), "full"); ok {
		t.Fatal("second session inherited the first session's step-up")
	}
	// Revoke the first session from the second: its step-up is gone.
	hdr := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf}
	if rr := h.do("POST", "/api/v1/auth/sessions/"+auth.KeyFor(first.Value)+"/revoke", "", hdr); rr.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", rr.Code)
	}
	if _, ok := h.srv.stepups.At(auth.KeyFor(first.Value), "full"); ok {
		t.Fatal("revocation left the step-up")
	}
	// Logout clears the current session's step-up.
	if code := h.stepUp(fullPW); code != 200 {
		t.Fatal(code)
	}
	if rr := h.do("POST", "/api/v1/auth/logout", "", hdr); rr.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rr.Code)
	}
	if _, ok := h.srv.stepups.At(auth.KeyFor(second.Value), "full"); ok {
		t.Fatal("logout left the step-up")
	}
}
