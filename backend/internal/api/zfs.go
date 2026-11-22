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

// zfsProtectedGET is the read-only OpenZFS bar on an identifier-bearing GET.
// The checks live in auditedGET; this wrapper keeps the ZFS audit message.
func (s *Server) zfsProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	return s.auditedGET(w, r, sess, "ZFS inventory requires an access audit logger.")
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
