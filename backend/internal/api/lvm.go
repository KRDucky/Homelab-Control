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

// lvmProtectedGET is the read-only LVM bar on an identifier-bearing GET:
// the session CSRF token before anything is read or audited.
func (s *Server) lvmProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	if !auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_rejected", "missing or invalid CSRF token")
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != "null" && !s.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_rejected", "request origin not allowed")
		return false
	}
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "LVM inventory requires an access audit logger.")
		return false
	}
	return true
}

// handleLVM serves the read-only LVM inventory. No PV, VG, or LV create or
// remove exists, and lvm is never executed: this endpoint Stats well-known
// binaries, reads /proc/devices, and lists bounded sysfs dm names.
func (s *Server) handleLVM(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.lvmProtectedGET(w, r, sess) {
		return
	}
	if s.lvmCollect == nil || s.lvmSem == nil {
		writeError(w, http.StatusServiceUnavailable, "lvm_unavailable", "LVM inventory is unavailable.")
		return
	}
	select {
	case s.lvmSem <- struct{}{}:
		defer func() { <-s.lvmSem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "lvm_busy", "LVM inventory is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := s.lvmCollect(ctx)
	if err != nil {
		_ = s.auditLog("lvm.inventory.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "lvm_unavailable", "LVM inventory could not be collected safely.")
		return
	}
	detail := "devices=" + strconv.Itoa(len(snap.Lvm.Devices))
	if err := s.auditLog("lvm.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "LVM inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}
