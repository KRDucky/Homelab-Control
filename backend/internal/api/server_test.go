package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/hostinfo"
)

const testOrigin = "http://hc.test"

type harness struct {
	t      *testing.T
	srv    *Server
	audit  *bytes.Buffer
	users  *auth.UsersFile
	cookie *http.Cookie
	csrf   string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h1, _ := auth.HashPassword("reader-password-123", auth.MinIterations)
	h2, _ := auth.HashPassword("noscope-password-123", auth.MinIterations)
	uf := &auth.UsersFile{Version: 1, Users: []auth.User{
		{Username: "reader", PasswordHash: h1, Scopes: []string{auth.ScopeSystemRead}},
		{Username: "noscope", PasswordHash: h2, Scopes: nil},
	}}
	root, _ := filepath.Abs("../hostinfo/testdata/host")
	col := &hostinfo.Collector{Roots: hostinfo.Roots{Proc: root + "/proc", Sys: root + "/sys", Etc: root + "/etc"},
		Statfs: func(string) (uint64, uint64, uint64, error) { return 10, 5, 5, nil }}
	buf := &bytes.Buffer{}
	ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>hc</title>")}, "assets/app.js": {Data: []byte("console.log(1)")}}
	return &harness{t: t, srv: New(Config{}, auth.NewStore(uf), audit.New(buf), col, ui), audit: buf, users: uf}
}

func (h *harness) do(method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, "http://hc.test"+path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if h.cookie != nil {
		req.AddCookie(h.cookie)
	}
	rr := httptest.NewRecorder()
	h.srv.ServeHTTP(rr, req)
	return rr
}

func (h *harness) login(user, pw string) *httptest.ResponseRecorder {
	h.t.Helper()
	b, _ := json.Marshal(map[string]string{"username": user, "password": pw})
	rr := h.do("POST", "/api/v1/auth/login", string(b), map[string]string{"Content-Type": "application/json", "Origin": testOrigin})
	if rr.Code == http.StatusOK {
		for _, c := range rr.Result().Cookies() {
			if c.Name == "hc_session" {
				h.cookie = c
			}
		}
		var s sessionResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &s)
		h.csrf = s.CSRFToken
	}
	return rr
}

func TestHealthIsPublicAndMinimal(t *testing.T) {
	h := newHarness(t)
	rr := h.do("GET", "/api/v1/health", "", nil)
	if rr.Code != 200 || strings.Contains(rr.Body.String(), "fixture-host") {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
}

func TestOverviewRequiresAuthentication(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rr.Code)
	}
	h.cookie = &http.Cookie{Name: "hc_session", Value: "forged"}
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("forged cookie: got %d", rr.Code)
	}
}

func TestLoginAndReadOverview(t *testing.T) {
	h := newHarness(t)
	rr := h.login("reader", "reader-password-123")
	if rr.Code != 200 || h.cookie == nil || h.csrf == "" {
		t.Fatalf("login failed: %d %s", rr.Code, rr.Body)
	}
	c := h.cookie
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("cookie attributes: %+v", c)
	}
	if strings.Contains(rr.Body.String(), "password") {
		t.Fatal("login response leaks password data")
	}
	rr = h.do("GET", "/api/v1/host/overview", "", nil)
	if rr.Code != 200 {
		t.Fatalf("overview %d %s", rr.Code, rr.Body)
	}
	var o hostinfo.Overview
	if err := json.Unmarshal(rr.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	if o.System.Hostname != "fixture-host" || o.Memory.TotalBytes == 0 {
		t.Fatalf("overview content: %+v", o.System)
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("API response cacheable")
	}
	if !strings.Contains(h.audit.String(), `"event":"auth.login"`) || !strings.Contains(h.audit.String(), `"outcome":"success"`) {
		t.Fatalf("login not audited: %s", h.audit)
	}
	if strings.Contains(h.audit.String(), "reader-password-123") || strings.Contains(h.audit.String(), c.Value) || strings.Contains(h.audit.String(), h.csrf) {
		t.Fatal("secret written to audit log")
	}
}

func TestScopeEnforcedServerSide(t *testing.T) {
	h := newHarness(t)
	if rr := h.login("noscope", "noscope-password-123"); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusForbidden {
		t.Fatalf("missing scope: got %d", rr.Code)
	}
	if !strings.Contains(h.audit.String(), "authz.denied") {
		t.Fatal("denial not audited")
	}
}

func TestBadCredentialsAndThrottle(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 5; i++ {
		if rr := h.login("reader", "wrong-password-xx"); rr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, rr.Code)
		}
	}
	if rr := h.login("reader", "reader-password-123"); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected throttling, got %d", rr.Code)
	}
	if rr := h.login("ghost", "whatever-password"); rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Body.String(), "invalid username or password") {
		t.Fatal("unknown user must be indistinguishable from bad password")
	}
}

func TestLoginRequiresSameOrigin(t *testing.T) {
	h := newHarness(t)
	body := `{"username":"reader","password":"reader-password-123"}`
	for _, origin := range []string{"", "null", "http://evil.test", "https://hc.test", "http://hc.test.evil.test"} {
		rr := h.do("POST", "/api/v1/auth/login", body, map[string]string{"Content-Type": "application/json", "Origin": origin})
		if rr.Code != http.StatusForbidden {
			t.Errorf("origin %q: got %d", origin, rr.Code)
		}
	}
}

func TestLoginStrictJSON(t *testing.T) {
	h := newHarness(t)
	hdr := map[string]string{"Origin": testOrigin, "Content-Type": "application/json"}
	if rr := h.do("POST", "/api/v1/auth/login", `{"username":"reader","password":"reader-password-123","admin":true}`, hdr); rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rr.Code)
	}
	if rr := h.do("POST", "/api/v1/auth/login", "username=reader", map[string]string{"Origin": testOrigin, "Content-Type": "application/x-www-form-urlencoded"}); rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form post: %d", rr.Code)
	}
	big := `{"username":"reader","password":"` + strings.Repeat("a", 70<<10) + `"}`
	if rr := h.do("POST", "/api/v1/auth/login", big, hdr); rr.Code != http.StatusBadRequest {
		t.Fatalf("oversized body: %d", rr.Code)
	}
}

func TestLogoutRequiresCSRFToken(t *testing.T) {
	h := newHarness(t)
	h.login("reader", "reader-password-123")
	if rr := h.do("POST", "/api/v1/auth/logout", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden {
		t.Fatalf("logout without token: %d", rr.Code)
	}
	if rr := h.do("POST", "/api/v1/auth/logout", "", map[string]string{"Origin": testOrigin, "X-CSRF-Token": "wrong"}); rr.Code != http.StatusForbidden {
		t.Fatalf("logout with wrong token: %d", rr.Code)
	}
	if rr := h.do("POST", "/api/v1/auth/logout", "", map[string]string{"Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden {
		t.Fatalf("cross-origin logout: %d", rr.Code)
	}
	if rr := h.do("POST", "/api/v1/auth/logout", "", map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf}); rr.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rr.Code)
	}
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatal("session survived logout")
	}
}

func TestDisabledAccountLosesSession(t *testing.T) {
	h := newHarness(t)
	h.login("reader", "reader-password-123")
	h.users.Users[0].Disabled = true
	h.srv.users = auth.NewStore(h.users)
	if rr := h.do("GET", "/api/v1/host/overview", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("disabled user kept access: %d", rr.Code)
	}
}

func TestSessionFixationDefence(t *testing.T) {
	h := newHarness(t)
	h.login("reader", "reader-password-123")
	first := h.cookie.Value
	h.login("reader", "reader-password-123")
	if h.cookie.Value == first {
		t.Fatal("session ID not rotated on login")
	}
	if _, ok := h.srv.Sessions().Get(first); ok {
		t.Fatal("previous session still valid after re-login")
	}
}

func TestSecurityHeadersAndUnknownRoutes(t *testing.T) {
	h := newHarness(t)
	rr := h.do("GET", "/", "", nil)
	for k, want := range map[string]string{"X-Frame-Options": "DENY", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer"} {
		if rr.Header().Get(k) != want {
			t.Errorf("%s = %q", k, rr.Header().Get(k))
		}
	}
	if csp := rr.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") || strings.Contains(csp, "unsafe") {
		t.Errorf("CSP: %s", csp)
	}
	if rr := h.do("GET", "/api/v1/exec?cmd=id", "", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown api route: %d", rr.Code)
	}
	if rr := h.do("GET", "/some/client/route", "", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "<title>hc") {
		t.Fatalf("SPA fallback: %d", rr.Code)
	}
	if rr := h.do("GET", "/../../etc/passwd", "", nil); strings.Contains(rr.Body.String(), "root:") {
		t.Fatal("path traversal")
	}
	if rr := h.do("POST", "/", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST to UI: %d", rr.Code)
	}
}

func TestSecureCookieModeUsesHostPrefix(t *testing.T) {
	h := newHarness(t)
	h.srv.cfg.SecureCookies = true
	b := `{"username":"reader","password":"reader-password-123"}`
	req := httptest.NewRequest("POST", "https://hc.test/api/v1/auth/login", strings.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://hc.test")
	rr := httptest.NewRecorder()
	h.srv.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	c := rr.Result().Cookies()[0]
	if c.Name != "__Host-hc_session" || !c.Secure {
		t.Fatalf("cookie %+v", c)
	}
	if rr.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS missing in secure mode")
	}
}
