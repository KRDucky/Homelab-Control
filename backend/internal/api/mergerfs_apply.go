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
	"homelab-control/backend/internal/mergerfsapply"
)

type mergerfsBody struct {
	Action       string   `json:"action"`
	Mountpoint   string   `json:"mountpoint"`
	Branches     []string `json:"branches"`
	Policy       string   `json:"policy"`
	MinFree      string   `json:"minFree"`
	Parity       []string `json:"parity"`
	Content      []string `json:"content"`
	ScrubPercent int      `json:"scrubPercent"`
	Files        []string `json:"files"`
	Token        string   `json:"token"`
}

func (b mergerfsBody) request() mergerfsapply.Request {
	return mergerfsapply.Request{
		Action: b.Action, Mountpoint: b.Mountpoint, Branches: b.Branches, Policy: b.Policy,
		MinFree: b.MinFree, Parity: b.Parity, Content: b.Content, ScrubPercent: b.ScrubPercent, Files: b.Files,
	}
}

func (b mergerfsBody) args() map[string]any {
	return map[string]any{
		"action": b.Action, "mountpoint": b.Mountpoint, "branches": b.Branches, "policy": b.Policy,
		"minFree": b.MinFree, "parity": b.Parity, "content": b.Content, "scrubPercent": b.ScrubPercent,
		"files": b.Files,
	}
}

func mergerfsDetail(b mergerfsBody) string {
	spec, err := mergerfsapply.Validate(b.request())
	if err != nil {
		return "action=" + b.Action
	}
	return spec.AuditDetail()
}

func (s *Server) handleMergerFSPlan(op, event string) ctxHandler {
	return func(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
		var req mergerfsBody
		if !decodeStrict(w, r, &req) {
			return
		}
		if req.Token != "" {
			writeError(w, http.StatusBadRequest, "bad_request", "a plan does not take a token")
			return
		}
		if _, err := mergerfsapply.Validate(req.request()); err != nil {
			s.auditLog(event, sess.Username, r, "rejected", "action="+req.Action)
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if s.broker == nil {
			brokerError(w, brokerclient.ErrUnavailable)
			return
		}
		var plan map[string]any
		err := s.broker.CallWithArgs(r.Context(), op, req.args(), actorFor(sess), &plan)
		if err != nil {
			s.auditLog(event, sess.Username, r, errCode(err), mergerfsDetail(req))
			writeOpError(w, err)
			return
		}
		if aerr := s.auditLog(event, sess.Username, r, "success", mergerfsDetail(req)); aerr != nil {
			writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so the change cannot be reviewed.")
			return
		}
		writeJSON(w, http.StatusOK, plan)
	}
}

func (s *Server) handleMergerFSApply(op, event string) ctxHandler {
	return func(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
		var req mergerfsBody
		if !decodeStrict(w, r, &req) {
			return
		}
		if _, err := mergerfsapply.Validate(req.request()); err != nil {
			s.auditLog(event, sess.Username, r, "rejected", "action="+req.Action)
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
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
			s.auditLog(event, sess.Username, r, "step_up_required", mergerfsDetail(req))
			writeError(w, http.StatusForbidden, "step_up_required", "Re-enter your password to confirm this MergerFS operation.")
			return
		}
		actor.StepUpAt = at.Unix()
		if aerr := s.auditLog(event, sess.Username, r, "requested", mergerfsDetail(req)); aerr != nil {
			writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The audit log cannot be written, so nothing was changed.")
			return
		}
		args := req.args()
		args["token"] = req.Token
		var out map[string]any
		err := s.broker.CallWithArgs(context.WithoutCancel(r.Context()), op, args, actor, &out)
		if err != nil {
			var pe *proto.Error
			if !errors.As(err, &pe) {
				s.auditLog(event, sess.Username, r, "outcome_unknown", mergerfsDetail(req))
				writeError(w, http.StatusBadGateway, "outcome_unknown", "The privileged helper did not return a result. Check the pool before trying again.")
				return
			}
			s.auditLog(event, sess.Username, r, errCode(err), mergerfsDetail(req))
			writeOpError(w, err)
			return
		}
		outcome, _ := out["outcome"].(string)
		s.auditLog(event, sess.Username, r, outcome, mergerfsDetail(req))
		writeJSON(w, http.StatusOK, out)
	}
}
