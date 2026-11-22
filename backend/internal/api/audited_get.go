// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"net/http"

	"homelab-control/backend/internal/auth"
)

// auditedGET is the shared bar for identifier-bearing GETs. Server.handle
// skips Origin and CSRF for GET, so each such route must call this (or a
// thin wrapper) before it reads or audits. A same-origin browser GET omits
// Origin; an opaque "null" Origin is treated the same way. A present
// cross-origin Origin still has to match originAllowed. auditMissing is the
// 503 text when no access logger is configured. Layout, storage, compose,
// platforms, and Traefik stay on their own checks.
func (s *Server) auditedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session, auditMissing string) bool {
	if !auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_rejected", "missing or invalid CSRF token")
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != "null" && !s.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_rejected", "request origin not allowed")
		return false
	}
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", auditMissing)
		return false
	}
	return true
}
