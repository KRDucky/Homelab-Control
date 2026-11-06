// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package oidclogin implements single sign-on with one administrator-configured
// OpenID Connect provider: authorization-code flow with PKCE (S256), a
// one-use expiring state bound to the browser by a cookie, a nonce, and ID
// token verification (signature, issuer, audience, expiry) by
// github.com/coreos/go-oidc. It only proves which external subject signed in;
// the caller maps that subject to an explicitly bound local account and issues
// the existing session. Tokens are never stored or logged.
package oidclogin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// CallbackPath is the only accepted redirect path; RedirectURL must end in it.
const CallbackPath = "/api/v1/auth/oidc/callback"

const (
	pendingTTL     = 10 * time.Minute
	maxPending     = 1024
	requestTimeout = 10 * time.Second
	maxResponse    = 1 << 20
)

// SigningAlgs are the accepted ID token signature algorithms ("none" and
// symmetric algorithms are never accepted).
var SigningAlgs = []string{oidc.RS256, oidc.ES256}

// Binding maps one external subject of the configured issuer to one existing
// local account. Only exact (issuer, subject) pairs are trusted; email,
// username or group claims are never used for linking.
type Binding struct {
	Subject  string `json:"subject"`
	Username string `json:"username"`
}

// Config is the on-disk OIDC setting (mode 0600: it may hold the client secret).
type Config struct {
	Version      int       `json:"version"`
	Issuer       string    `json:"issuer"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret,omitempty"` // empty: public client (PKCE only)
	RedirectURL  string    `json:"redirect_url"`
	Accounts     []Binding `json:"accounts"`
}

func isLoopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// Validate checks the security-relevant shape of the configuration.
func (c *Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported oidc config version %d", c.Version)
	}
	iss, err := url.Parse(c.Issuer)
	if err != nil || iss.Scheme != "https" || iss.Host == "" || iss.RawQuery != "" || iss.Fragment != "" || iss.User != nil {
		return errors.New("issuer must be an https URL without query, fragment or credentials")
	}
	if c.ClientID == "" || len(c.ClientID) > 256 || len(c.ClientSecret) > 1024 {
		return errors.New("client_id is required (at most 256 bytes); client_secret at most 1024 bytes")
	}
	ru, err := url.Parse(c.RedirectURL)
	if err != nil || ru.Host == "" || ru.Path != CallbackPath || ru.RawQuery != "" || ru.Fragment != "" || ru.User != nil {
		return fmt.Errorf("redirect_url must be scheme://host[:port]%s", CallbackPath)
	}
	if ru.Scheme != "https" && !(ru.Scheme == "http" && isLoopbackHost(ru.Hostname())) {
		return errors.New("redirect_url must use https (plain http only on loopback for development)")
	}
	seen := map[string]bool{}
	for _, b := range c.Accounts {
		if b.Subject == "" || len(b.Subject) > 255 || b.Username == "" {
			return errors.New("each account binding needs a subject (at most 255 bytes) and a username")
		}
		if seen[b.Subject] {
			return fmt.Errorf("subject %q is bound more than once", b.Subject)
		}
		seen[b.Subject] = true
	}
	return nil
}

// LoadConfig reads and validates the config, refusing symlinks and files
// accessible to group or other (same policy as the users file).
func LoadConfig(path string) (*Config, error) {
	fh, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s: must be a regular file with mode 0600", path)
	}
	dec := json.NewDecoder(io.LimitReader(fh, 64<<10))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// Failure is a login failure with a fixed, non-sensitive reason for the audit
// log. It never carries tokens, codes or provider response bodies.
type Failure struct{ Reason string }

func (f *Failure) Error() string { return "oidc login failed: " + f.Reason }

func fail(reason string) error { return &Failure{Reason: reason} }

type pending struct {
	binding  [32]byte // SHA-256 of the browser-binding cookie value
	nonce    string
	verifier string // PKCE code_verifier
	expires  time.Time
}

// Flow holds the provider client and the pending logins (in memory: a restart
// cancels logins in progress, the safe direction).
type Flow struct {
	cfg    Config
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
	pending  map[[32]byte]pending // by SHA-256(state)
}

// limitBody caps every provider response body: go-oidc reads discovery and
// JWKS documents without a size limit.
type limitBody struct{ rt http.RoundTripper }

func (l limitBody) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := l.rt.RoundTrip(r)
	if err == nil {
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.LimitReader(resp.Body, maxResponse), resp.Body}
	}
	return resp, err
}

// New returns a Flow. rootCAs nil uses the system roots (tests inject a CA).
func New(cfg Config, rootCAs *x509.CertPool) *Flow {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootCAs}
	return &Flow{
		cfg: cfg,
		client: &http.Client{
			Timeout:       requestTimeout,
			Transport:     limitBody{tr},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		now:     time.Now,
		pending: map[[32]byte]pending{},
	}
}

func (f *Flow) ctx(parent context.Context) (context.Context, context.CancelFunc) {
	c, cancel := context.WithTimeout(parent, requestTimeout)
	return oidc.ClientContext(c, f.client), cancel
}

// discoveryCategory maps a discovery error to a fixed description. It never
// returns text taken from the error itself.
func discoveryCategory(err error) string {
	var ue *url.Error
	var im *oidc.IssuerMismatchError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.As(err, &ue):
		return "could not connect (network or TLS error)"
	case errors.As(err, &im):
		return "issuer mismatch: the provider advertises a different issuer (check a trailing slash)"
	default:
		return "provider rejected the request or returned an invalid discovery document"
	}
}

// ensureProvider runs discovery once (lazily, so an unreachable provider
// never stops hc-api or local login) and pins the discovered endpoints.
func (f *Flow) ensureProvider(parent context.Context) error {
	if f.provider != nil {
		return nil
	}
	ctx, cancel := f.ctx(parent)
	defer cancel()
	p, err := oidc.NewProvider(ctx, f.cfg.Issuer) // also checks the advertised issuer equals ours
	if err != nil {
		// Journal only (not audit or browser), and only a fixed category:
		// the upstream error can quote the provider's response body.
		log.Printf("oidc discovery for %s failed: %s", f.cfg.Issuer, discoveryCategory(err))
		return fail("provider discovery failed")
	}
	var meta struct {
		JWKS string `json:"jwks_uri"`
	}
	if err := p.Claims(&meta); err != nil {
		return fail("provider discovery failed")
	}
	ep := p.Endpoint()
	for _, raw := range []string{ep.AuthURL, ep.TokenURL, meta.JWKS} {
		if u, err := url.Parse(raw); err != nil || u.Scheme != "https" || u.Host == "" {
			return fail("provider endpoint is not https")
		}
	}
	f.provider = p
	f.verifier = p.Verifier(&oidc.Config{ClientID: f.cfg.ClientID, SupportedSigningAlgs: SigningAlgs})
	f.oauth = oauth2.Config{ClientID: f.cfg.ClientID, ClientSecret: f.cfg.ClientSecret, Endpoint: ep,
		RedirectURL: f.cfg.RedirectURL, Scopes: []string{oidc.ScopeOpenID}}
	return nil
}

func random() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never fails (Go ≥ 1.24)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Begin starts a login. It returns the provider authorization URL and the
// browser-binding value the caller must set as a cookie.
func (f *Flow) Begin(ctx context.Context) (authURL, binding string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.ensureProvider(ctx); err != nil {
		return "", "", err
	}
	now := f.now()
	for k, p := range f.pending {
		if now.After(p.expires) {
			delete(f.pending, k)
		}
	}
	for len(f.pending) >= maxPending { // evict the oldest, like the session cap
		var oldest [32]byte
		var at time.Time
		for k, p := range f.pending {
			if at.IsZero() || p.expires.Before(at) {
				oldest, at = k, p.expires
			}
		}
		delete(f.pending, oldest)
	}
	state, nonce, verifier := random(), random(), oauth2.GenerateVerifier()
	binding = random()
	f.pending[sha256.Sum256([]byte(state))] = pending{binding: sha256.Sum256([]byte(binding)), nonce: nonce,
		verifier: verifier, expires: now.Add(pendingTTL)}
	return f.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), binding, nil
}

// Complete finishes a login from the callback parameters and the binding
// cookie. It returns the verified subject. The state is consumed whether or
// not the rest succeeds.
func (f *Flow) Complete(ctx context.Context, state, code, binding string) (string, error) {
	if state == "" || code == "" || len(state) > 256 || len(code) > 4096 {
		return "", fail("malformed callback")
	}
	key := sha256.Sum256([]byte(state))
	f.mu.Lock()
	p, ok := f.pending[key]
	delete(f.pending, key)
	o, v := f.oauth, f.verifier
	f.mu.Unlock()
	if !ok || f.now().After(p.expires) {
		return "", fail("unknown or expired state")
	}
	bh := sha256.Sum256([]byte(binding))
	if binding == "" || subtle.ConstantTimeCompare(bh[:], p.binding[:]) != 1 {
		return "", fail("browser binding mismatch")
	}
	cctx, cancel := f.ctx(ctx)
	defer cancel()
	tok, err := o.Exchange(cctx, code, oauth2.VerifierOption(p.verifier))
	if err != nil {
		return "", fail("code exchange failed")
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return "", fail("no id_token in token response")
	}
	idt, err := v.Verify(cctx, raw)
	if err != nil {
		return "", fail("id_token verification failed")
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(p.nonce)) != 1 {
		return "", fail("nonce mismatch")
	}
	if len(idt.Audience) > 1 {
		var c struct {
			AZP string `json:"azp"`
		}
		if idt.Claims(&c) != nil || c.AZP != f.cfg.ClientID {
			return "", fail("azp mismatch")
		}
	}
	if idt.Subject == "" {
		return "", fail("empty subject")
	}
	return idt.Subject, nil
}

// Username returns the local account bound to a subject of the configured
// issuer (exact match).
func (f *Flow) Username(subject string) (string, bool) {
	i := slices.IndexFunc(f.cfg.Accounts, func(b Binding) bool { return b.Subject == subject })
	if i < 0 {
		return "", false
	}
	return f.cfg.Accounts[i].Username, true
}

// Bindings returns the configured bindings (startup checks).
func (f *Flow) Bindings() []Binding { return slices.Clone(f.cfg.Accounts) }

// SetClock replaces the clock (tests only).
func (f *Flow) SetClock(now func() time.Time) { f.now = now }
