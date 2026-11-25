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
	"homelab-control/backend/internal/btrfsinfo"
	"homelab-control/backend/internal/storagetools"
)

// btrfsProtectedGET is the read-only Btrfs bar on an identifier-bearing GET.
// The checks live in auditedGET; this wrapper keeps the Btrfs audit message.
func (s *Server) btrfsProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	return s.auditedGET(w, r, sess, "Btrfs inventory requires an access audit logger.")
}

// btrfsResponse is the unprivileged inventory plus the per-filesystem report
// of the broker's btrfs.status op (btrfs-progs run by the read-only helper).
type btrfsResponse struct {
	btrfsinfo.Snapshot
	Status toolStatus[storagetools.BtrfsReport] `json:"status"`
}

// handleBtrfs serves the read-only Btrfs view: tooling, the kernel module and
// mounts (unprivileged), and, when the kernel has Btrfs and the broker is
// reachable, each filesystem's allocation, device errors, scrub status and
// subvolumes. No format, balance, scrub start, replace or other write exists.
func (s *Server) handleBtrfs(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.btrfsProtectedGET(w, r, sess) {
		return
	}
	if s.btrfsCollect == nil || s.btrfsSem == nil {
		writeError(w, http.StatusServiceUnavailable, "btrfs_unavailable", "Btrfs inventory is unavailable.")
		return
	}
	select {
	case s.btrfsSem <- struct{}{}:
		defer func() { <-s.btrfsSem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "btrfs_busy", "Btrfs inventory is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := s.btrfsCollect(ctx)
	if err != nil {
		_ = s.auditLog("btrfs.inventory.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "btrfs_unavailable", "Btrfs inventory could not be collected safely.")
		return
	}
	resp := btrfsResponse{Snapshot: snap, Status: toolReport[storagetools.BtrfsReport](r.Context(), s, sess, "btrfs.status", snap.KernelBtrfs.Present, "The kernel has no Btrfs loaded, so there are no filesystems to report.", "Filesystem status")}
	detail := "mounts=" + strconv.Itoa(len(snap.Btrfs.Mounts))
	if resp.Status.Report != nil {
		detail += " filesystems=" + strconv.Itoa(len(resp.Status.Report.Filesystems))
	}
	if err := s.auditLog("btrfs.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Btrfs inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
