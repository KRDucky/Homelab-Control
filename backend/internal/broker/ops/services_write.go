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
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/servicepolicy"
)

// services.unit.plan / services.unit.apply — the first privileged write
// (M2-2; ADR 0011; owner decisions D-2…D-5). Review rules:
//   - only start/stop/restart of loaded .service units; nothing else;
//   - args strictly decoded; unit grammar servicepolicy.ValidUnit; the unit
//     must be one systemd knows (LoadState=loaded);
//   - the protected-unit policy is applied to the canonical ID, every alias,
//     the unit-file name and template bases, plus RefuseManualStart/Stop and
//     dependency propagation — here in the broker, regardless of the API;
//   - plan mints a token bound to actor, session, op, action, canonical
//     target, risk and state digest; apply consumes it, locks the target,
//     revalidates, journals the intent (fsync), runs the root template, reads
//     the state back and journals the observed outcome;
//   - no automatic retry, no rollback.

const serviceOpName = "services.unit"

// lockDomain is the exclusive lock of one service unit.
func lockDomain(id string) string { return "service:" + id }

type planArgs struct {
	Unit   string `json:"unit"`
	Action string `json:"action"`
}

type applyArgs struct {
	Unit   string `json:"unit"`
	Action string `json:"action"`
	Token  string `json:"token"`
}

func validWriteArgs(unit, action string) error {
	if !servicepolicy.ValidAction(action) {
		return &proto.Error{Code: proto.CodeBadRequest, Message: "action must be start, stop or restart"}
	}
	if !servicepolicy.ValidUnit(unit) {
		return &proto.Error{Code: proto.CodeBadRequest, Message: "unit must be a .service unit name (letters, digits, :_.@-; no templates or escapes)"}
	}
	return nil
}

// writeAuditArgs is the broker audit summary for plan and apply. It never
// includes the token.
func writeAuditArgs(raw json.RawMessage) string {
	var a struct {
		Unit   string `json:"unit"`
		Action string `json:"action"`
		Token  string `json:"token"`
	}
	if proto.DecodeArgs(raw, &a) != nil || validWriteArgs(a.Unit, a.Action) != nil {
		return "invalid args"
	}
	return "unit=" + a.Unit + " action=" + a.Action
}

func (env *Env) units() UnitController {
	if env.Units != nil {
		return env.Units
	}
	return SystemctlController
}

func requireSystemd(env *Env) error {
	if env.Units == nil && (env.Runner == nil || !env.Runner.Available("systemctl")) {
		return &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl is not installed; this host does not appear to use systemd"}
	}
	if fi, err := os.Stat(env.Roots.Path("/run/systemd/system")); err != nil || !fi.IsDir() {
		return &proto.Error{Code: proto.CodeUnavailable, Message: "systemd is not running as the init system on this host"}
	}
	if env.Writes == nil {
		return &proto.Error{Code: proto.CodeUnavailable, Message: "the broker operation journal is not available, so write operations are disabled"}
	}
	return nil
}

func denied(format string, a ...any) error {
	return &proto.Error{Code: proto.CodePolicyDenied, Message: fmt.Sprintf(format, a...)}
}

func union(p UnitProps, keys ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range keys {
		for _, n := range p.Deps[k] {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Dependency keys. Stopping (or restarting) a unit also stops (or restarts)
// the units listed under stopKeys; starting it starts the inactive units under
// startKeys and may trigger the units under triggerKeys.
var (
	stopKeys    = []string{"RequiredBy", "BoundBy", "ConsistsOf", "RequisiteOf", "PropagatesStopTo"}
	startKeys   = []string{"Requires", "Requisite", "Wants", "BindsTo", "Upholds"}
	triggerKeys = []string{"OnFailure", "OnSuccess"}
	// stopActions can fire when a unit stops (cleanly or not); startActions
	// also include the start-rate limit.
	stopActions  = []string{"FailureAction", "SuccessAction", "JobTimeoutAction"}
	startActions = []string{"FailureAction", "SuccessAction", "JobTimeoutAction", "StartLimitAction"}
)

// maxWalk bounds the dependency walk: the number of distinct related units
// inspected for one operation, across all directions. A graph larger than
// this is refused rather than partly checked.
const maxWalk = 96

// evaluate applies the unit-local policy: load state, unit type, protected
// names (canonical ID, aliases, unit file, template base) and systemd's own
// manual start/stop restrictions. assess adds the dependency checks.
func evaluate(action servicepolicy.Action, p UnitProps) error {
	if p.LoadState != "loaded" {
		switch p.LoadState {
		case "not-found":
			return &proto.Error{Code: proto.CodeNotFound, Message: "systemd does not know this unit"}
		case "masked":
			return denied("this unit is masked; unmasking is not authorized in this version")
		default:
			return denied("this unit is not loaded (load state %q)", p.LoadState)
		}
	}
	if !servicepolicy.ValidUnit(p.ID) {
		return denied("only .service units can be managed (the unit resolves to %q)", p.ID)
	}
	if pr := protection(p); pr.Protected {
		return denied("%s is protected: %s", p.ID, pr.Reason)
	}
	stopping := action == servicepolicy.Stop || action == servicepolicy.Restart
	starting := action == servicepolicy.Start || action == servicepolicy.Restart
	if stopping && p.RefuseManualStop {
		return denied("the unit declares RefuseManualStop=yes")
	}
	if starting && p.RefuseManualStart {
		return denied("the unit declares RefuseManualStart=yes")
	}
	if stopping && !p.CanStop {
		return denied("systemd reports that this unit cannot be stopped")
	}
	if starting && !p.CanStart {
		return denied("systemd reports that this unit cannot be started")
	}
	return nil
}

// protection applies the deny-list to every name systemd knows the unit by.
func protection(p UnitProps) servicepolicy.Protection {
	return servicepolicy.NamesProtection(append(append([]string{p.ID}, p.Names...), p.FragmentPath)...)
}

// unsafeActions returns the first *Action= property among keys that could
// reboot or power off the host.
func unsafeActions(p UnitProps, keys []string) (string, bool) {
	for _, k := range keys {
		if !servicepolicy.SafeEmergencyAction(p.Actions[k]) {
			return k + "=" + p.Actions[k], true
		}
	}
	return "", false
}

func dangerousTrigger(p UnitProps) (string, bool) {
	for _, n := range union(p, triggerKeys...) {
		if servicepolicy.DangerousTarget(n) {
			return n, true
		}
	}
	return "", false
}

func running(s string) bool {
	return s == "active" || s == "activating" || s == "reloading" || s == "refreshing" || s == "deactivating"
}

// assessment is the reviewed effect of one operation.
type assessment struct {
	risk  string
	cons  []string
	stops []string // other units systemd stops or restarts (if running)
	// starts are other, currently inactive units the operation starts.
	starts []string
	// hidden counts start dependencies whose names cannot be queried
	// (escaped mount and device names); they are listed as a consequence.
	hidden int
	// digest covers the affected-unit lists, so a plan is invalidated when
	// the set of units it would touch changes.
	digest string
}

// assessor walks the dependency graph for one operation. Every unit it
// inspects counts against one budget (maxWalk); results are cached by name.
type assessor struct {
	ctx    context.Context
	env    *Env
	self   string // the unit being operated on
	cache  map[string]UnitProps
	seen   int
	stops  map[string]bool // running units that would be stopped or restarted
	starts map[string]bool // inactive units that would be started
	hidden map[string]bool // start dependencies that cannot be queried
	// visited guards against re-checking a unit for the same direction.
	stopDone, startDone map[string]bool
}

func newAssessor(ctx context.Context, env *Env, p UnitProps) *assessor {
	a := &assessor{ctx: ctx, env: env, self: p.ID, cache: map[string]UnitProps{}, stops: map[string]bool{},
		starts: map[string]bool{}, hidden: map[string]bool{}, stopDone: map[string]bool{}, startDone: map[string]bool{}}
	a.remember(p)
	return a
}

func (a *assessor) remember(p UnitProps) {
	a.cache[p.ID] = p
	for _, n := range p.Names {
		a.cache[n] = p
	}
}

// show returns the units for names (queryable names only), from the cache or
// in batches from systemd, charging new ones to the budget.
func (a *assessor) show(names []string) ([]UnitProps, error) {
	var need []string
	for _, n := range names {
		if _, ok := a.cache[n]; !ok && !slices.Contains(need, n) {
			need = append(need, n)
		}
	}
	if a.seen+len(need) > maxWalk {
		return nil, denied("%s has more than %d related units to check; manage it on the host", a.self, maxWalk)
	}
	a.seen += len(need)
	for i := 0; i < len(need); i += showBatch {
		ps, err := a.env.units().ShowMany(a.ctx, a.env, need[i:min(i+showBatch, len(need))])
		if err != nil {
			return nil, err
		}
		for j, p := range ps {
			a.remember(p)
			a.cache[need[i+j]] = p
		}
	}
	out := make([]UnitProps, 0, len(names))
	for _, n := range names {
		out = append(out, a.cache[n])
	}
	return out, nil
}

// split separates queryable dependency names from uninspectable ones.
func split(names []string) (ok, hidden []string) {
	for _, n := range names {
		if inspectable(n) {
			ok = append(ok, n)
		} else {
			hidden = append(hidden, n)
		}
	}
	return ok, hidden
}

// isolating reports whether a unit's triggers use a job mode that would
// isolate (stop everything else) when they fire.
func isolating(p UnitProps) bool {
	return p.OnFailureJobMode == "isolate" || p.OnSuccessJobMode == "isolate"
}

// stopping checks u and every unit the stop of u propagates to,
// transitively. includeSelf applies the per-unit checks to u as well.
func (a *assessor) stopping(u UnitProps, includeSelf bool) error {
	frontier := []UnitProps{u}
	if includeSelf {
		if err := a.stopCheck(u); err != nil {
			return err
		}
	}
	a.stopDone[u.ID] = true
	for len(frontier) > 0 {
		var names []string
		for _, f := range frontier {
			names = append(names, union(f, stopKeys...)...)
		}
		ok, hidden := split(names)
		if len(hidden) > 0 {
			return denied("stopping %s would also affect %s, which cannot be inspected safely", a.self, hidden[0])
		}
		deps, err := a.show(ok)
		if err != nil {
			return err
		}
		frontier = nil
		for _, d := range deps {
			if a.stopDone[d.ID] {
				continue
			}
			a.stopDone[d.ID] = true
			// systemd propagates the stop job through inactive units too, so
			// the walk always continues.
			frontier = append(frontier, d)
			if err := a.stopCheck(d); err != nil {
				return err
			}
		}
	}
	return a.unneeded(u)
}

// unneeded handles StopWhenUnneeded=: a running unit that u needs and that
// stops once it is no longer needed may be stopped after u stops. Whether
// something else still needs it is not modelled, so it is checked as if it
// were stopped.
func (a *assessor) unneeded(u UnitProps) error {
	names := union(u, "Requires", "Requisite", "Wants", "BindsTo")
	ok, _ := split(names)
	ds, err := a.show(ok)
	if err != nil {
		return err
	}
	for _, d := range ds {
		if d.StopWhenUnneeded && running(d.ActiveState) && !a.stopDone[d.ID] {
			if err := a.stopping(d, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// stopCheck applies the per-unit stop checks to one unit that would be
// stopped. An inactive, loaded unit is not affected (systemd stops only
// running units); one whose state cannot be verified counts as running.
func (a *assessor) stopCheck(d UnitProps) error {
	if !running(d.ActiveState) && d.LoadState == "loaded" {
		return nil
	}
	if d.ID != a.self {
		if pr := protection(d); pr.Protected {
			return denied("this would stop or restart %s, which is protected: %s", d.ID, pr.Reason)
		}
		a.stops[d.ID] = true
	}
	if k, bad := unsafeActions(d, stopActions); bad {
		return denied("this would stop %s, which sets %s (reboot or power off)", d.ID, k)
	}
	if isolating(d) {
		return denied("%s triggers units in isolate mode, which would stop other services", d.ID)
	}
	// A stop fires OnSuccess= (clean stop) or OnFailure= (failed stop):
	// those units are started, so they get the full start checks.
	if trig := union(d, triggerKeys...); len(trig) > 0 {
		if err := a.startingNames(trig); err != nil {
			return err
		}
	}
	return nil
}

// startingNames resolves names and applies the start checks to each.
func (a *assessor) startingNames(names []string) error {
	ok, hidden := split(names)
	for _, n := range append(append([]string{}, ok...), hidden...) {
		if servicepolicy.DangerousTarget(n) {
			return denied("this could start %s (shutdown, reboot or recovery mode)", n)
		}
	}
	for _, n := range hidden {
		if strings.HasSuffix(n, ".service") {
			return denied("this would start %s, which cannot be inspected safely", n)
		}
		a.hidden[n] = true
	}
	us, err := a.show(ok)
	if err != nil {
		return err
	}
	for _, u := range us {
		if err := a.starting(u); err != nil {
			return err
		}
	}
	return nil
}

// starting applies the start checks to u (which is started, or restarted)
// and walks, transitively, the inactive units its start pulls in.
func (a *assessor) starting(u UnitProps) error {
	if a.startDone[u.ID] {
		return nil
	}
	a.startDone[u.ID] = true
	if servicepolicy.DangerousTarget(u.ID) {
		return denied("this could start %s (shutdown, reboot or recovery mode)", u.ID)
	}
	if u.LoadState != "loaded" {
		return nil // not-found or masked: systemd will not start it
	}
	if k, bad := unsafeActions(u, startActions); bad {
		return denied("%s sets %s, which can reboot or power off the host", u.ID, k)
	}
	if isolating(u) {
		return denied("%s triggers units in isolate mode, which would stop other services", u.ID)
	}
	if u.ID != a.self && !running(u.ActiveState) {
		a.starts[u.ID] = true
	}
	// Conflicts: systemd stops a running conflicting unit, which is a stop
	// with all its own propagation. Names are checked first so that a
	// protected conflict is refused even when it cannot be inspected.
	conflicts := union(u, "Conflicts", "ConflictedBy")
	for _, n := range conflicts {
		if pr := servicepolicy.NameProtection(n); pr.Protected {
			return denied("starting %s would stop %s, which is protected: %s", u.ID, n, pr.Reason)
		}
	}
	cok, chidden := split(conflicts)
	if len(chidden) > 0 {
		return denied("starting %s would stop %s, which cannot be inspected safely", u.ID, chidden[0])
	}
	cs, err := a.show(cok)
	if err != nil {
		return err
	}
	for _, c := range cs {
		if running(c.ActiveState) && !a.stopDone[c.ID] {
			if err := a.stopping(c, true); err != nil {
				return err
			}
		}
	}
	// Pulled-in and triggered units. Only inactive ones are walked further:
	// the dependencies of a running unit are already satisfied. A dangerous
	// unit is refused by name and not walked (its own graph would only hide
	// the reason).
	deps := union(u, append(append([]string{}, startKeys...), triggerKeys...)...)
	for _, n := range deps {
		if servicepolicy.DangerousTarget(n) {
			return denied("starting %s could pull in %s (shutdown, reboot or recovery mode)", u.ID, n)
		}
	}
	dok, dhidden := split(deps)
	for _, n := range dhidden {
		// Escaped mount, device and slice names are expected (RequiresMountsFor=,
		// -.mount); a service that cannot be inspected is not started unchecked.
		if strings.HasSuffix(n, ".service") {
			return denied("starting %s would also start %s, which cannot be inspected safely", u.ID, n)
		}
		a.hidden[n] = true
	}
	ds, err := a.show(dok)
	if err != nil {
		return err
	}
	for _, d := range ds {
		if !running(d.ActiveState) {
			if err := a.starting(d); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedKeys(m map[string]bool, except string) []string {
	var out []string
	for k := range m {
		if k != except {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// assess applies the dependency policy (D-5: no bypass through aliases,
// templates, alternate names — or dependencies). The graph is walked
// transitively within one budget (maxWalk units):
//   - every running unit a stop propagates to (RequiredBy, BoundBy,
//     ConsistsOf, RequisiteOf, PropagatesStopTo) must not be protected,
//     carry a reboot/power-off *Action= or trigger units in isolate mode;
//     units its stop triggers (OnSuccess=/OnFailure=) get the start checks;
//     a dependent whose state cannot be verified counts as running and one
//     whose name cannot be queried is refused;
//   - every unit that is started (the unit itself, the inactive units its
//     start pulls in, triggered units, and for restart the dependents that are
//     restarted with it) must not be a shutdown/reboot/recovery unit, carry
//     such an action or isolate, and every running unit it conflicts with
//     gets the full stop checks.
func assess(ctx context.Context, env *Env, action servicepolicy.Action, p UnitProps) (assessment, error) {
	var as assessment
	if err := evaluate(action, p); err != nil {
		return as, err
	}
	a := newAssessor(ctx, env, p)
	switch action {
	case servicepolicy.Stop:
		if err := a.stopping(p, true); err != nil {
			return as, err
		}
	case servicepolicy.Restart:
		if err := a.stopping(p, true); err != nil {
			return as, err
		}
		// Restart propagates as restart: the dependents start again.
		if err := a.starting(p); err != nil {
			return as, err
		}
		for _, n := range sortedKeys(a.stops, p.ID) {
			if err := a.starting(a.cache[n]); err != nil {
				return as, err
			}
		}
	case servicepolicy.Start:
		if err := a.starting(p); err != nil {
			return as, err
		}
	}
	as.stops = sortedKeys(a.stops, p.ID)
	as.starts = sortedKeys(a.starts, p.ID)
	as.hidden = len(a.hidden)

	as.risk = RiskReversible
	switch action {
	case servicepolicy.Stop:
		as.risk = RiskDisruptive
		as.cons = append(as.cons, "The service stops now. Clients connected to it lose their connection.",
			"Boot-time enablement is not changed: the service starts again at the next boot if it is enabled.")
	case servicepolicy.Restart:
		as.risk = RiskDisruptive
		as.cons = append(as.cons, "The service stops and starts again. Clients connected to it are interrupted.",
			"If the service is not running, restart starts it.")
	case servicepolicy.Start:
		as.cons = append(as.cons, "systemd starts the service and any dependencies it requires that are not yet running.",
			"Boot-time enablement is not changed.")
		if len(as.stops) > 0 {
			as.risk = RiskDisruptive // it stops running units (Conflicts=)
		}
	}
	if len(as.stops) > 0 {
		as.cons = append(as.cons, "Other running units are stopped or restarted with it (listed below).")
	}
	if len(as.starts) > 0 {
		as.cons = append(as.cons, "Inactive units it depends on, or that it triggers, are started too (listed below).")
	}
	if as.hidden > 0 {
		as.cons = append(as.cons, fmt.Sprintf("It also depends on %d mount, device or other units whose names cannot be inspected here; systemd starts them if needed.", as.hidden))
	}
	as.cons = append(as.cons, "This is a runtime action. There is no automatic rollback.")
	sum := sha256.Sum256([]byte(fmt.Sprintf("%q|%q|%d|%s", as.stops, as.starts, as.hidden, as.risk)))
	as.digest = hex.EncodeToString(sum[:8])
	if len(as.stops) > 50 {
		as.stops = as.stops[:50]
	}
	if len(as.starts) > 50 {
		as.starts = as.starts[:50]
	}
	return as, nil
}

func effectText(action servicepolicy.Action, p UnitProps) string {
	now := p.ActiveState
	if p.SubState != "" {
		now += " (" + p.SubState + ")"
	}
	switch action {
	case servicepolicy.Start:
		if p.ActiveState == "active" {
			return fmt.Sprintf("Start %s. It is already %s, so systemd will make no change.", p.ID, now)
		}
		return fmt.Sprintf("Start %s, which is currently %s.", p.ID, now)
	case servicepolicy.Stop:
		if p.ActiveState == "inactive" {
			return fmt.Sprintf("Stop %s. It is already %s, so systemd will make no change.", p.ID, now)
		}
		return fmt.Sprintf("Stop %s, which is currently %s.", p.ID, now)
	default:
		return fmt.Sprintf("Restart %s, which is currently %s.", p.ID, now)
	}
}

func unitPlan(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a planArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be {\"unit\": string, \"action\": string}"}
	}
	if err := validWriteArgs(a.Unit, a.Action); err != nil {
		return nil, err
	}
	if env.Actor.User == "" || env.Actor.Session == "" {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "write operations must be requested from an authenticated session"}
	}
	if err := requireSystemd(env); err != nil {
		return nil, err
	}
	// Cheap name check first: the requested name itself must not be protected.
	if pr := servicepolicy.NameProtection(a.Unit); pr.Protected {
		return nil, denied("%s is protected: %s", a.Unit, pr.Reason)
	}
	action := servicepolicy.Action(a.Action)
	p, err := env.units().Show(ctx, env, a.Unit)
	if err != nil {
		return nil, err
	}
	as, err := assess(ctx, env, action, p)
	if err != nil {
		return nil, err
	}
	if env.Writes.Locked(lockDomain(p.ID)) {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another operation on this unit is in progress"}
	}
	tok, b, err := env.Writes.Mint(writes.Binding{
		Op: serviceOpName, Action: a.Action, Target: p.ID, Actor: env.Actor.User,
		Session: env.Actor.Session, StateDigest: p.Digest() + "." + as.digest, Risk: as.risk,
	})
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "could not issue a plan token"}
	}
	return ServicePlan{
		Unit: p.ID, Requested: a.Unit, Action: a.Action, Risk: as.risk,
		StepUpRequired: as.risk != RiskReversible, Token: tok, ExpiresAt: b.Expires,
		Observed: p.Observed(), Effect: effectText(action, p), Consequences: as.cons, AlsoAffected: as.stops,
		AlsoStarted: as.starts,
	}, nil
}

// Timing of apply: the root child gets actTimeout; then the state is read
// back after settleMin and polled until it leaves a transitional state or
// settleMax passes. The op timeout (registry) covers all of it.
var (
	actTimeout  = 30 * time.Second
	settleMin   = 1500 * time.Millisecond
	settleMax   = 10 * time.Second
	settlePoll  = 500 * time.Millisecond
	showTimeout = 5 * time.Second
)

func transitional(s string) bool {
	switch s {
	case "activating", "deactivating", "reloading", "refreshing":
		return true
	}
	return false
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// observe reads the unit state back until it settles. It uses a context that
// is independent of the op deadline so that the outcome can still be read
// and journaled when systemctl itself timed out.
func observe(env *Env, id string) (*UnitProps, error) {
	ctx, cancel := context.WithTimeout(context.Background(), settleMax+showTimeout)
	defer cancel()
	if !sleepCtx(ctx, settleMin) {
		return nil, ctx.Err()
	}
	deadline := time.Now().Add(settleMax - settleMin)
	for {
		sctx, scancel := context.WithTimeout(ctx, showTimeout)
		p, err := env.units().Show(sctx, env, id)
		scancel()
		if err != nil {
			return nil, err
		}
		if !transitional(p.ActiveState) || time.Now().After(deadline) {
			return &p, nil
		}
		if !sleepCtx(ctx, settlePoll) {
			return &p, nil
		}
	}
}

// classify turns the systemctl result and the observed state into an outcome.
func classify(action servicepolicy.Action, res runner.Result, runErr error, obs *UnitProps, obsErr error) (outcome, msg string) {
	switch {
	case errors.Is(runErr, runner.ErrTimeout):
		outcome, msg = OutcomeUncertain, "systemctl did not finish within "+actTimeout.String()+"; systemd may still be processing the job."
	case runErr != nil:
		outcome, msg = OutcomeFailed, "systemctl could not be run: "+runErr.Error()
	case res.ExitCode != 0:
		outcome, msg = OutcomeFailed, fmt.Sprintf("systemctl exited with status %d: %s", res.ExitCode, clean(res.Stderr, 300))
	}
	if obsErr != nil || obs == nil {
		if outcome == "" {
			outcome = OutcomeUncertain
		}
		return outcome, strings.TrimSpace(msg + " The unit state could not be read back, so the result is not verified.")
	}
	st := obs.ActiveState
	if obs.SubState != "" {
		st += " (" + obs.SubState + ")"
	}
	observed := " Observed state: " + st + "."
	if obs.Result != "" && obs.Result != "success" {
		observed = " Observed state: " + st + ", result " + obs.Result + "."
	}
	if outcome != "" {
		return outcome, strings.TrimSpace(msg + observed)
	}
	switch action {
	case servicepolicy.Start, servicepolicy.Restart:
		switch {
		case obs.ActiveState == "active":
			return OutcomeSucceeded, "The service is running." + observed
		case obs.ActiveState == "failed":
			return OutcomeFailed, "systemd accepted the request, but the service failed." + observed
		case transitional(obs.ActiveState):
			return OutcomeUncertain, "The service has not finished starting." + observed
		case obs.ActiveState == "inactive" && obs.Type == "oneshot" && obs.Result == "success":
			return OutcomeSucceeded, "The one-shot service ran to completion." + observed
		default:
			return OutcomeUncertain, "systemd accepted the request, but the service is not running." + observed
		}
	default: // stop
		switch {
		case obs.ActiveState == "inactive":
			return OutcomeSucceeded, "The service is stopped." + observed
		case obs.ActiveState == "failed":
			return OutcomeSucceeded, "The service is stopped; systemd marks it as failed." + observed
		case transitional(obs.ActiveState):
			return OutcomeUncertain, "The service has not finished stopping." + observed
		default:
			return OutcomeFailed, "The service is still running." + observed
		}
	}
}

func recovery(id string, action servicepolicy.Action, before UnitProps) []string {
	return []string{
		"No automatic rollback was performed. Before this operation the unit was " + before.ActiveState + " (" + before.SubState + ").",
		"Review the unit's recent logs on this page for the reason.",
		"On the host, `systemctl status " + id + "` and `journalctl -u " + id + "` show the full state and log.",
	}
}

func unitApply(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a applyArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be {\"unit\": string, \"action\": string, \"token\": string}"}
	}
	if err := validWriteArgs(a.Unit, a.Action); err != nil {
		return nil, err
	}
	if err := requireSystemd(env); err != nil {
		return nil, err
	}
	if env.RootExec == nil {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "root execution is not available to this operation"}
	}
	action := servicepolicy.Action(a.Action)
	// 1. Verify and consume the token (bound to actor, session, op, action
	//    and canonical target; the step-up check happens inside, atomically).
	b, err := env.Writes.Consume(a.Token, writes.Request{
		Op: serviceOpName, Action: a.Action, Target: a.Unit, Actor: env.Actor.User,
		Session: env.Actor.Session, StepUpAt: env.Actor.StepUpAt,
	})
	switch {
	case errors.Is(err, writes.ErrStepUp):
		return nil, &proto.Error{Code: proto.CodeStepUpRequired, Message: "re-enter your password to confirm this disruptive operation"}
	case err != nil:
		return nil, &proto.Error{Code: proto.CodeInvalidToken, Message: "the plan is invalid, expired or already used; review the operation again"}
	}
	// 2. Lock the target.
	unlock, err := env.Writes.Lock(lockDomain(b.Target))
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another operation on this unit is in progress"}
	}
	defer unlock()
	// 3. Revalidate: same identity, same state, policy still allows it.
	sctx, scancel := context.WithTimeout(ctx, showTimeout)
	before, err := env.units().Show(sctx, env, b.Target)
	scancel()
	if err != nil {
		return nil, err
	}
	if before.ID != b.Target || !strings.HasPrefix(b.StateDigest, before.Digest()+".") {
		return nil, &proto.Error{Code: proto.CodeConflict, Message: "the unit changed since the operation was reviewed; nothing was done. Review it again."}
	}
	vctx, vcancel := context.WithTimeout(ctx, 2*showTimeout)
	as, err := assess(vctx, env, action, before)
	vcancel()
	if err != nil {
		return nil, err
	}
	if as.risk != b.Risk || before.Digest()+"."+as.digest != b.StateDigest {
		return nil, &proto.Error{Code: proto.CodeConflict, Message: "the units affected by the operation changed since it was reviewed; nothing was done. Review it again."}
	}
	// 4. Durable intent before anything runs.
	beforeObs, _ := json.Marshal(before.Observed())
	rec := writes.Record{Op: serviceOpName, Action: a.Action, Target: b.Target, Actor: env.Actor.User,
		RequestID: env.Actor.RequestID, TokenID: b.ID, Risk: b.Risk, State: b.StateDigest, Observed: beforeObs}
	opID, err := env.Writes.Intent(rec)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal cannot be written; nothing was executed"}
	}
	rec.OpID = opID
	// From here on the intent is durable. If the handler panics before the
	// outcome is written, record that the result is unknown instead of
	// leaving the operation pending until the next broker restart.
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
	// 5. Execute the fixed root template.
	actx, acancel := context.WithTimeout(context.Background(), actTimeout)
	res, runErr := env.units().Act(actx, env, action, b.Target)
	acancel()
	// 6. Read the state back and record the observed outcome.
	obs, obsErr := observe(env, b.Target)
	outcome, msg := classify(action, res, runErr, obs, obsErr)
	out := ServiceOutcome{OperationID: opID, Unit: b.Target, Action: a.Action, Outcome: outcome,
		Message: msg, StartedAt: started, FinishedAt: time.Now().UTC()}
	if runErr == nil {
		ec := res.ExitCode
		out.ExitCode = &ec
		rec.ExitCode = &ec
	}
	if obs != nil {
		o := obs.Observed()
		out.Observed = &o
		rec.Observed, _ = json.Marshal(o)
	} else {
		rec.Observed = nil
	}
	if outcome != OutcomeSucceeded {
		out.Recovery = recovery(b.Target, action, before)
	}
	rec.Outcome, rec.Message = outcome, msg
	outcomeWritten = true
	if err := env.Writes.Outcome(rec); err != nil {
		out.Message += " Warning: the outcome could not be written to the operation journal."
	}
	return out, nil
}

const maxListedOps = 100

func journalList(_ context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	if env.Writes == nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the broker operation journal is not available"}
	}
	entries, err := env.Writes.Read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal cannot be read"}
	}
	res := OperationList{Operations: []OperationRecord{}, Source: "broker operation journal (hash-chained, root-only)"}
	for i := len(entries) - 1; i >= 0 && len(res.Operations) < maxListedOps; i-- {
		e := entries[i]
		r := OperationRecord{ID: e.Intent.OpID, Op: e.Intent.Op, Action: e.Intent.Action, Unit: e.Intent.Target,
			Actor: e.Intent.Actor, RequestID: e.Intent.RequestID, Risk: e.Intent.Risk, Outcome: OutcomePending,
			StartedAt: e.IntentAt}
		if e.Final != nil {
			t := e.FinishedAt
			r.Outcome, r.Message, r.FinishedAt = e.Final.Outcome, e.Final.Message, &t
			if len(e.Final.Observed) > 0 {
				var o UnitObserved
				if json.Unmarshal(e.Final.Observed, &o) == nil {
					r.Observed = &o
				}
			}
		}
		if !slices.Contains([]string{OutcomeSucceeded, OutcomeFailed, OutcomeUncertain, OutcomeInterrupted, OutcomePending}, r.Outcome) {
			r.Outcome = OutcomeUncertain
		}
		res.Operations = append(res.Operations, r)
	}
	return res, nil
}

// InspectUnit is the writes.Inspector used at broker start to describe the
// state of units whose operation was interrupted.
func InspectUnit(env *Env) writes.Inspector {
	return func(target string) (json.RawMessage, string) {
		if !servicepolicy.ValidUnit(target) || (env.Units == nil && env.Runner == nil) {
			return nil, ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), showTimeout)
		defer cancel()
		p, err := env.units().Show(ctx, env, target)
		if err != nil {
			return nil, "unknown (the state could not be read)"
		}
		b, _ := json.Marshal(p.Observed())
		return b, p.ActiveState + " (" + p.SubState + ")"
	}
}
