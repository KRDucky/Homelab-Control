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
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/brokerclient"
)

type ext4FormatRequest struct {
	ByID          string   `json:"by_id"`
	IdentityKind  string   `json:"identity_kind"`
	IdentityValue string   `json:"identity_value"`
	Confirm       []string `json:"confirm"`
	Token         string   `json:"token"`
}

func (req ext4FormatRequest) args() map[string]any {
	return map[string]any{
		"by_id": req.ByID, "identity_kind": req.IdentityKind, "identity_value": req.IdentityValue, "confirm": req.Confirm,
	}
}

func ext4FormatAudit(req ext4FormatRequest) string {
	if req.IdentityKind == "" {
		return "invalid args"
	}
	return "kind=" + req.IdentityKind
}

func (s *Server) handleExt4FormatPlan(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var req ext4FormatRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if req.Token != "" {
		writeError(w, http.StatusBadRequest, "bad_request", "a plan does not take a token")
		return
	}
	if s.broker == nil {
		brokerError(w, brokerclient.ErrUnavailable)
		return
	}
	var plan types.Ext4FormatPlan
	err := s.broker.CallWithArgs(r.Context(), "ext4.format.plan", req.args(), actorFor(sess), &plan)
	if err != nil {
		s.auditLog("ext4.format.plan", sess.Username, r, errCode(err), ext4FormatAudit(req))
		writeOpError(w, err)
		return
	}
	if aerr := s.auditLog("ext4.format.plan", sess.Username, r, "success", ext4FormatAudit(req)+" risk="+plan.Risk); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so the format cannot be reviewed.")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleExt4FormatApply(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var req ext4FormatRequest
	if !decodeStrict(w, r, &req) {
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
	if stepped {
		actor.StepUpAt = at.Unix()
	}
	if aerr := s.auditLog("ext4.format.apply", sess.Username, r, "requested", ext4FormatAudit(req)+" req="+actor.RequestID); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so nothing was formatted.")
		return
	}
	args := req.args()
	args["token"] = req.Token
	var out types.Ext4FormatOutcome
	err := s.broker.CallWithArgs(context.WithoutCancel(r.Context()), "ext4.format.apply", args, actor, &out)
	if err != nil {
		var pe *proto.Error
		if !errors.As(err, &pe) {
			s.auditLog("ext4.format.apply", sess.Username, r, "outcome_unknown", ext4FormatAudit(req)+" req="+actor.RequestID)
			writeError(w, http.StatusBadGateway, "outcome_unknown", "The privileged helper did not return a result, so the disk may or may not have been formatted. Check the disk before trying again.")
			return
		}
		s.auditLog("ext4.format.apply", sess.Username, r, errCode(err), ext4FormatAudit(req)+" req="+actor.RequestID)
		writeOpError(w, err)
		return
	}
	s.auditLog("ext4.format.apply", sess.Username, r, out.Outcome, ext4FormatAudit(req)+" op="+out.OperationID)
	writeJSON(w, http.StatusOK, out)
}
