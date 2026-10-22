// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/state"
)

// stateHarness wires a real state database: persistent sessions and the
// audit trail in SQLite, as in production.
func stateHarness(t *testing.T, scopes []string) (*harness, *state.DB) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "s")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	al, err := audit.OpenStore(db.Audit())
	if err != nil {
		t.Fatal(err)
	}
	h := harnessWith(t, nil, nil) // login replaced below
	h.srv.audit = al
	h.srv.WithSessionStore(db.Sessions()).WithAuditPager(db.Audit())
	h1, _ := auth.HashPassword("full-password-1234", auth.MinIterations)
	h.users.Users = append(h.users.Users, auth.User{Username: "auditor", PasswordHash: h1, Scopes: scopes})
	h.srv.users = auth.NewStore(h.users)
	if rr := h.login("auditor", "full-password-1234"); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	return h, db
}

func listSessions(t *testing.T, h *harness) sessionsResponse {
	t.Helper()
	rr := h.do("GET", "/api/v1/auth/sessions", "", nil)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	var s sessionsResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &s)
	return s
}

func TestSessionsListAndRevokeOwnOnly(t *testing.T) {
	h, db := stateHarness(t, []string{auth.ScopeSystemRead})
	mine, mineCSRF := h.cookie, h.csrf
	// A second session of the same user (another browser: no cookie sent, so
	// the fixation defence does not end the first), and another user's.
	h.cookie = nil
	h.login("auditor", "full-password-1234")
	second := h.cookie
	other := auth.NewSessionManagerWith(db.Sessions())
	_, os, _ := other.Create(auth.User{Username: "reader"}, "10.0.0.9", "curl")

	h.cookie, h.csrf = mine, mineCSRF
	s := listSessions(t, h)
	if !s.Persistent || len(s.Sessions) != 2 || s.IdleTimeout != 1800 {
		t.Fatalf("%+v", s)
	}
	cur := 0
	for _, x := range s.Sessions {
		if x.Current {
			cur++
			if x.ID != auth.KeyFor(mine.Value) {
				t.Fatal("wrong current session")
			}
		}
		if x.ID == os.Key || strings.Contains(x.ID, mine.Value) {
			t.Fatal("foreign session listed or raw cookie exposed")
		}
	}
	if cur != 1 {
		t.Fatalf("current marked %d times", cur)
	}
	hdr := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf}
	// Revoking needs CSRF + Origin.
	if rr := h.do("POST", "/api/v1/auth/sessions/"+auth.KeyFor(second.Value)+"/revoke", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden {
		t.Fatalf("revoke without CSRF: %d", rr.Code)
	}
	// Another user's session: 404, untouched.
	if rr := h.do("POST", "/api/v1/auth/sessions/"+os.Key+"/revoke", "", hdr); rr.Code != http.StatusNotFound {
		t.Fatalf("foreign revoke: %d", rr.Code)
	}
	if _, ok, _ := db.Sessions().Get(os.Key); !ok {
		t.Fatal("foreign session deleted")
	}
	for _, bad := range []string{"x", strings.Repeat("A", 64), "../" + os.Key} {
		// 400 from validation, 404 from the router, or the router's 307 path cleaning.
		if rr := h.do("POST", "/api/v1/auth/sessions/"+bad+"/revoke", "", hdr); rr.Code != http.StatusBadRequest && rr.Code != http.StatusNotFound && rr.Code != http.StatusTemporaryRedirect {
			t.Fatalf("%q: %d", bad, rr.Code)
		}
	}
	if _, ok, _ := db.Sessions().Get(os.Key); !ok {
		t.Fatal("foreign session deleted via a malformed id")
	}
	// Revoke the other own session: it stops working immediately.
	if rr := h.do("POST", "/api/v1/auth/sessions/"+auth.KeyFor(second.Value)+"/revoke", "", hdr); rr.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", rr.Code, rr.Body)
	}
	h.cookie = second
	if rr := h.do("GET", "/api/v1/auth/session", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatal("revoked session still works")
	}
	// Revoking the current session clears the cookie.
	h.cookie = mine
	rr := h.do("POST", "/api/v1/auth/sessions/"+auth.KeyFor(mine.Value)+"/revoke", "", hdr)
	if rr.Code != http.StatusNoContent || !strings.Contains(rr.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("self revoke: %d %v", rr.Code, rr.Header())
	}
	if rr := h.do("GET", "/api/v1/auth/session", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatal("current session survived self-revoke")
	}
}

func TestSessionsSurviveServerRestart(t *testing.T) {
	h, db := stateHarness(t, []string{auth.ScopeSystemRead})
	// A new Server over the same database stands in for a restart.
	h2 := newHarness(t)
	h2.users, h2.srv.users = h.users, h.srv.users
	h2.srv.WithSessionStore(db.Sessions())
	h2.cookie = h.cookie
	if rr := h2.do("GET", "/api/v1/host/overview", "", nil); rr.Code != 200 {
		t.Fatalf("session lost on restart: %d", rr.Code)
	}
	// CSRF token survives too, so the UI keeps working without a reload.
	if rr := h2.do("POST", "/api/v1/auth/logout", "", map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf}); rr.Code != http.StatusNoContent {
		t.Fatalf("logout after restart: %d", rr.Code)
	}
}

func TestAuditEndpoint(t *testing.T) {
	h, _ := stateHarness(t, []string{auth.ScopeSystemRead})
	if rr := h.do("GET", "/api/v1/audit", "", nil); rr.Code != http.StatusForbidden {
		t.Fatalf("audit without audit.read: %d", rr.Code)
	}
	if c := caps(t, h); c["audit"].Status != CapForbidden {
		t.Fatalf("capability %+v", c["audit"])
	}

	h, _ = stateHarness(t, []string{auth.ScopeSystemRead, auth.ScopeAuditRead})
	h.login("nobody-here", "wrong-password-123") // a failed login to find
	rr := h.do("GET", "/api/v1/audit?limit=2", "", nil)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	var a auditResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &a)
	if len(a.Events) != 2 || a.NextBefore == 0 || a.Events[0].ID < a.Events[1].ID {
		t.Fatalf("%+v", a)
	}
	rr = h.do("GET", "/api/v1/audit?event=auth.login", "", nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &a)
	failures := 0
	for _, e := range a.Events {
		if !strings.HasPrefix(e.Event.Event, "auth.login") {
			t.Fatalf("filter leaked %s", e.Event.Event)
		}
		if e.Outcome == "failure" {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("failed login not audited: %+v", a.Events)
	}
	if strings.Contains(rr.Body.String(), "wrong-password") || strings.Contains(rr.Body.String(), h.cookie.Value) || strings.Contains(rr.Body.String(), h.csrf) {
		t.Fatal("secret in audit trail")
	}
	for _, q := range []string{"limit=0", "limit=501", "limit=x", "before=-1", "before=x", "event=" + strings.Repeat("a", 80), "event=AUTH", "event=a%27%20OR%201=1"} {
		if rr := h.do("GET", "/api/v1/audit?"+q, "", nil); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, rr.Code)
		}
	}
}

func TestAuditUnavailableWithoutStateDB(t *testing.T) {
	h := harnessWith(t, nil, append(allScopes, auth.ScopeAuditRead))
	if rr := h.do("GET", "/api/v1/audit", "", nil); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("%d", rr.Code)
	}
	if c := caps(t, h); c["audit"].Status != CapUnavailable {
		t.Fatalf("%+v", c["audit"])
	}
	if s := listSessions(t, h); s.Persistent {
		t.Fatal("in-memory sessions reported persistent")
	}
}
