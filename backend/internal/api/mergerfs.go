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
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/brokerclient"
)

// mergerfsProtectedGET is the read-only MergerFS + SnapRAID bar on an
// identifier-bearing GET. The checks live in auditedGET; this wrapper keeps
// the MergerFS audit message.
func (s *Server) mergerfsProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	return s.auditedGET(w, r, sess, "MergerFS inventory requires an access audit logger.")
}

// handleMergerFS serves the read-only MergerFS + optional SnapRAID inventory.
// No pool create, mount, policy write, or snapraid sync/diff/fix path exists:
// this endpoint parses bounded /proc/self/mountinfo, /etc/fstab, systemd
// .mount units, and one configured snapraid.conf, then fstatfs of root-owned
// live pool branch directories under the collect root. Listed SnapRAID paths
// are not opened as such; fstab/systemd paths are not opened. Capacity timeout
// degrades usage and must not fail the inventory.
func (s *Server) handleMergerFS(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.mergerfsProtectedGET(w, r, sess) {
		return
	}
	if s.mergerfsCollect == nil || s.mergerfsSem == nil {
		writeError(w, http.StatusServiceUnavailable, "mergerfs_unavailable", "MergerFS inventory is unavailable.")
		return
	}
	select {
	case s.mergerfsSem <- struct{}{}:
		defer func() { <-s.mergerfsSem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "mergerfs_busy", "MergerFS inventory is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := s.mergerfsCollect(ctx)
	if err != nil {
		_ = s.auditLog("mergerfs.inventory.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "mergerfs_unavailable", "MergerFS inventory could not be collected safely.")
		return
	}
	// Audit counts only, never branch, data, parity, content or unit paths.
	detail := "pools=" + strconv.Itoa(len(snap.MergerFS.Pools)) + " configured=" + strconv.Itoa(len(snap.MergerFS.Configured)) + " snapraid=" + strconv.FormatBool(snap.SnapRAID.Present)
	if err := s.auditLog("mergerfs.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "MergerFS inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// handleSnapraidStatus runs the closed read-only broker op mergerfs.snapraid.status.
// The request has no arguments: the broker's argv is exactly "snapraid status".
// Sync, diff, fix, scrub, and smart are not run. The audit record is counts
// and the exit code, never summary text (a warning line can name a path).
func (s *Server) handleSnapraidStatus(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.mergerfsProtectedGET(w, r, sess) {
		return
	}
	if s.broker == nil {
		_ = s.auditLog("mergerfs.snapraid.status", sess.Username, r, "unavailable", "broker-down")
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	var res types.SnapraidStatus
	err := s.broker.Call(r.Context(), "mergerfs.snapraid.status", actorFor(sess), &res)
	outcome := "returned"
	detail := "exit=" + strconv.Itoa(res.ExitCode) + " lines=" + strconv.Itoa(len(res.Summary)) + " sync=" + strconv.FormatBool(res.SyncInProgress)
	if err != nil {
		outcome = "unavailable"
		detail = "broker-error"
	}
	if aerr := s.auditLog("mergerfs.snapraid.status", sess.Username, r, outcome, detail); err == nil && aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "SnapRAID status cannot be returned because its access could not be audited.")
		return
	}
	if err != nil {
		brokerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
