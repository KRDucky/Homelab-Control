// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/partplan"
)

type partitionApplyRequest struct {
	ByID          string           `json:"by_id"`
	IdentityKind  string           `json:"identity_kind"`
	IdentityValue string           `json:"identity_value"`
	Confirm       []string         `json:"confirm"`
	DiskBytes     uint64           `json:"disk_bytes"`
	Parts         []partplan.Slice `json:"parts"`
	Token         string           `json:"token"`
}

func (req partitionApplyRequest) args() map[string]any {
	return map[string]any{
		"by_id": req.ByID, "identity_kind": req.IdentityKind, "identity_value": req.IdentityValue,
		"confirm": req.Confirm, "disk_bytes": req.DiskBytes, "parts": req.Parts,
	}
}

func partitionApplyAudit(req partitionApplyRequest) string {
	if req.IdentityKind == "" {
		return "invalid args"
	}
	n := len(req.Parts)
	if n < 0 || n > 128 {
		return "kind=" + req.IdentityKind + " slices=invalid"
	}
	return "kind=" + req.IdentityKind + " slices=" + strconv.Itoa(n)
}

func (s *Server) handlePartitionApplyPlan(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var req partitionApplyRequest
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
	var plan types.PartitionWritePlan
	err := s.broker.CallWithArgs(r.Context(), "storage.partition.plan", req.args(), actorFor(sess), &plan)
	if err != nil {
		s.auditLog("storage.partition.plan", sess.Username, r, errCode(err), partitionApplyAudit(req))
		writeOpError(w, err)
		return
	}
	if aerr := s.auditLog("storage.partition.plan", sess.Username, r, "success", partitionApplyAudit(req)+" risk="+plan.Risk); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so the partition cannot be reviewed.")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handlePartitionApplyApply(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	var req partitionApplyRequest
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
	if aerr := s.auditLog("storage.partition.apply", sess.Username, r, "requested", partitionApplyAudit(req)+" req="+actor.RequestID); aerr != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so nothing was partitioned.")
		return
	}
	args := req.args()
	args["token"] = req.Token
	var out types.PartitionWriteOutcome
	err := s.broker.CallWithArgs(context.WithoutCancel(r.Context()), "storage.partition.apply", args, actor, &out)
	if err != nil {
		var pe *proto.Error
		if !errors.As(err, &pe) {
			s.auditLog("storage.partition.apply", sess.Username, r, "outcome_unknown", partitionApplyAudit(req)+" req="+actor.RequestID)
			writeError(w, http.StatusBadGateway, "outcome_unknown", "The privileged helper did not return a result, so the disk may or may not have been partitioned. Check the disk before trying again.")
			return
		}
		s.auditLog("storage.partition.apply", sess.Username, r, errCode(err), partitionApplyAudit(req)+" req="+actor.RequestID)
		writeOpError(w, err)
		return
	}
	s.auditLog("storage.partition.apply", sess.Username, r, out.Outcome, partitionApplyAudit(req)+" op="+out.OperationID)
	writeJSON(w, http.StatusOK, out)
}
