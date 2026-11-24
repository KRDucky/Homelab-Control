// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/partplan"
)

// handlePartitionDryRun validates a proposed layout and returns the report.
// It does not collect storage, call the broker, or write a device. Scope is
// storage.read. The audit detail is the slice count only.
func (s *Server) handlePartitionDryRun(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_rejected", "missing or invalid CSRF token")
		return
	}
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "A partition dry run requires an access audit logger.")
		return
	}
	var req partplan.Request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		s.refusePartitionPlan(w, r, sess)
		return
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		s.refusePartitionPlan(w, r, sess)
		return
	}
	report := partplan.DryRun(req)
	outcome := "rejected"
	if report.OK {
		outcome = "validated"
	}
	if err := s.auditLog("storage.plan.dry-run", sess.Username, r, outcome, "parts="+strconv.Itoa(len(req.Parts))); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The partition dry run could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) refusePartitionPlan(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if err := s.auditLog("storage.plan.dry-run", sess.Username, r, "malformed", ""); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The partition dry run could not be audited.")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_body", "The partition plan is not valid JSON.")
}
