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

// ext4ProtectedGET is the read-only ext4 bar on an identifier-bearing GET.
// The checks live in auditedGET; this wrapper keeps the ext4 audit message.
func (s *Server) ext4ProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	return s.auditedGET(w, r, sess, "ext4 inventory requires an access audit logger.")
}

// handleExt4 serves the read-only ext4 inventory. No mkfs, tune2fs, resize2fs, or e2fsck exists. dumpe2fs is
// never executed: this endpoint Stats a well-known binary, reads
// /proc/filesystems, and parses bounded mountinfo.
func (s *Server) handleExt4(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.ext4ProtectedGET(w, r, sess) {
		return
	}
	if s.ext4Collect == nil || s.ext4Sem == nil {
		writeError(w, http.StatusServiceUnavailable, "ext4_unavailable", "ext4 inventory is unavailable.")
		return
	}
	select {
	case s.ext4Sem <- struct{}{}:
		defer func() { <-s.ext4Sem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "ext4_busy", "ext4 inventory is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := s.ext4Collect(ctx)
	if err != nil {
		_ = s.auditLog("ext4.inventory.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "ext4_unavailable", "ext4 inventory could not be collected safely.")
		return
	}
	detail := "mounts=" + strconv.Itoa(len(snap.Ext4.Mounts))
	if err := s.auditLog("ext4.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "ext4 inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}
