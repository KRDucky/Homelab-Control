// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"errors"
	"net/http"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/oidclogin"
)

// OIDC single sign-on (working skeleton). The provider only proves which
// subject signed in; the session issued is the ordinary local session of the
// bound account (same cookie, CSRF token, scopes, revocation and audit). An
// OIDC login never records a password step-up (D-3): privileged applies still
// require re-entering the local account's password.

// loginFailedURL is the only place a failed SSO login is sent (no
// caller-supplied return URL, so no open redirect).
const loginFailedURL = "/?login=sso_failed"

// WithOIDC enables single sign-on with one configured provider.
func (s *Server) WithOIDC(f *oidclogin.Flow) *Server { s.oidc = f; return s }

func (s *Server) oidcCookieName() string {
	if s.cfg.SecureCookies {
		return "__Host-hc_oidc"
	}
	return "hc_oidc"
}

// setOIDCCookie sets or clears the browser-binding cookie. SameSite=Lax is
// required: the provider's redirect back is a cross-site top-level GET, on
// which Strict cookies are not sent. It authorizes nothing by itself.
func (s *Server) setOIDCCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: s.oidcCookieName(), Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
}

type oidcStatus struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) handleOIDCStatus(w http.ResponseWriter, _ *http.Request, _ *auth.Session, _ string) {
	writeJSON(w, http.StatusOK, oidcStatus{Enabled: s.oidc != nil})
}

func failureReason(err error) string {
	var f *oidclogin.Failure
	if errors.As(err, &f) {
		return f.Reason
	}
	return "internal error"
}

func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request, _ *auth.Session, _ string) {
	if s.oidc == nil {
		writeError(w, http.StatusNotFound, "not_found", "single sign-on is not configured")
		return
	}
	authURL, binding, err := s.oidc.Begin(r.Context())
	if err != nil {
		s.auditLog("auth.oidc.login", "", r, "failure", failureReason(err))
		http.Redirect(w, r, loginFailedURL, http.StatusSeeOther)
		return
	}
	s.setOIDCCookie(w, binding, 600)
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request, _ *auth.Session, _ string) {
	if s.oidc == nil {
		writeError(w, http.StatusNotFound, "not_found", "single sign-on is not configured")
		return
	}
	s.setOIDCCookie(w, "", -1) // one attempt per binding, whatever the outcome
	failed := func(user, reason string) {
		s.auditLog("auth.oidc.login", user, r, "failure", reason)
		http.Redirect(w, r, loginFailedURL, http.StatusSeeOther)
	}
	q := r.URL.Query()
	if q.Has("error") { // the provider's error text is not logged or reflected
		failed("", "provider returned an error")
		return
	}
	binding := ""
	if c, err := r.Cookie(s.oidcCookieName()); err == nil {
		binding = c.Value
	}
	sub, err := s.oidc.Complete(r.Context(), q.Get("state"), q.Get("code"), binding)
	if err != nil {
		failed("", failureReason(err))
		return
	}
	name, ok := s.oidc.Username(sub)
	if !ok {
		failed("", "no local account is bound to subject "+truncate(sub, 128))
		return
	}
	u, ok := s.users.Lookup(name)
	if !ok || u.Disabled {
		failed(name, "bound account unavailable")
		return
	}
	if old, err := r.Cookie(s.cookieName()); err == nil { // fixation defence, as for password login
		s.sessions.Destroy(old.Value)
	}
	sid, _, err := s.sessions.Create(u, remoteIP(r), r.UserAgent())
	if err != nil {
		failed(name, "could not create session")
		return
	}
	// Fail closed: the session is handed to the browser only once its login
	// is in the audit trail. A missing or failing audit logger destroys it.
	if s.audit == nil || s.auditLog("auth.oidc.login", u.Username, r, "success", "") != nil {
		s.sessions.Destroy(sid)
		http.Redirect(w, r, loginFailedURL, http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: sid, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func truncate(v string, n int) string {
	if len(v) > n {
		return v[:n]
	}
	return v
}
