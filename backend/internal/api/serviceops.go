// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/servicepolicy"
)

// M2-2: systemd service start/stop/restart (ADR 0011; owner decisions D-2…D-5).
//
// The API is one of two enforcement points; the broker is the authoritative
// one. Here: scope services.operate, Origin + CSRF (Server.handle), strict
// JSON, unit/action validation, the name-level protected-unit policy, step-up
// for stop/restart, and an audit record written BEFORE the broker executes
// (fail closed). The broker repeats the scope check, applies the full policy,
// verifies the single-use plan token and the step-up time, locks, revalidates
// and journals.

type planRequest struct {
	Action string `json:"action"`
}

type applyRequest struct {
	Action string `json:"action"`
	Token  string `json:"token"`
}

var planTokenRE = regexp.MustCompile(`^hcp1\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]{43}$`)

// validTarget validates the path unit and the action and applies the
// name-level policy. It writes the error response itself.
func (s *Server) validTarget(w http.ResponseWriter, r *http.Request, sess *auth.Session, event, unit, action string) bool {
	if !servicepolicy.ValidUnit(unit) {
		writeError(w, http.StatusBadRequest, "bad_request", "unit must be a .service unit name (letters, digits, :_.@-; no templates or escapes)")
		return false
	}
	if !servicepolicy.ValidAction(action) {
		writeError(w, http.StatusBadRequest, "bad_request", "action must be start, stop or restart")
		return false
	}
	if p := servicepolicy.NameProtection(unit); p.Protected {
		s.auditLog(event, sess.Username, r, "policy_denied", unit+" "+action)
		writeError(w, http.StatusForbidden, "policy_denied", unit+" is protected: "+p.Reason)
		return false
	}
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return false
	}
	return true
}

// writeOpError maps broker refusals of write operations to HTTP responses
// with their specific, user-facing messages.
func writeOpError(w http.ResponseWriter, err error) {
	var pe *proto.Error
	if errors.As(err, &pe) {
		switch pe.Code {
		case proto.CodePolicyDenied:
			writeError(w, http.StatusForbidden, "policy_denied", pe.Message)
			return
		case proto.CodeStepUpRequired:
			writeError(w, http.StatusForbidden, "step_up_required", pe.Message)
			return
		case proto.CodeInvalidToken:
			writeError(w, http.StatusConflict, "plan_invalid", pe.Message)
			return
		case proto.CodeConflict:
			writeError(w, http.StatusConflict, "conflict", pe.Message)
			return
		case proto.CodeBusy:
			writeError(w, http.StatusConflict, "busy", pe.Message)
			return
		case proto.CodeUnavailable:
			writeError(w, http.StatusServiceUnavailable, "unavailable", pe.Message)
			return
		}
	}
	brokerError(w, err)
}

func errCode(err error) string {
	var pe *proto.Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	if errors.Is(err, brokerclient.ErrUnavailable) {
		return "broker_unavailable"
	}
	return "error"
}

// handleUnitPlan asks the broker to review an operation and mint a plan token.
func (s *Server) handleUnitPlan(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	unit := r.PathValue("unit")
	var req planRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if !s.validTarget(w, r, sess, "services.op.plan", unit, req.Action) {
		return
	}
	var plan types.ServicePlan
	err := s.broker.CallWithArgs(r.Context(), "services.unit.plan", map[string]string{"unit": unit, "action": req.Action}, actorFor(sess), &plan)
	if err != nil {
		s.auditLog("services.op.plan", sess.Username, r, errCode(err), unit+" "+req.Action)
		writeOpError(w, err)
		return
	}
	// Fail closed: the plan token is only released once the review is audited.
	if aerr := s.auditLog("services.op.plan", sess.Username, r, "success", plan.Unit+" "+req.Action+" risk="+plan.Risk); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so the operation cannot be reviewed.")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// handleUnitApply executes a reviewed operation with its plan token.
func (s *Server) handleUnitApply(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	unit := r.PathValue("unit")
	var req applyRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if !s.validTarget(w, r, sess, "services.op.apply", unit, req.Action) {
		return
	}
	if len(req.Token) > 2048 || !planTokenRE.MatchString(req.Token) {
		writeError(w, http.StatusBadRequest, "bad_request", "the plan token is missing or malformed")
		return
	}
	actor := actorFor(sess)
	at, stepped := s.stepups.At(sess.Key, sess.Username)
	if stepped {
		actor.StepUpAt = at.Unix()
	}
	// Stop and restart are always disruptive (D-3); for start the broker
	// decides from the unit's dependencies and refuses without a step-up.
	if !stepped && (req.Action == string(servicepolicy.Stop) || req.Action == string(servicepolicy.Restart)) {
		s.auditLog("services.op.apply", sess.Username, r, "step_up_required", unit+" "+req.Action)
		writeError(w, http.StatusForbidden, "step_up_required", "Re-enter your password to confirm this disruptive operation.")
		return
	}
	// Fail closed: nothing is executed unless the request is audited first.
	if aerr := s.auditLog("services.op.apply", sess.Username, r, "requested", unit+" "+req.Action+" req="+actor.RequestID); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so nothing was executed.")
		return
	}
	var out types.ServiceOutcome
	// Not tied to the browser request: closing the tab must not cut the
	// broker connection while systemd works, or the outcome would be lost
	// and misreported. The broker client's own deadline still applies.
	err := s.broker.CallWithArgs(context.WithoutCancel(r.Context()), "services.unit.apply", map[string]string{"unit": unit, "action": req.Action, "token": req.Token}, actor, &out)
	if err != nil {
		var pe *proto.Error
		if !errors.As(err, &pe) {
			// No answer from the broker (connection lost, timeout): the
			// operation may or may not have run. Say so; do not report the
			// helper as simply "down".
			s.auditLog("services.op.apply", sess.Username, r, "outcome_unknown", unit+" "+req.Action+" req="+actor.RequestID)
			writeError(w, http.StatusBadGateway, "outcome_unknown", "The privileged helper did not return a result, so the operation may or may not have run. Check the unit's state on this page and the operation journal on the Audit page.")
			return
		}
		s.auditLog("services.op.apply", sess.Username, r, errCode(err), unit+" "+req.Action+" req="+actor.RequestID)
		writeOpError(w, err)
		return
	}
	s.auditLog("services.op.apply", sess.Username, r, out.Outcome, out.Unit+" "+out.Action+" op="+out.OperationID)
	writeJSON(w, http.StatusOK, out)
}

// handleOperations lists recent privileged operations from the broker journal.
func (s *Server) handleOperations(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	var res types.OperationList
	if err := s.broker.Call(r.Context(), "operations.journal.list", actorFor(sess), &res); err != nil {
		writeOpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
