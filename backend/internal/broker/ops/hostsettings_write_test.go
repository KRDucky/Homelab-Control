// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/hostinfo"
)

type fakeHostSettings struct {
	mu        sync.Mutex
	static    string
	pretty    string
	prettySet bool
	tz        string
	zones     []string
	acts      []string
	failSet   string
}

func (f *fakeHostSettings) Status(context.Context, *Env) (HostSettingsObserved, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := HostSettingsObserved{
		StaticHostname: f.static, StaticStatus: hostinfo.SettingObserved,
		Timezone: f.tz, TimezoneStatus: hostinfo.SettingObserved,
		TransientHostname: f.static, TransientStatus: hostinfo.SettingObserved,
	}
	if f.prettySet {
		o.PrettyHostname, o.PrettyStatus = f.pretty, hostinfo.SettingObserved
	} else {
		o.PrettyStatus = hostinfo.SettingNotSet
	}
	return o, nil
}

func (f *fakeHostSettings) ListTimezones(context.Context, *Env) ([]string, error) {
	if f.zones != nil {
		return append([]string{}, f.zones...), nil
	}
	return []string{"Etc/UTC", "Europe/Berlin", "America/New_York"}, nil
}

func (f *fakeHostSettings) SetStatic(_ context.Context, env *Env, name string) (runner.Result, error) {
	if env.RootExec == nil {
		return runner.Result{}, runner.ErrNotAllowed
	}
	if _, err := runner.RootArgv(runner.RootHostnamectlSetStatic, name); err != nil {
		return runner.Result{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acts = append(f.acts, "static")
	if f.failSet == "static" {
		return runner.Result{ExitCode: 1, Stderr: "failed"}, nil
	}
	f.static = name
	return runner.Result{ExitCode: 0}, nil
}

func (f *fakeHostSettings) SetPretty(_ context.Context, env *Env, name string) (runner.Result, error) {
	if env.RootExec == nil {
		return runner.Result{}, runner.ErrNotAllowed
	}
	if _, err := runner.RootArgv(runner.RootHostnamectlSetPretty, name); err != nil {
		return runner.Result{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acts = append(f.acts, "pretty")
	if f.failSet == "pretty" {
		return runner.Result{ExitCode: 1}, nil
	}
	f.pretty, f.prettySet = name, name != ""
	return runner.Result{ExitCode: 0}, nil
}

func (f *fakeHostSettings) SetTimezone(_ context.Context, env *Env, name string) (runner.Result, error) {
	if env.RootExec == nil {
		return runner.Result{}, runner.ErrNotAllowed
	}
	if _, err := runner.RootArgv(runner.RootTimedatectlSetTimezone, name); err != nil {
		return runner.Result{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acts = append(f.acts, "timezone")
	if f.failSet == "timezone" {
		return runner.Result{ExitCode: 1}, nil
	}
	f.tz = name
	return runner.Result{ExitCode: 0}, nil
}

type hsenv struct {
	env     *Env
	fake    *fakeHostSettings
	journal string
}

func newHSEnv(t *testing.T) *hsenv {
	t.Helper()
	jp := filepath.Join(t.TempDir(), "operations.jsonl")
	l, err := audit.Open(jp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	fw, err := writes.New(l, jp)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeHostSettings{static: "fixture-static", pretty: "Fixture Lab", prettySet: true, tz: "Europe/Berlin"}
	env := &Env{Writes: fw, HostSettings: fake, RootExec: new(runner.Runner).RootExec(),
		Actor: proto.Actor{User: "alice", Session: "sess-a", Scopes: []string{"host.settings.apply"}, RequestID: "r1"}}
	return &hsenv{env: env, fake: fake, journal: jp}
}

func ptr(s string) *string { return &s }

func hsArgs(static, pretty, tz *string) json.RawMessage {
	a := hostSettingsArgs{StaticHostname: static, PrettyHostname: pretty, Timezone: tz}
	b, _ := json.Marshal(a)
	return b
}

func hsApplyArgs(static, pretty, tz *string, token string) json.RawMessage {
	a := hostSettingsApplyArgs{StaticHostname: static, PrettyHostname: pretty, Timezone: tz, Token: token}
	b, _ := json.Marshal(a)
	return b
}

func TestParseHostnamectlAndTimedatectlStatus(t *testing.T) {
	var o HostSettingsObserved
	parseHostnamectlStatus([]byte(" Static hostname: nas-01\n Pretty hostname: Lab NAS\n Transient hostname: nas-01\n"), &o)
	if o.StaticHostname != "nas-01" || o.PrettyHostname != "Lab NAS" || o.TransientHostname != "nas-01" {
		t.Fatalf("%+v", o)
	}
	var o2 HostSettingsObserved
	parseHostnamectlStatus([]byte(" Static hostname: n/a\n"), &o2)
	if o2.StaticStatus != hostinfo.SettingNotSet || o2.PrettyStatus != hostinfo.SettingNotSet {
		t.Fatalf("%+v", o2)
	}
	var tz HostSettingsObserved
	parseTimedatectlStatus([]byte("               Local time: Fri 2026-11-06 12:00:00 CET\n                Time zone: Europe/Berlin (CET, +0100)\n"), &tz)
	if tz.Timezone != "Europe/Berlin" || tz.TimezoneStatus != hostinfo.SettingObserved {
		t.Fatalf("%+v", tz)
	}
}

func TestHostSettingsPlanApplyAndReadback(t *testing.T) {
	h := newHSEnv(t)
	raw, err := hostSettingsPlan(context.Background(), h.env, hsArgs(ptr("nas-01"), ptr(""), ptr("Etc/UTC")))
	if err != nil {
		t.Fatal(err)
	}
	plan := raw.(HostSettingsPlan)
	if plan.Risk != RiskReversible || plan.StepUpRequired || !plan.ConfirmationRequired || plan.Token == "" || len(plan.Fields) != 3 {
		t.Fatalf("%+v", plan)
	}
	if !strings.Contains(strings.Join(plan.Warnings, "\n"), "TLS") || !strings.Contains(strings.Join(plan.Warnings, "\n"), "/etc/hosts") {
		t.Fatalf("missing hostname warning: %v", plan.Warnings)
	}
	if !strings.Contains(plan.Effect, "static") || !strings.Contains(plan.Observed.Notes[len(plan.Observed.Notes)-1], "ProtectHostname") {
		t.Fatalf("%s %v", plan.Effect, plan.Observed.Notes)
	}
	if hostSettingsAuditArgs(hsApplyArgs(ptr("nas-01"), ptr(""), ptr("Etc/UTC"), plan.Token)) != "fields=static_hostname,pretty_hostname,timezone" {
		t.Fatal(hostSettingsAuditArgs(hsApplyArgs(ptr("nas-01"), ptr(""), ptr("Etc/UTC"), plan.Token)))
	}
	out, err := hostSettingsApply(context.Background(), h.env, hsApplyArgs(ptr("nas-01"), ptr(""), ptr("Etc/UTC"), plan.Token))
	if err != nil {
		t.Fatal(err)
	}
	got := out.(HostSettingsOutcome)
	if got.Outcome != OutcomeSucceeded || got.Observed == nil || got.Observed.StaticHostname != "nas-01" || got.Observed.PrettyStatus != hostinfo.SettingNotSet || got.Observed.Timezone != "Etc/UTC" {
		t.Fatalf("%+v", got)
	}
	if strings.Join(h.fake.acts, ",") != "static,pretty,timezone" {
		t.Fatalf("acts %v", h.fake.acts)
	}
	j, _ := os.ReadFile(h.journal)
	js := string(j)
	if strings.Contains(js, plan.Token) || strings.Contains(js, "nas-01") || strings.Contains(js, "Etc/UTC") {
		t.Fatalf("journal leaked token or values: %s", js)
	}
	if !strings.Contains(js, `"fields":["static_hostname","pretty_hostname","timezone"]`) {
		t.Fatalf("journal missing field names: %s", js)
	}
}

func TestHostSettingsTokenReplayAndConflict(t *testing.T) {
	h := newHSEnv(t)
	plan, err := hostSettingsPlan(context.Background(), h.env, hsArgs(ptr("nas-01"), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	tok := plan.(HostSettingsPlan).Token
	if _, err := hostSettingsApply(context.Background(), h.env, hsApplyArgs(ptr("nas-01"), nil, nil, tok)); err != nil {
		t.Fatal(err)
	}
	_, err = hostSettingsApply(context.Background(), h.env, hsApplyArgs(ptr("nas-01"), nil, nil, tok))
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeInvalidToken {
		t.Fatalf("replay: %v", err)
	}

	h2 := newHSEnv(t)
	p2, err := hostSettingsPlan(context.Background(), h2.env, hsArgs(ptr("nas-01"), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	h2.fake.mu.Lock()
	h2.fake.static = "other-host"
	h2.fake.mu.Unlock()
	_, err = hostSettingsApply(context.Background(), h2.env, hsApplyArgs(ptr("nas-01"), nil, nil, p2.(HostSettingsPlan).Token))
	if !errors.As(err, &pe) || pe.Code != proto.CodeConflict {
		t.Fatalf("conflict: %v", err)
	}
	if len(h2.fake.acts) != 0 {
		t.Fatalf("conflict executed: %v", h2.fake.acts)
	}
}

func TestHostSettingsRejectsInvalidAndUnknownTimezone(t *testing.T) {
	h := newHSEnv(t)
	for _, raw := range []json.RawMessage{
		hsArgs(ptr("NAS"), nil, nil),
		hsArgs(ptr("nas;reboot"), nil, nil),
		hsArgs(nil, ptr("a\nb"), nil),
		hsArgs(nil, nil, ptr("../../etc/passwd")),
		hsArgs(nil, nil, ptr("Europe/Nowhere")),
		[]byte(`{}`),
		[]byte(`{"path":"/etc/shadow"}`),
	} {
		_, err := hostSettingsPlan(context.Background(), h.env, raw)
		var pe *proto.Error
		if !errors.As(err, &pe) || (pe.Code != proto.CodeBadRequest && pe.Code != proto.CodeUnavailable) {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if len(h.fake.acts) != 0 {
		t.Fatal("invalid plan executed")
	}
}

func TestHostSettingsAuditArgsNeverIncludesValuesOrToken(t *testing.T) {
	s := hostSettingsAuditArgs(hsApplyArgs(ptr("nas-01"), ptr("Ian's NAS"), ptr("Etc/UTC"), "hcp1.aaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	if s != "fields=static_hostname,pretty_hostname,timezone" {
		t.Fatal(s)
	}
	if strings.Contains(s, "nas-01") || strings.Contains(s, "hcp1") || strings.Contains(s, "Ian") {
		t.Fatal(s)
	}
}

func TestHostSettingsApplyRequiresRootHandle(t *testing.T) {
	h := newHSEnv(t)
	plan, err := hostSettingsPlan(context.Background(), h.env, hsArgs(ptr("nas-01"), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	h.env.RootExec = nil
	_, err = hostSettingsApply(context.Background(), h.env, hsApplyArgs(ptr("nas-01"), nil, nil, plan.(HostSettingsPlan).Token))
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeForbidden {
		t.Fatalf("%v", err)
	}
}

func TestHostSettingsUnchangedRefused(t *testing.T) {
	h := newHSEnv(t)
	_, err := hostSettingsPlan(context.Background(), h.env, hsArgs(ptr("fixture-static"), nil, nil))
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeBadRequest || !strings.Contains(pe.Message, "nothing would change") {
		t.Fatalf("%v", err)
	}
}
