// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package testidp is an isolated, in-process OpenID Connect provider for
// tests only (never linked into a binary). It runs on an httptest TLS server,
// enforces exact client_id/redirect_uri, client authentication and the PKCE
// S256 challenge at the token endpoint, and signs ID tokens with go-oidc's
// oidctest helper. Knobs let tests produce specific bad tokens.
package testidp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	jose "github.com/go-jose/go-jose/v4"
)

// IdP is the test provider. Tamper fields are read when a token is issued.
type IdP struct {
	*httptest.Server
	ClientID, ClientSecret, RedirectURL string
	Subject                             string

	// Tamper knobs (zero values produce a valid token).
	Audience   []string      // overrides aud
	Issuer     string        // overrides iss
	Nonce      *string       // overrides nonce
	Lifetime   time.Duration // default 5 minutes; negative = expired
	OtherKey   bool          // sign with a key that is not in the JWKS
	ErrorParam string        // authorize redirects back with ?error=
	// Interactive makes /authorize show a page with a "Continue" link (the
	// navigation back is then initiated by the IdP's site, as with a real
	// login form) instead of redirecting at once.
	Interactive bool

	key, other *rsa.PrivateKey
	mu         sync.Mutex
	codes      map[string]grant
	// LastIDToken is the most recently issued raw ID token (for leak checks).
	LastIDToken string
}

type grant struct{ challenge, nonce, redirect string }

// New starts a provider; it is closed when the test ends.
func New(t testing.TB, redirectURL string) *IdP {
	p := Start(redirectURL)
	t.Cleanup(p.Close)
	return p
}

// Start starts a provider the caller must Close (browser fixture).
func Start(redirectURL string) *IdP {
	k1, _ := rsa.GenerateKey(rand.Reader, 2048)
	k2, _ := rsa.GenerateKey(rand.Reader, 2048)
	p := &IdP{ClientID: "hc-test", ClientSecret: "test-client-secret", RedirectURL: redirectURL, Subject: "sub-alice",
		key: k1, other: k2, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /keys", p.keys)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	p.Server = httptest.NewTLSServer(mux)
	return p
}

func (p *IdP) discovery(w http.ResponseWriter, _ *http.Request) {
	u := p.URL
	_ = json.NewEncoder(w).Encode(map[string]any{"issuer": u, "authorization_endpoint": u + "/authorize",
		"token_endpoint": u + "/token", "jwks_uri": u + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic"}})
}

func (p *IdP) keys(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
}

func (p *IdP) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != p.ClientID || q.Get("redirect_uri") != p.RedirectURL || q.Get("response_type") != "code" ||
		q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("scope") != "openid" {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	back := url.Values{"state": {q.Get("state")}}
	if p.ErrorParam != "" {
		back.Set("error", p.ErrorParam)
	} else {
		code := rand.Text()
		p.mu.Lock()
		p.codes[code] = grant{challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), redirect: q.Get("redirect_uri")}
		p.mu.Unlock()
		back.Set("code", code)
	}
	target := p.RedirectURL + "?" + back.Encode()
	if p.Interactive {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>Test IdP</title><a href="` + html.EscapeString(target) + `">Continue</a>`))
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (p *IdP) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if r.ParseForm() != nil {
		ok = false
	} else if p.ClientSecret == "" && !ok { // public client: client_id in the form, no secret
		id, ok = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret") == ""
	}
	if !ok || id != p.ClientID || secret != p.ClientSecret || r.PostForm.Get("grant_type") != "authorization_code" {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	p.mu.Lock()
	g, ok := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code")) // one use
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || g.redirect != r.PostForm.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	aud, iss, nonce, life := []string{p.ClientID}, p.URL, g.nonce, 5*time.Minute
	if p.Audience != nil {
		aud = p.Audience
	}
	if p.Issuer != "" {
		iss = p.Issuer
	}
	if p.Nonce != nil {
		nonce = *p.Nonce
	}
	if p.Lifetime != 0 {
		life = p.Lifetime
	}
	now := time.Now()
	claims, _ := json.Marshal(map[string]any{"iss": iss, "sub": p.Subject, "aud": aud, "nonce": nonce,
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(life).Unix(), "email": "alice@example.test"})
	key := p.key
	if p.OtherKey {
		key = p.other
	}
	raw := oidctest.SignIDToken(key, "k1", "RS256", string(claims))
	p.mu.Lock()
	p.LastIDToken = raw
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test-access-token", "token_type": "Bearer", "expires_in": 300, "id_token": raw})
}

// RandomText returns a random string (throwaway fixture credentials).
func RandomText() string { return rand.Text() }
