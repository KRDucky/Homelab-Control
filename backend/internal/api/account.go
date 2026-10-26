// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/state"
)

// AuditPager reads the API audit trail (the state database).
type AuditPager interface {
	Page(before int64, limit int, prefix string) ([]state.AuditRecord, error)
}

// WithSessionStore makes sessions persistent (owner decision D-6).
func (s *Server) WithSessionStore(st auth.SessionStore) *Server {
	s.sessions = auth.NewSessionManagerWith(st)
	s.persistentSessions = true
	return s
}

// WithAuditPager enables GET /api/v1/audit.
func (s *Server) WithAuditPager(p AuditPager) *Server { s.auditPager = p; return s }

type sessionInfo struct {
	ID        string    `json:"id"` // SHA-256 of the cookie value: identifies, cannot be used to sign in
	Created   time.Time `json:"created"`
	LastSeen  time.Time `json:"last_seen"`
	RemoteIP  string    `json:"remote_ip,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
	Current   bool      `json:"current"`
}

type sessionsResponse struct {
	Persistent  bool          `json:"persistent"`
	IdleTimeout int           `json:"idle_timeout_seconds"`
	MaxLifetime int           `json:"max_lifetime_seconds"`
	Sessions    []sessionInfo `json:"sessions"`
}

// handleListSessions lists the caller's own sessions.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	list, err := s.sessions.List(sess.Username)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "state_unavailable", "Sessions could not be read from the state database.")
		return
	}
	resp := sessionsResponse{Persistent: s.persistentSessions, IdleTimeout: int(s.sessions.IdleTimeout.Seconds()),
		MaxLifetime: int(s.sessions.MaxLifetime.Seconds()), Sessions: []sessionInfo{}}
	for _, x := range list {
		resp.Sessions = append(resp.Sessions, sessionInfo{ID: x.Key, Created: x.Created, LastSeen: x.LastSeen,
			RemoteIP: x.RemoteIP, UserAgent: x.UserAgent, Current: x.Key == sess.Key})
	}
	writeJSON(w, http.StatusOK, resp)
}

var sessionKeyRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// handleRevokeSession ends one of the caller's own sessions.
func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	key := r.PathValue("id")
	if !sessionKeyRE.MatchString(key) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid session id")
		return
	}
	ok, err := s.sessions.Revoke(sess.Username, key)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "state_unavailable", "The session could not be revoked.")
		return
	}
	if !ok {
		// Unknown or another user's session: indistinguishable on purpose.
		s.auditLog("auth.session.revoke", sess.Username, r, "not_found", key[:12])
		writeError(w, http.StatusNotFound, "not_found", "no such session")
		return
	}
	s.stepups.Clear(key)
	s.auditLog("auth.session.revoke", sess.Username, r, "success", key[:12])
	if key == sess.Key {
		http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	}
	w.WriteHeader(http.StatusNoContent)
}

type auditResponse struct {
	Events     []state.AuditRecord `json:"events"`
	NextBefore int64               `json:"next_before,omitempty"`
	Source     string              `json:"source"`
}

var auditPrefixRE = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)

// handleAudit pages through the API audit trail, newest first.
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request, _ *auth.Session, _ string) {
	if s.auditPager == nil {
		writeError(w, http.StatusServiceUnavailable, "state_unavailable", "Audit history needs the state database (hc-api -state-db).")
		return
	}
	q := r.URL.Query()
	limit, before := 100, int64(0)
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be 1-500")
			return
		}
		limit = n
	}
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "bad_request", "before must be a positive record id")
			return
		}
		before = n
	}
	prefix := strings.TrimSpace(q.Get("event"))
	if prefix != "" && !auditPrefixRE.MatchString(prefix) {
		writeError(w, http.StatusBadRequest, "bad_request", "event filter must be an event name prefix such as auth.")
		return
	}
	ev, err := s.auditPager.Page(before, limit+1, prefix)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "state_unavailable", "The audit trail could not be read.")
		return
	}
	resp := auditResponse{Events: ev, Source: "hc-api state database (hash-chained; verify with hlctl audit verify --db)"}
	if len(ev) > limit {
		resp.Events = ev[:limit]
		resp.NextBefore = ev[limit-1].ID
	}
	writeJSON(w, http.StatusOK, resp)
}
