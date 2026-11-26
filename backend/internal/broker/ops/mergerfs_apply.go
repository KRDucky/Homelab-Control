// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
	"time"

	"homelab-control/backend/internal/broker/proto"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/mergerfsapply"
)

const (
	mergerfsPoolOp     = "mergerfs.pool.apply"
	mergerfsMaintainOp = "mergerfs.maintain.apply"
	mergerfsPoolPlan   = "mergerfs.pool.plan"
	mergerfsKeepPlan   = "mergerfs.maintain.plan"
	mergerfsArrayLock  = "mergerfs-array"
	mergerfsApplySock  = "/run/homelab-control/mergerfs-apply.sock"
)

// MergerFSPlan is the review the API shows. Argv is the exact command.
// Effect and consequences do not repeat filesystem paths.
type MergerFSPlan struct {
	Action         string    `json:"action"`
	Risk           string    `json:"risk"`
	StepUpRequired bool      `json:"step_up_required"`
	Token          string    `json:"token"`
	ExpiresAt      time.Time `json:"expires_at"`
	Effect         string    `json:"effect"`
	Consequences   []string  `json:"consequences"`
	Argv           []string  `json:"argv"`
}

// MergerFSOutcome is the apply result. Message is a fixed sentence.
type MergerFSOutcome struct {
	OperationID string `json:"operation_id"`
	Action      string `json:"action"`
	Outcome     string `json:"outcome"`
	Message     string `json:"message"`
	ExitCode    *int   `json:"exit_code,omitempty"`
}

type mergerfsArgs struct {
	mergerfsapply.Request
	Token string `json:"token,omitempty"`
}

func mergerfsAuditArgs(raw json.RawMessage) string {
	spec, _, err := mergerfsSpec(raw, false)
	if err != nil {
		return "unparsed"
	}
	return spec.AuditDetail()
}

func mergerfsSpec(raw json.RawMessage, apply bool) (mergerfsapply.Spec, string, error) {
	var args mergerfsArgs
	if err := proto.DecodeArgs(raw, &args); err != nil {
		return mergerfsapply.Spec{}, "", badMerger("arguments must be the closed mergerfs fields")
	}
	if !apply && args.Token != "" {
		return mergerfsapply.Spec{}, "", badMerger("a plan does not take a token")
	}
	spec, err := mergerfsapply.Validate(args.Request)
	if err != nil {
		return mergerfsapply.Spec{}, "", badMerger(err.Error())
	}
	return spec, args.Token, nil
}

func badMerger(msg string) error {
	return &proto.Error{Code: proto.CodeBadRequest, Message: msg}
}

func mergerfsFamily(op string, spec mergerfsapply.Spec) error {
	switch op {
	case mergerfsPoolPlan, mergerfsPoolOp:
		if spec.Action != mergerfsapply.ActionMount && spec.Action != mergerfsapply.ActionPersist {
			return badMerger("this operation only mounts a pool or saves its definition")
		}
	case mergerfsKeepPlan, mergerfsMaintainOp:
		if spec.Action != mergerfsapply.ActionSync && spec.Action != mergerfsapply.ActionScrub && spec.Action != mergerfsapply.ActionFix {
			return badMerger("this operation only syncs, scrubs, or recovers files")
		}
	default:
		return badMerger("unknown operation")
	}
	return nil
}

func mergerfsPlan(op string) func(context.Context, *Env, json.RawMessage) (any, error) {
	return func(_ context.Context, env *Env, raw json.RawMessage) (any, error) {
		spec, _, err := mergerfsSpec(raw, false)
		if err != nil {
			return nil, err
		}
		if err := mergerfsFamily(op, spec); err != nil {
			return nil, err
		}
		if env.Actor.User == "" || env.Actor.Session == "" {
			return nil, &proto.Error{Code: proto.CodeForbidden, Message: "write operations must be requested from an authenticated session"}
		}
		if env.Writes == nil {
			return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal is not available, so the change cannot be reviewed"}
		}
		if env.Writes.Locked(mergerfsArrayLock) {
			return nil, &proto.Error{Code: proto.CodeBusy, Message: "another MergerFS operation is in progress"}
		}
		argv, err := spec.Argv()
		if err != nil {
			return nil, badMerger("the command could not be built")
		}
		applyOp := mergerfsPoolOp
		if op == mergerfsKeepPlan {
			applyOp = mergerfsMaintainOp
		}
		tok, b, err := env.Writes.Mint(writes.Binding{
			Op: applyOp, Action: spec.Action, Target: spec.Target(), Actor: env.Actor.User,
			Session: env.Actor.Session, StateDigest: spec.Digest(), Risk: RiskDisruptive,
		})
		if err != nil {
			return nil, &proto.Error{Code: proto.CodeInternal, Message: "could not issue a plan token"}
		}
		if argv == nil {
			argv = []string{}
		}
		return MergerFSPlan{
			Action: spec.Action, Risk: RiskDisruptive, StepUpRequired: true, Token: tok, ExpiresAt: b.Expires,
			Effect: spec.Effect(), Consequences: spec.Consequences(), Argv: argv,
		}, nil
	}
}

func mergerfsApply(op string) func(context.Context, *Env, json.RawMessage) (any, error) {
	return func(_ context.Context, env *Env, raw json.RawMessage) (any, error) {
		spec, token, err := mergerfsSpec(raw, true)
		if err != nil {
			return nil, err
		}
		if err := mergerfsFamily(op, spec); err != nil {
			return nil, err
		}
		if token == "" {
			return nil, badMerger("the plan token is missing")
		}
		if env.Actor.User == "" || env.Actor.Session == "" {
			return nil, &proto.Error{Code: proto.CodeForbidden, Message: "write operations must be requested from an authenticated session"}
		}
		if env.Writes == nil {
			return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal is not available, so nothing was executed"}
		}
		b, err := env.Writes.Consume(token, writes.Request{
			Op: op, Action: spec.Action, Target: spec.Target(), Actor: env.Actor.User,
			Session: env.Actor.Session, StepUpAt: env.Actor.StepUpAt,
		})
		switch {
		case errors.Is(err, writes.ErrStepUp):
			return nil, &proto.Error{Code: proto.CodeStepUpRequired, Message: "re-enter your password to confirm this disruptive operation"}
		case err != nil:
			return nil, &proto.Error{Code: proto.CodeInvalidToken, Message: "the plan is invalid, expired or already used; review the operation again"}
		}
		if b.StateDigest != spec.Digest() {
			return nil, &proto.Error{Code: proto.CodeConflict, Message: "the request changed since it was reviewed; nothing was done. Review it again."}
		}
		unlock, err := env.Writes.Lock(mergerfsArrayLock)
		if err != nil {
			return nil, &proto.Error{Code: proto.CodeBusy, Message: "another MergerFS operation is in progress"}
		}
		defer unlock()
		rec := writes.Record{Op: op, Action: spec.Action, Target: spec.Target(), Actor: env.Actor.User,
			RequestID: env.Actor.RequestID, TokenID: b.ID, Risk: b.Risk, State: b.StateDigest}
		opID, err := env.Writes.Intent(rec)
		if err != nil {
			return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal cannot be written; nothing was executed"}
		}
		rec.OpID = opID
		req := mergerfsapply.Request{
			Version: mergerfsapply.ProtocolVersion, Action: spec.Action, Mountpoint: spec.Mountpoint,
			Branches: spec.Branches, Policy: spec.Policy, MinFree: spec.MinFree, Parity: spec.Parity,
			Content: spec.Content, ScrubPercent: spec.ScrubPercent, Files: spec.Files,
		}
		callCtx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
		res, callErr := env.callMergerFS(callCtx, req)
		cancel()
		out := MergerFSOutcome{OperationID: opID, Action: spec.Action}
		switch {
		case callErr != nil:
			out.Outcome = OutcomeUncertain
			out.Message = "The helper did not return a result. Check the pool before trying again. It was not retried."
		case res.Blocked != "":
			out.Outcome = OutcomeFailed
			out.Message = blockedMessage(res.Blocked)
		case res.OK:
			out.Outcome = OutcomeSucceeded
			out.Message = safeSummary(res.Summary)
			ec := res.ExitCode
			out.ExitCode = &ec
			rec.ExitCode = &ec
		default:
			out.Outcome = OutcomeFailed
			out.Message = safeSummary(res.Summary)
			ec := res.ExitCode
			out.ExitCode = &ec
			rec.ExitCode = &ec
		}
		rec.Outcome, rec.Message = out.Outcome, out.Message
		if err := env.Writes.Outcome(rec); err != nil {
			out.Message += " The outcome could not be written to the operation journal."
		}
		return out, nil
	}
}

func blockedMessage(reason string) string {
	switch reason {
	case mergerfsapply.BlockMissingDisk:
		return "A disk is missing. Sync and scrub were not started."
	case mergerfsapply.BlockRemovedFiles:
		return "Files were removed since the last sync. Sync and scrub were not started."
	case mergerfsapply.BlockDiffTruncated, mergerfsapply.BlockDiffUnreadable:
		return "snapraid diff could not be read, so sync and scrub were not started."
	default:
		return "The operation was refused before it started."
	}
}

func safeSummary(s string) string {
	if s == "" || strings.ContainsAny(s, `/\`) {
		return "The helper finished without a usable summary."
	}
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func (env *Env) callMergerFS(ctx context.Context, req mergerfsapply.Request) (mergerfsapply.Result, error) {
	if env.MergerFSApply != nil {
		return env.MergerFSApply(ctx, req)
	}
	return dialMergerFS(ctx, req)
}

// errHelperNoReply: the helper socket closed without a reply.
var errHelperNoReply = errors.New("the MergerFS helper closed the connection without a reply")

func dialMergerFS(ctx context.Context, req mergerfsapply.Request) (mergerfsapply.Result, error) {
	return dialMergerFSAt(ctx, mergerfsApplySock, req)
}

func dialMergerFSAt(ctx context.Context, sock string, req mergerfsapply.Request) (mergerfsapply.Result, error) {
	var zero mergerfsapply.Result
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return zero, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return zero, err
	}
	if _, err := conn.Write(append(body, '\n')); err != nil {
		return zero, err
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	b, err := io.ReadAll(io.LimitReader(conn, 1<<20))
	if err != nil && !(len(b) == 0 && errors.Is(err, syscall.ECONNRESET)) {
		return zero, err
	}
	if len(b) == 0 {
		// The socket accepted, but the helper unit did not run (its
		// ConditionPathExists marker is absent) or wrote nothing.
		return zero, errHelperNoReply
	}
	var res mergerfsapply.Result
	if err := json.Unmarshal(b, &res); err != nil {
		return zero, err
	}
	if res.Version != mergerfsapply.ProtocolVersion {
		return zero, errors.New("helper protocol")
	}
	return res, nil
}
