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
	"homelab-control/backend/internal/mdadminfo"
	"homelab-control/backend/internal/storagetools"
)

// mdadmProtectedGET is the read-only software RAID bar on an
// identifier-bearing GET. The checks live in auditedGET; this wrapper keeps
// the mdadm audit message.
func (s *Server) mdadmProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	return s.auditedGET(w, r, sess, "Software RAID inventory requires an access audit logger.")
}

// mdadmResponse is the unprivileged /proc/mdstat inventory plus the broker's
// mdadm.status report (mdadm --detail per array, run by the read-only helper).
type mdadmResponse struct {
	mdadminfo.Snapshot
	Status toolStatus[storagetools.MdReport] `json:"status"`
}

// handleMdadm serves the read-only software RAID view: tooling and
// /proc/mdstat (unprivileged), and, when md arrays exist and the broker is
// reachable, mdadm --detail of each array. No create, assemble, add, remove,
// fail or grow exists.
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
	resp := mdadmResponse{Snapshot: snap, Status: toolReport[storagetools.MdReport](r.Context(), s, sess, "mdadm.status",
		snap.MD.Present, "The kernel has no md arrays (/proc/mdstat is absent), so there is nothing for mdadm to describe.", "Array detail")}
	detail := "arrays=" + strconv.Itoa(len(snap.MD.Arrays))
	if err := s.auditLog("mdadm.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Software RAID inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
