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

// mdadmProtectedGET is the read-only software RAID bar on an
// identifier-bearing GET. The checks live in auditedGET; this wrapper keeps
// the mdadm audit message.
func (s *Server) mdadmProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	return s.auditedGET(w, r, sess, "Software RAID inventory requires an access audit logger.")
}

// handleMdadm serves the read-only software RAID inventory. No create,
// assemble, add, remove, grow, or mdadm execution exists: this endpoint Stats
// a well-known binary and reads bounded /proc/mdstat.
func (s *Server) handleMdadm(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.mdadmProtectedGET(w, r, sess) {
		return
	}
	if s.mdadmCollect == nil || s.mdadmSem == nil {
		writeError(w, http.StatusServiceUnavailable, "mdadm_unavailable", "Software RAID inventory is unavailable.")
		return
	}
	select {
	case s.mdadmSem <- struct{}{}:
		defer func() { <-s.mdadmSem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "mdadm_busy", "Software RAID inventory is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := s.mdadmCollect(ctx)
	if err != nil {
		_ = s.auditLog("mdadm.inventory.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "mdadm_unavailable", "Software RAID inventory could not be collected safely.")
		return
	}
	detail := "arrays=" + strconv.Itoa(len(snap.MD.Arrays))
	if err := s.auditLog("mdadm.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Software RAID inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}
