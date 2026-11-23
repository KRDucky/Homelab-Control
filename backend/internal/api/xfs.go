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

// xfsProtectedGET is the read-only XFS bar on an identifier-bearing GET.
// The checks live in auditedGET; this wrapper keeps the XFS audit message.
func (s *Server) xfsProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	return s.auditedGET(w, r, sess, "XFS inventory requires an access audit logger.")
}

// handleXFS serves the read-only XFS inventory. No mkfs.xfs, xfs_repair, or xfs_growfs exists. xfs_info is
// never executed: this endpoint Stats a well-known binary, reads
// /proc/filesystems, and parses bounded mountinfo.
func (s *Server) handleXFS(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.xfsProtectedGET(w, r, sess) {
		return
	}
	if s.xfsCollect == nil || s.xfsSem == nil {
		writeError(w, http.StatusServiceUnavailable, "xfs_unavailable", "XFS inventory is unavailable.")
		return
	}
	select {
	case s.xfsSem <- struct{}{}:
		defer func() { <-s.xfsSem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "xfs_busy", "XFS inventory is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := s.xfsCollect(ctx)
	if err != nil {
		_ = s.auditLog("xfs.inventory.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "xfs_unavailable", "XFS inventory could not be collected safely.")
		return
	}
	detail := "mounts=" + strconv.Itoa(len(snap.Xfs.Mounts))
	if err := s.auditLog("xfs.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "XFS inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}
