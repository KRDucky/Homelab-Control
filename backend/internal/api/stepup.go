// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"homelab-control/backend/internal/auth"
)

// Step-up authentication (owner decision D-3, ADR 0011 §3): a password
// re-entry that authorizes disruptive and destructive operations for five
// minutes, for this session only. The password is checked here and never
// leaves hc-api; the broker only receives the time of the re-entry.

// decodeStrict decodes a small JSON body, rejecting unknown fields, trailing
// data and non-JSON content types.
func decodeStrict(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "expected application/json")
		return false
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil || dec.More() {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return false
	}
	return true
}

type stepUpRequest struct {
	Password string `json:"password"`
}

type stepUpResponse struct {
	StepUpUntil time.Time `json:"step_up_until"`
}

// handleStepUp re-verifies the session user's password (D-3). Failures count
// toward the same throttle as logins. The password never leaves hc-api: the
// broker only receives the time of the re-entry.
func (s *Server) handleStepUp(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var req stepUpRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	ip := remoteIP(r)
	if req.Password == "" || len(req.Password) > 1024 {
		s.auditLog("auth.step_up", sess.Username, r, "failure", "malformed")
		writeError(w, http.StatusForbidden, "invalid_credentials", "The password is not correct.")
		return
	}
	if !s.throttle.Allowed(sess.Username, ip) {
		s.auditLog("auth.step_up", sess.Username, r, "throttled", "")
		writeError(w, http.StatusTooManyRequests, "throttled", "Too many failed attempts; try again later.")
		return
	}
	select {
	case s.loginSem <- struct{}{}:
		defer func() { <-s.loginSem }()
	case <-r.Context().Done():
		return
	}
	if _, ok := s.users.Authenticate(sess.Username, req.Password); !ok {
		s.throttle.Fail(sess.Username, ip)
		s.auditLog("auth.step_up", sess.Username, r, "failure", "")
		writeError(w, http.StatusForbidden, "invalid_credentials", "The password is not correct.")
		return
	}
	s.throttle.Succeed(sess.Username, ip)
	until := s.stepups.Record(sess.Key, sess.Username)
	s.auditLog("auth.step_up", sess.Username, r, "success", "")
	writeJSON(w, http.StatusOK, stepUpResponse{StepUpUntil: until.UTC()})
}
