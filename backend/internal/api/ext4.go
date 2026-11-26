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
	"homelab-control/backend/internal/ext4info"
	"homelab-control/backend/internal/storagetools"
)

// ext4ProtectedGET is the read-only ext4 bar on an identifier-bearing GET.
// The checks live in auditedGET; this wrapper keeps the ext4 audit message.
func (s *Server) ext4ProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	return s.auditedGET(w, r, sess, "ext4 inventory requires an access audit logger.")
}

// ext4Response is the unprivileged inventory plus the per-filesystem report
// of the broker's ext4.status op (tune2fs -l run by the read-only helper).
type ext4Response struct {
	ext4info.Snapshot
	Status toolStatus[storagetools.Ext4Report] `json:"status"`
}

// handleExt4 serves the ext4 view: tooling, the kernel module and mounts
// (unprivileged), and, when the kernel has ext4 and the broker is reachable,
// each mounted filesystem's superblock from tune2fs -l. This GET does not
// format, grow, or repair a filesystem.
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
	resp := ext4Response{Snapshot: snap, Status: toolReport[storagetools.Ext4Report](r.Context(), s, sess, "ext4.status", snap.KernelExt4.Present,
		"The kernel has no ext4 loaded, so there are no filesystems to report.", "Superblock status")}
	detail := "mounts=" + strconv.Itoa(len(snap.Ext4.Mounts))
	if resp.Status.Report != nil {
		detail += " filesystems=" + strconv.Itoa(len(resp.Status.Report.Filesystems))
	}
	if err := s.auditLog("ext4.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "ext4 inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
