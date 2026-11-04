// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/servicepolicy"
)

// M2-3: read-only unit views and boot-time enable/disable (ADR 0011 review:
// docs/m2/M2-3-DESIGN-REVIEW.md). As for M2-2, hc-api validates, applies the
// name-level policy, requires step-up and audits before the broker runs
// anything; the broker re-checks everything and is authoritative.

// validViewUnit is the grammar of read-only unit queries (also checked by the broker).
func validViewUnit(u string) bool {
	return types.LogsUnitRE.MatchString(u) && !strings.Contains(u, "..")
}

func (s *Server) handleTimers(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	var res types.TimerList
	if err := s.broker.Call(r.Context(), "services.timers.list", actorFor(sess), &res); err != nil {
		brokerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSockets(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	var res types.SocketList
	if err := s.broker.Call(r.Context(), "services.sockets.list", actorFor(sess), &res); err != nil {
		brokerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// unitDetailResponse adds, for the UI only, the name-level protection label
// and whether this account may review a boot-time change.
type unitDetailResponse struct {
	types.UnitDetail
	Protected *servicepolicy.Protection `json:"protected,omitempty"`
}

func (s *Server) handleUnitDetail(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	unit := r.PathValue("unit")
	if !validViewUnit(unit) {
		writeError(w, http.StatusBadRequest, "bad_request", "unit name is not valid")
		return
	}
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	var d types.UnitDetail
	if err := s.broker.CallWithArgs(r.Context(), "services.unit.detail", map[string]string{"unit": unit}, actorFor(sess), &d); err != nil {
		brokerError(w, err)
		return
	}
	out := unitDetailResponse{UnitDetail: d}
	name := d.ID
	if name == "" {
		name = unit
	}
	if p := servicepolicy.NamesProtection(append([]string{name, unit}, d.Names...)...); p.Protected {
		out.Protected = &p
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUnitContents returns the redacted unit file. Unit files can contain
// sensitive values, so this needs services.logs and is audited (who, which
// unit; never content) before anything is released.
func (s *Server) handleUnitContents(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	unit := r.PathValue("unit")
	if !validViewUnit(unit) {
		writeError(w, http.StatusBadRequest, "bad_request", "unit name is not valid")
		return
	}
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	var c types.UnitContents
	err := s.broker.CallWithArgs(r.Context(), "services.unit.contents", map[string]string{"unit": unit}, actorFor(sess), &c)
	outcome := "success"
	if err != nil {
		outcome = errCode(err)
	}
	if aerr := s.auditLog("services.unit.contents.read", sess.Username, r, outcome, unit); aerr != nil && err == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so the unit file was withheld.")
		return
	}
	if err != nil {
		brokerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

type enablementRequest struct {
	Action string `json:"action"`
}

type enablementApplyRequest struct {
	Action string `json:"action"`
	Token  string `json:"token"`
}

func (s *Server) validEnablementTarget(w http.ResponseWriter, r *http.Request, sess *auth.Session, event, unit, action string) bool {
	if !servicepolicy.ValidEnablementUnit(unit) {
		writeError(w, http.StatusBadRequest, "bad_request", "unit must be a .service unit name without templates, instances or escapes")
		return false
	}
	if !servicepolicy.ValidEnablementAction(action) {
		writeError(w, http.StatusBadRequest, "bad_request", "action must be enable or disable")
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

func (s *Server) handleEnablementPlan(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	unit := r.PathValue("unit")
	var req enablementRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if !s.validEnablementTarget(w, r, sess, "services.enablement.plan", unit, req.Action) {
		return
	}
	var plan types.EnablementPlan
	err := s.broker.CallWithArgs(r.Context(), "services.enablement.plan", map[string]string{"unit": unit, "action": req.Action}, actorFor(sess), &plan)
	if err != nil {
		s.auditLog("services.enablement.plan", sess.Username, r, errCode(err), unit+" "+req.Action)
		writeOpError(w, err)
		return
	}
	if aerr := s.auditLog("services.enablement.plan", sess.Username, r, "success", plan.Unit+" "+req.Action+" risk="+plan.Risk); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so the change cannot be reviewed.")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleEnablementApply(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	unit := r.PathValue("unit")
	var req enablementApplyRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if !s.validEnablementTarget(w, r, sess, "services.enablement.apply", unit, req.Action) {
		return
	}
	if len(req.Token) > 2048 || !planTokenRE.MatchString(req.Token) {
		writeError(w, http.StatusBadRequest, "bad_request", "the plan token is missing or malformed")
		return
	}
	actor := actorFor(sess)
	at, stepped := s.stepups.At(sess.Key, sess.Username)
	if !stepped {
		// Boot-time changes are always in the disruptive class (step-up).
		s.auditLog("services.enablement.apply", sess.Username, r, "step_up_required", unit+" "+req.Action)
		writeError(w, http.StatusForbidden, "step_up_required", "Re-enter your password to confirm this boot-time change.")
		return
	}
	actor.StepUpAt = at.Unix()
	if aerr := s.auditLog("services.enablement.apply", sess.Username, r, "requested", unit+" "+req.Action+" req="+actor.RequestID); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so nothing was changed.")
		return
	}
	var out types.EnablementOutcome
	err := s.broker.CallWithArgs(context.WithoutCancel(r.Context()), "services.enablement.apply",
		map[string]string{"unit": unit, "action": req.Action, "token": req.Token}, actor, &out)
	if err != nil {
		var pe *proto.Error
		if !errors.As(err, &pe) {
			s.auditLog("services.enablement.apply", sess.Username, r, "outcome_unknown", unit+" "+req.Action+" req="+actor.RequestID)
			writeError(w, http.StatusBadGateway, "outcome_unknown", "The privileged helper did not return a result, so the change may or may not have been made. Check the unit's boot setting on this page and the operation journal on the Audit page.")
			return
		}
		s.auditLog("services.enablement.apply", sess.Username, r, errCode(err), unit+" "+req.Action+" req="+actor.RequestID)
		writeOpError(w, err)
		return
	}
	s.auditLog("services.enablement.apply", sess.Username, r, out.Outcome, out.Unit+" "+out.Action+" op="+out.OperationID)
	writeJSON(w, http.StatusOK, out)
}
