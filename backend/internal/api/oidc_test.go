// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/oidclogin"
	"homelab-control/backend/internal/oidclogin/testidp"
)

const ssoRedirect = "https://hc.test" + oidclogin.CallbackPath

type ssoHarness struct {
	*harness
	idp  *testidp.IdP
	flow *oidclogin.Flow
}

// newSSO wires a harness to an isolated in-process TLS test IdP. The "full"
// account has configureScopes; sub-alice is bound to it, sub-reader to the
// disabled "off" account.
func newSSO(t *testing.T) *ssoHarness { return newSSOWith(t, "test-client-secret") }

func newSSOWith(t *testing.T, secret string) *ssoHarness {
	h := harnessWith(t, m23Fake(), configureScopes)
	h.cookie, h.csrf = nil, ""
	hash, _ := auth.HashPassword("off-password-1234", auth.MinIterations)
	h.users.Users = append(h.users.Users, auth.User{Username: "off", PasswordHash: hash, Scopes: configureScopes, Disabled: true})
	h.srv.users = auth.NewStore(h.users)
	idp := testidp.New(t, ssoRedirect)
	idp.ClientSecret = secret
	pool := x509.NewCertPool()
	pool.AddCert(idp.Certificate())
	cfg := oidclogin.Config{Version: 1, Issuer: idp.URL, ClientID: idp.ClientID, ClientSecret: idp.ClientSecret, RedirectURL: ssoRedirect,
		Accounts: []oidclogin.Binding{{Subject: "sub-alice", Username: "full"}, {Subject: "sub-off", Username: "off"}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	f := oidclogin.New(cfg, pool)
	h.srv.WithOIDC(f)
	return &ssoHarness{harness: h, idp: idp, flow: f}
}

func (s *ssoHarness) get(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	s.srv.ServeHTTP(rr, req)
	return rr
}

func cookieNamed(rr *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rr.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// start begins a login and returns the binding cookie and the IdP's redirect
// back to our callback (path+query), as a browser would see them.
func (s *ssoHarness) start() (*http.Cookie, string) {
	s.t.Helper()
	rr := s.get("/api/v1/auth/oidc/start")
	if rr.Code != http.StatusSeeOther {
		s.t.Fatalf("start: %d %s", rr.Code, rr.Body)
	}
	bind := cookieNamed(rr, "hc_oidc")
	if bind == nil || !bind.HttpOnly || bind.SameSite != http.SameSiteLaxMode {
		s.t.Fatalf("binding cookie: %+v", bind)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, s.idp.URL+"/authorize?") || !strings.Contains(loc, "code_challenge_method=S256") || !strings.Contains(loc, "nonce=") {
		s.t.Fatalf("authorize URL: %s", loc)
	}
	c := s.idp.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Get(loc)
	if err != nil {
		s.t.Fatal(err)
	}
	resp.Body.Close()
	back := resp.Header.Get("Location")
	if !strings.HasPrefix(back, ssoRedirect+"?") {
		s.t.Fatalf("idp redirect: %d %s", resp.StatusCode, back)
	}
	return bind, strings.TrimPrefix(back, "https://hc.test")
}

func (s *ssoHarness) callbackFails(t *testing.T, path string, cookies []*http.Cookie, reason string) {
	t.Helper()
	rr := s.get(path, cookies...)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != loginFailedURL || cookieNamed(rr, "hc_session") != nil {
		t.Fatalf("callback: %d %v", rr.Code, rr.Header())
	}
	if !strings.Contains(s.audit.String(), reason) {
		t.Fatalf("audit lacks %q: %s", reason, s.audit)
	}
}

func TestOIDCLoginIssuesExistingSession(t *testing.T) {
	s := newSSO(t)
	if rr := s.get("/api/v1/auth/oidc"); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"enabled":true`) {
		t.Fatalf("status: %d %s", rr.Code, rr.Body)
	}
	bind, cb := s.start()
	rr := s.get(cb, bind)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" {
		t.Fatalf("callback: %d %s %s", rr.Code, rr.Header().Get("Location"), s.audit)
	}
	sc := cookieNamed(rr, "hc_session")
	if sc == nil || !sc.HttpOnly || sc.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie: %+v", sc)
	}
	if c := cookieNamed(rr, "hc_oidc"); c == nil || c.MaxAge >= 0 {
		t.Fatalf("binding cookie not cleared: %+v", c)
	}
	// The ordinary session: local scopes, CSRF token, no step-up.
	s.cookie = sc
	sr := s.do("GET", "/api/v1/auth/session", "", nil)
	if sr.Code != 200 || !strings.Contains(sr.Body.String(), `"username":"full"`) || strings.Contains(sr.Body.String(), "step_up_until") {
		t.Fatalf("session: %d %s", sr.Code, sr.Body)
	}
	s.csrf = extractCSRF(t, sr.Body.String())
	// OIDC login never satisfies the password step-up for privileged applies.
	if r := s.post(enPlan, `{"action":"enable"}`); r.Code != 200 {
		t.Fatalf("plan: %d %s", r.Code, r.Body)
	}
	if r := s.post(enApply, `{"action":"enable","token":"`+validToken+`"}`); r.Code != http.StatusForbidden || !strings.Contains(r.Body.String(), "step_up_required") {
		t.Fatalf("apply without step-up: %d %s", r.Code, r.Body)
	}
	if r := s.do("POST", "/api/v1/auth/logout", "", map[string]string{"Origin": testOrigin, "X-CSRF-Token": s.csrf}); r.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", r.Code)
	}
	if r := s.do("GET", "/api/v1/auth/session", "", nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", r.Code)
	}
	a := s.audit.String()
	if !strings.Contains(a, `"event":"auth.oidc.login","user":"full"`) || !strings.Contains(a, `"outcome":"success"`) {
		t.Fatalf("audit: %s", a)
	}
	for _, secret := range []string{s.idp.LastIDToken, "test-access-token", s.idp.ClientSecret, "code="} {
		if secret == "" || strings.Contains(a, secret) {
			t.Fatalf("audit leaks %q", secret)
		}
	}
	// Replaying the same callback (state already consumed) fails.
	s.callbackFails(t, cb, []*http.Cookie{bind}, "unknown or expired state")
}

// A public client (no secret, PKCE only) completes the same flow; the test IdP
// then accepts only client_id in the form and no secret.
func TestOIDCPublicClient(t *testing.T) {
	s := newSSOWith(t, "")
	bind, cb := s.start()
	rr := s.get(cb, bind)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" || cookieNamed(rr, "hc_session") == nil {
		t.Fatalf("public client: %d %v %s", rr.Code, rr.Header(), s.audit)
	}
}

// An OIDC login that cannot be audited issues no cookie and leaves no session.
func TestOIDCLoginFailsClosedOnAudit(t *testing.T) {
	for name, al := range map[string]*audit.Logger{"broken": audit.New(brokenWriter{}), "nil": nil} {
		t.Run(name, func(t *testing.T) {
			s := newSSO(t)
			before, err := s.srv.sessions.List("full")
			if err != nil {
				t.Fatal(err)
			}
			bind, cb := s.start()
			s.srv.audit = al
			rr := s.get(cb, bind)
			if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != loginFailedURL || cookieNamed(rr, "hc_session") != nil {
				t.Fatalf("callback: %d %v", rr.Code, rr.Header())
			}
			after, err := s.srv.sessions.List("full")
			if err != nil || len(after) != len(before) {
				t.Fatalf("sessions before %d after %d (%v)", len(before), len(after), err)
			}
		})
	}
}

func extractCSRF(t *testing.T, body string) string {
	_, rest, ok := strings.Cut(body, `"csrf_token":"`)
	tok, _, _ := strings.Cut(rest, `"`)
	if !ok || tok == "" {
		t.Fatalf("no csrf token: %s", body)
	}
	return tok
}

func TestOIDCRejections(t *testing.T) {
	str := func(v string) *string { return &v }
	cases := []struct {
		name   string
		tamper func(s *ssoHarness)
		mangle func(cb string, bind *http.Cookie) (string, []*http.Cookie)
		reason string
	}{
		{"no binding cookie", nil, func(cb string, _ *http.Cookie) (string, []*http.Cookie) { return cb, nil }, "browser binding mismatch"},
		{"wrong binding cookie", nil, func(cb string, b *http.Cookie) (string, []*http.Cookie) {
			return cb, []*http.Cookie{{Name: b.Name, Value: "x" + b.Value}}
		}, "browser binding mismatch"},
		{"unknown state", nil, func(cb string, b *http.Cookie) (string, []*http.Cookie) {
			u, _ := url.Parse(cb)
			q := u.Query()
			q.Set("state", "forged")
			return u.Path + "?" + q.Encode(), []*http.Cookie{b}
		}, "unknown or expired state"},
		{"pkce verifier mismatch (code from another login)", nil, func(cb string, b *http.Cookie) (string, []*http.Cookie) {
			return cb, []*http.Cookie{b} // swapped below
		}, "code exchange failed"},
		{"provider error", func(s *ssoHarness) { s.idp.ErrorParam = "access_denied" }, nil, "provider returned an error"},
		{"wrong audience", func(s *ssoHarness) { s.idp.Audience = []string{"other-client"} }, nil, "id_token verification failed"},
		{"extra audience without azp", func(s *ssoHarness) { s.idp.Audience = []string{s.idp.ClientID, "other"} }, nil, "azp mismatch"},
		{"wrong issuer", func(s *ssoHarness) { s.idp.Issuer = "https://evil.test" }, nil, "id_token verification failed"},
		{"expired token", func(s *ssoHarness) { s.idp.Lifetime = -time.Minute }, nil, "id_token verification failed"},
		{"foreign signing key", func(s *ssoHarness) { s.idp.OtherKey = true }, nil, "id_token verification failed"},
		{"nonce mismatch", func(s *ssoHarness) { s.idp.Nonce = str("replayed-nonce") }, nil, "nonce mismatch"},
		{"unbound subject", func(s *ssoHarness) { s.idp.Subject = "sub-mallory" }, nil, "no local account is bound to subject sub-mallory"},
		{"disabled bound account", func(s *ssoHarness) { s.idp.Subject = "sub-off" }, nil, "bound account unavailable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSSO(t)
			if c.tamper != nil {
				c.tamper(s)
			}
			bind, cb := s.start()
			cookies := []*http.Cookie{bind}
			if strings.HasPrefix(c.name, "pkce") {
				// A code issued for login A presented with login B's state and
				// binding: the IdP rejects B's code_verifier.
				_, cbA := s.start()
				uA, _ := url.Parse(cbA)
				uB, _ := url.Parse(cb)
				q := uB.Query()
				q.Set("code", uA.Query().Get("code"))
				cb = uB.Path + "?" + q.Encode()
			} else if c.mangle != nil {
				cb, cookies = c.mangle(cb, bind)
			}
			s.callbackFails(t, cb, cookies, c.reason)
		})
	}
}

func TestOIDCExpiredStateAndDisabled(t *testing.T) {
	s := newSSO(t)
	bind, cb := s.start()
	s.flow.SetClock(func() time.Time { return time.Now().Add(11 * time.Minute) })
	s.callbackFails(t, cb, []*http.Cookie{bind}, "unknown or expired state")

	h := newHarness(t) // no OIDC configured
	for _, p := range []string{"/api/v1/auth/oidc/start", "/api/v1/auth/oidc/callback?state=a&code=b"} {
		rr := httptest.NewRecorder()
		h.srv.ServeHTTP(rr, httptest.NewRequest("GET", p, nil))
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s without OIDC: %d", p, rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	h.srv.ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/auth/oidc", nil))
	if !strings.Contains(rr.Body.String(), `"enabled":false`) {
		t.Fatal(rr.Body)
	}
}

func TestOIDCConfigValidation(t *testing.T) {
	ok := oidclogin.Config{Version: 1, Issuer: "https://idp.test/realm", ClientID: "hc", RedirectURL: "https://hc.lan" + oidclogin.CallbackPath}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(c *oidclogin.Config){
		"http issuer":       func(c *oidclogin.Config) { c.Issuer = "http://idp.test" },
		"issuer query":      func(c *oidclogin.Config) { c.Issuer = "https://idp.test/?x=1" },
		"no client":         func(c *oidclogin.Config) { c.ClientID = "" },
		"redirect path":     func(c *oidclogin.Config) { c.RedirectURL = "https://hc.lan/elsewhere" },
		"redirect http lan": func(c *oidclogin.Config) { c.RedirectURL = "http://hc.lan" + oidclogin.CallbackPath },
		"redirect query":    func(c *oidclogin.Config) { c.RedirectURL += "?next=//evil" },
		"duplicate subject": func(c *oidclogin.Config) {
			c.Accounts = []oidclogin.Binding{{Subject: "s", Username: "a"}, {Subject: "s", Username: "b"}}
		},
		"version": func(c *oidclogin.Config) { c.Version = 2 },
	} {
		c := ok
		mut(&c)
		if c.Validate() == nil {
			t.Errorf("%s accepted", name)
		}
	}
	dev := ok
	dev.RedirectURL = "http://127.0.0.1:9180" + oidclogin.CallbackPath
	if err := dev.Validate(); err != nil {
		t.Errorf("loopback dev redirect: %v", err)
	}
}
