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

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/hostinfo"
)

// host.settings.plan / host.settings.apply — set static hostname, pretty
// hostname and timezone (M2-6; ADR 0011 pair). Review rules:
//   - closed hostnamectl/timedatectl argv templates only; values after --;
//   - empty pretty hostname is removal;
//   - risk class reversible (confirmation required, no step-up);
//   - lock domain host-settings; token target is "host" so the journal never
//     stores the requested names as the target;
//   - state digest binds observed + requested values without putting them in
//     Action/Target; revalidation refuses if either moved;
//   - read-back is hostnamectl/timedatectl, never hc-api's UTS namespace
//     (ProtectHostname=yes);
//   - AuditArgs are field names only, never values or the token.

const hostSettingsOpName = "host.settings"

const hostSettingsLock = "host-settings"

const hostSettingsTarget = "host"

const hostSettingsAction = "set"

const hostSettingsHostnameWarning = "Changing the static host name can invalidate TLS certificates that name this host, mDNS/Avahi names, and entries in /etc/hosts. Homelab Control does not update those."

const hostSettingsReadbackNote = "This result reports hostnamectl and timedatectl. The kernel host name seen by hc-api may stay at the value from when that service started (ProtectHostname=yes); it is not proof that the kernel was renamed."

type hostSettingsArgs struct {
	StaticHostname *string `json:"static_hostname"`
	PrettyHostname *string `json:"pretty_hostname"`
	Timezone       *string `json:"timezone"`
}

type hostSettingsApplyArgs struct {
	StaticHostname *string `json:"static_hostname"`
	PrettyHostname *string `json:"pretty_hostname"`
	Timezone       *string `json:"timezone"`
	Token          string  `json:"token"`
}

func hostSettingsFields(a hostSettingsArgs) []string {
	var f []string
	if a.StaticHostname != nil {
		f = append(f, "static_hostname")
	}
	if a.PrettyHostname != nil {
		f = append(f, "pretty_hostname")
	}
	if a.Timezone != nil {
		f = append(f, "timezone")
	}
	return f
}

func hostSettingsAuditArgs(raw json.RawMessage) string {
	var a hostSettingsApplyArgs
	if proto.DecodeArgs(raw, &a) != nil {
		return "invalid args"
	}
	fields := hostSettingsFields(hostSettingsArgs{StaticHostname: a.StaticHostname, PrettyHostname: a.PrettyHostname, Timezone: a.Timezone})
	if len(fields) == 0 {
		return "invalid args"
	}
	return "fields=" + strings.Join(fields, ",")
}

func validHostSettingsArgs(a hostSettingsArgs) error {
	if a.StaticHostname == nil && a.PrettyHostname == nil && a.Timezone == nil {
		return &proto.Error{Code: proto.CodeBadRequest, Message: "request at least one of static_hostname, pretty_hostname or timezone"}
	}
	if a.StaticHostname != nil {
		if r := hostinfo.ValidStaticHostname(*a.StaticHostname); r != "" {
			return &proto.Error{Code: proto.CodeBadRequest, Message: r}
		}
	}
	if a.PrettyHostname != nil {
		if r := hostinfo.ValidPrettyHostname(*a.PrettyHostname); r != "" {
			return &proto.Error{Code: proto.CodeBadRequest, Message: r}
		}
	}
	if a.Timezone != nil {
		if r := hostinfo.ValidTimezoneName(*a.Timezone); r != "" {
			return &proto.Error{Code: proto.CodeBadRequest, Message: r}
		}
	}
	return nil
}

// HostSettingsController reads hostnamectl/timedatectl and applies the three
// closed templates. Production uses the runner; tests substitute a fake.
type HostSettingsController interface {
	Status(ctx context.Context, env *Env) (HostSettingsObserved, error)
	ListTimezones(ctx context.Context, env *Env) ([]string, error)
	SetStatic(ctx context.Context, env *Env, name string) (runner.Result, error)
	SetPretty(ctx context.Context, env *Env, name string) (runner.Result, error)
	SetTimezone(ctx context.Context, env *Env, name string) (runner.Result, error)
}

type hostnamectlTimedatectl struct{}

// SystemHostSettings is the production HostSettingsController.
var SystemHostSettings HostSettingsController = hostnamectlTimedatectl{}

func (env *Env) hostSettings() HostSettingsController {
	if env.HostSettings != nil {
		return env.HostSettings
	}
	return SystemHostSettings
}

func runHostTemplate(ctx context.Context, env *Env, name string, t runner.RootTemplate, unprivArgs []string, targets ...string) (runner.Result, error) {
	if env.RootExec != nil {
		return env.RootExec.Run(ctx, t, targets...)
	}
	if env.Runner == nil || !env.Runner.Available(name) {
		return runner.Result{}, &proto.Error{Code: proto.CodeUnavailable, Message: name + " is not available on this host"}
	}
	return env.Runner.Run(ctx, name, unprivArgs...)
}

func (hostnamectlTimedatectl) Status(ctx context.Context, env *Env) (HostSettingsObserved, error) {
	var out HostSettingsObserved
	hn, hnErr := runHostTemplate(ctx, env, "hostnamectl", runner.RootHostnamectlStatus, []string{"--no-ask-password", "status"})
	if hnErr != nil {
		out.StaticStatus, out.PrettyStatus, out.TransientStatus = hostinfo.SettingUnavailable, hostinfo.SettingUnavailable, hostinfo.SettingUnavailable
		out.Notes = append(out.Notes, "hostnamectl status could not be run.")
	} else if hn.ExitCode != 0 {
		out.StaticStatus, out.PrettyStatus, out.TransientStatus = hostinfo.SettingUnavailable, hostinfo.SettingUnavailable, hostinfo.SettingUnavailable
		out.Notes = append(out.Notes, "hostnamectl status exited with status "+fmt.Sprint(hn.ExitCode)+".")
	} else {
		parseHostnamectlStatus(hn.Stdout, &out)
	}
	td, tdErr := runHostTemplate(ctx, env, "timedatectl", runner.RootTimedatectlStatus, []string{"--no-ask-password", "status"})
	if tdErr != nil {
		out.TimezoneStatus = hostinfo.SettingUnavailable
		out.Notes = append(out.Notes, "timedatectl status could not be run.")
	} else if td.ExitCode != 0 {
		out.TimezoneStatus = hostinfo.SettingUnavailable
		out.Notes = append(out.Notes, "timedatectl status exited with status "+fmt.Sprint(td.ExitCode)+".")
	} else {
		parseTimedatectlStatus(td.Stdout, &out)
	}
	if out.StaticStatus == "" {
		out.StaticStatus = hostinfo.SettingUnavailable
	}
	if out.PrettyStatus == "" {
		out.PrettyStatus = hostinfo.SettingNotSet
	}
	if out.TimezoneStatus == "" {
		out.TimezoneStatus = hostinfo.SettingUnavailable
	}
	return out, nil
}

func (hostnamectlTimedatectl) ListTimezones(ctx context.Context, env *Env) ([]string, error) {
	res, err := runHostTemplate(ctx, env, "timedatectl", runner.RootTimedatectlListTimezones, []string{"--no-ask-password", "list-timezones"})
	if err != nil {
		return nil, runErr(err)
	}
	if res.ExitCode != 0 {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "timedatectl list-timezones failed"}
	}
	var out []string
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || hostinfo.ValidTimezoneName(line) != "" {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

func (hostnamectlTimedatectl) SetStatic(ctx context.Context, env *Env, name string) (runner.Result, error) {
	if env.RootExec == nil {
		return runner.Result{}, runner.ErrNotAllowed
	}
	return env.RootExec.Run(ctx, runner.RootHostnamectlSetStatic, name)
}

func (hostnamectlTimedatectl) SetPretty(ctx context.Context, env *Env, name string) (runner.Result, error) {
	if env.RootExec == nil {
		return runner.Result{}, runner.ErrNotAllowed
	}
	return env.RootExec.Run(ctx, runner.RootHostnamectlSetPretty, name)
}

func (hostnamectlTimedatectl) SetTimezone(ctx context.Context, env *Env, name string) (runner.Result, error) {
	if env.RootExec == nil {
		return runner.Result{}, runner.ErrNotAllowed
	}
	return env.RootExec.Run(ctx, runner.RootTimedatectlSetTimezone, name)
}

func parseHostnamectlField(b []byte, label string) (value, status string) {
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(trim, label) {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(trim, label))
		if v == "" || strings.EqualFold(v, "n/a") {
			return "", hostinfo.SettingNotSet
		}
		return v, hostinfo.SettingObserved
	}
	return "", hostinfo.SettingNotSet
}

func parseHostnamectlStatus(b []byte, out *HostSettingsObserved) {
	out.StaticHostname, out.StaticStatus = parseHostnamectlField(b, "Static hostname:")
	out.PrettyHostname, out.PrettyStatus = parseHostnamectlField(b, "Pretty hostname:")
	out.TransientHostname, out.TransientStatus = parseHostnamectlField(b, "Transient hostname:")
}

func parseTimedatectlStatus(b []byte, out *HostSettingsObserved) {
	v, st := parseHostnamectlField(b, "Time zone:")
	if st != hostinfo.SettingObserved {
		out.Timezone, out.TimezoneStatus = "", st
		if st == hostinfo.SettingNotSet {
			out.TimezoneStatus = hostinfo.SettingUnavailable
			out.Notes = append(out.Notes, "timedatectl status did not report a time zone.")
		}
		return
	}
	if i := strings.IndexAny(v, " ("); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	if hostinfo.ValidTimezoneName(v) != "" {
		out.TimezoneStatus = hostinfo.SettingUnavailable
		out.Notes = append(out.Notes, "timedatectl reported a time zone name that is not shown.")
		return
	}
	out.Timezone, out.TimezoneStatus = v, hostinfo.SettingObserved
}

func requireHostSettings(env *Env) error {
	if env.Writes == nil {
		return &proto.Error{Code: proto.CodeUnavailable, Message: "the broker operation journal is not available, so write operations are disabled"}
	}
	if env.HostSettings != nil {
		return nil
	}
	if env.Runner == nil || !env.Runner.Available("hostnamectl") || !env.Runner.Available("timedatectl") {
		return &proto.Error{Code: proto.CodeUnavailable, Message: "hostnamectl or timedatectl is not available on this host"}
	}
	return nil
}

func observedValue(status, value string) string {
	if status == hostinfo.SettingObserved {
		return value
	}
	return ""
}

func hostSettingsDigest(obs HostSettingsObserved, a hostSettingsArgs) string {
	staticReq, prettyReq, tzReq := "-", "-", "-"
	if a.StaticHostname != nil {
		staticReq = "S=" + *a.StaticHostname
	}
	if a.PrettyHostname != nil {
		prettyReq = "P=" + *a.PrettyHostname
	}
	if a.Timezone != nil {
		tzReq = "Z=" + *a.Timezone
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s",
		obs.StaticStatus, observedValue(obs.StaticStatus, obs.StaticHostname),
		obs.PrettyStatus, observedValue(obs.PrettyStatus, obs.PrettyHostname),
		obs.TimezoneStatus, observedValue(obs.TimezoneStatus, obs.Timezone),
		staticReq, prettyReq, tzReq,
		obs.TransientStatus, observedValue(obs.TransientStatus, obs.TransientHostname))))
	return hex.EncodeToString(sum[:16])
}

func fieldCurrent(obs HostSettingsObserved, field string) (value, status string) {
	switch field {
	case "static_hostname":
		return obs.StaticHostname, obs.StaticStatus
	case "pretty_hostname":
		return obs.PrettyHostname, obs.PrettyStatus
	case "timezone":
		return obs.Timezone, obs.TimezoneStatus
	}
	return "", hostinfo.SettingUnavailable
}

func planChanges(obs HostSettingsObserved, a hostSettingsArgs) (changes []HostSettingChange, would int, err error) {
	add := func(field, requested string) {
		cur, st := fieldCurrent(obs, field)
		c := HostSettingChange{Field: field, Current: cur, Requested: requested}
		switch {
		case st == hostinfo.SettingUnavailable:
			c.Outcome, c.Reason = "cannot-compare", "The current value could not be read from hostnamectl/timedatectl."
			err = &proto.Error{Code: proto.CodeUnavailable, Message: "the current " + field + " could not be read, so the change cannot be reviewed"}
		case cur == requested && st == hostinfo.SettingObserved:
			c.Outcome = "unchanged"
		case requested == "" && field == "pretty_hostname" && st == hostinfo.SettingNotSet:
			c.Outcome = "unchanged"
		default:
			c.Outcome = "would-change"
			would++
		}
		changes = append(changes, c)
	}
	if a.StaticHostname != nil {
		add("static_hostname", *a.StaticHostname)
	}
	if a.PrettyHostname != nil {
		add("pretty_hostname", *a.PrettyHostname)
	}
	if a.Timezone != nil {
		add("timezone", *a.Timezone)
	}
	return changes, would, err
}

func hostSettingsWarnings(a hostSettingsArgs, changes []HostSettingChange) []string {
	var w []string
	for _, c := range changes {
		if c.Field == "static_hostname" && c.Outcome == "would-change" {
			w = append(w, hostSettingsHostnameWarning)
			if a.StaticHostname != nil && (*a.StaticHostname == "localhost" || strings.HasPrefix(*a.StaticHostname, "localhost.")) {
				w = append(w, "A static host name of localhost makes this host hard to tell apart on the network.")
			}
		}
	}
	return w
}

func hostSettingsEffect(changes []HostSettingChange) string {
	var parts []string
	for _, c := range changes {
		if c.Outcome != "would-change" {
			continue
		}
		switch c.Field {
		case "static_hostname":
			parts = append(parts, "set the static host name")
		case "pretty_hostname":
			if c.Requested == "" {
				parts = append(parts, "remove the pretty host name")
			} else {
				parts = append(parts, "set the pretty host name")
			}
		case "timezone":
			parts = append(parts, "set the time zone")
		}
	}
	if len(parts) == 0 {
		return "No host setting would change."
	}
	return "This would " + strings.Join(parts, ", ") + "."
}

func hostSettingsConsequences(changes []HostSettingChange) []string {
	c := []string{
		"Confirmation is required. This class is reversible: it does not require a password re-entry.",
		"There is no automatic rollback. To undo it, review the previous values.",
		hostSettingsReadbackNote,
	}
	for _, ch := range changes {
		if ch.Field == "static_hostname" && ch.Outcome == "would-change" {
			c = append([]string{hostSettingsHostnameWarning}, c...)
			break
		}
	}
	return c
}

func timezoneAllowed(zones []string, name string) bool {
	for _, z := range zones {
		if z == name {
			return true
		}
	}
	return false
}

func reviewHostSettings(ctx context.Context, env *Env, a hostSettingsArgs) (HostSettingsObserved, []HostSettingChange, string, error) {
	obs, err := env.hostSettings().Status(ctx, env)
	if err != nil {
		return obs, nil, "", err
	}
	if a.Timezone != nil {
		zones, err := env.hostSettings().ListTimezones(ctx, env)
		if err != nil {
			return obs, nil, "", err
		}
		if !timezoneAllowed(zones, *a.Timezone) {
			return obs, nil, "", &proto.Error{Code: proto.CodeBadRequest, Message: "this time zone is not in timedatectl list-timezones on this host"}
		}
	}
	changes, would, err := planChanges(obs, a)
	if err != nil {
		return obs, changes, "", err
	}
	if would == 0 {
		return obs, changes, "", &proto.Error{Code: proto.CodeBadRequest, Message: "nothing would change; the requested values already match hostnamectl/timedatectl"}
	}
	return obs, changes, hostSettingsDigest(obs, a), nil
}

func hostSettingsPlan(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a hostSettingsArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be an object of static_hostname, pretty_hostname and/or timezone"}
	}
	if err := validHostSettingsArgs(a); err != nil {
		return nil, err
	}
	if env.Actor.User == "" || env.Actor.Session == "" {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "write operations must be requested from an authenticated session"}
	}
	if err := requireHostSettings(env); err != nil {
		return nil, err
	}
	obs, changes, digest, err := reviewHostSettings(ctx, env, a)
	if err != nil {
		return nil, err
	}
	if env.Writes.Locked(hostSettingsLock) {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another host-settings operation is in progress"}
	}
	tok, b, err := env.Writes.Mint(writes.Binding{
		Op: hostSettingsOpName, Action: hostSettingsAction, Target: hostSettingsTarget,
		Actor: env.Actor.User, Session: env.Actor.Session, StateDigest: digest, Risk: RiskReversible,
	})
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "could not issue a plan token"}
	}
	obs.Notes = append(obs.Notes, hostSettingsReadbackNote)
	return HostSettingsPlan{
		Action: hostSettingsAction, Risk: RiskReversible, StepUpRequired: false, ConfirmationRequired: true,
		Token: tok, ExpiresAt: b.Expires, Observed: obs, Effect: hostSettingsEffect(changes),
		Consequences: hostSettingsConsequences(changes), Warnings: hostSettingsWarnings(a, changes),
		Changes: changes, Fields: hostSettingsFields(a),
	}, nil
}

type appliedStep struct {
	field string
	res   runner.Result
	err   error
}

func hostSettingsApply(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a hostSettingsApplyArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be the reviewed fields plus token"}
	}
	req := hostSettingsArgs{StaticHostname: a.StaticHostname, PrettyHostname: a.PrettyHostname, Timezone: a.Timezone}
	if err := validHostSettingsArgs(req); err != nil {
		return nil, err
	}
	if err := requireHostSettings(env); err != nil {
		return nil, err
	}
	if env.RootExec == nil {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "root execution is not available to this operation"}
	}
	b, err := env.Writes.Consume(a.Token, writes.Request{
		Op: hostSettingsOpName, Action: hostSettingsAction, Target: hostSettingsTarget,
		Actor: env.Actor.User, Session: env.Actor.Session, StepUpAt: env.Actor.StepUpAt,
	})
	switch {
	case errors.Is(err, writes.ErrStepUp):
		return nil, &proto.Error{Code: proto.CodeStepUpRequired, Message: "re-enter your password to confirm this change"}
	case err != nil:
		return nil, &proto.Error{Code: proto.CodeInvalidToken, Message: "the plan is invalid, expired or already used; review the change again"}
	}
	unlock, err := env.Writes.Lock(hostSettingsLock)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another host-settings operation is in progress"}
	}
	defer unlock()
	vctx, vcancel := context.WithTimeout(ctx, 3*showTimeout)
	obs, _, digest, err := reviewHostSettings(vctx, env, req)
	vcancel()
	if err != nil {
		return nil, err
	}
	if digest != b.StateDigest {
		return nil, &proto.Error{Code: proto.CodeConflict, Message: "the host settings changed since the change was reviewed; nothing was done. Review it again."}
	}
	before, _ := json.Marshal(struct {
		Fields []string `json:"fields"`
	}{Fields: hostSettingsFields(req)})
	rec := writes.Record{Op: hostSettingsOpName, Action: hostSettingsAction, Target: hostSettingsTarget, Actor: env.Actor.User,
		RequestID: env.Actor.RequestID, TokenID: b.ID, Risk: b.Risk, State: b.StateDigest, Observed: before}
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
	var steps []appliedStep
	run := func(field string, fn func(context.Context) (runner.Result, error)) {
		actx, acancel := context.WithTimeout(context.Background(), actTimeout)
		res, runErr := fn(actx)
		acancel()
		steps = append(steps, appliedStep{field: field, res: res, err: runErr})
	}
	if req.StaticHostname != nil && (obs.StaticStatus != hostinfo.SettingObserved || obs.StaticHostname != *req.StaticHostname) {
		name := *req.StaticHostname
		run("static_hostname", func(c context.Context) (runner.Result, error) { return env.hostSettings().SetStatic(c, env, name) })
	}
	if req.PrettyHostname != nil && !(obs.PrettyStatus == hostinfo.SettingObserved && obs.PrettyHostname == *req.PrettyHostname) &&
		!(obs.PrettyStatus == hostinfo.SettingNotSet && *req.PrettyHostname == "") {
		name := *req.PrettyHostname
		run("pretty_hostname", func(c context.Context) (runner.Result, error) { return env.hostSettings().SetPretty(c, env, name) })
	}
	if req.Timezone != nil && (obs.TimezoneStatus != hostinfo.SettingObserved || obs.Timezone != *req.Timezone) {
		name := *req.Timezone
		run("timezone", func(c context.Context) (runner.Result, error) { return env.hostSettings().SetTimezone(c, env, name) })
	}
	rctx, rcancel := context.WithTimeout(context.Background(), showTimeout)
	after, afterErr := env.hostSettings().Status(rctx, env)
	rcancel()
	if afterErr == nil {
		after.Notes = append(after.Notes, hostSettingsReadbackNote)
	}
	outcome, msg := classifyHostSettings(req, steps, after, afterErr)
	out := HostSettingsOutcome{OperationID: opID, Action: hostSettingsAction, Outcome: outcome, Message: msg,
		Fields: hostSettingsFields(req), StartedAt: started, FinishedAt: time.Now().UTC()}
	if afterErr == nil {
		out.Observed = &after
		jo, _ := json.Marshal(struct {
			Fields []string              `json:"fields"`
			After  *HostSettingsObserved `json:"after,omitempty"`
		}{Fields: out.Fields, After: statusOnly(after)})
		rec.Observed = jo
	}
	if outcome != OutcomeSucceeded {
		out.Recovery = []string{
			"No automatic rollback was performed.",
			"On the host, `hostnamectl status` and `timedatectl status` show the current values.",
			"Review the change again here once the host is in a consistent state; partial changes are not repaired automatically.",
		}
	}
	rec.Outcome, rec.Message = outcome, msg
	outcomeWritten = true
	if err := env.Writes.Outcome(rec); err != nil {
		out.Message += " Warning: the outcome could not be written to the operation journal."
	}
	return out, nil
}

func statusOnly(o HostSettingsObserved) *HostSettingsObserved {
	return &HostSettingsObserved{
		StaticStatus: o.StaticStatus, PrettyStatus: o.PrettyStatus, TimezoneStatus: o.TimezoneStatus,
		TransientStatus: o.TransientStatus, Notes: []string{hostSettingsReadbackNote},
	}
}

func classifyHostSettings(req hostSettingsArgs, steps []appliedStep, after HostSettingsObserved, afterErr error) (string, string) {
	var timedOut, execFailed bool
	var failField string
	for _, s := range steps {
		if errors.Is(s.err, runner.ErrTimeout) {
			timedOut = true
			failField = s.field
		} else if s.err != nil {
			execFailed = true
			failField = s.field
			var ex *runner.ExecError
			if errors.As(s.err, &ex) && ex.Class != "" {
				failField += " (" + ex.Class + ")"
			}
		} else if s.res.ExitCode != 0 {
			execFailed = true
			failField = s.field
		}
	}
	if afterErr != nil {
		if timedOut {
			return OutcomeUncertain, "A hostnamectl/timedatectl change did not finish within " + actTimeout.String() + ". The result could not be read back, so it is not verified."
		}
		if execFailed {
			return OutcomeFailed, "A hostnamectl/timedatectl change failed (" + failField + "). The result could not be read back, so it is not verified."
		}
		return OutcomeUncertain, "The change ran, but hostnamectl/timedatectl could not be read back, so the result is not verified."
	}
	ok := 0
	want := 0
	match := func(field, requested, status, value string) {
		want++
		if field == "pretty_hostname" && requested == "" && (status == hostinfo.SettingNotSet || value == "") {
			ok++
			return
		}
		if status == hostinfo.SettingObserved && value == requested {
			ok++
		}
	}
	if req.StaticHostname != nil {
		match("static_hostname", *req.StaticHostname, after.StaticStatus, after.StaticHostname)
	}
	if req.PrettyHostname != nil {
		match("pretty_hostname", *req.PrettyHostname, after.PrettyStatus, after.PrettyHostname)
	}
	if req.Timezone != nil {
		match("timezone", *req.Timezone, after.TimezoneStatus, after.Timezone)
	}
	observed := " Observed from hostnamectl/timedatectl after the change."
	switch {
	case timedOut:
		return OutcomeUncertain, "A hostnamectl/timedatectl change did not finish within " + actTimeout.String() + "." + observed
	case execFailed && ok == 0:
		return OutcomeFailed, "hostnamectl/timedatectl could not apply the change (" + failField + ")." + observed
	case ok == want && !execFailed:
		return OutcomeSucceeded, "hostnamectl/timedatectl report the requested values." + observed + " " + hostSettingsReadbackNote
	case ok == want && execFailed:
		return OutcomeUncertain, "A helper reported failure (" + failField + ") but hostnamectl/timedatectl now match the request." + observed
	default:
		return OutcomeFailed, "The change is only partly applied (" + fmt.Sprintf("%d of %d fields match", ok, want) + ")." + observed
	}
}
