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
	"os"
	"path/filepath"
	"strings"
	"time"

	"homelab-control/backend/internal/aptapply"
	"homelab-control/backend/internal/broker/proto"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/safefile"
)

// APT settings and mirror writes (owner authorization 2026-11-24). The broker
// reviews and journals; the opt-in hc-apt-apply helper writes the files.
// aptApplySock is the helper socket; tests point it at a temporary socket.
var aptApplySock = "/run/homelab-control/apt-apply.sock"

const (
	aptFilesLock      = "apt-files"
	aptConfigApplyOp  = "packages.aptconfig.apply"
	aptMirrorApplyOp  = "packages.mirror.apply"
	aptHelperDownText = "The APT settings helper is not available (it is opt-in: /etc/homelab-control/apt-apply.enabled)."
)

func badApt(msg string) error { return &proto.Error{Code: proto.CodeBadRequest, Message: msg} }

type aptConfigArgs struct {
	Reset    bool              `json:"reset,omitempty"`
	Tunables aptapply.Tunables `json:"tunables"`
	Token    string            `json:"token,omitempty"`
}

type aptMirrorArgs struct {
	File  string `json:"file"`
	From  string `json:"from"`
	To    string `json:"to"`
	Token string `json:"token,omitempty"`
}

func aptConfigAuditArgs(raw json.RawMessage) string {
	var a aptConfigArgs
	if proto.DecodeArgs(raw, &a) != nil {
		return "invalid args"
	}
	if a.Reset {
		return "action=" + aptapply.ActionConfRemove
	}
	return "action=" + aptapply.ActionConfWrite + " proxy=" + map[bool]string{true: "set", false: "none"}[a.Tunables.HTTPProxy != ""]
}

func aptMirrorAuditArgs(raw json.RawMessage) string {
	var a aptMirrorArgs
	if proto.DecodeArgs(raw, &a) != nil {
		return "invalid args"
	}
	return "file=" + clean(filepath.Base(a.File), 32) + " to=" + clean(mirrorHost(a.To), 80)
}

func mirrorHost(u string) string {
	rest := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	h, _, _ := strings.Cut(rest, "/")
	return h
}

func readAptFile(env *Env, abs string) ([]byte, bool, error) {
	b, err := safefile.Read(env.Roots.Path(abs), aptapply.MaxFileBytes)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, &proto.Error{Code: proto.CodeUnavailable, Message: "the APT file cannot be read safely"}
	}
	return b, true, nil
}

func aptDistro(env *Env) (aptapply.Distro, error) {
	d, err := aptapply.ReadOSRelease(env.Roots.Root)
	if err != nil {
		return aptapply.Distro{}, &proto.Error{Code: proto.CodeUnavailable, Message: "os-release cannot be read"}
	}
	if (d.ID != "debian" && d.ID != "ubuntu") || d.Codename == "" {
		return d, &proto.Error{Code: proto.CodeUnavailable, Message: "APT settings and mirrors are supported on Debian and Ubuntu only"}
	}
	return d, nil
}

func aptEffective(ctx context.Context, env *Env) (*aptapply.Effective, string) {
	if env.AptConfigDump != nil {
		out, err := env.AptConfigDump(ctx)
		if err != nil {
			return nil, "apt-config could not be run"
		}
		e, err := aptapply.ParseDump(out)
		if err != nil {
			return nil, "apt-config printed an unexpected value"
		}
		return &e, ""
	}
	if env.Runner == nil || !env.Runner.Available("apt-config") {
		return nil, "apt-config is not installed"
	}
	res, err := env.Runner.RunAptConfigDump(ctx)
	if err != nil || res.ExitCode != 0 {
		return nil, "apt-config could not be run"
	}
	e, err := aptapply.ParseDump(string(res.Stdout))
	if err != nil {
		return nil, "apt-config printed an unexpected value"
	}
	return &e, ""
}

func aptConfigRead(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := proto.DecodeArgs(raw, &struct{}{}); err != nil {
		return nil, badApt("this operation takes no arguments")
	}
	if _, err := aptDistro(env); err != nil {
		return nil, err
	}
	old, present, err := readAptFile(env, aptapply.ConfPath)
	if err != nil {
		return nil, err
	}
	v := aptapply.AptConfigView{Path: aptapply.ConfPath, ManagedPresent: present}
	if present {
		t, ok := aptapply.ParseManaged(string(old))
		v.Managed, v.ManagedByHand = t, !ok
		if !ok {
			v.Managed = aptapply.Tunables{}
		}
	}
	v.Effective, v.EffectiveError = aptEffective(ctx, env)
	return v, nil
}

// aptConfigReview computes the request, old digest and new content.
func aptConfigReview(env *Env, a aptConfigArgs) (aptapply.Request, []byte, string, error) {
	if _, err := aptDistro(env); err != nil {
		return aptapply.Request{}, nil, "", err
	}
	old, present, err := readAptFile(env, aptapply.ConfPath)
	if err != nil {
		return aptapply.Request{}, nil, "", err
	}
	req := aptapply.Request{Version: aptapply.ProtocolVersion, Action: aptapply.ActionConfWrite, Tunables: a.Tunables}
	if present {
		req.Digest = aptapply.Digest(old)
	}
	next := ""
	if a.Reset {
		if a.Tunables != (aptapply.Tunables{}) {
			return req, nil, "", badApt("a reset takes no settings")
		}
		if !present {
			return req, nil, "", &proto.Error{Code: proto.CodeConflict, Message: "Homelab Control does not manage any APT settings yet; there is nothing to reset"}
		}
		req.Action = aptapply.ActionConfRemove
	} else {
		s, err := aptapply.Render(a.Tunables)
		if err != nil {
			return req, nil, "", badApt(err.Error())
		}
		next = s
	}
	if err := aptapply.Validate(req); err != nil {
		return req, nil, "", badApt("the request is not a valid APT settings change")
	}
	return req, old, next, nil
}

func aptConfigPlan(_ context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a aptConfigArgs
	if err := proto.DecodeArgs(raw, &a); err != nil || a.Token != "" {
		return nil, badApt("arguments must be the APT settings or reset")
	}
	if err := requireAptWrite(env); err != nil {
		return nil, err
	}
	req, old, next, err := aptConfigReview(env, a)
	if err != nil {
		return nil, err
	}
	if !a.Reset && string(old) == next {
		return nil, &proto.Error{Code: proto.CodeConflict, Message: "these settings are already applied; nothing would change"}
	}
	tok, b, err := env.Writes.Mint(writes.Binding{
		Op: aptConfigApplyOp, Action: req.Action, Target: aptapply.ConfPath, Actor: env.Actor.User,
		Session: env.Actor.Session, StateDigest: req.Digest + "/" + aptapply.Digest([]byte(next)), Risk: RiskReversible,
	})
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "could not issue a plan token"}
	}
	p := aptapply.AptPlan{Action: req.Action, Risk: RiskReversible, ConfirmationRequired: true, Token: tok, ExpiresAt: b.Expires,
		File: aptapply.ConfPath, Before: string(old), After: next}
	if a.Reset {
		p.Effect = "Remove " + aptapply.ConfPath + " so APT uses the rest of its configuration."
	} else {
		p.Effect = "Write " + aptapply.ConfPath + " with the reviewed settings."
	}
	p.Consequences = []string{
		"The previous file is kept as a backup in " + aptapply.BackupDir + ".",
		"The settings take effect the next time APT runs; nothing is downloaded or installed now.",
		"A configuration file that sorts after 60homelab-control can still override a setting; the result reports it.",
	}
	return p, nil
}

func requireAptWrite(env *Env) error {
	if env.Actor.User == "" || env.Actor.Session == "" {
		return &proto.Error{Code: proto.CodeForbidden, Message: "write operations must be requested from an authenticated session"}
	}
	if env.Writes == nil {
		return &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal is not available, so the change cannot be reviewed"}
	}
	if env.Writes.Locked(aptFilesLock) {
		return &proto.Error{Code: proto.CodeBusy, Message: "another APT settings or mirror change is in progress"}
	}
	return nil
}

func aptConfigApply(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a aptConfigArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, badApt("arguments must be the reviewed APT settings plus token")
	}
	if a.Token == "" {
		return nil, badApt("the plan token is missing")
	}
	action := aptapply.ActionConfWrite
	if a.Reset {
		action = aptapply.ActionConfRemove
	}
	return aptApply(ctx, env, a.Token, aptConfigApplyOp, action, aptapply.ConfPath, func() (aptapply.Request, string, error) {
		req, _, next, err := aptConfigReview(env, a)
		return req, req.Digest + "/" + aptapply.Digest([]byte(next)), err
	}, func(out *aptapply.AptOutcome) {
		if a.Reset || out.Outcome != OutcomeSucceeded {
			return
		}
		e, msg := aptEffective(ctx, env)
		if e == nil {
			out.Message += " The effective values could not be read back (" + msg + ")."
			return
		}
		out.Overrides = aptapply.Overrides(a.Tunables, *e)
		if len(out.Overrides) > 0 {
			out.Message += " Another APT configuration file overrides some of these settings."
		}
	})
}

// aptApply is the shared consume → revalidate → journal → helper → journal path.
func aptApply(_ context.Context, env *Env, token, op, action, target string,
	review func() (aptapply.Request, string, error), after func(*aptapply.AptOutcome)) (any, error) {
	if env.Actor.User == "" || env.Actor.Session == "" {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "write operations must be requested from an authenticated session"}
	}
	if env.Writes == nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal is not available, so nothing was changed"}
	}
	b, err := env.Writes.Consume(token, writes.Request{
		Op: op, Action: action, Target: target, Actor: env.Actor.User,
		Session: env.Actor.Session, StepUpAt: env.Actor.StepUpAt,
	})
	switch {
	case errors.Is(err, writes.ErrStepUp):
		return nil, &proto.Error{Code: proto.CodeStepUpRequired, Message: "re-enter your password to confirm this change"}
	case err != nil:
		return nil, &proto.Error{Code: proto.CodeInvalidToken, Message: "the plan is invalid, expired or already used; review the change again"}
	}
	unlock, err := env.Writes.Lock(aptFilesLock)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another APT settings or mirror change is in progress"}
	}
	defer unlock()
	req, state, err := review()
	if err != nil {
		return nil, err
	}
	if state != b.StateDigest {
		return nil, &proto.Error{Code: proto.CodeConflict, Message: "the file or the request changed since it was reviewed; nothing was changed. Review it again."}
	}
	rec := writes.Record{Op: op, Action: action, Target: target, Actor: env.Actor.User,
		RequestID: env.Actor.RequestID, TokenID: b.ID, Risk: b.Risk, State: b.StateDigest}
	opID, err := env.Writes.Intent(rec)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal cannot be written; nothing was changed"}
	}
	rec.OpID = opID
	callCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	res, callErr := env.callAptApply(callCtx, req)
	cancel()
	out := aptapply.AptOutcome{OperationID: opID, Action: action}
	switch {
	case callErr != nil:
		out.Outcome = OutcomeUncertain
		out.Message = aptHelperDownText + " If it started, check " + target + " before trying again. It was not retried."
	case res.OK:
		out.Outcome, out.Message, out.Backup = OutcomeSucceeded, safeSummary(res.Message), res.Backup
	default:
		out.Outcome, out.Message = OutcomeFailed, safeSummary(res.Message)
	}
	if after != nil {
		after(&out)
	}
	rec.Outcome, rec.Message = out.Outcome, out.Message
	if err := env.Writes.Outcome(rec); err != nil {
		out.Message += " The outcome could not be written to the operation journal."
	}
	return out, nil
}

func aptMirrorInventory(_ context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := proto.DecodeArgs(raw, &struct{}{}); err != nil {
		return nil, badApt("this operation takes no arguments")
	}
	d, err := aptDistro(env)
	if err != nil {
		return nil, err
	}
	inv := aptapply.MirrorInventory{Distro: d, Files: []aptapply.MirrorFile{}, Entries: []aptapply.Entry{}}
	for _, f := range aptapply.VendorFiles {
		b, present, err := readAptFile(env, f)
		if !present && err == nil {
			continue
		}
		mf := aptapply.MirrorFile{Path: f, Format: map[bool]string{true: "deb822", false: "one-line"}[strings.HasSuffix(f, ".sources")]}
		if err != nil {
			mf.Error = "The file cannot be read safely."
			inv.Files = append(inv.Files, mf)
			continue
		}
		mf.Digest = aptapply.Digest(b)
		es, perr := aptapply.ParseSources(f, string(b), d)
		if perr != nil {
			mf.Error = "The file cannot be parsed safely; its mirrors are not changed here."
		}
		inv.Files = append(inv.Files, mf)
		inv.Entries = append(inv.Entries, es...)
	}
	return inv, nil
}

func aptMirrorReview(env *Env, a aptMirrorArgs) (aptapply.Request, []aptapply.AptLineDiff, string, error) {
	if !aptapply.IsVendorFile(a.File) {
		return aptapply.Request{}, nil, "", badApt("the file is not a vendor APT source file")
	}
	d, err := aptDistro(env)
	if err != nil {
		return aptapply.Request{}, nil, "", err
	}
	old, present, err := readAptFile(env, a.File)
	if err != nil {
		return aptapply.Request{}, nil, "", err
	}
	if !present {
		return aptapply.Request{}, nil, "", &proto.Error{Code: proto.CodeConflict, Message: "the source file no longer exists"}
	}
	req := aptapply.Request{Version: aptapply.ProtocolVersion, Action: aptapply.ActionMirrorSet, File: a.File,
		From: a.From, To: a.To, Digest: aptapply.Digest(old)}
	if err := aptapply.Validate(req); err != nil {
		return req, nil, "", badApt("the mirror change is not valid: " + err.Error())
	}
	next, _, err := aptapply.SetMirror(a.File, string(old), d, a.From, a.To)
	if err != nil {
		return req, nil, "", &proto.Error{Code: proto.CodeConflict, Message: "the source file cannot be changed safely: " + err.Error()}
	}
	return req, lineDiff(string(old), next), req.Digest + "/" + aptapply.Digest([]byte(next)), nil
}

// lineDiff lists changed lines of two texts with the same line count (a URI
// edit never adds or removes lines).
func lineDiff(a, b string) []aptapply.AptLineDiff {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	var out []aptapply.AptLineDiff
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			out = append(out, aptapply.AptLineDiff{Line: i + 1, Before: al[i], After: bl[i]})
		}
	}
	return out
}

func aptMirrorPlan(_ context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a aptMirrorArgs
	if err := proto.DecodeArgs(raw, &a); err != nil || a.Token != "" {
		return nil, badApt("arguments must be file, from and to")
	}
	if err := requireAptWrite(env); err != nil {
		return nil, err
	}
	req, lines, state, err := aptMirrorReview(env, a)
	if err != nil {
		return nil, err
	}
	tok, b, err := env.Writes.Mint(writes.Binding{
		Op: aptMirrorApplyOp, Action: req.Action, Target: a.File, Actor: env.Actor.User,
		Session: env.Actor.Session, StateDigest: state, Risk: RiskReversible,
	})
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "could not issue a plan token"}
	}
	return aptapply.AptPlan{Action: req.Action, Risk: RiskReversible, ConfirmationRequired: true, Token: tok, ExpiresAt: b.Expires,
		File: a.File, Lines: lines,
		Effect: "Use " + mirrorHost(a.To) + " instead of " + mirrorHost(a.From) + " for the distribution archive in " + a.File + ".",
		Consequences: []string{
			"Only the archive URI changes; suites, components, options and the signing key stay as they are, so APT keeps checking signatures with the same key.",
			"Security updates stay on the distribution's security server.",
			"The previous file is kept as a backup in " + aptapply.BackupDir + ".",
			"Package lists are not refreshed now; the new mirror is used the next time the package lists are updated.",
		}}, nil
}

func aptMirrorApply(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a aptMirrorArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, badApt("arguments must be file, from, to and token")
	}
	if a.Token == "" {
		return nil, badApt("the plan token is missing")
	}
	return aptApply(ctx, env, a.Token, aptMirrorApplyOp, aptapply.ActionMirrorSet, a.File, func() (aptapply.Request, string, error) {
		req, _, state, err := aptMirrorReview(env, a)
		return req, state, err
	}, nil)
}

func (env *Env) callAptApply(ctx context.Context, req aptapply.Request) (aptapply.Result, error) {
	if env.AptApply != nil {
		return env.AptApply(ctx, req)
	}
	return dialAptApply(ctx, req)
}

func dialAptApply(ctx context.Context, req aptapply.Request) (aptapply.Result, error) {
	var zero aptapply.Result
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", aptApplySock)
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
	b, err := io.ReadAll(io.LimitReader(conn, 64<<10))
	if err != nil {
		return zero, err
	}
	var res aptapply.Result
	if err := json.Unmarshal(b, &res); err != nil {
		return zero, err
	}
	if res.Version != aptapply.ProtocolVersion {
		return zero, errors.New("helper protocol")
	}
	return res, nil
}
