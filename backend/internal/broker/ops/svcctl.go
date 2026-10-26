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
	"regexp"
	"sort"
	"strconv"
	"strings"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/servicepolicy"
)

// UnitProps is the parsed, allow-listed output of `systemctl show` for one
// unit: identity, state, the safety-relevant properties and dependency lists.
type UnitProps struct {
	ID                string
	Names             []string
	Description       string
	LoadState         string
	ActiveState       string
	SubState          string
	Result            string
	UnitFileState     string
	FragmentPath      string
	MainPID           int
	ExecMainStatus    int
	InvocationID      string
	Type              string
	StateChange       string
	RefuseManualStart bool
	RefuseManualStop  bool
	// OnFailureJobMode/OnSuccessJobMode: "isolate" would stop other units
	// when a trigger fires.
	OnFailureJobMode string
	OnSuccessJobMode string
	// StopWhenUnneeded: systemd stops the unit once nothing needs it.
	StopWhenUnneeded bool
	CanStart         bool
	CanStop          bool
	// Deps holds the dependency properties listed in depProps.
	Deps map[string][]string
	// Actions holds FailureAction=, SuccessAction=, StartLimitAction=,
	// JobTimeoutAction= (anything other than "none" can reboot the host).
	Actions map[string]string
}

var depProps = []string{
	"Requires", "Requisite", "Wants", "BindsTo", "Upholds", "PartOf",
	"Conflicts", "ConflictedBy", "RequiredBy", "RequisiteOf", "BoundBy",
	"ConsistsOf", "PropagatesStopTo", "OnFailure", "OnSuccess",
}

var actionProps = []string{"FailureAction", "SuccessAction", "StartLimitAction", "JobTimeoutAction"}

var scalarProps = []string{
	"Id", "Names", "Description", "LoadState", "ActiveState", "SubState", "Result",
	"UnitFileState", "FragmentPath", "MainPID", "ExecMainStatus", "InvocationID", "Type",
	"StateChangeTimestamp", "RefuseManualStart", "RefuseManualStop", "CanStart", "CanStop",
	"OnFailureJobMode", "OnSuccessJobMode", "StopWhenUnneeded",
}

// showArgs is the fixed argv of the state query (runner.SystemctlShowArgs).
// The names have passed servicepolicy.ValidUnit or inspectableRE, so none can
// start with '-', and they follow "--" anyway.
func showArgs(units ...string) []string { return runner.SystemctlShowArgs(units...) }

// runShow runs the state query. Inside services.unit.apply (the only op that
// holds the root handle) it runs as the fixed, read-only root template, so a
// compromised hc-api — which shares the uid of unprivileged children — cannot
// alter the facts the final decision and the outcome rest on. Everywhere else
// (plan, recovery without a handle) it runs unprivileged; apply revalidates.
func runShow(ctx context.Context, env *Env, units ...string) (runner.Result, error) {
	if env.RootExec != nil {
		return env.RootExec.Run(ctx, runner.RootSystemctlShow, units...)
	}
	return env.Runner.Run(ctx, "systemctl", showArgs(units...)...)
}

// ParseShow parses `systemctl show` key=value output. Unknown keys are
// ignored; values are cleaned and capped.
func ParseShow(b []byte) (UnitProps, error) {
	p := UnitProps{Deps: map[string][]string{}, Actions: map[string]string{}}
	seen := false
	keys := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimRight(line, "\r"), "=")
		if !ok {
			continue
		}
		// One value per property: a repeated key means the output is not
		// what this parser expects, so it is refused rather than "last wins".
		if keys[k] {
			return p, errors.New("systemctl show repeated property " + clean(k, 40))
		}
		keys[k] = true
		switch k {
		case "Id":
			p.ID, seen = strings.TrimSpace(v), true
		case "Names":
			p.Names = unitList(v)
		case "Description":
			p.Description = clean(v, 256)
		case "LoadState":
			p.LoadState = state(v)
		case "ActiveState":
			p.ActiveState = state(v)
		case "SubState":
			p.SubState = state(v)
		case "Result":
			p.Result = state(v)
		case "UnitFileState":
			p.UnitFileState = state(v)
		case "FragmentPath":
			p.FragmentPath = clean(v, 512)
		case "MainPID":
			p.MainPID, _ = strconv.Atoi(strings.TrimSpace(v))
		case "ExecMainStatus":
			p.ExecMainStatus, _ = strconv.Atoi(strings.TrimSpace(v))
		case "InvocationID":
			if id := strings.TrimSpace(v); len(id) <= 64 && isHex(id) {
				p.InvocationID = id
			}
		case "Type":
			p.Type = state(v)
		case "StateChangeTimestamp":
			p.StateChange = clean(v, 64)
		case "RefuseManualStart":
			p.RefuseManualStart = strings.TrimSpace(v) == "yes"
		case "RefuseManualStop":
			p.RefuseManualStop = strings.TrimSpace(v) == "yes"
		case "CanStart":
			p.CanStart = strings.TrimSpace(v) == "yes"
		case "CanStop":
			p.CanStop = strings.TrimSpace(v) == "yes"
		case "OnFailureJobMode":
			p.OnFailureJobMode = state(v)
		case "OnSuccessJobMode":
			p.OnSuccessJobMode = state(v)
		case "StopWhenUnneeded":
			p.StopWhenUnneeded = strings.TrimSpace(v) == "yes"
		default:
			for _, d := range depProps {
				if k == d {
					p.Deps[k] = unitList(v)
				}
			}
			for _, a := range actionProps {
				if k == a {
					p.Actions[k] = clean(v, 64)
				}
			}
		}
	}
	if !seen || !unitNameRE.MatchString(p.ID) {
		return p, errors.New("systemctl show returned no valid unit ID")
	}
	return p, nil
}

func state(v string) string {
	v = strings.TrimSpace(v)
	if !stateRE.MatchString(v) {
		return "unknown"
	}
	return v
}

func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func unitList(v string) []string {
	var out []string
	for _, n := range strings.Fields(v) {
		if unitNameRE.MatchString(n) && len(out) < 256 {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Observed converts props into the public observed-state type.
func (p UnitProps) Observed() UnitObserved {
	return UnitObserved{
		ID: p.ID, Names: p.Names, Description: p.Description, LoadState: p.LoadState,
		ActiveState: p.ActiveState, SubState: p.SubState, Result: p.Result,
		UnitFileState: p.UnitFileState, MainPID: p.MainPID, ExecMainStatus: p.ExecMainStatus,
		InvocationID: p.InvocationID, Type: p.Type, StateSince: p.StateChange,
	}
}

// Digest is the state digest a plan token is bound to. Any change to the
// unit's identity, aliases, unit file, run state, invocation, safety
// properties or dependencies between plan and apply changes it.
func (p UnitProps) Digest() string {
	type canon struct {
		ID, Load, Active, Sub, File, Fragment, Invocation, Type, OnFailureJob, OnSuccessJob string
		MainPID                                                                             int
		Names                                                                               []string
		RefuseStart, RefuseStop, CanStart, CanStop, StopUnneeded                            bool
		Deps                                                                                map[string][]string
		Actions                                                                             map[string]string
	}
	b, _ := json.Marshal(canon{p.ID, p.LoadState, p.ActiveState, p.SubState, p.UnitFileState, p.FragmentPath,
		p.InvocationID, p.Type, p.OnFailureJobMode, p.OnSuccessJobMode, p.MainPID, p.Names, p.RefuseManualStart, p.RefuseManualStop, p.CanStart, p.CanStop, p.StopWhenUnneeded,
		p.Deps, p.Actions})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// UnitController reads unit state and performs the approved verbs. The
// production implementation is systemctlController; tests substitute a fake.
type UnitController interface {
	Show(ctx context.Context, env *Env, unit string) (UnitProps, error)
	// ShowMany reads the same properties for several units of any type
	// (dependency walk). Every name must match inspectableRE. The result has
	// one entry per name, in order; unknown units come back not-found.
	ShowMany(ctx context.Context, env *Env, units []string) ([]UnitProps, error)
	Act(ctx context.Context, env *Env, action servicepolicy.Action, unit string) (runner.Result, error)
}

// inspectableRE is the grammar of a dependency the broker can query: any unit
// type, but no leading '-', no systemd escapes ('\') and no separators, so the
// name is also a valid runner argument. Names outside it (for example "-.mount"
// or escaped mount and device units) are reported as uninspectable.
var inspectableRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.@-]{0,99}\.(service|socket|timer|mount|automount|path|target|swap|slice|scope|device)$`)

func inspectable(n string) bool {
	return inspectableRE.MatchString(n) && !strings.Contains(n, "..") && !strings.Contains(n, "@.")
}

// showBatch bounds the units queried by one systemctl call.
const showBatch = 32

type systemctlController struct{}

// SystemctlController is the production UnitController.
var SystemctlController UnitController = systemctlController{}

// Show runs the unprivileged, read-only state query.
func (systemctlController) Show(ctx context.Context, env *Env, unit string) (UnitProps, error) {
	if !servicepolicy.ValidUnit(unit) {
		return UnitProps{}, &proto.Error{Code: proto.CodeBadRequest, Message: "unit name is not valid"}
	}
	res, err := runShow(ctx, env, unit)
	if err != nil {
		return UnitProps{}, runErr(err)
	}
	if res.ExitCode != 0 {
		return UnitProps{}, &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl show failed: " + clean(res.Stderr, 200)}
	}
	p, err := ParseShow(res.Stdout)
	if err != nil {
		return UnitProps{}, &proto.Error{Code: proto.CodeInternal, Message: "unexpected systemctl show output"}
	}
	return p, nil
}

// ShowMany runs the unprivileged state query for a batch of dependency units.
// systemctl prints one block per unit, separated by an empty line, in order.
func (systemctlController) ShowMany(ctx context.Context, env *Env, units []string) ([]UnitProps, error) {
	if len(units) == 0 || len(units) > showBatch {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "dependency query out of bounds"}
	}
	for _, u := range units {
		if !inspectable(u) {
			return nil, &proto.Error{Code: proto.CodeInternal, Message: "dependency name cannot be queried"}
		}
	}
	res, err := runShow(ctx, env, units...)
	if err != nil {
		return nil, runErr(err)
	}
	if res.ExitCode != 0 {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl show failed: " + clean(res.Stderr, 200)}
	}
	return ParseShowMany(res.Stdout, len(units))
}

// ParseShowMany splits multi-unit `systemctl show` output into n blocks.
func ParseShowMany(b []byte, n int) ([]UnitProps, error) {
	var out []UnitProps
	for _, blk := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n\n") {
		if strings.TrimSpace(blk) == "" {
			continue
		}
		p, err := ParseShow([]byte(blk))
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if len(out) != n {
		return nil, errors.New("systemctl show returned an unexpected number of units")
	}
	return out, nil
}

var actTemplates = map[servicepolicy.Action]runner.RootTemplate{
	servicepolicy.Start:   runner.RootSystemctlStart,
	servicepolicy.Stop:    runner.RootSystemctlStop,
	servicepolicy.Restart: runner.RootSystemctlRestart,
}

// Act runs `systemctl --no-ask-password --job-mode=fail <verb> -- <unit>` as
// root through the root-exec handle. env.RootExec is only set for ops registered
// as root-exec (server.go); for every other op it is nil and this fails.
func (systemctlController) Act(ctx context.Context, env *Env, action servicepolicy.Action, unit string) (runner.Result, error) {
	t, ok := actTemplates[action]
	if !ok {
		return runner.Result{}, runner.ErrUnknownTemplate
	}
	if env.RootExec == nil {
		return runner.Result{}, runner.ErrNotAllowed
	}
	return env.RootExec.Run(ctx, t, unit)
}
