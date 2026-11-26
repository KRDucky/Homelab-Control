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

// f2fsProtectedGET is the read-only f2fs bar on an identifier-bearing GET.
// The checks live in auditedGET; this wrapper keeps the f2fs audit message.
func (s *Server) f2fsProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	return s.auditedGET(w, r, sess, "f2fs inventory requires an access audit logger.")
}

// handleF2fs serves the read-only f2fs inventory. No mkfs.f2fs or resize.f2fs exists. dump.f2fs is
// never executed: this endpoint Stats a well-known binary, reads
// /proc/filesystems, and parses bounded mountinfo.
func (s *Server) handleF2fs(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.f2fsProtectedGET(w, r, sess) {
		return
	}
	if s.f2fsCollect == nil || s.f2fsSem == nil {
		writeError(w, http.StatusServiceUnavailable, "f2fs_unavailable", "f2fs inventory is unavailable.")
		return
	}
	select {
	case s.f2fsSem <- struct{}{}:
		defer func() { <-s.f2fsSem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "f2fs_busy", "f2fs inventory is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := s.f2fsCollect(ctx)
	if err != nil {
		_ = s.auditLog("f2fs.inventory.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "f2fs_unavailable", "f2fs inventory could not be collected safely.")
		return
	}
	detail := "mounts=" + strconv.Itoa(len(snap.F2fs.Mounts)) + " filesystems=" + strconv.Itoa(len(snap.Status.Filesystems))
	if err := s.auditLog("f2fs.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "f2fs inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}
