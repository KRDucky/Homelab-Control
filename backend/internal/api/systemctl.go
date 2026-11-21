// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"errors"
	"net/http"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/servicepolicy"
)

type systemctlPlan struct {
	Action         string   `json:"action"`
	Unit           string   `json:"unit,omitempty"`
	Wants          string   `json:"wants,omitempty"`
	Name           string   `json:"name,omitempty"`
	Risk           string   `json:"risk"`
	StepUpRequired bool     `json:"step_up_required"`
	Token          string   `json:"token"`
	ExpiresAt      string   `json:"expires_at"`
	Effect         string   `json:"effect"`
	Consequences   []string `json:"consequences"`
}

type systemctlOutcome struct {
	OperationID string `json:"operation_id"`
	Action      string `json:"action"`
	Unit        string `json:"unit,omitempty"`
	Outcome     string `json:"outcome"`
	Message     string `json:"message"`
	ExitCode    *int   `json:"exit_code,omitempty"`
}

// Closed systemctl verbs (owner 2026-11-21). The broker is authoritative for
// the verb table. hc-api checks the scope, the protected-unit names, step-up
// and the audit record. Drop-in content is not written to the audit log.

type systemctlRequest struct {
	Action  string `json:"action"`
	Unit    string `json:"unit"`
	Wants   string `json:"wants"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Token   string `json:"token"`
}

func systemctlDetail(req systemctlRequest) string {
	s := req.Action
	if req.Unit != "" {
		s += " " + req.Unit
	}
	if req.Wants != "" {
		s += " wants=" + req.Wants
	}
	if req.Name != "" {
		s += " dropin=" + req.Name
	}
	if req.Content != "" {
		s += " bytes=" + itoa(len(req.Content))
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func (s *Server) systemctlDenied(w http.ResponseWriter, r *http.Request, sess *auth.Session, event string, req systemctlRequest) bool {
	for _, name := range []string{req.Unit, req.Wants} {
		if name == "" {
			continue
		}
		if p := servicepolicy.NameProtection(name); p.Protected {
			s.auditLog(event, sess.Username, r, "policy_denied", systemctlDetail(req))
			writeError(w, http.StatusForbidden, "policy_denied", name+" is protected: "+p.Reason)
			return true
		}
	}
	return false
}

func (s *Server) handleSystemctlPlan(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var req systemctlRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if req.Token != "" {
		writeError(w, http.StatusBadRequest, "bad_request", "a plan does not take a token")
		return
	}
	if s.systemctlDenied(w, r, sess, "services.systemctl.plan", req) {
		return
	}
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	var plan systemctlPlan
	err := s.broker.CallWithArgs(r.Context(), "services.systemctl.plan", map[string]string{
		"action": req.Action, "unit": req.Unit, "wants": req.Wants, "name": req.Name, "content": req.Content,
	}, actorFor(sess), &plan)
	if err != nil {
		s.auditLog("services.systemctl.plan", sess.Username, r, errCode(err), systemctlDetail(req))
		writeOpError(w, err)
		return
	}
	if aerr := s.auditLog("services.systemctl.plan", sess.Username, r, "success", systemctlDetail(req)+" risk="+plan.Risk); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so the change cannot be reviewed.")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleSystemctlApply(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var req systemctlRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if s.systemctlDenied(w, r, sess, "services.systemctl.apply", req) {
		return
	}
	if len(req.Token) > 2048 || !planTokenRE.MatchString(req.Token) {
		writeError(w, http.StatusBadRequest, "bad_request", "the plan token is missing or malformed")
		return
	}
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	actor := actorFor(sess)
	at, stepped := s.stepups.At(sess.Key, sess.Username)
	if !stepped {
		s.auditLog("services.systemctl.apply", sess.Username, r, "step_up_required", systemctlDetail(req))
		writeError(w, http.StatusForbidden, "step_up_required", "Re-enter your password to confirm this systemctl operation.")
		return
	}
	actor.StepUpAt = at.Unix()
	if aerr := s.auditLog("services.systemctl.apply", sess.Username, r, "requested", systemctlDetail(req)+" req="+actor.RequestID); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so nothing was changed.")
		return
	}
	var out systemctlOutcome
	err := s.broker.CallWithArgs(context.WithoutCancel(r.Context()), "services.systemctl.apply", map[string]string{
		"action": req.Action, "unit": req.Unit, "wants": req.Wants, "name": req.Name, "content": req.Content, "token": req.Token,
	}, actor, &out)
	if err != nil {
		var pe *proto.Error
		if !errors.As(err, &pe) {
			s.auditLog("services.systemctl.apply", sess.Username, r, "outcome_unknown", systemctlDetail(req)+" req="+actor.RequestID)
			writeError(w, http.StatusBadGateway, "outcome_unknown", "The privileged helper did not return a result, so the change may or may not have been made. Check the unit and the operation journal on the Audit page.")
			return
		}
		s.auditLog("services.systemctl.apply", sess.Username, r, errCode(err), systemctlDetail(req)+" req="+actor.RequestID)
		writeOpError(w, err)
		return
	}
	s.auditLog("services.systemctl.apply", sess.Username, r, out.Outcome, systemctlDetail(req)+" op="+out.OperationID)
	writeJSON(w, http.StatusOK, out)
}
