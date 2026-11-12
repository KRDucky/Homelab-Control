// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"homelab-control/backend/internal/auth"
)

// mergerfsProtectedGET is the read-only MergerFS + SnapRAID bar on an
// identifier-bearing GET: the session CSRF token before anything is read or
// audited, the same as packagesProtectedGET. A present (cross-origin) Origin
// must still match originAllowed.
func (s *Server) mergerfsProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	if !auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_rejected", "missing or invalid CSRF token")
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != "null" && !s.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_rejected", "request origin not allowed")
		return false
	}
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "MergerFS inventory requires an access audit logger.")
		return false
	}
	return true
}

// handleMergerFS serves the read-only MergerFS + optional SnapRAID inventory.
// No pool create, mount, policy write, or snapraid sync/diff/fix path exists:
// this endpoint only parses bounded /proc/self/mountinfo and one configured
// snapraid.conf, and opens none of the listed branch or data paths.
func (s *Server) handleMergerFS(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.mergerfsProtectedGET(w, r, sess) {
		return
	}
	if s.mergerfsCollect == nil || s.mergerfsSem == nil {
		writeError(w, http.StatusServiceUnavailable, "mergerfs_unavailable", "MergerFS inventory is unavailable.")
		return
	}
	select {
	case s.mergerfsSem <- struct{}{}:
		defer func() { <-s.mergerfsSem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "mergerfs_busy", "MergerFS inventory is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := s.mergerfsCollect(ctx)
	if err != nil || ctx.Err() != nil {
		_ = s.auditLog("mergerfs.inventory.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "mergerfs_unavailable", "MergerFS inventory could not be collected safely.")
		return
	}
	// Audit counts only, never branch, data, parity or content paths.
	detail := "pools=" + strconv.Itoa(len(snap.MergerFS.Pools)) + " snapraid=" + strconv.FormatBool(snap.SnapRAID.Present)
	if err := s.auditLog("mergerfs.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "MergerFS inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}
