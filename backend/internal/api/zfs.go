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

// zfsProtectedGET is the read-only OpenZFS bar on an identifier-bearing GET:
// the session CSRF token before anything is read or audited.
func (s *Server) zfsProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	if !auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_rejected", "missing or invalid CSRF token")
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != "null" && !s.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_rejected", "request origin not allowed")
		return false
	}
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "ZFS inventory requires an access audit logger.")
		return false
	}
	return true
}

// handleZFS serves the read-only OpenZFS inventory. No pool create, import,
// export, or zfs/zpool execution exists: this endpoint Stats well-known
// binaries, reads /proc/filesystems, and parses bounded mountinfo.
func (s *Server) handleZFS(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.zfsProtectedGET(w, r, sess) {
		return
	}
	if s.zfsCollect == nil || s.zfsSem == nil {
		writeError(w, http.StatusServiceUnavailable, "zfs_unavailable", "ZFS inventory is unavailable.")
		return
	}
	select {
	case s.zfsSem <- struct{}{}:
		defer func() { <-s.zfsSem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "zfs_busy", "ZFS inventory is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := s.zfsCollect(ctx)
	if err != nil {
		_ = s.auditLog("zfs.inventory.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "zfs_unavailable", "ZFS inventory could not be collected safely.")
		return
	}
	detail := "mounts=" + strconv.Itoa(len(snap.Zfs.Mounts))
	if err := s.auditLog("zfs.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "ZFS inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}
