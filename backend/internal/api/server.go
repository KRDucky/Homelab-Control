// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package api implements the versioned Homelab Control HTTP API (v1).
//
// Security properties enforced here (see docs/adr/0002-api-session-model.md):
//   - every /api/v1 route except /health and /auth/login requires a session;
//   - every route declares a required RBAC scope, checked server-side;
//   - unsafe methods require a matching Origin and a per-session CSRF token;
//   - JSON bodies are size-limited and decoded strictly;
//   - responses carry restrictive security headers and are never cached.
//
// This package must not execute processes. TestNoProcessExecution enforces it.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/containeridentity"
	"homelab-control/backend/internal/filesinfo"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/hwinfo"
	"homelab-control/backend/internal/mergerfsinfo"
	"homelab-control/backend/internal/modules"
	"homelab-control/backend/internal/oidclogin"
	"homelab-control/backend/internal/platform"
	"homelab-control/backend/internal/storageinfo"
	"homelab-control/backend/internal/traefikclient"
	"homelab-control/backend/internal/version"
)

// Version is the build version (see internal/version).
var Version = version.Version

// Config holds server settings.
type Config struct {
	// SecureCookies must be true whenever TLS terminates at this process or
	// at a trusted reverse proxy. It enables the __Host- cookie prefix.
	SecureCookies bool
	// AllowedOrigins are additional exact origins (scheme://host[:port]) that
	// may perform unsafe requests, e.g. when served behind a reverse proxy.
	AllowedOrigins []string
	LegacyURL      string
}

// Server wires handlers, sessions and collectors.
type Server struct {
	traefik           traefikclient.Helper
	traefikDynamicDir string
	composeApplyDir   string
	traefikSetupDir   string
	discovery         composediscovery.Provider
	platforms         *platform.Registry
	identity          *containeridentity.Config
	storageCollect    func(context.Context) (storageinfo.Graph, error)
	storageSem        chan struct{}
	storageSnapshots  StorageSnapshotStore
	mergerfsCollect   func(context.Context) (mergerfsinfo.Snapshot, error)
	mergerfsSem       chan struct{}
	snapraidConfig    string
	layouts           LayoutStore
	cfg               Config
	users             *auth.Store
	sessions          *auth.SessionManager
	throttle          *auth.Throttle
	audit             *audit.Logger
	collector         *hostinfo.Collector
	ui                fs.FS
	loginSem          chan struct{}
	mux               *http.ServeMux
	overview          overviewCache
	broker            Broker
	hw                *hwinfo.Collector
	bstate            brokerState

	persistentSessions bool
	auditPager         AuditPager
	stepups            *auth.StepUps
	// mods is the administrator's module setting (M2-3). Routes of a
	// disabled module answer exactly like unknown routes.
	mods modules.Set
	// files is the administrator's confined-root allow-list (file manager).
	// The zero value has no roots (fail closed, no browse).
	files filesinfo.Set
	// oidc is the optional single sign-on provider (nil = disabled).
	oidc *oidclogin.Flow
}

type overviewCache struct {
	mu   sync.Mutex
	at   time.Time
	data *hostinfo.Overview
}

// New constructs a Server. ui may be nil (API only).
func New(cfg Config, users *auth.Store, audit *audit.Logger, collector *hostinfo.Collector, ui fs.FS) *Server {
	s := &Server{
		cfg: cfg, users: users, sessions: auth.NewSessionManager(), throttle: auth.NewThrottle(),
		audit: audit, collector: collector, ui: ui, loginSem: make(chan struct{}, 4), mux: http.NewServeMux(),
		stepups: auth.NewStepUps(),
	}
	s.storageCollect = func(ctx context.Context) (storageinfo.Graph, error) { return storageinfo.Collect(ctx, "/") }
	s.storageSem = make(chan struct{}, 1)
	s.snapraidConfig = mergerfsinfo.DefaultSnapRAIDPath
	s.mergerfsCollect = func(ctx context.Context) (mergerfsinfo.Snapshot, error) {
		return mergerfsinfo.Collect(ctx, "/", s.snapraidConfig, s.packageFamily())
	}
	s.mergerfsSem = make(chan struct{}, 1)
	s.routes()
	return s
}

// WithModules applies the administrator's module setting (M2-3).
func (s *Server) WithModules(m modules.Set) *Server { s.mods = m; return s }

// WithSnapRAIDConfig sets the snapraid.conf path for the read-only MergerFS
// page (default /etc/snapraid.conf).
func (s *Server) WithSnapRAIDConfig(path string) *Server { s.snapraidConfig = path; return s }

// WithFiles applies the administrator's confined-root allow-list.
func (s *Server) WithFiles(f filesinfo.Set) *Server { s.files = f; return s }

// Sessions exposes the session manager (tests).
func (s *Server) Sessions() *auth.SessionManager { return s.sessions }

func (s *Server) cookieName() string {
	if s.cfg.SecureCookies {
		return "__Host-hc_session"
	}
	return "hc_session"
}

// scopePublic marks the few routes that do not require a session.
const scopePublic = ""

func (s *Server) routes() {
	s.handle("GET /api/v1/networking/reverse-proxy/traefik/inventory", auth.ScopeNetworkingProxyRead, s.handleTraefikInventory)
	s.handle("GET /api/v1/networking/reverse-proxy/traefik/diagnostics", auth.ScopeNetworkingProxyRead, s.handleTraefikDiagnostics)
	s.handle("POST /api/v1/networking/reverse-proxy/traefik/labels", auth.ScopeNetworkingProxyRead, s.handleTraefikLabels)
	s.handle("GET /api/v1/networking/reverse-proxy/traefik/applications", auth.ScopeComposeRead, s.handleComposeDiscovery)
	s.handle("POST /api/v1/networking/reverse-proxy/traefik/compose-preview", auth.ScopeNetworkingProxyRead, s.handleComposePreview)
	s.handle("POST /api/v1/networking/reverse-proxy/traefik/file-apply", auth.ScopeNetworkingProxyApply, s.handleTraefikFileApply)
	s.handle("POST /api/v1/networking/reverse-proxy/traefik/compose-apply", auth.ScopeNetworkingProxyApply, s.handleTraefikComposeApply)
	s.handle("POST /api/v1/networking/reverse-proxy/traefik/setup", auth.ScopeNetworkingProxyApply, s.handleTraefikSetup)
	s.handle("POST /api/v1/networking/reverse-proxy/traefik/platform-preview", auth.ScopeNetworkingProxyRead, s.handlePlatformPreview)
	s.handle("GET /api/v1/platforms", auth.ScopePlatformsRead, s.handlePlatforms)
	s.handle("GET /api/v1/platforms/{id}/inventory", auth.ScopePlatformsRead, s.handlePlatformInventory)
	s.handle("GET /api/v1/containers/identity/profiles", auth.ScopePlatformsRead, s.handleIdentityProfiles)
	s.handle("POST /api/v1/containers/identity/preview", auth.ScopePlatformsRead, s.handleIdentityPreview)
	s.handle("GET /api/v1/health", scopePublic, s.handleHealth)
	s.handle("POST /api/v1/auth/login", scopePublic, s.handleLogin)
	s.handle("GET /api/v1/auth/oidc", scopePublic, s.handleOIDCStatus)
	s.handle("GET /api/v1/auth/oidc/start", scopePublic, s.handleOIDCStart)
	s.handle("GET /api/v1/auth/oidc/callback", scopePublic, s.handleOIDCCallback)
	s.handle("POST /api/v1/auth/logout", sessionOnly, s.handleLogout)
	s.handle("GET /api/v1/auth/session", sessionOnly, s.handleSession)
	s.handle("GET /api/v1/auth/sessions", sessionOnly, s.handleListSessions)
	s.handle("POST /api/v1/auth/sessions/{id}/revoke", sessionOnly, s.handleRevokeSession)
	s.handle("GET /api/v1/audit", auth.ScopeAuditRead, s.handleAudit)
	s.handle("GET /api/v1/host/overview", auth.ScopeSystemRead, s.handleOverview)
	// M2-6: read-only host settings, non-executing preview, and ADR 0011 plan/apply.
	s.handle("GET /api/v1/host/settings", auth.ScopeSystemRead, s.handleHostSettings)
	s.handle("POST /api/v1/host/settings/preview", auth.ScopeHostSettingsPreview, s.handleHostSettingsPreview)
	s.handle("POST /api/v1/host/settings/plan", auth.ScopeHostSettingsApply, s.handleHostSettingsPlan)
	s.handle("POST /api/v1/host/settings/apply", auth.ScopeHostSettingsApply, s.handleHostSettingsApply)
	s.handle("GET /api/v1/capabilities", sessionOnly, s.handleCapabilities)
	s.handle("GET /api/v1/services", auth.ScopeServicesRead, s.handleServices)
	s.handle("GET /api/v1/services/units/{unit}/logs", auth.ScopeServicesLogs, s.handleUnitLogs)
	s.handle("POST /api/v1/auth/step-up", sessionOnly, s.handleStepUp)
	s.handle("POST /api/v1/services/units/{unit}/plan", auth.ScopeServicesOperate, s.handleUnitPlan)
	s.handle("POST /api/v1/services/units/{unit}/apply", auth.ScopeServicesOperate, s.handleUnitApply)
	// M2-3: read-only views and boot-time enable/disable.
	s.handle("GET /api/v1/services/timers", auth.ScopeServicesRead, s.handleTimers)
	s.handle("GET /api/v1/services/sockets", auth.ScopeServicesRead, s.handleSockets)
	s.handle("GET /api/v1/services/units/{unit}", auth.ScopeServicesRead, s.handleUnitDetail)
	s.handle("GET /api/v1/services/units/{unit}/contents", auth.ScopeServicesFiles, s.handleUnitContents)
	s.handle("POST /api/v1/services/units/{unit}/enablement/plan", auth.ScopeServicesConfigure, s.handleEnablementPlan)
	s.handle("POST /api/v1/services/units/{unit}/enablement/apply", auth.ScopeServicesConfigure, s.handleEnablementApply)
	s.handle("GET /api/v1/operations", auth.ScopeAuditRead, s.handleOperations)
	s.handle("GET /api/v1/hardware", auth.ScopeHardwareRead, s.handleHardware)
	s.handle("GET /api/v1/storage", auth.ScopeStorageRead, s.handleStorage)
	s.handle("GET /api/v1/files/roots", auth.ScopeFilesRead, s.handleFilesRoots)
	s.handle("GET /api/v1/files/entries", auth.ScopeFilesRead, s.handleFilesEntries)
	// M2-5 Stage A: application-owned manual drive-bay layout (no host writes).
	s.handle("GET /api/v1/storage/layout", auth.ScopeStorageRead, s.handleLayout)
	s.handle("POST /api/v1/storage/layout/chassis", auth.ScopeLayoutManage, s.handleLayoutCreate)
	s.handle("PUT /api/v1/storage/layout/chassis/{id}", auth.ScopeLayoutManage, s.handleLayoutUpdate)
	s.handle("DELETE /api/v1/storage/layout/chassis/{id}", auth.ScopeLayoutManage, s.handleLayoutDelete)
	s.handle("PUT /api/v1/storage/layout/chassis/{id}/bays/{row}/{col}/assignment", auth.ScopeLayoutManage, s.handleLayoutAssign)
	s.handle("DELETE /api/v1/storage/layout/chassis/{id}/bays/{row}/{col}/assignment", auth.ScopeLayoutManage, s.handleLayoutUnassign)
	// M4-1b: read-only Software Center (modules.json module, no writes).
	s.handle("GET /api/v1/packages/search", auth.ScopePackagesRead, s.handlePackagesSearch)
	s.handle("GET /api/v1/packages/installed", auth.ScopePackagesRead, s.handlePackagesInstalled)
	s.handle("GET /api/v1/packages/updates", auth.ScopePackagesRead, s.handlePackagesUpdates)
	s.handle("GET /api/v1/packages/sources", auth.ScopePackagesRead, s.handlePackagesSources)
	// M8 first checkpoint: read-only MergerFS + optional SnapRAID (no broker ops).
	s.handle("GET /api/v1/mergerfs", auth.ScopeMergerFSRead, s.handleMergerFS)
	s.handle("GET /api/v1/legacy/webmin", auth.ScopeSystemRead, s.handleLegacy)
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "unknown API route")
	})
	s.mux.HandleFunc("/", s.handleUI)
}

// sessionOnly requires authentication but no particular scope.
const sessionOnly = "\x00session"

type ctxHandler func(w http.ResponseWriter, r *http.Request, sess *auth.Session, sid string)

// handle registers a route with mandatory authn/authz/CSRF wrapping. There is
// deliberately no way to register an /api route that bypasses this function.
func (s *Server) handle(pattern, scope string, h ctxHandler) {
	_, routePath, _ := strings.Cut(pattern, " ")
	module := modules.ForRoute(routePath)
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		// A disabled module's routes do not exist: same answer as the
		// catch-all for unknown routes, before authentication or CSRF checks.
		if !s.mods.Enabled(module) {
			writeError(w, http.StatusNotFound, "not_found", "unknown API route")
			return
		}
		unsafe := r.Method != http.MethodGet && r.Method != http.MethodHead
		if unsafe && !s.originAllowed(r) {
			s.auditLog("csrf.origin_rejected", "", r, "denied", r.Header.Get("Origin"))
			writeError(w, http.StatusForbidden, "origin_rejected", "request origin not allowed")
			return
		}
		if scope == scopePublic {
			h(w, r, nil, "")
			return
		}
		c, err := r.Cookie(s.cookieName())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "login required")
			return
		}
		sess, ok := s.sessions.Get(c.Value)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "session expired or invalid")
			return
		}
		// Re-check the account on every request so disabling a user takes effect.
		u, ok := s.users.Lookup(sess.Username)
		if !ok || u.Disabled {
			s.sessions.Destroy(c.Value)
			s.stepups.ClearUser(sess.Username)
			writeError(w, http.StatusUnauthorized, "unauthenticated", "account unavailable")
			return
		}
		// Effective scopes: those granted at login AND still granted to the
		// account now. Removing a scope (e.g. services.operate) therefore takes
		// effect on persisted sessions too; adding one needs a new login.
		sess.Scopes = slices.DeleteFunc(sess.Scopes, func(sc string) bool { return !slices.Contains(u.Scopes, sc) })
		if unsafe && !auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) {
			s.auditLog("csrf.token_rejected", sess.Username, r, "denied", "")
			writeError(w, http.StatusForbidden, "csrf_rejected", "missing or invalid CSRF token")
			return
		}
		if scope != sessionOnly && !sess.HasScope(scope) {
			s.auditLog("authz.denied", sess.Username, r, "denied", scope)
			writeError(w, http.StatusForbidden, "forbidden", "missing scope "+scope)
			return
		}
		h(w, r, sess, c.Value)
	})
}

// originAllowed implements the Origin check for unsafe requests. Requests
// without an Origin header are rejected: all browsers send Origin on
// cross-origin and same-origin POSTs, and M0 has no non-browser API clients.
func (s *Server) originAllowed(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	for _, a := range s.cfg.AllowedOrigins {
		if strings.EqualFold(o, a) {
			return true
		}
	}
	wantScheme := "http"
	if r.TLS != nil || s.cfg.SecureCookies {
		wantScheme = "https"
	}
	return u.Scheme == wantScheme && strings.EqualFold(u.Host, r.Host)
}

// ServeHTTP applies global headers and limits.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), usb=(), serial=()")
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	if s.cfg.SecureCookies {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.Set("Cache-Control", "no-store")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	s.mux.ServeHTTP(w, r)
}

type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: code, Message: msg})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) auditLog(event, user string, r *http.Request, outcome, detail string) error {
	if s.audit == nil {
		return nil
	}
	err := s.audit.Log(audit.Event{Event: event, User: user, Remote: remoteIP(r), Outcome: outcome, Detail: detail})
	if err != nil {
		log.Printf("audit write failed (event %s): %v", event, err)
	}
	return err
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request, _ *auth.Session, _ string) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "api": "v1"})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type sessionResponse struct {
	Username  string   `json:"username"`
	Scopes    []string `json:"scopes"`
	CSRFToken string   `json:"csrf_token"`
	Version   string   `json:"version"`
	LegacyURL string   `json:"legacy_url,omitempty"`
	// StepUpUntil is when the session's password re-entry (D-3) lapses.
	StepUpUntil *time.Time `json:"step_up_until,omitempty"`
}

func (s *Server) sessionBody(sess *auth.Session) sessionResponse {
	r := sessionResponse{Username: sess.Username, Scopes: sess.Scopes, CSRFToken: sess.CSRFToken, Version: Version, LegacyURL: s.cfg.LegacyURL}
	if at, ok := s.stepups.At(sess.Key, sess.Username); ok {
		u := at.Add(auth.StepUpWindow).UTC()
		r.StepUpUntil = &u
	}
	return r
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request, _ *auth.Session, _ string) {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "expected application/json")
		return
	}
	var req loginRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	ip := remoteIP(r)
	if auth.ValidateUsername(req.Username) != nil || req.Password == "" || len(req.Password) > 1024 {
		s.auditLog("auth.login", "", r, "failure", "malformed credentials")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	if !s.throttle.Allowed(req.Username, ip) {
		s.auditLog("auth.login", req.Username, r, "throttled", "")
		writeError(w, http.StatusTooManyRequests, "throttled", "too many failed attempts; try again later")
		return
	}
	select {
	case s.loginSem <- struct{}{}:
		defer func() { <-s.loginSem }()
	case <-r.Context().Done():
		return
	}
	u, ok := s.users.Authenticate(req.Username, req.Password)
	if !ok {
		s.throttle.Fail(req.Username, ip)
		s.auditLog("auth.login", req.Username, r, "failure", "")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	s.throttle.Succeed(req.Username, ip)
	// Fixation defence: any session cookie presented at login is discarded.
	if old, err := r.Cookie(s.cookieName()); err == nil {
		s.sessions.Destroy(old.Value)
	}
	sid, sess, err := s.sessions.Create(u, ip, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create session")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: sid, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	s.auditLog("auth.login", u.Username, r, "success", "")
	writeJSON(w, http.StatusOK, s.sessionBody(sess))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, sess *auth.Session, sid string) {
	s.sessions.Destroy(sid)
	s.stepups.Clear(sess.Key)
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	s.auditLog("auth.logout", sess.Username, r, "success", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	writeJSON(w, http.StatusOK, s.sessionBody(sess))
}

// handleOverview serves a cached overview (2 s) so rapid refreshes cannot be
// used to keep the collector busy.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request, _ *auth.Session, _ string) {
	s.overview.mu.Lock()
	defer s.overview.mu.Unlock()
	if s.overview.data == nil || time.Since(s.overview.at) > 2*time.Second {
		o := s.collector.Collect()
		s.overview.data, s.overview.at = &o, time.Now()
	}
	writeJSON(w, http.StatusOK, s.overview.data)
}

// handleUI serves the embedded single-page app. Unknown paths fall back to
// index.html so client-side routes work; path traversal is impossible because
// fs.FS rejects ".." and the path is cleaned first.
func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	if s.ui == nil {
		http.Error(w, "UI not built: run `make frontend` (API is available under /api/v1)", http.StatusNotFound)
		return
	}
	p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if p == "" {
		p = "index.html"
	}
	if st, err := fs.Stat(s.ui, p); err != nil || st.IsDir() {
		if errors.Is(err, fs.ErrNotExist) || (err == nil && st.IsDir()) {
			p = "index.html"
		}
	}
	if strings.HasPrefix(p, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if _, err := fs.Stat(s.ui, p); err != nil {
		http.Error(w, "UI not built: run `make frontend` (API is available under /api/v1)", http.StatusNotFound)
		return
	}
	http.ServeFileFS(w, r, s.ui, p)
}
