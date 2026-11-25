// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"homelab-control/backend/internal/storagetools"
	"homelab-control/backend/internal/zfsinfo"
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

// zfsResponse is the unprivileged inventory plus the pool report that the
// broker's zfs.status op returns (zpool list/status and zfs list, run by the
// read-only storage helper).
type zfsResponse struct {
	zfsinfo.Snapshot
	Status toolStatus[storagetools.ZFSReport] `json:"status"`
}

// handleZFS serves the read-only OpenZFS view: tooling, the kernel module and
// mounted datasets (unprivileged), and, when the kernel has ZFS and the broker
// is reachable, pools with health, capacity, scrub and the vdev tree. Nothing
// creates, imports, exports or changes a pool.
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
	resp := zfsResponse{Snapshot: snap, Status: toolReport[storagetools.ZFSReport](r.Context(), s, sess, "zfs.status", snap.KernelZfs.Present, "The kernel has no ZFS loaded, so there are no pools to report.", "Pool status")}
	detail := "mounts=" + strconv.Itoa(len(snap.Zfs.Mounts))
	if resp.Status.Report != nil {
		detail += " pools=" + strconv.Itoa(len(resp.Status.Report.Pools))
	}
	if err := s.auditLog("zfs.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "ZFS inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
