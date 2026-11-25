// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"errors"

	"context"
	"homelab-control/backend/internal/broker/proto"
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
	Status zfsStatusView `json:"status"`
}

type zfsStatusView struct {
	Available bool                    `json:"available"`
	Reason    string                  `json:"reason,omitempty"`
	Report    *storagetools.ZFSReport `json:"report,omitempty"`
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
	resp := zfsResponse{Snapshot: snap, Status: s.zfsPools(r.Context(), sess, snap)}
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

// zfsPools asks the broker for the pool report. It is skipped when the kernel
// has no ZFS, and every failure becomes a reason instead of failing the page.
func (s *Server) zfsPools(ctx context.Context, sess *auth.Session, snap zfsinfo.Snapshot) zfsStatusView {
	if !snap.KernelZfs.Present {
		return zfsStatusView{Reason: "The kernel has no ZFS loaded, so there are no pools to report."}
	}
	if s.broker == nil {
		return zfsStatusView{Reason: brokerDownReason}
	}
	// No deadline of its own: the broker client's per-op deadline (derived
	// from the helper's budget) governs, and the request context still
	// cancels when the browser goes away.
	var rep storagetools.ZFSReport
	if err := s.broker.Call(ctx, "zfs.status", actorFor(sess), &rep); err != nil {
		reason := "Pool status is unavailable: the read-only storage helper (homelab-control-storage-probe.socket) did not answer."
		var pe *proto.Error
		if errors.As(err, &pe) && pe.Message != "" {
			reason = "Pool status is unavailable: " + pe.Message
		}
		return zfsStatusView{Reason: reason}
	}
	return zfsStatusView{Available: true, Report: &rep}
}
