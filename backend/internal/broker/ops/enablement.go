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
	"strings"
	"time"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/servicepolicy"
)

// services.enablement.plan / services.enablement.apply — boot-time enable and
// disable of one .service unit (M2-3; ADR 0011 review in
// docs/m2/M2-3-DESIGN-REVIEW.md §3). Review rules:
//   - two actions, enable and disable; never --now, --runtime or a reload;
//   - only loaded, non-template, non-instance .service units whose fresh
//     is-enabled state is exactly "disabled" (enable) or "enabled" (disable),
//     defined in the vendor/admin unit directories, not SysV, not transient;
//   - [Install] read as root from fragment + drop-ins; RequiredBy=, UpheldBy=,
//     Also=, DefaultInstance= and anything unparseable are refused;
//   - protected-unit policy on the canonical ID, aliases, unit file, install
//     aliases and WantedBy targets; for enable also the boot-time start walk;
//   - token op "services.enablement" (distinct from start/stop), disruptive
//     class (step-up), locks service:<id> + unit-files, root revalidation,
//     journal intent before and outcome after, independent read-back.

const enablementOpName = "services.enablement"

// unitFilesLock serializes enablement changes host-wide.
const unitFilesLock = "unit-files"

// Enablement actions (servicepolicy is the shared, pure grammar).
const (
	ActionEnable  = servicepolicy.Enable
	ActionDisable = servicepolicy.Disable
)

// systemdUnitDir is where systemctl enable writes its links (always the
// administrator directory, whatever the fragment's location).
const systemdUnitDir = "/etc/systemd/system/"

type enablementArgs struct {
	Unit   string `json:"unit"`
	Action string `json:"action"`
}

type enablementApplyArgs struct {
	Unit   string `json:"unit"`
	Action string `json:"action"`
	Token  string `json:"token"`
}

// ValidEnablementUnit is the grammar of an enablement target: a .service unit
// name without template or instance ('@').
func ValidEnablementUnit(unit string) bool { return servicepolicy.ValidEnablementUnit(unit) }

func validEnablementArgs(unit, action string) error {
	if !servicepolicy.ValidEnablementAction(action) {
		return &proto.Error{Code: proto.CodeBadRequest, Message: "action must be enable or disable"}
	}
	if !ValidEnablementUnit(unit) {
		return &proto.Error{Code: proto.CodeBadRequest, Message: "unit must be a .service unit name without templates, instances or escapes"}
	}
	return nil
}

func enablementAuditArgs(raw json.RawMessage) string {
	var a struct {
		Unit   string `json:"unit"`
		Action string `json:"action"`
		Token  string `json:"token"`
	}
	if proto.DecodeArgs(raw, &a) != nil || validEnablementArgs(a.Unit, a.Action) != nil {
		return "invalid args"
	}
	return "unit=" + a.Unit + " action=" + a.Action
}

// InstallProps is the parsed output of the fixed unit-file query.
type InstallProps struct {
	ID           string
	Names        []string
	LoadState    string
	FragmentPath string
	SourcePath   string
	DropInPaths  []string
	Transient    bool
	NeedReload   bool
	Preset       string
}

// ParseShowInstall parses `systemctl show` output of runner.ShowInstallArgs.
func ParseShowInstall(b []byte) (InstallProps, error) {
	var p InstallProps
	keys := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimRight(line, "\r"), "=")
		if !ok {
			continue
		}
		if keys[k] {
			return p, errors.New("systemctl show repeated property " + clean(k, 40))
		}
		keys[k] = true
		switch k {
		case "Id":
			p.ID = strings.TrimSpace(v)
		case "Names":
			p.Names = unitList(v)
		case "LoadState":
			p.LoadState = state(v)
		case "FragmentPath":
			p.FragmentPath = strings.TrimSpace(v)
		case "SourcePath":
			p.SourcePath = strings.TrimSpace(v)
		case "DropInPaths":
			p.DropInPaths = strings.Fields(v)
		case "Transient":
			p.Transient = strings.TrimSpace(v) == "yes"
		case "NeedDaemonReload":
			p.NeedReload = strings.TrimSpace(v) == "yes"
		case "UnitFilePreset":
			p.Preset = state(v)
		}
	}
	if !unitNameRE.MatchString(p.ID) {
		return p, errors.New("systemctl show returned no valid unit ID")
	}
	return p, nil
}

// EnablementController reads unit-file facts and performs enable/disable.
// The production implementation runs systemctl; tests substitute a fake.
type EnablementController interface {
	ShowInstall(ctx context.Context, env *Env, unit string) (InstallProps, error)
	// IsEnabled returns systemd's enablement state, computed from disk.
	IsEnabled(ctx context.Context, env *Env, unit string) (string, error)
	Change(ctx context.Context, env *Env, action, unit string) (runner.Result, error)
}

type systemctlEnablement struct{}

// SystemctlEnablement is the production EnablementController.
var SystemctlEnablement EnablementController = systemctlEnablement{}

func (env *Env) enablement() EnablementController {
	if env.Enablement != nil {
		return env.Enablement
	}
	return SystemctlEnablement
}

// ShowInstall runs the unit-file query: as the fixed root template inside
// apply (which holds the root handle), unprivileged elsewhere.
func (systemctlEnablement) ShowInstall(ctx context.Context, env *Env, unit string) (InstallProps, error) {
	if !servicepolicy.ValidUnit(unit) {
		return InstallProps{}, &proto.Error{Code: proto.CodeBadRequest, Message: "unit name is not valid"}
	}
	var res runner.Result
	var err error
	if env.RootExec != nil {
		res, err = env.RootExec.Run(ctx, runner.RootSystemctlShowInstall, unit)
	} else {
		res, err = env.Runner.Run(ctx, "systemctl", runner.ShowInstallArgs(unit)...)
	}
	if err != nil {
		return InstallProps{}, runErr(err)
	}
	if res.ExitCode != 0 {
		return InstallProps{}, &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl show failed: " + clean(res.Stderr, 200)}
	}
	p, err := ParseShowInstall(res.Stdout)
	if err != nil {
		return InstallProps{}, &proto.Error{Code: proto.CodeInternal, Message: "unexpected systemctl show output"}
	}
	return p, nil
}

// IsEnabled runs `systemctl is-enabled -- unit`. Its exit status encodes the
// state (non-zero for disabled), so only the printed word is used.
func (systemctlEnablement) IsEnabled(ctx context.Context, env *Env, unit string) (string, error) {
	if !servicepolicy.ValidUnit(unit) {
		return "", &proto.Error{Code: proto.CodeBadRequest, Message: "unit name is not valid"}
	}
	var res runner.Result
	var err error
	if env.RootExec != nil {
		res, err = env.RootExec.Run(ctx, runner.RootSystemctlIsEnabled, unit)
	} else {
		res, err = env.Runner.Run(ctx, "systemctl", runner.IsEnabledArgs(unit)...)
	}
	if err != nil {
		return "", runErr(err)
	}
	word := strings.TrimSpace(strings.SplitN(string(res.Stdout), "\n", 2)[0])
	if word == "" || !stateRE.MatchString(word) {
		return "", &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl is-enabled gave no usable answer: " + clean(res.Stderr, 200)}
	}
	return word, nil
}

var changeTemplates = map[string]runner.RootTemplate{
	ActionEnable:  runner.RootSystemctlEnable,
	ActionDisable: runner.RootSystemctlDisable,
}

func (systemctlEnablement) Change(ctx context.Context, env *Env, action, unit string) (runner.Result, error) {
	t, ok := changeTemplates[action]
	if !ok {
		return runner.Result{}, runner.ErrUnknownTemplate
	}
	if env.RootExec == nil {
		return runner.Result{}, runner.ErrNotAllowed
	}
	return env.RootExec.Run(ctx, t, unit)
}

// enablementReview is the reviewed effect of one enablement change.
type enablementReview struct {
	p     UnitProps
	ip    InstallProps
	info  InstallInfo
	state string   // is-enabled before
	links []string // links enable creates / disable removes (absolute paths)
	// Boot-time start walk (enable only).
	stops, starts []string
	hidden        int
	digest        string
}

var stateRefusal = map[string]string{
	"static":          "it has no [Install] section, so it is started only as a dependency or by activation; there is nothing to enable or disable",
	"indirect":        "it is enabled indirectly (through Also= or another unit), which this version does not change",
	"generated":       "it is generated at boot (for example from /etc/fstab or an init script), so its enablement is not a unit-file setting",
	"transient":       "it is a transient unit created at runtime",
	"masked":          "it is masked; unmasking is not authorized in this version",
	"masked-runtime":  "it is masked until the next boot; unmasking is not authorized in this version",
	"linked":          "its unit file is linked from outside the unit directories, which this version does not manage",
	"linked-runtime":  "its unit file is linked until the next boot, which this version does not manage",
	"enabled-runtime": "it is enabled only until the next boot (runtime enablement), which this version does not change",
	"alias":           "the name is an alias; manage the unit by its own name",
	"not-found":       "systemd has no unit file for it",
	"bad":             "systemd reports its unit file as invalid",
}

func reviewEnablement(ctx context.Context, env *Env, action, requested string) (enablementReview, error) {
	var r enablementReview
	if pr := servicepolicy.NameProtection(requested); pr.Protected {
		return r, denied("%s is protected: %s", requested, pr.Reason)
	}
	p, err := env.units().Show(ctx, env, requested)
	if err != nil {
		return r, err
	}
	switch p.LoadState {
	case "loaded":
	case "not-found":
		return r, &proto.Error{Code: proto.CodeNotFound, Message: "systemd does not know this unit"}
	case "masked":
		return r, denied("this unit is masked; unmasking is not authorized in this version")
	default:
		return r, denied("this unit is not loaded (load state %q)", p.LoadState)
	}
	if !ValidEnablementUnit(p.ID) {
		return r, denied("only .service units without templates or instances can be enabled or disabled here (the unit resolves to %q)", p.ID)
	}
	if pr := protection(p); pr.Protected {
		return r, denied("%s is protected: %s", p.ID, pr.Reason)
	}
	ip, err := env.enablement().ShowInstall(ctx, env, p.ID)
	if err != nil {
		return r, err
	}
	if ip.ID != p.ID || ip.LoadState != "loaded" {
		return r, &proto.Error{Code: proto.CodeConflict, Message: "systemd answered inconsistently about this unit; nothing was done"}
	}
	if ip.SourcePath != "" {
		return r, denied("%s is generated from %s (for example a SysV init script); enabling it would run distribution scripts this version does not review", p.ID, clean(ip.SourcePath, 200))
	}
	if ip.Transient {
		return r, denied("%s is a transient unit created at runtime", p.ID)
	}
	st, err := env.enablement().IsEnabled(ctx, env, p.ID)
	if err != nil {
		return r, err
	}
	want := map[string]string{ActionEnable: "disabled", ActionDisable: "enabled"}[action]
	if st != want {
		switch {
		case action == ActionEnable && st == "enabled":
			return r, denied("%s is already enabled at boot; nothing would change", p.ID)
		case action == ActionDisable && st == "disabled":
			return r, denied("%s is already disabled at boot; nothing would change", p.ID)
		case stateRefusal[st] != "":
			return r, denied("%s cannot be %sd here: %s", p.ID, action, stateRefusal[st])
		default:
			return r, denied("%s reports the enablement state %q, which this version does not change", p.ID, st)
		}
	}
	info, err := ReadInstall(env.Roots, ip.FragmentPath, ip.DropInPaths)
	if err != nil {
		if IsUnsupported(err) {
			return r, denied("%s cannot be %sd here: %s", p.ID, action, err.Error())
		}
		return r, denied("the unit files of %s could not be read safely", p.ID)
	}
	switch {
	case len(info.Also) > 0:
		return r, denied("%s declares Also=%s; enabling or disabling it would change other units too, which this version does not support", p.ID, strings.Join(info.Also, " "))
	case len(info.RequiredBy) > 0 || len(info.UpheldBy) > 0:
		return r, denied("%s declares RequiredBy= or UpheldBy=; a failure of such a unit can block its targets at boot, which this version does not support", p.ID)
	case len(info.DefaultInstance) > 0:
		return r, denied("%s declares DefaultInstance=, which is not supported here", p.ID)
	case len(info.WantedBy) == 0 && len(info.Alias) == 0:
		return r, denied("%s has no WantedBy= or Alias= in its [Install] section; there is nothing to enable", p.ID)
	}
	for _, t := range info.WantedBy {
		if pr := servicepolicy.NameProtection(t); pr.Protected {
			return r, denied("%s is wanted by %s, which is protected: %s", p.ID, t, pr.Reason)
		}
		if servicepolicy.DangerousTarget(t) {
			return r, denied("%s is wanted by %s (shutdown, reboot or recovery); this version does not change such units", p.ID, t)
		}
		r.links = append(r.links, systemdUnitDir+t+".wants/"+p.ID)
	}
	for _, a := range info.Alias {
		if !ValidEnablementUnit(a) || a == p.ID {
			return r, denied("%s declares Alias=%s, which this version does not support", p.ID, a)
		}
		if pr := servicepolicy.NameProtection(a); pr.Protected {
			return r, denied("%s declares the alias %s, which is protected: %s", p.ID, a, pr.Reason)
		}
		// An alias must not name another unit: systemd would create
		// /etc/systemd/system/<alias> and, at the next reload or boot, that
		// link would replace the other unit's definition (measured, probe 4).
		if action == ActionEnable {
			if err := aliasIsFree(ctx, env, p.ID, a); err != nil {
				return r, err
			}
		}
		r.links = append(r.links, systemdUnitDir+a)
	}
	if action == ActionDisable {
		// A protected unit that declares Requisite= on this one would fail at
		// boot once this one is no longer started there.
		for _, n := range p.Deps["RequisiteOf"] {
			if pr := servicepolicy.NameProtection(n); pr.Protected {
				return r, denied("%s is a requisite of %s, which is protected: %s", p.ID, n, pr.Reason)
			}
		}
	}
	// The links must all be in the opposite state now; anything else is an
	// inconsistent unit this version does not repair. A file that is not a
	// symbolic link at a link path is never overwritten or removed.
	for _, l := range r.links {
		if fi, err := os.Lstat(env.Roots.Path(l)); err == nil && fi.Mode()&os.ModeSymlink == 0 {
			return r, denied("%s cannot be %sd here: %s exists and is not a symbolic link. Check it on the host.", p.ID, action, l)
		}
	}
	for _, l := range linkStates(env, r.links) {
		if l.Present == (action == ActionEnable) {
			return r, denied("%s is in an inconsistent state: %s is already %s. Check the unit on the host.", p.ID, l.Path,
				map[bool]string{true: "present", false: "absent"}[l.Present])
		}
	}
	if action == ActionEnable {
		// What the unit's start at boot would do, with the M2-2 start checks.
		a := newAssessor(ctx, env, p)
		if err := a.starting(p); err != nil {
			return r, err
		}
		r.stops, r.starts, r.hidden = sortedKeys(a.stops, p.ID), sortedKeys(a.starts, p.ID), len(a.hidden)
	}
	r.p, r.ip, r.info, r.state = p, ip, info, st
	sum := sha256.Sum256([]byte(fmt.Sprintf("%q|%q|%q|%q|%q|%v|%s|%q|%q|%d",
		ip.ID, ip.Names, ip.FragmentPath, ip.DropInPaths, ip.SourcePath, ip.Transient, st,
		info.Digest(ip.FragmentPath, ip.DropInPaths), append(append([]string{}, r.stops...), r.starts...), r.hidden)))
	r.digest = p.Digest() + "." + hex.EncodeToString(sum[:12])
	return r, nil
}

func (r enablementReview) consequences(action string) []string {
	var c []string
	if action == ActionEnable {
		c = append(c,
			"systemd will start the service at the next boot, as part of "+strings.Join(nonEmpty(r.info.WantedBy, "its alias only"), ", ")+".",
			"This does not start it now: its current state stays "+r.p.ActiveState+". Use Start for that.")
		if len(r.stops) > 0 {
			c = append(c, "When it starts at boot, systemd stops the conflicting units listed below if they are running.")
		}
		if len(r.starts) > 0 {
			c = append(c, "When it starts, systemd also starts the units it depends on (listed below).")
		}
		if r.hidden > 0 {
			c = append(c, fmt.Sprintf("It also depends on %d mount, device or other units whose names cannot be inspected here.", r.hidden))
		}
	} else {
		c = append(c,
			"systemd will no longer start the service at boot through "+strings.Join(nonEmpty(r.info.WantedBy, "its alias"), ", ")+".",
			"This does not stop it now: its current state stays "+r.p.ActiveState+". Use Stop for that.",
			"It can still start at boot if another unit requires it, or through socket, timer, path or D-Bus activation.")
		if len(r.info.Alias) > 0 {
			c = append(c, "Its alias names ("+strings.Join(r.info.Alias, ", ")+") are removed too; anything that refers to the service by an alias will no longer find it after the next reload or boot.")
		}
	}
	c = append(c,
		"The running systemd manager is not reloaded: the change takes effect at the next boot, and until then `systemctl status` on the host may still show the old setting.",
		"There is no automatic rollback. To undo it, review the opposite action.")
	return c
}

// aliasIsFree refuses an alias name that systemd already knows as a unit file
// or as a unit other than id (both answered by root at apply).
func aliasIsFree(ctx context.Context, env *Env, id, alias string) error {
	st, err := env.enablement().IsEnabled(ctx, env, alias)
	if err != nil {
		return err
	}
	if st != "not-found" {
		return denied("%s declares Alias=%s, but a unit file with that name already exists (%s); enabling it would replace that unit's definition", id, alias, st)
	}
	q, err := env.units().Show(ctx, env, alias)
	if err != nil {
		return err
	}
	if q.LoadState != "not-found" && q.ID != id {
		return denied("%s declares Alias=%s, but systemd already knows that name as %s; enabling it would replace that unit", id, alias, q.ID)
	}
	return nil
}

func nonEmpty(l []string, empty string) []string {
	if len(l) == 0 {
		return []string{empty}
	}
	return l
}

func unitEnablementPlan(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a enablementArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be {\"unit\": string, \"action\": string}"}
	}
	if err := validEnablementArgs(a.Unit, a.Action); err != nil {
		return nil, err
	}
	if env.Actor.User == "" || env.Actor.Session == "" {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "write operations must be requested from an authenticated session"}
	}
	if err := requireSystemd(env); err != nil {
		return nil, err
	}
	r, err := reviewEnablement(ctx, env, a.Action, a.Unit)
	if err != nil {
		return nil, err
	}
	if env.Writes.Locked(lockDomain(r.p.ID)) || env.Writes.Locked(unitFilesLock) {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another operation on this unit or on unit files is in progress"}
	}
	tok, b, err := env.Writes.Mint(writes.Binding{
		Op: enablementOpName, Action: a.Action, Target: r.p.ID, Actor: env.Actor.User,
		Session: env.Actor.Session, StateDigest: r.digest, Risk: RiskDisruptive,
	})
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "could not issue a plan token"}
	}
	obs := r.p.Observed()
	obs.UnitFileState = r.state // fresh from disk, not systemd's cached value
	effect := fmt.Sprintf("Enable %s at boot. It is currently disabled.", r.p.ID)
	if a.Action == ActionDisable {
		effect = fmt.Sprintf("Disable %s at boot. It is currently enabled.", r.p.ID)
	}
	var notes []string
	if r.ip.NeedReload {
		notes = append(notes, "systemd reports that its loaded copy of this unit differs from the files on disk (NeedDaemonReload=yes). This is expected after an earlier boot-time change made without a reload; the review uses the files on disk.")
	}
	return EnablementPlan{
		Unit: r.p.ID, Requested: a.Unit, Action: a.Action, Risk: RiskDisruptive, StepUpRequired: true,
		Token: tok, ExpiresAt: b.Expires, Observed: obs, BootState: r.state, Effect: effect,
		Consequences: r.consequences(a.Action), Links: r.links, WantedBy: r.info.WantedBy, Aliases: r.info.Alias,
		AlsoAffected: capList(r.stops, 50), AlsoStarted: capList(r.starts, 50), Preset: r.ip.Preset, Notes: notes,
	}, nil
}

func capList(l []string, n int) []string {
	if len(l) > n {
		return l[:n]
	}
	return l
}

// linkStates lstats the expected links itself (independent of systemctl).
func linkStates(env *Env, links []string) []LinkObservation {
	out := make([]LinkObservation, 0, len(links))
	for _, l := range links {
		fi, err := os.Lstat(env.Roots.Path(l))
		out = append(out, LinkObservation{Path: l, Present: err == nil && fi.Mode()&os.ModeSymlink != 0})
	}
	return out
}

// reportedChanges keeps systemctl's own "Created symlink"/"Removed" lines.
func reportedChanges(res runner.Result) []string {
	var out []string
	for _, l := range strings.Split(string(res.Stdout)+"\n"+res.Stderr, "\n") {
		l = strings.TrimSpace(l)
		if (strings.HasPrefix(l, "Created symlink") || strings.HasPrefix(l, "Removed")) && len(out) < 16 {
			out = append(out, clean(l, 300))
		}
	}
	return out
}

// classifyEnablement compares the request with what was observed afterwards.
func classifyEnablement(action string, res runner.Result, runErr error, before, after string, afterErr error, links []LinkObservation) (string, string) {
	want := map[string]string{ActionEnable: "enabled", ActionDisable: "disabled"}[action]
	wantPresent := action == ActionEnable
	ok, changed := 0, 0
	for _, l := range links {
		if l.Present == wantPresent {
			ok++
		}
	}
	changed = ok // links started in the opposite state (revalidated), so a match is a change
	linkText := fmt.Sprintf("%d of %d expected links %s", ok, len(links), map[bool]string{true: "present", false: "removed"}[wantPresent])
	if errors.Is(runErr, runner.ErrTimeout) {
		return OutcomeUncertain, "systemctl did not finish within " + actTimeout.String() + ". Observed afterwards: " + linkText + "."
	}
	if runErr != nil {
		msg := "systemctl could not be run"
		var ex *runner.ExecError
		if errors.As(runErr, &ex) && ex.Class != "" {
			msg += " (" + ex.Class + ")"
		}
		return OutcomeFailed, msg + "; nothing was executed. Observed: " + linkText + "."
	}
	if afterErr != nil {
		return OutcomeUncertain, fmt.Sprintf("systemctl exited with status %d, but the enablement state could not be read back, so the result is not verified. Observed: %s.", res.ExitCode, linkText)
	}
	observed := fmt.Sprintf(" Observed: is-enabled reports %q; %s.", after, linkText)
	switch {
	case res.ExitCode == 0 && after == want && ok == len(links):
		return OutcomeSucceeded, map[string]string{ActionEnable: "The service is enabled at boot.", ActionDisable: "The service is disabled at boot."}[action] + observed
	case changed == 0 && after == before:
		if res.ExitCode != 0 {
			return OutcomeFailed, fmt.Sprintf("systemctl exited with status %d: %s Nothing appears to have changed.%s", res.ExitCode, clean(res.Stderr, 300), observed)
		}
		return OutcomeFailed, "systemctl reported success, but no change is observed." + observed
	default:
		prefix := "The change is only partly applied."
		if res.ExitCode != 0 {
			prefix = fmt.Sprintf("systemctl exited with status %d (%s), and the change is only partly applied.", res.ExitCode, clean(res.Stderr, 200))
		}
		return OutcomeFailed, prefix + observed
	}
}

func unitEnablementApply(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a enablementApplyArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be {\"unit\": string, \"action\": string, \"token\": string}"}
	}
	if err := validEnablementArgs(a.Unit, a.Action); err != nil {
		return nil, err
	}
	if err := requireSystemd(env); err != nil {
		return nil, err
	}
	if env.RootExec == nil {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "root execution is not available to this operation"}
	}
	// 1. Verify and consume the token (step-up checked inside, atomically).
	b, err := env.Writes.Consume(a.Token, writes.Request{
		Op: enablementOpName, Action: a.Action, Target: a.Unit, Actor: env.Actor.User,
		Session: env.Actor.Session, StepUpAt: env.Actor.StepUpAt,
	})
	switch {
	case errors.Is(err, writes.ErrStepUp):
		return nil, &proto.Error{Code: proto.CodeStepUpRequired, Message: "re-enter your password to confirm this boot-time change"}
	case err != nil:
		return nil, &proto.Error{Code: proto.CodeInvalidToken, Message: "the plan is invalid, expired or already used; review the change again"}
	}
	// 2. Lock the unit (shared with start/stop) and the unit files.
	unlock, err := env.Writes.Lock(lockDomain(b.Target))
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another operation on this unit is in progress"}
	}
	defer unlock()
	unlockFiles, err := env.Writes.Lock(unitFilesLock)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another change to unit files is in progress"}
	}
	defer unlockFiles()
	// 3. Revalidate everything with root reads (env.RootExec is set here).
	vctx, vcancel := context.WithTimeout(ctx, 3*showTimeout)
	r, err := reviewEnablement(vctx, env, a.Action, b.Target)
	vcancel()
	if err != nil {
		var pe *proto.Error
		if errors.As(err, &pe) && pe.Code == proto.CodePolicyDenied {
			pe.Message = "The unit changed since it was reviewed and is no longer allowed: " + pe.Message + " Nothing was done."
		}
		return nil, err
	}
	if r.p.ID != b.Target || r.digest != b.StateDigest {
		return nil, &proto.Error{Code: proto.CodeConflict, Message: "the unit or its unit files changed since the change was reviewed; nothing was done. Review it again."}
	}
	// 4. Durable intent before anything runs.
	beforeObs := r.p.Observed()
	beforeObs.UnitFileState = r.state
	bo, _ := json.Marshal(beforeObs)
	rec := writes.Record{Op: enablementOpName, Action: a.Action, Target: b.Target, Actor: env.Actor.User,
		RequestID: env.Actor.RequestID, TokenID: b.ID, Risk: b.Risk, State: b.StateDigest, Observed: bo}
	opID, err := env.Writes.Intent(rec)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal cannot be written; nothing was executed"}
	}
	rec.OpID = opID
	outcomeWritten := false
	defer func() {
		if p := recover(); p != nil {
			if !outcomeWritten {
				fail := rec
				fail.Outcome = OutcomeUncertain
				fail.Message = "The privileged helper failed while carrying out this change; its result is unknown. It was not retried."
				_ = env.Writes.Outcome(fail)
			}
			panic(p)
		}
	}()
	started := time.Now().UTC()
	// 5. The fixed root template (no --now, no reload).
	actx, acancel := context.WithTimeout(context.Background(), actTimeout)
	res, runErr := env.enablement().Change(actx, env, a.Action, b.Target)
	acancel()
	// 6. Independent read-back: is-enabled (root, from disk) and the links.
	rctx, rcancel := context.WithTimeout(context.Background(), showTimeout)
	after, afterErr := env.enablement().IsEnabled(rctx, env, b.Target)
	rcancel()
	links := linkStates(env, r.links)
	outcome, msg := classifyEnablement(a.Action, res, runErr, r.state, after, afterErr, links)
	out := EnablementOutcome{OperationID: opID, Unit: b.Target, Action: a.Action, Outcome: outcome, Message: msg,
		BootStateBefore: r.state, Links: links, Reported: reportedChanges(res), StartedAt: started, FinishedAt: time.Now().UTC()}
	if afterErr == nil {
		out.BootState = after
	}
	if runErr == nil {
		ec := res.ExitCode
		out.ExitCode, rec.ExitCode = &ec, &ec
	}
	obs := r.p.Observed()
	obs.UnitFileState = out.BootState
	out.Observed = &obs
	rec.Observed, _ = json.Marshal(obs)
	if outcome != OutcomeSucceeded {
		out.Recovery = []string{
			"No automatic rollback was performed. Before this change the unit was " + r.state + " at boot.",
			"On the host, `systemctl is-enabled " + b.Target + "` shows the current setting and `ls -l /etc/systemd/system/*.wants/` the links.",
			"Review the change again here once the unit is in a consistent state; partial changes are not repaired automatically.",
		}
	}
	rec.Outcome, rec.Message = outcome, msg
	outcomeWritten = true
	if err := env.Writes.Outcome(rec); err != nil {
		out.Message += " Warning: the outcome could not be written to the operation journal."
	}
	return out, nil
}
