// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/servicepolicy"
)

// services.systemctl.plan / services.systemctl.apply — closed systemctl verbs.
// Owner 2026-11-21: a function of systemctl/systemd is in the production admin.
// This is not a shell. Each verb is one root template. The older start/stop
// and enable/disable ops stay .service-only and are not renamed.

const (
	systemctlOpName = "services.systemctl"
	systemctlLock   = "systemd-manager"
	maxDropInBytes  = 32768
)

// systemctlExec is the root systemctl surface. *runner.Root implements it.
// Tests substitute a fake so apply does not spawn a process.
type systemctlExec interface {
	Run(ctx context.Context, t runner.RootTemplate, targets ...string) (runner.Result, error)
	RunStdin(ctx context.Context, t runner.RootTemplate, stdin []byte, targets ...string) (runner.Result, error)
}

// systemctlTemplates is the closed set services.systemctl.apply may run.
// The registry snapshot test pins this list.
var systemctlTemplates = []runner.RootTemplate{
	runner.RootSystemctlStartUnit,
	runner.RootSystemctlStopUnit,
	runner.RootSystemctlRestartUnit,
	runner.RootSystemctlReload,
	runner.RootSystemctlTryRestart,
	runner.RootSystemctlReloadOrRestart,
	runner.RootSystemctlKill,
	runner.RootSystemctlResetFailed,
	runner.RootSystemctlFreeze,
	runner.RootSystemctlThaw,
	runner.RootSystemctlIsolate,
	runner.RootSystemctlMask,
	runner.RootSystemctlUnmask,
	runner.RootSystemctlReenable,
	runner.RootSystemctlPreset,
	runner.RootSystemctlRevert,
	runner.RootSystemctlSetDefault,
	runner.RootSystemctlEnableUnit,
	runner.RootSystemctlDisableUnit,
	runner.RootSystemctlDaemonReload,
	runner.RootSystemctlDaemonReexec,
	runner.RootSystemctlPoweroff,
	runner.RootSystemctlReboot,
	runner.RootSystemctlHalt,
	runner.RootSystemctlSuspend,
	runner.RootSystemctlHibernate,
	runner.RootSystemctlHybridSleep,
	runner.RootSystemctlKexec,
	runner.RootSystemctlSoftReboot,
	runner.RootSystemctlResetFailedAll,
	runner.RootSystemctlAddWants,
	runner.RootSystemctlAddRequires,
	runner.RootSystemctlEdit,
}

type ctlKind string

const (
	ctlHost   ctlKind = "host"
	ctlUnit   ctlKind = "unit"
	ctlWants  ctlKind = "wants"
	ctlDropIn ctlKind = "dropin"
)

type ctlSpec struct {
	kind      ctlKind
	tpl       runner.RootTemplate
	plain     bool // no template or instance marker
	activates bool // refuse units that would power the host
}

// ctlSpecs is the verb table. Anything absent is a bad request. link,
// set-property, a chosen kill signal, and switch-root are not here: they
// need a path, a property string, or a signal, which is not a closed template.
var ctlSpecs = map[string]ctlSpec{
	"start":             {ctlUnit, runner.RootSystemctlStartUnit, false, true},
	"stop":              {ctlUnit, runner.RootSystemctlStopUnit, false, false},
	"restart":           {ctlUnit, runner.RootSystemctlRestartUnit, false, true},
	"reload":            {ctlUnit, runner.RootSystemctlReload, false, false},
	"try-restart":       {ctlUnit, runner.RootSystemctlTryRestart, false, true},
	"reload-or-restart": {ctlUnit, runner.RootSystemctlReloadOrRestart, false, true},
	"kill":              {ctlUnit, runner.RootSystemctlKill, false, false},
	"reset-failed":      {ctlUnit, runner.RootSystemctlResetFailed, false, false},
	"freeze":            {ctlUnit, runner.RootSystemctlFreeze, false, false},
	"thaw":              {ctlUnit, runner.RootSystemctlThaw, false, false},
	"isolate":           {ctlUnit, runner.RootSystemctlIsolate, false, true},
	"mask":              {ctlUnit, runner.RootSystemctlMask, true, false},
	"unmask":            {ctlUnit, runner.RootSystemctlUnmask, true, false},
	"reenable":          {ctlUnit, runner.RootSystemctlReenable, true, false},
	"preset":            {ctlUnit, runner.RootSystemctlPreset, true, false},
	"revert":            {ctlUnit, runner.RootSystemctlRevert, true, false},
	"set-default":       {ctlUnit, runner.RootSystemctlSetDefault, true, true},
	"enable":            {ctlUnit, runner.RootSystemctlEnableUnit, true, false},
	"disable":           {ctlUnit, runner.RootSystemctlDisableUnit, true, false},
	"daemon-reload":     {ctlHost, runner.RootSystemctlDaemonReload, false, false},
	"daemon-reexec":     {ctlHost, runner.RootSystemctlDaemonReexec, false, false},
	"poweroff":          {ctlHost, runner.RootSystemctlPoweroff, false, false},
	"reboot":            {ctlHost, runner.RootSystemctlReboot, false, false},
	"halt":              {ctlHost, runner.RootSystemctlHalt, false, false},
	"suspend":           {ctlHost, runner.RootSystemctlSuspend, false, false},
	"hibernate":         {ctlHost, runner.RootSystemctlHibernate, false, false},
	"hybrid-sleep":      {ctlHost, runner.RootSystemctlHybridSleep, false, false},
	"kexec":             {ctlHost, runner.RootSystemctlKexec, false, false},
	"soft-reboot":       {ctlHost, runner.RootSystemctlSoftReboot, false, false},
	"reset-failed-all":  {ctlHost, runner.RootSystemctlResetFailedAll, false, false},
	"add-wants":         {ctlWants, runner.RootSystemctlAddWants, true, true},
	"add-requires":      {ctlWants, runner.RootSystemctlAddRequires, true, true},
	"dropin":            {ctlDropIn, runner.RootSystemctlEdit, true, false},
}

type systemctlFields struct {
	Action  string `json:"action"`
	Unit    string `json:"unit"`
	Wants   string `json:"wants"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Token   string `json:"token"`
}

type systemctlPlanArgs struct {
	Action  string `json:"action"`
	Unit    string `json:"unit"`
	Wants   string `json:"wants"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type systemctlCall struct {
	action, unit, wants, name, target, digest string
	content                                   string
	spec                                      ctlSpec
}

// SystemctlPlan is the review result. Content is never copied into it.
type SystemctlPlan struct {
	Action         string    `json:"action"`
	Unit           string    `json:"unit,omitempty"`
	Wants          string    `json:"wants,omitempty"`
	Name           string    `json:"name,omitempty"`
	Risk           string    `json:"risk"`
	StepUpRequired bool      `json:"step_up_required"`
	Token          string    `json:"token"`
	ExpiresAt      time.Time `json:"expires_at"`
	Effect         string    `json:"effect"`
	Consequences   []string  `json:"consequences"`
}

// SystemctlOutcome is systemctl's exit status. It is not a second read of
// unit state: a power verb may drop the connection before that read could run.
type SystemctlOutcome struct {
	OperationID string    `json:"operation_id"`
	Action      string    `json:"action"`
	Unit        string    `json:"unit,omitempty"`
	Outcome     string    `json:"outcome"`
	Message     string    `json:"message"`
	ExitCode    *int      `json:"exit_code,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
}

func systemctlAuditArgs(raw json.RawMessage) string {
	var a systemctlFields
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return "unparsed"
	}
	s := a.Action
	if a.Unit != "" {
		s += " " + a.Unit
	}
	if a.Wants != "" {
		s += " wants=" + a.Wants
	}
	if a.Name != "" {
		s += " dropin=" + a.Name
	}
	if a.Content != "" {
		s += fmt.Sprintf(" bytes=%d", len(a.Content))
	}
	return s
}

func badCtl(msg string) error {
	return &proto.Error{Code: proto.CodeBadRequest, Message: msg}
}

func parseSystemctl(a systemctlFields) (systemctlCall, error) {
	spec, ok := ctlSpecs[a.Action]
	if !ok {
		return systemctlCall{}, badCtl("action is not one of the closed systemctl verbs")
	}
	c := systemctlCall{action: a.Action, unit: a.Unit, wants: a.Wants, name: a.Name, content: a.Content, spec: spec}
	switch spec.kind {
	case ctlHost:
		if a.Unit != "" || a.Wants != "" || a.Name != "" || a.Content != "" {
			return systemctlCall{}, badCtl("this verb does not take a unit, drop-in or dependency")
		}
		c.target = ""
	case ctlUnit:
		if a.Wants != "" || a.Name != "" || a.Content != "" {
			return systemctlCall{}, badCtl("this verb only takes a unit")
		}
		if err := ctlUnitOK(a.Unit, spec); err != nil {
			return systemctlCall{}, err
		}
		c.target = a.Unit
	case ctlWants:
		if a.Name != "" || a.Content != "" {
			return systemctlCall{}, badCtl("this verb takes a unit and the unit that should depend on it")
		}
		if err := ctlUnitOK(a.Unit, spec); err != nil {
			return systemctlCall{}, err
		}
		if err := ctlUnitOK(a.Wants, spec); err != nil {
			return systemctlCall{}, err
		}
		c.target = a.Wants + ">" + a.Unit
	case ctlDropIn:
		if a.Wants != "" {
			return systemctlCall{}, badCtl("a drop-in does not take a dependency")
		}
		if err := ctlUnitOK(a.Unit, spec); err != nil {
			return systemctlCall{}, err
		}
		if !servicepolicy.ValidDropInName(a.Name) {
			return systemctlCall{}, badCtl("drop-in name must be one file name ending in .conf")
		}
		if a.Content == "" || len(a.Content) > maxDropInBytes || !utf8.ValidString(a.Content) || strings.ContainsRune(a.Content, 0) {
			return systemctlCall{}, badCtl("drop-in content must be UTF-8, contain no NUL, and be 1 to 32768 bytes")
		}
		c.target = a.Unit + ">" + a.Name
	default:
		return systemctlCall{}, badCtl("action is not one of the closed systemctl verbs")
	}
	sum := sha256.Sum256([]byte(c.action + "\n" + c.unit + "\n" + c.wants + "\n" + c.name + "\n" + c.content))
	c.digest = hex.EncodeToString(sum[:])
	return c, nil
}

func ctlUnitOK(unit string, spec ctlSpec) error {
	ok := servicepolicy.ValidQueryUnit(unit)
	if spec.plain {
		ok = servicepolicy.ValidQueryPlain(unit)
	}
	if !ok {
		return badCtl("unit name is not valid for this verb")
	}
	if pr := servicepolicy.NameProtection(unit); pr.Protected {
		return denied("%s is protected: %s", unit, pr.Reason)
	}
	if spec.activates && servicepolicy.DangerousTarget(unit) {
		return denied("%s would power off, reboot or suspend the host; use the host power verb", unit)
	}
	return nil
}

func ctlEffect(c systemctlCall) (string, []string) {
	cons := []string{
		"No automatic rollback is performed.",
		"The broker runs one fixed systemctl template. It does not run a shell.",
	}
	switch c.spec.kind {
	case ctlHost:
		switch c.action {
		case "daemon-reload":
			return "Reload systemd unit files. Running services are not restarted by this verb.", append(cons, "PID 1 reloads its configuration.")
		case "daemon-reexec":
			return "Re-execute PID 1.", append(cons, "The broker connection can drop while systemd re-executes.")
		case "reset-failed-all":
			return "Clear the failed state of every unit.", cons
		default:
			return "Ask systemd to " + c.action + " the host.", append(cons, "The web session can drop before a result comes back.")
		}
	case ctlDropIn:
		cons = append(cons, "systemd is not reloaded. Run daemon-reload before PID 1 uses the new file.")
		return fmt.Sprintf("Replace drop-in %s for %s (%d bytes).", c.name, c.unit, len(c.content)), cons
	case ctlWants:
		verb := "Wants"
		if c.action == "add-requires" {
			verb = "Requires"
		}
		cons = append(cons, "systemd is not reloaded. Run daemon-reload before PID 1 uses the new dependency.")
		return fmt.Sprintf("Add %s from %s to %s.", verb, c.wants, c.unit), cons
	default:
		if c.action == "isolate" {
			cons = append(cons, "isolate stops units that the target does not pull in.")
		}
		if c.spec.plain && c.action != "set-default" && c.action != "isolate" {
			cons = append(cons, "systemd is not reloaded. Run daemon-reload before PID 1 uses this change.")
		}
		return c.action + " " + c.unit + ".", cons
	}
}

func (env *Env) systemctlRunner() systemctlExec {
	if env.Systemctl != nil {
		return env.Systemctl
	}
	if env.RootExec == nil {
		return nil
	}
	return env.RootExec
}

func systemctlPlan(_ context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a systemctlPlanArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, badCtl("arguments must be the closed systemctl fields")
	}
	c, err := parseSystemctl(systemctlFields{Action: a.Action, Unit: a.Unit, Wants: a.Wants, Name: a.Name, Content: a.Content})
	if err != nil {
		return nil, err
	}
	if env.Actor.User == "" || env.Actor.Session == "" {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "write operations must be requested from an authenticated session"}
	}
	if env.Writes == nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal is not available, so the change cannot be reviewed"}
	}
	if env.Writes.Locked(systemctlLock) {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another systemctl operation is in progress"}
	}
	effect, cons := ctlEffect(c)
	tok, b, err := env.Writes.Mint(writes.Binding{
		Op: systemctlOpName, Action: c.action, Target: c.target, Actor: env.Actor.User,
		Session: env.Actor.Session, StateDigest: c.digest, Risk: RiskDisruptive,
	})
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "could not issue a plan token"}
	}
	return SystemctlPlan{
		Action: c.action, Unit: c.unit, Wants: c.wants, Name: c.name,
		Risk: RiskDisruptive, StepUpRequired: true, Token: tok, ExpiresAt: b.Expires,
		Effect: effect, Consequences: cons,
	}, nil
}

func systemctlApply(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a systemctlFields
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, badCtl("arguments must be the closed systemctl fields and a plan token")
	}
	c, err := parseSystemctl(a)
	if err != nil {
		return nil, err
	}
	if a.Token == "" {
		return nil, badCtl("the plan token is missing")
	}
	if env.Actor.User == "" || env.Actor.Session == "" {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "write operations must be requested from an authenticated session"}
	}
	if env.Writes == nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal is not available, so nothing was executed"}
	}
	execer := env.systemctlRunner()
	if execer == nil {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "root execution is not available to this operation"}
	}
	b, err := env.Writes.Consume(a.Token, writes.Request{
		Op: systemctlOpName, Action: c.action, Target: c.target, Actor: env.Actor.User,
		Session: env.Actor.Session, StepUpAt: env.Actor.StepUpAt,
	})
	switch {
	case errors.Is(err, writes.ErrStepUp):
		return nil, &proto.Error{Code: proto.CodeStepUpRequired, Message: "re-enter your password to confirm this disruptive operation"}
	case err != nil:
		return nil, &proto.Error{Code: proto.CodeInvalidToken, Message: "the plan is invalid, expired or already used; review the operation again"}
	}
	if b.StateDigest != c.digest {
		return nil, &proto.Error{Code: proto.CodeConflict, Message: "the request changed since it was reviewed; nothing was done. Review it again."}
	}
	unlock, err := env.Writes.Lock(systemctlLock)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another systemctl operation is in progress"}
	}
	defer unlock()
	// Re-check policy after the token is bound. parseSystemctl already did;
	// a second call refuses a protected name even if the token was minted
	// under an older policy in this process.
	if _, err := parseSystemctl(a); err != nil {
		return nil, err
	}
	rec := writes.Record{Op: systemctlOpName, Action: c.action, Target: c.target, Actor: env.Actor.User,
		RequestID: env.Actor.RequestID, TokenID: b.ID, Risk: b.Risk, State: b.StateDigest}
	opID, err := env.Writes.Intent(rec)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal cannot be written; nothing was executed"}
	}
	rec.OpID = opID
	outcomeWritten := false
	defer func() {
		if r := recover(); r != nil {
			if !outcomeWritten {
				fail := rec
				fail.Outcome = OutcomeUncertain
				fail.Message = "The privileged helper failed while carrying out this operation; its result is unknown. It was not retried."
				_ = env.Writes.Outcome(fail)
			}
			panic(r)
		}
	}()
	started := time.Now().UTC()
	actx, cancel := context.WithTimeout(context.Background(), actTimeout)
	var res runner.Result
	var runErr error
	if c.spec.kind == ctlDropIn {
		res, runErr = execer.RunStdin(actx, c.spec.tpl, []byte(c.content), c.name, c.unit)
	} else if c.spec.kind == ctlWants {
		res, runErr = execer.Run(actx, c.spec.tpl, c.wants, c.unit)
	} else if c.spec.kind == ctlHost {
		res, runErr = execer.Run(actx, c.spec.tpl)
	} else {
		res, runErr = execer.Run(actx, c.spec.tpl, c.unit)
	}
	cancel()
	outcome, msg := classifySystemctl(res, runErr)
	if c.spec.kind == ctlHost && outcome == OutcomeSucceeded && c.action != "daemon-reload" && c.action != "reset-failed-all" {
		msg += " The host may drop this connection."
	}
	out := SystemctlOutcome{OperationID: opID, Action: c.action, Unit: c.unit, Outcome: outcome,
		Message: msg, StartedAt: started, FinishedAt: time.Now().UTC()}
	if runErr == nil {
		ec := res.ExitCode
		out.ExitCode = &ec
		rec.ExitCode = &ec
	}
	rec.Outcome, rec.Message = outcome, msg
	outcomeWritten = true
	if err := env.Writes.Outcome(rec); err != nil {
		out.Message += " Warning: the outcome could not be written to the operation journal."
	}
	return out, nil
}

func classifySystemctl(res runner.Result, runErr error) (string, string) {
	switch {
	case errors.Is(runErr, runner.ErrTimeout):
		return OutcomeUncertain, "systemctl did not finish within " + actTimeout.String() + "; systemd may still be processing the request."
	case runErr != nil:
		msg := "systemctl could not be run"
		var ex *runner.ExecError
		if errors.As(runErr, &ex) && ex.Class != "" {
			msg += " (" + ex.Class + ")"
		}
		return OutcomeFailed, msg
	case res.ExitCode != 0:
		return OutcomeFailed, fmt.Sprintf("systemctl exited with status %d: %s", res.ExitCode, clean(res.Stderr, 300))
	default:
		return OutcomeSucceeded, "systemd accepted the request. The result is systemctl's exit status."
	}
}
