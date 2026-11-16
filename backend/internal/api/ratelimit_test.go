// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/ratelimit"
)

func TestRateLimitDoesNotCallBroker(t *testing.T) {
	fb := &fakeBroker{results: map[string]any{"packages.search": map[string]any{"packages": []any{}}}}
	h := harnessWith(t, fb, []string{auth.ScopePackagesRead})
	h.srv.userLimit = ratelimit.NewTokenBucket(1, time.Hour)
	hdrs := packagesHeaders(h)

	if rr := h.do("GET", "/api/v1/packages/search?q=x", "", hdrs); rr.Code != http.StatusOK {
		t.Fatalf("first: %d %s", rr.Code, rr.Body)
	}
	before := len(fb.actors)
	if before == 0 {
		t.Fatal("broker was not called on the allowed request")
	}

	rr := h.do("GET", "/api/v1/packages/search?q=x", "", hdrs)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429: %s", rr.Code, rr.Body)
	}
	if len(fb.actors) != before {
		t.Fatalf("broker called on 429: %d -> %d", before, len(fb.actors))
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
	if !strings.Contains(rr.Body.String(), "rate_limited") {
		t.Fatalf("body: %s", rr.Body)
	}
}

func TestRateLimitIndependentUsers(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeSystemRead})
	h.srv.userLimit = ratelimit.NewTokenBucket(1, time.Hour)

	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusOK {
		t.Fatalf("first: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("second: got %d, want 429", rr.Code)
	}

	// A second account with the same scope is independent of the first.
	pw, _ := auth.HashPassword("other-password-123", auth.MinIterations)
	h.users.Users = append(h.users.Users, auth.User{Username: "other", PasswordHash: pw, Scopes: []string{auth.ScopeSystemRead}})
	h.srv.users = auth.NewStore(h.users)
	if rr := h.login("other", "other-password-123"); rr.Code != http.StatusOK {
		t.Fatalf("login other: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusOK {
		t.Fatalf("other user denied: %d %s", rr.Code, rr.Body)
	}
}

func TestPreauthLoginFloodBounded(t *testing.T) {
	h := newHarness(t)
	body := `{"username":"reader","password":"wrong-password"}`
	hdr := map[string]string{"Content-Type": "application/json", "Origin": testOrigin}
	for i := 0; i < 25; i++ {
		rr := h.do("POST", "/api/v1/auth/login", body, hdr)
		// Refusal responses are unchanged: invalid_credentials then throttled.
		if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusTooManyRequests {
			t.Fatalf("attempt %d: got %d", i, rr.Code)
		}
	}
	raw := h.audit.String()
	if n := strings.Count(raw, `"event":"auth.login"`); n > 1 {
		t.Fatalf("auth.login events = %d, want <= 1: %s", n, raw)
	}
	if !strings.Contains(raw, "audit.suppressed") {
		t.Fatalf("missing suppressed summary: %s", raw)
	}
}

func TestGlobalConcurrencyLimitsHandler(t *testing.T) {
	fb := &fakeBroker{results: map[string]any{"packages.search": map[string]any{"packages": []any{}}}}
	h := harnessWith(t, fb, []string{auth.ScopePackagesRead})
	// Force saturation with a single-slot global semaphore.
	h.srv.globalSem = make(chan struct{}, 1)
	h.srv.globalSem <- struct{}{}
	hdrs := packagesHeaders(h)

	rr := h.do("GET", "/api/v1/packages/search?q=x", "", hdrs)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503: %s", rr.Code, rr.Body)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
	if !strings.Contains(rr.Body.String(), "service_busy") {
		t.Fatalf("body: %s", rr.Body)
	}
	if len(fb.actors) != 0 {
		t.Fatalf("broker called while saturated: %d", len(fb.actors))
	}

	// Release the slot; the request must now reach the handler and broker.
	<-h.srv.globalSem
	if rr := h.do("GET", "/api/v1/packages/search?q=x", "", hdrs); rr.Code != http.StatusOK {
		t.Fatalf("after release: %d %s", rr.Code, rr.Body)
	}
	if len(fb.actors) != 1 {
		t.Fatalf("broker calls after release = %d, want 1", len(fb.actors))
	}
}

func TestGlobalSaturationDoesNotDrainQuota(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeSystemRead})
	h.srv.userLimit = ratelimit.NewTokenBucket(2, time.Hour)
	h.srv.globalSem = make(chan struct{}, 1)
	h.srv.globalSem <- struct{}{}

	// Rejected by global saturation, not by the per-user quota.
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("saturated: %d %s", rr.Code, rr.Body)
	}
	// The user must still have their full burst (2) once the slot is free.
	<-h.srv.globalSem
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusOK {
		t.Fatalf("first after release: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusOK {
		t.Fatalf("second after release: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("third: got %d, want 429", rr.Code)
	}
}

func TestLoginSaturationNoHashing(t *testing.T) {
	h := newHarness(t)
	h.srv.loginSem = make(chan struct{}, 1)
	h.srv.loginSem <- struct{}{}
	// Correct credentials: a 503 proves admission rejected before hashing
	// (otherwise the login would succeed with 200).
	body := `{"username":"reader","password":"reader-password-123"}`
	hdr := map[string]string{"Content-Type": "application/json", "Origin": testOrigin}
	rr := h.do("POST", "/api/v1/auth/login", body, hdr)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503: %s", rr.Code, rr.Body)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
	if !strings.Contains(rr.Body.String(), "service_busy") {
		t.Fatalf("body: %s", rr.Body)
	}
}

func TestStepUpSaturationNoHashing(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeSystemRead})
	h.srv.loginSem = make(chan struct{}, 1)
	h.srv.loginSem <- struct{}{}
	body := `{"password":"full-password-1234"}`
	hdr := map[string]string{"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": h.csrf}
	rr := h.do("POST", "/api/v1/auth/step-up", body, hdr)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503: %s", rr.Code, rr.Body)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
	if !strings.Contains(rr.Body.String(), "service_busy") {
		t.Fatalf("body: %s", rr.Body)
	}
}

func TestRateLimitConfig(t *testing.T) {
	// The zero Config used by newHarness selects the production defaults.
	h := newHarness(t)
	if h.srv.userLimit.Burst != 30 || h.srv.userLimit.Refill != 5*time.Second {
		t.Fatalf("defaults: burst %d refill %v", h.srv.userLimit.Burst, h.srv.userLimit.Refill)
	}
	// Custom values are honored; non-positive values fall back to defaults.
	var buf bytes.Buffer
	users := auth.NewStore(&auth.UsersFile{Version: 1})
	col := &hostinfo.Collector{}
	custom := New(Config{RateLimitBurst: 7, RateLimitRefill: 3 * time.Second}, users, audit.New(&buf), col, nil)
	if custom.userLimit.Burst != 7 || custom.userLimit.Refill != 3*time.Second {
		t.Fatalf("custom: burst %d refill %v", custom.userLimit.Burst, custom.userLimit.Refill)
	}
	fallback := New(Config{RateLimitBurst: -1, RateLimitRefill: 0}, users, audit.New(&buf), col, nil)
	if fallback.userLimit.Burst != 30 || fallback.userLimit.Refill != 5*time.Second {
		t.Fatalf("fallback: burst %d refill %v", fallback.userLimit.Burst, fallback.userLimit.Refill)
	}
}

func TestPreauthCapsOIDCLoginFailure(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest("GET", "http://hc.test/x", nil)
	// First failed OIDC login is recorded; the rest within the window are
	// suppressed even though the bound username is nonempty.
	for i := 0; i < 6; i++ {
		if err := h.srv.auditLog("auth.oidc.login", "bounduser", req, "failure", "bound account unavailable"); err != nil {
			t.Fatal(err)
		}
	}
	raw := h.audit.String()
	if n := strings.Count(raw, `"event":"auth.oidc.login"`); n > 1 {
		t.Fatalf("auth.oidc.login events = %d, want <= 1: %s", n, raw)
	}
	if !strings.Contains(raw, "audit.suppressed") {
		t.Fatalf("missing suppressed summary: %s", raw)
	}
}

func TestOIDCConcurrencyGuardSaturatesStart(t *testing.T) {
	s := newSSO(t)
	s.srv.oidcSem = make(chan struct{}, 1)
	s.srv.oidcSem <- struct{}{}
	rr := s.get("/api/v1/auth/oidc/start")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("code %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Location"), "sso_failed") {
		t.Fatalf("location: %s", rr.Header().Get("Location"))
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
}
