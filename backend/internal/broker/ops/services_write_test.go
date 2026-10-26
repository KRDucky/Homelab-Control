// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/servicepolicy"
)

// fakeUnits is an in-memory systemd. Units are looked up by any of their names.
type fakeUnits struct {
	mu     sync.Mutex
	units  map[string]*UnitProps
	shows  atomic.Int32
	acts   []string
	actFn  func(action servicepolicy.Action, p *UnitProps) (runner.Result, error)
	showFn func(p *UnitProps) error
}

func newFake(units ...UnitProps) *fakeUnits {
	f := &fakeUnits{units: map[string]*UnitProps{}}
	for i := range units {
		u := units[i]
		if u.Deps == nil {
			u.Deps = map[string][]string{}
		}
		if u.Actions == nil {
			u.Actions = map[string]string{}
		}
		f.units[u.ID] = &u
		for _, n := range u.Names {
			f.units[n] = &u
		}
	}
	return f
}

func (f *fakeUnits) Show(_ context.Context, _ *Env, unit string) (UnitProps, error) {
	f.shows.Add(1)
	if !servicepolicy.ValidUnit(unit) {
		return UnitProps{}, &proto.Error{Code: proto.CodeBadRequest, Message: "unit name is not valid"}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.units[unit]
	if !ok {
		return UnitProps{ID: unit, LoadState: "not-found", ActiveState: "inactive", SubState: "dead"}, nil
	}
	if f.showFn != nil {
		if err := f.showFn(p); err != nil {
			return UnitProps{}, err
		}
	}
	c := *p
	return c, nil
}

func (f *fakeUnits) ShowMany(_ context.Context, _ *Env, units []string) ([]UnitProps, error) {
	f.shows.Add(1)
	if len(units) == 0 || len(units) > showBatch {
		return nil, errors.New("batch out of bounds")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]UnitProps, 0, len(units))
	for _, n := range units {
		if !inspectable(n) {
			return nil, errors.New("not inspectable: " + n)
		}
		if p, ok := f.units[n]; ok {
			out = append(out, *p)
			continue
		}
		out = append(out, UnitProps{ID: n, LoadState: "not-found", ActiveState: "inactive", SubState: "dead",
			Deps: map[string][]string{}, Actions: map[string]string{}})
	}
	return out, nil
}

func (f *fakeUnits) Act(_ context.Context, env *Env, action servicepolicy.Action, unit string) (runner.Result, error) {
	if env.RootExec == nil {
		return runner.Result{}, runner.ErrNotAllowed
	}
	if _, err := runner.RootArgv(actTemplates[action], unit); err != nil {
		return runner.Result{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acts = append(f.acts, string(action)+" "+unit)
	p := f.units[unit]
	if f.actFn != nil {
		return f.actFn(action, p)
	}
	switch action {
	case servicepolicy.Stop:
		p.ActiveState, p.SubState, p.MainPID = "inactive", "dead", 0
	default:
		p.ActiveState, p.SubState, p.MainPID = "active", "running", 4242
		p.InvocationID = "0123456789abcdef0123456789abcdef"
	}
	return runner.Result{ExitCode: 0}, nil
}

func svc(id string, active string) UnitProps {
	sub := "running"
	if active != "active" {
		sub = "dead"
	}
	return UnitProps{ID: id, Names: []string{id}, LoadState: "loaded", ActiveState: active, SubState: sub,
		Type: "simple", UnitFileState: "enabled", FragmentPath: "/etc/systemd/system/" + id,
		CanStart: true, CanStop: true, InvocationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Deps: map[string][]string{"Conflicts": {"shutdown.target"}}, Actions: map[string]string{"FailureAction": "none"}}
}

type wenv struct {
	env     *Env
	fake    *fakeUnits
	journal string
}

func fastTimings(t *testing.T) {
	t.Helper()
	a, b, c, d := settleMin, settleMax, settlePoll, actTimeout
	settleMin, settleMax, settlePoll = 0, 300*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { settleMin, settleMax, settlePoll, actTimeout = a, b, c, d })
}

func newWriteEnv(t *testing.T, units ...UnitProps) *wenv {
	t.Helper()
	fastTimings(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "run/systemd/system"), 0o755); err != nil {
		t.Fatal(err)
	}
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
	fake := newFake(units...)
	env := &Env{Roots: Roots{Root: root}, Writes: fw, Units: fake, RootExec: new(runner.Runner).RootExec(),
		Actor: proto.Actor{User: "alice", Session: "sess-a", Scopes: []string{"services.operate"}, RequestID: "r1"}}
	return &wenv{env: env, fake: fake, journal: jp}
}

func args(m map[string]string) json.RawMessage { b, _ := json.Marshal(m); return b }

func (w *wenv) plan(t *testing.T, unit, action string) (ServicePlan, error) {
	t.Helper()
	r, err := unitPlan(context.Background(), w.env, args(map[string]string{"unit": unit, "action": action}))
	if err != nil {
		return ServicePlan{}, err
	}
	return r.(ServicePlan), nil
}

func (w *wenv) apply(t *testing.T, env *Env, unit, action, token string) (ServiceOutcome, error) {
	t.Helper()
	r, err := unitApply(context.Background(), env, args(map[string]string{"unit": unit, "action": action, "token": token}))
	if err != nil {
		return ServiceOutcome{}, err
	}
	return r.(ServiceOutcome), nil
}

func withStepUp(e *Env, at time.Time) *Env {
	c := *e
	c.Actor.StepUpAt = at.Unix()
	return &c
}

func code(err error) string {
	var pe *proto.Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	if err == nil {
		return ""
	}
	return "non-proto: " + err.Error()
}

func TestStopFlowWithStepUpAndVerifiedOutcome(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "active"))
	p, err := w.plan(t, "demo.service", "stop")
	if err != nil {
		t.Fatal(err)
	}
	if p.Unit != "demo.service" || p.Risk != RiskDisruptive || !p.StepUpRequired || p.Token == "" ||
		time.Until(p.ExpiresAt) > writes.TokenTTL || p.Observed.ActiveState != "active" {
		t.Fatalf("plan: %+v", p)
	}
	if !strings.Contains(strings.Join(p.Consequences, " "), "no automatic rollback") {
		t.Errorf("plan must state that there is no rollback: %v", p.Consequences)
	}
	// W6: no step-up → refused, nothing executed, token still usable.
	if _, err := w.apply(t, w.env, "demo.service", "stop", p.Token); code(err) != proto.CodeStepUpRequired {
		t.Fatalf("apply without step-up: %v", err)
	}
	if _, err := w.apply(t, withStepUp(w.env, time.Now().Add(-6*time.Minute)), "demo.service", "stop", p.Token); code(err) != proto.CodeStepUpRequired {
		t.Fatalf("apply with stale step-up: %v", err)
	}
	if len(w.fake.acts) != 0 {
		t.Fatal("executed without step-up")
	}
	out, err := w.apply(t, withStepUp(w.env, time.Now()), "demo.service", "stop", p.Token)
	if err != nil {
		t.Fatal(err)
	}
	// W11: the observed state is reported, not just "success".
	if out.Outcome != OutcomeSucceeded || out.Observed == nil || out.Observed.ActiveState != "inactive" ||
		out.ExitCode == nil || *out.ExitCode != 0 || out.OperationID == "" {
		t.Fatalf("outcome: %+v", out)
	}
	if w.env.Writes.Locked(lockDomain("demo.service")) {
		t.Fatal("lock not released")
	}
	// W2: replay.
	if _, err := w.apply(t, withStepUp(w.env, time.Now()), "demo.service", "stop", p.Token); code(err) != proto.CodeInvalidToken {
		t.Fatalf("replay: %v", err)
	}
	if len(w.fake.acts) != 1 {
		t.Fatalf("acts: %v", w.fake.acts)
	}
	// W5: intent and outcome are journaled.
	entries, err := w.env.Writes.Read()
	if err != nil || len(entries) != 1 || entries[0].Final == nil || entries[0].Final.Outcome != OutcomeSucceeded ||
		entries[0].Intent.Actor != "alice" || entries[0].Intent.RequestID != "r1" || entries[0].Intent.TokenID == "" {
		t.Fatalf("journal: %v %+v", err, entries)
	}
	raw, _ := os.ReadFile(w.journal)
	if strings.Contains(string(raw), p.Token) {
		t.Fatal("token written to the journal")
	}
}

func TestStartIsReversibleWithoutStepUp(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "inactive"))
	p, err := w.plan(t, "demo.service", "start")
	if err != nil || p.Risk != RiskReversible || p.StepUpRequired {
		t.Fatalf("plan: %v %+v", err, p)
	}
	out, err := w.apply(t, w.env, "demo.service", "start", p.Token)
	if err != nil || out.Outcome != OutcomeSucceeded || out.Observed.ActiveState != "active" {
		t.Fatalf("apply: %v %+v", err, out)
	}
}

func TestStartConflictingWithServicesIsDisruptive(t *testing.T) {
	u := svc("demo.service", "inactive")
	u.Deps["Conflicts"] = []string{"other.service", "shutdown.target"}
	w := newWriteEnv(t, u, svc("other.service", "active"))
	p, err := w.plan(t, "demo.service", "start")
	if err != nil || p.Risk != RiskDisruptive || !p.StepUpRequired || len(p.AlsoAffected) != 1 || p.AlsoAffected[0] != "other.service" {
		t.Fatalf("plan: %v %+v", err, p)
	}
	// A conflicting unit that is not running is not stopped: the start stays reversible.
	w2 := newWriteEnv(t, u, svc("other.service", "inactive"))
	if p, err := w2.plan(t, "demo.service", "start"); err != nil || p.Risk != RiskReversible || len(p.AlsoAffected) != 0 {
		t.Fatalf("inactive conflict: %v %+v", err, p)
	}
}

func TestRestartIsDisruptive(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "active"))
	p, err := w.plan(t, "demo.service", "restart")
	if err != nil || p.Risk != RiskDisruptive {
		t.Fatalf("plan: %v %+v", err, p)
	}
	out, err := w.apply(t, withStepUp(w.env, time.Now()), "demo.service", "restart", p.Token)
	if err != nil || out.Outcome != OutcomeSucceeded {
		t.Fatalf("apply: %v %+v", err, out)
	}
}

func TestSubstitutionsRejected(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "active"), svc("other.service", "active"))
	p, _ := w.plan(t, "demo.service", "stop")
	now := time.Now()
	cases := map[string]func() (*Env, string, string){
		"target": func() (*Env, string, string) { return withStepUp(w.env, now), "other.service", "stop" },
		"action": func() (*Env, string, string) { return withStepUp(w.env, now), "demo.service", "restart" },
		"actor": func() (*Env, string, string) {
			e := withStepUp(w.env, now)
			e.Actor.User = "mallory"
			return e, "demo.service", "stop"
		},
		"session": func() (*Env, string, string) {
			e := withStepUp(w.env, now)
			e.Actor.Session = "sess-b"
			return e, "demo.service", "stop"
		},
	}
	for name, c := range cases {
		e, unit, action := c()
		if _, err := w.apply(t, e, unit, action, p.Token); code(err) != proto.CodeInvalidToken {
			t.Errorf("%s substitution: %v", name, err)
		}
	}
	if len(w.fake.acts) != 0 {
		t.Fatalf("executed: %v", w.fake.acts)
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "active"))
	now := time.Now()
	w.env.Writes.SetClock(func() time.Time { return now })
	p, _ := w.plan(t, "demo.service", "stop")
	now = now.Add(writes.TokenTTL + time.Second)
	if _, err := w.apply(t, withStepUp(w.env, now), "demo.service", "stop", p.Token); code(err) != proto.CodeInvalidToken {
		t.Fatalf("expired: %v", err)
	}
}

// W3: state changed between plan and apply → conflict, nothing executed.
func TestChangedStateIsConflict(t *testing.T) {
	changes := map[string]func(p *UnitProps){
		"active state":    func(p *UnitProps) { p.ActiveState, p.SubState = "failed", "failed" },
		"invocation":      func(p *UnitProps) { p.InvocationID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" },
		"new alias":       func(p *UnitProps) { p.Names = append(p.Names, "extra.service") },
		"unit file":       func(p *UnitProps) { p.FragmentPath = "/etc/systemd/system/elsewhere.service" },
		"dependency":      func(p *UnitProps) { p.Deps["RequiredBy"] = []string{"app.service"} },
		"refuse stop":     func(p *UnitProps) { p.RefuseManualStop = true },
		"failure action":  func(p *UnitProps) { p.Actions["FailureAction"] = "reboot" },
		"main pid change": func(p *UnitProps) { p.MainPID = 99 },
	}
	for name, change := range changes {
		w := newWriteEnv(t, svc("demo.service", "active"))
		p, err := w.plan(t, "demo.service", "stop")
		if err != nil {
			t.Fatal(err)
		}
		w.fake.mu.Lock()
		change(w.fake.units["demo.service"])
		w.fake.mu.Unlock()
		if _, err := w.apply(t, withStepUp(w.env, time.Now()), "demo.service", "stop", p.Token); code(err) != proto.CodeConflict {
			t.Errorf("%s: %v", name, err)
		}
		if len(w.fake.acts) != 0 {
			t.Errorf("%s: executed", name)
		}
		if entries, _ := w.env.Writes.Read(); len(entries) != 0 {
			t.Errorf("%s: intent written for a refused operation", name)
		}
	}
}

// W10: protected units, by name, alias, template base and unit-file name.
func TestProtectedUnitsRefusedInBroker(t *testing.T) {
	names := []string{
		"ssh.service", "sshd.service", "sshd@0-10.0.0.1:22-10.0.0.2:5555.service", "homelab-control.service",
		"homelab-control-broker.service", "dbus.service", "dbus-broker.service", "polkit.service",
		"systemd-journald.service", "systemd-logind.service", "systemd-networkd.service", "systemd-resolved.service",
		"systemd-udevd.service", "systemd-reboot.service", "systemd-poweroff.service", "getty@tty1.service",
		"serial-getty@ttyS0.service", "autovt@tty2.service", "user@1000.service", "user-runtime-dir@1000.service",
		"NetworkManager.service", "networking.service", "wpa_supplicant.service", "iwd.service",
		"display-manager.service", "gdm.service", "sddm.service", "lightdm.service", "firewalld.service",
		"ufw.service", "nftables.service", "tailscaled.service", "wg-quick@wg0.service", "zfs-zed.service",
		"zfs-mount.service", "auditd.service", "emergency.service", "rescue.service", "SSHD.service",
	}
	for _, n := range names {
		for _, action := range []string{"start", "stop", "restart"} {
			u := svc(n, "active")
			w := newWriteEnv(t, u)
			if _, err := w.plan(t, n, action); code(err) != proto.CodePolicyDenied {
				t.Errorf("%s %s: %v", action, n, err)
			}
		}
	}
}

func TestAliasAndFragmentBypassRefused(t *testing.T) {
	// The requested name is harmless, but systemd resolves it to sshd.
	alias := svc("ssh.service", "active")
	alias.Names = []string{"innocent.service", "ssh.service"}
	// Not an alias, but its unit file is sshd's.
	frag := svc("copy.service", "active")
	frag.FragmentPath = "/usr/lib/systemd/system/sshd.service"
	// An alias in Names only.
	named := svc("web.service", "active")
	named.Names = []string{"web.service", "homelab-control.service"}
	for req, u := range map[string]UnitProps{"innocent.service": alias, "copy.service": frag, "web.service": named} {
		w := newWriteEnv(t, u)
		if _, err := w.plan(t, req, "stop"); code(err) != proto.CodePolicyDenied {
			t.Errorf("%s: %v", req, err)
		}
	}
}

func TestRefuseManualAndCapabilities(t *testing.T) {
	u := svc("demo.service", "active")
	u.RefuseManualStop = true
	w := newWriteEnv(t, u)
	for _, a := range []string{"stop", "restart"} {
		if _, err := w.plan(t, "demo.service", a); code(err) != proto.CodePolicyDenied {
			t.Errorf("%s with RefuseManualStop: %v", a, err)
		}
	}
	if _, err := w.plan(t, "demo.service", "start"); err != nil {
		t.Errorf("start with RefuseManualStop only: %v", err)
	}
	u2 := svc("demo2.service", "inactive")
	u2.RefuseManualStart = true
	u2.CanStop = false
	w2 := newWriteEnv(t, u2)
	for _, a := range []string{"start", "stop", "restart"} {
		if _, err := w2.plan(t, "demo2.service", a); code(err) != proto.CodePolicyDenied {
			t.Errorf("%s: %v", a, err)
		}
	}
}

func TestDependencyPolicy(t *testing.T) {
	cases := []struct {
		name   string
		action string
		mut    func(*UnitProps)
	}{
		{"stop propagates to sshd", "stop", func(p *UnitProps) { p.Deps["RequiredBy"] = []string{"sshd.service"} }},
		{"stop propagates via BindsTo", "stop", func(p *UnitProps) { p.Deps["BoundBy"] = []string{"NetworkManager.service"} }},
		{"restart propagates via PartOf", "restart", func(p *UnitProps) { p.Deps["ConsistsOf"] = []string{"dbus.service"} }},
		{"start stops ssh", "start", func(p *UnitProps) { p.Deps["Conflicts"] = []string{"ssh.service"} }},
		{"start conflicted by firewall", "start", func(p *UnitProps) { p.Deps["ConflictedBy"] = []string{"ufw.service"} }},
		{"start pulls reboot", "start", func(p *UnitProps) { p.Deps["Wants"] = []string{"reboot.target"} }},
		{"start requires poweroff svc", "start", func(p *UnitProps) { p.Deps["Requires"] = []string{"systemd-poweroff.service"} }},
		{"on failure emergency", "start", func(p *UnitProps) { p.Deps["OnFailure"] = []string{"emergency.target"} }},
		{"failure action reboot", "start", func(p *UnitProps) { p.Actions["FailureAction"] = "reboot-force" }},
		{"success action poweroff", "restart", func(p *UnitProps) { p.Actions["SuccessAction"] = "poweroff" }},
		{"start limit action", "start", func(p *UnitProps) { p.Actions["StartLimitAction"] = "reboot" }},
	}
	for _, c := range cases {
		u := svc("demo.service", "inactive")
		c.mut(&u)
		w := newWriteEnv(t, u)
		if _, err := w.plan(t, "demo.service", c.action); code(err) != proto.CodePolicyDenied {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// Ordinary dependents are listed, not refused.
	u := svc("db.service", "active")
	u.Deps["RequiredBy"] = []string{"app.service"}
	w := newWriteEnv(t, u, svc("app.service", "active"))
	p, err := w.plan(t, "db.service", "stop")
	if err != nil || len(p.AlsoAffected) != 1 || p.AlsoAffected[0] != "app.service" {
		t.Fatalf("plan: %v %+v", err, p)
	}
}

func TestLoadStates(t *testing.T) {
	w := newWriteEnv(t)
	if _, err := w.plan(t, "missing.service", "start"); code(err) != proto.CodeNotFound {
		t.Errorf("not-found: %v", err)
	}
	m := svc("masked.service", "inactive")
	m.LoadState = "masked"
	w2 := newWriteEnv(t, m)
	if _, err := w2.plan(t, "masked.service", "start"); code(err) != proto.CodePolicyDenied {
		t.Errorf("masked: %v", err)
	}
	// An alias that resolves to a non-.service unit.
	sock := svc("weird.socket", "active")
	sock.Names = []string{"weird.service", "weird.socket"}
	sock.ID = "weird.socket"
	w3 := newWriteEnv(t, sock)
	if _, err := w3.plan(t, "weird.service", "stop"); code(err) != proto.CodePolicyDenied {
		t.Errorf("non-service target: %v", err)
	}
}

// Argument injection: nothing malformed reaches systemctl (no Show, no Act).
func TestInvalidTargetsAndActions(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "active"))
	bad := []string{
		"", "-x.service", "--now.service", "-.service", "a;b.service", "$(id).service", "`id`.service",
		"a b.service", "a\nb.service", "a\x00.service", "a|b.service", "a&&b.service", ">x.service",
		"foo@.service", "../etc.service", "a/b.service", "x.socket", "x.timer", "x.target", "x.mount", "x",
		"demo", "demo.service ", " demo.service", "demo.service\n", "demo.service;reboot",
		"foo\\x2dbar.service", strings.Repeat("a", 101) + ".service", "--", "--force", "demo.service --now",
	}
	for _, u := range bad {
		if _, err := w.plan(t, u, "stop"); code(err) != proto.CodeBadRequest {
			t.Errorf("plan unit %q: %v", u, err)
		}
		if _, err := w.apply(t, withStepUp(w.env, time.Now()), u, "stop", "hcp1.a.b"); code(err) != proto.CodeBadRequest {
			t.Errorf("apply unit %q: %v", u, err)
		}
	}
	for _, a := range []string{"", "enable", "disable", "mask", "unmask", "reload", "daemon-reload", "kill",
		"isolate", "edit", "reboot", "poweroff", "START", "stop;reboot", "stop --force", "reset-failed"} {
		if _, err := w.plan(t, "demo.service", a); code(err) != proto.CodeBadRequest {
			t.Errorf("action %q: %v", a, err)
		}
	}
	for _, raw := range []string{`{"unit":"demo.service","action":"stop","extra":1}`, `{"unit":["demo.service"],"action":"stop"}`,
		`{"unit":"demo.service","action":"stop"} {}`, `[]`, `"stop"`} {
		if _, err := unitPlan(context.Background(), w.env, json.RawMessage(raw)); code(err) != proto.CodeBadRequest {
			t.Errorf("args %s: %v", raw, err)
		}
	}
	if n := w.fake.shows.Load(); n != 0 {
		t.Fatalf("systemctl show was reached %d times with invalid input", n)
	}
	if len(w.fake.acts) != 0 {
		t.Fatal("act reached")
	}
}

func TestPlanRequiresSessionBinding(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "active"))
	w.env.Actor.Session = ""
	if _, err := w.plan(t, "demo.service", "stop"); code(err) != proto.CodeForbidden {
		t.Fatalf("plan without session: %v", err)
	}
}

// W4: locks.
func TestLockedTargetIsBusy(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "active"))
	p, _ := w.plan(t, "demo.service", "stop")
	rel, _ := w.env.Writes.Lock(lockDomain("demo.service"))
	if _, err := w.plan(t, "demo.service", "stop"); code(err) != proto.CodeBusy {
		t.Errorf("plan on locked target: %v", err)
	}
	if _, err := w.apply(t, withStepUp(w.env, time.Now()), "demo.service", "stop", p.Token); code(err) != proto.CodeBusy {
		t.Errorf("apply on locked target: %v", err)
	}
	rel()
	if len(w.fake.acts) != 0 {
		t.Fatal("executed while locked")
	}
}

func TestConcurrentAppliesExecuteOnce(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "active"))
	p, _ := w.plan(t, "demo.service", "restart")
	env := withStepUp(w.env, time.Now())
	var wg sync.WaitGroup
	var ok, refused atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := w.apply(t, env, "demo.service", "restart", p.Token); err == nil {
				ok.Add(1)
			} else if c := code(err); c == proto.CodeInvalidToken || c == proto.CodeBusy {
				refused.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || refused.Load() != 15 || len(w.fake.acts) != 1 {
		t.Fatalf("ok=%d refused=%d acts=%v", ok.Load(), refused.Load(), w.fake.acts)
	}
}

// W11/W12: failures and uncertain results are reported as observed, with
// recovery guidance and no rollback.
func TestFailureReporting(t *testing.T) {
	cases := []struct {
		name    string
		action  string
		initial string
		actFn   func(servicepolicy.Action, *UnitProps) (runner.Result, error)
		outcome string
		state   string
	}{
		{"start then crash", "start", "inactive", func(_ servicepolicy.Action, p *UnitProps) (runner.Result, error) {
			p.ActiveState, p.SubState, p.Result = "failed", "failed", "exit-code"
			return runner.Result{}, nil
		}, OutcomeFailed, "failed"},
		{"systemctl non-zero", "start", "inactive", func(_ servicepolicy.Action, p *UnitProps) (runner.Result, error) {
			p.ActiveState, p.SubState, p.Result = "failed", "failed", "exit-code"
			return runner.Result{ExitCode: 1, Stderr: "Job for demo.service failed because the control process exited with error code."}, nil
		}, OutcomeFailed, "failed"},
		{"systemctl timeout", "restart", "active", func(_ servicepolicy.Action, p *UnitProps) (runner.Result, error) {
			p.ActiveState, p.SubState = "activating", "start"
			return runner.Result{ExitCode: -1}, runner.ErrTimeout
		}, OutcomeUncertain, "activating"},
		{"still starting", "start", "inactive", func(_ servicepolicy.Action, p *UnitProps) (runner.Result, error) {
			p.ActiveState, p.SubState = "activating", "start-pre"
			return runner.Result{}, nil
		}, OutcomeUncertain, "activating"},
		{"exited immediately", "start", "inactive", func(_ servicepolicy.Action, p *UnitProps) (runner.Result, error) {
			p.ActiveState, p.SubState, p.Result = "inactive", "dead", "success"
			return runner.Result{}, nil
		}, OutcomeUncertain, "inactive"},
		{"stop leaves it running", "stop", "active", func(_ servicepolicy.Action, p *UnitProps) (runner.Result, error) {
			return runner.Result{}, nil
		}, OutcomeFailed, "active"},
		{"stop ends failed", "stop", "active", func(_ servicepolicy.Action, p *UnitProps) (runner.Result, error) {
			p.ActiveState, p.SubState, p.Result = "failed", "failed", "exit-code"
			return runner.Result{}, nil
		}, OutcomeSucceeded, "failed"},
	}
	for _, c := range cases {
		w := newWriteEnv(t, svc("demo.service", c.initial))
		w.fake.actFn = c.actFn
		p, err := w.plan(t, "demo.service", c.action)
		if err != nil {
			t.Fatalf("%s: plan %v", c.name, err)
		}
		out, err := w.apply(t, withStepUp(w.env, time.Now()), "demo.service", c.action, p.Token)
		if err != nil {
			t.Fatalf("%s: apply %v", c.name, err)
		}
		if out.Outcome != c.outcome || out.Observed == nil || out.Observed.ActiveState != c.state {
			t.Errorf("%s: %+v", c.name, out)
		}
		if c.outcome != OutcomeSucceeded {
			if len(out.Recovery) == 0 || !strings.Contains(out.Recovery[0], "No automatic rollback") {
				t.Errorf("%s: recovery guidance missing: %v", c.name, out.Recovery)
			}
		}
		entries, _ := w.env.Writes.Read()
		if len(entries) != 1 || entries[0].Final == nil || entries[0].Final.Outcome != c.outcome {
			t.Errorf("%s: journal %+v", c.name, entries)
		}
	}
}

func TestUnreadableStateAfterActIsUncertain(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "inactive"))
	p, _ := w.plan(t, "demo.service", "start")
	acted := false
	w.fake.actFn = func(servicepolicy.Action, *UnitProps) (runner.Result, error) {
		acted = true
		return runner.Result{}, nil
	}
	w.fake.showFn = func(*UnitProps) error {
		if acted {
			return &proto.Error{Code: proto.CodeTimeout, Message: "x"}
		}
		return nil
	}
	out, err := w.apply(t, w.env, "demo.service", "start", p.Token)
	if err != nil || out.Outcome != OutcomeUncertain || out.Observed != nil || !strings.Contains(out.Message, "not verified") {
		t.Fatalf("%v %+v", err, out)
	}
}

func TestNothingRunsWithoutJournalOrRoot(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "active"))
	p, _ := w.plan(t, "demo.service", "stop")
	// The broker server only sets RootExec for root-exec ops; without it apply refuses.
	noRoot := withStepUp(w.env, time.Now())
	noRoot.RootExec = nil
	if _, err := w.apply(t, noRoot, "demo.service", "stop", p.Token); code(err) != proto.CodeForbidden {
		t.Fatalf("apply without root handle: %v", err)
	}
	// Journal that cannot be written: intent fails, nothing executes.
	fw, _ := writes.New(nil, "")
	w2 := newWriteEnv(t, svc("demo.service", "active"))
	w2.env.Writes = fw
	p2, err := w2.plan(t, "demo.service", "stop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w2.apply(t, withStepUp(w2.env, time.Now()), "demo.service", "stop", p2.Token); code(err) != proto.CodeUnavailable {
		t.Fatalf("apply without journal: %v", err)
	}
	if len(w2.fake.acts) != 0 {
		t.Fatal("executed without a durable intent")
	}
	w2.env.Writes = nil
	if _, err := w2.plan(t, "demo.service", "stop"); code(err) != proto.CodeUnavailable {
		t.Fatalf("plan without framework: %v", err)
	}
}

func TestSystemdNotBooted(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "active"))
	_ = os.RemoveAll(filepath.Join(w.env.Roots.Root, "run/systemd"))
	if _, err := w.plan(t, "demo.service", "stop"); code(err) != proto.CodeUnavailable {
		t.Fatalf("plan without systemd: %v", err)
	}
}

func TestJournalListAndInterrupted(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "inactive"), svc("slow.service", "inactive"))
	p, _ := w.plan(t, "demo.service", "start")
	if _, err := w.apply(t, w.env, "demo.service", "start", p.Token); err != nil {
		t.Fatal(err)
	}
	// Simulate a broker killed after the intent of a second operation.
	if _, err := w.env.Writes.Intent(writes.Record{Op: serviceOpName, Action: "start", Target: "slow.service", Actor: "alice", Risk: RiskReversible}); err != nil {
		t.Fatal(err)
	}
	n, err := w.env.Writes.Recover(InspectUnit(w.env))
	if err != nil || n != 1 {
		t.Fatalf("recover %d %v", n, err)
	}
	r, err := journalList(context.Background(), w.env, nil)
	if err != nil {
		t.Fatal(err)
	}
	ops := r.(OperationList).Operations
	if len(ops) != 2 || ops[0].Unit != "slow.service" || ops[0].Outcome != OutcomeInterrupted ||
		ops[0].Observed == nil || ops[0].Observed.ActiveState != "inactive" ||
		ops[1].Unit != "demo.service" || ops[1].Outcome != OutcomeSucceeded {
		t.Fatalf("list: %+v", ops)
	}
	if _, err := journalList(context.Background(), w.env, json.RawMessage(`{"n":5}`)); code(err) != proto.CodeBadRequest {
		t.Fatal("journal list accepted arguments")
	}
}

func TestAuditArgsNeverContainToken(t *testing.T) {
	s := writeAuditArgs(json.RawMessage(`{"unit":"demo.service","action":"stop","token":"hcp1.SECRETPAYLOAD.SIG"}`))
	if s != "unit=demo.service action=stop" || strings.Contains(s, "hcp1") {
		t.Fatalf("audit args %q", s)
	}
	if writeAuditArgs(json.RawMessage(`{"unit":"a;b","action":"stop"}`)) != "invalid args" {
		t.Fatal("invalid args echoed")
	}
}

// FuzzWriteArgs: whatever passes the broker's validation is also accepted by
// the runner's independent root-template validator, cannot look like a flag
// and contains no shell or control characters.
func FuzzWriteArgs(f *testing.F) {
	for _, s := range []string{"demo.service", "-x.service", "a;b.service", "getty@tty1.service", "foo@.service", "a\x00.service", "--", "sshd.service"} {
		f.Add(s, "stop")
	}
	f.Fuzz(func(t *testing.T, unit, action string) {
		raw, _ := json.Marshal(map[string]string{"unit": unit, "action": action})
		var a planArgs
		if proto.DecodeArgs(raw, &a) != nil || validWriteArgs(a.Unit, a.Action) != nil {
			return
		}
		if strings.HasPrefix(a.Unit, "-") || strings.ContainsAny(a.Unit, " \t\n\r\x00;|&$`'\"<>()\\/*?[]{}!#~") {
			t.Fatalf("accepted %q", a.Unit)
		}
		argv, err := runner.RootArgv(actTemplates[servicepolicy.Action(a.Action)], a.Unit)
		if err != nil {
			t.Fatalf("runner rejects %q/%q accepted by the op: %v", a.Unit, a.Action, err)
		}
		if argv[len(argv)-2] != "--" || argv[len(argv)-1] != a.Unit {
			t.Fatalf("argv %v", argv)
		}
	})
}

// TestTransitiveDependencyPolicy: protection and shutdown checks follow the
// dependency graph, not just direct dependencies (D-5 bypass prevention).
func TestTransitiveDependencyPolicy(t *testing.T) {
	// db <- app (Requires=db) <- sshd (Requires=app): stopping db stops sshd.
	db := svc("db.service", "active")
	db.Deps["RequiredBy"] = []string{"app.service"}
	app := svc("app.service", "active")
	app.Deps["RequiredBy"] = []string{"sshd.service"}
	sshd := svc("sshd.service", "active")
	w := newWriteEnv(t, db, app, sshd)
	for _, a := range []string{"stop", "restart"} {
		if _, err := w.plan(t, "db.service", a); code(err) != proto.CodePolicyDenied {
			t.Errorf("%s two hops from sshd: %v", a, err)
		}
	}
	// A dependent two hops away that reboots the host when it stops.
	app2 := svc("app.service", "active")
	app2.Deps["RequiredBy"] = []string{"leaf.service"}
	leaf := svc("leaf.service", "active")
	leaf.Actions["SuccessAction"] = "reboot"
	w2 := newWriteEnv(t, db, app2, leaf)
	if _, err := w2.plan(t, "db.service", "stop"); code(err) != proto.CodePolicyDenied {
		t.Errorf("transitive SuccessAction: %v", err)
	}
	// start: front (inactive) wants mid (inactive) which wants reboot.target.
	front := svc("front.service", "inactive")
	front.Deps["Wants"] = []string{"mid.service"}
	mid := svc("mid.service", "inactive")
	mid.Deps["Wants"] = []string{"reboot.target"}
	// reboot.target itself has a large graph; the walk must stop there and
	// name it rather than run into the walk bound.
	reboot := UnitProps{ID: "reboot.target", Names: []string{"reboot.target"}, LoadState: "loaded",
		ActiveState: "inactive", SubState: "dead", Deps: map[string][]string{}, Actions: map[string]string{}}
	units := []UnitProps{front, mid}
	for i := 0; i < maxWalk+5; i++ {
		n := fmt.Sprintf("shutdown-dep-%d.service", i)
		reboot.Deps["Requires"] = append(reboot.Deps["Requires"], n)
		units = append(units, svc(n, "inactive"))
	}
	w3 := newWriteEnv(t, append(units, reboot)...)
	if _, err := w3.plan(t, "front.service", "start"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "reboot.target") {
		t.Errorf("transitive reboot.target: %v", err)
	}
	// start: a pulled-in inactive unit conflicts with a protected unit.
	mid2 := svc("mid.service", "inactive")
	mid2.Deps["Conflicts"] = []string{"NetworkManager.service"}
	w4 := newWriteEnv(t, front, mid2)
	if _, err := w4.plan(t, "front.service", "start"); code(err) != proto.CodePolicyDenied {
		t.Errorf("transitive protected conflict: %v", err)
	}
	// Active dependencies are not walked for start: their own deps are already up.
	mid3 := svc("mid.service", "active")
	mid3.Deps["Wants"] = []string{"reboot.target"}
	w5 := newWriteEnv(t, front, mid3)
	p, err := w5.plan(t, "front.service", "start")
	if err != nil || len(p.AlsoStarted) != 0 {
		t.Errorf("active dependency walked: %v %+v", err, p)
	}
	// Inactive pulled-in units are listed.
	w6 := newWriteEnv(t, front, svc("mid.service", "inactive"))
	p, err = w6.plan(t, "front.service", "start")
	if err != nil || len(p.AlsoStarted) != 1 || p.AlsoStarted[0] != "mid.service" {
		t.Errorf("also started: %v %+v", err, p)
	}
}

// An inactive protected dependent is not stopped by systemd, so it does not
// block the stop; a not-found one is treated as running and does.
func TestInactiveProtectedDependent(t *testing.T) {
	db := svc("db.service", "active")
	db.Deps["RequiredBy"] = []string{"sshd.service"}
	w := newWriteEnv(t, db, svc("sshd.service", "inactive"))
	if p, err := w.plan(t, "db.service", "stop"); err != nil || len(p.AlsoAffected) != 0 {
		t.Errorf("inactive protected dependent: %v %+v", err, p)
	}
	w2 := newWriteEnv(t, db) // sshd.service not found
	if _, err := w2.plan(t, "db.service", "stop"); code(err) != proto.CodePolicyDenied {
		t.Errorf("unverifiable protected dependent: %v", err)
	}
}

func TestStopTimeActionsAndTriggers(t *testing.T) {
	for name, mut := range map[string]func(*UnitProps){
		"SuccessAction on stop": func(p *UnitProps) { p.Actions["SuccessAction"] = "poweroff" },
		"FailureAction on stop": func(p *UnitProps) { p.Actions["FailureAction"] = "reboot" },
		"OnSuccess reboot":      func(p *UnitProps) { p.Deps["OnSuccess"] = []string{"reboot.target"} },
		"OnFailure emergency":   func(p *UnitProps) { p.Deps["OnFailure"] = []string{"emergency.target"} },
	} {
		u := svc("demo.service", "active")
		mut(&u)
		w := newWriteEnv(t, u)
		if _, err := w.plan(t, "demo.service", "stop"); code(err) != proto.CodePolicyDenied {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestUninspectableDependents(t *testing.T) {
	// Stop: a dependent whose name cannot be queried is refused.
	u := svc("demo.service", "active")
	u.Deps["RequiredBy"] = []string{`srv-a\x2db.mount`}
	w := newWriteEnv(t, u)
	if _, err := w.plan(t, "demo.service", "stop"); code(err) != proto.CodePolicyDenied {
		t.Errorf("uninspectable dependent: %v", err)
	}
	// Start: uninspectable requirements (e.g. -.mount) are allowed and disclosed.
	s := svc("demo.service", "inactive")
	s.Deps["Requires"] = []string{"-.mount", "system.slice"}
	w2 := newWriteEnv(t, s)
	p, err := w2.plan(t, "demo.service", "start")
	if err != nil {
		t.Fatalf("start with -.mount: %v", err)
	}
	found := false
	for _, c := range p.Consequences {
		found = found || strings.Contains(c, "cannot be inspected")
	}
	if !found {
		t.Errorf("hidden dependency not disclosed: %+v", p.Consequences)
	}
}

func TestWalkIsBounded(t *testing.T) {
	u := svc("hub.service", "active")
	var units []UnitProps
	for i := 0; i < maxWalk+1; i++ {
		n := "dep" + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + strings.Repeat("y", i/26) + ".service"
		u.Deps["RequiredBy"] = append(u.Deps["RequiredBy"], n)
		units = append(units, svc(n, "active"))
	}
	w := newWriteEnv(t, append([]UnitProps{u}, units...)...)
	if _, err := w.plan(t, "hub.service", "stop"); code(err) != proto.CodePolicyDenied {
		t.Errorf("unbounded walk: %v", err)
	}
}

// TestAffectedSetChangeInvalidatesPlan: the plan token covers the list of
// affected units, so a new dependent appearing between plan and apply is a
// conflict, and nothing runs.
func TestAffectedSetChangeInvalidatesPlan(t *testing.T) {
	// db's own properties stay the same; only a dependent's state changes, so
	// the unit digest alone would not notice. The affected-set digest does.
	db := svc("db.service", "active")
	db.Deps["RequiredBy"] = []string{"app.service"}
	w := newWriteEnv(t, db, svc("app.service", "inactive"))
	p, err := w.plan(t, "db.service", "stop")
	if err != nil || len(p.AlsoAffected) != 0 {
		t.Fatalf("plan: %v %+v", err, p)
	}
	w.fake.mu.Lock()
	w.fake.units["app.service"].ActiveState = "active"
	w.fake.units["app.service"].SubState = "running"
	w.fake.mu.Unlock()
	if _, err := w.apply(t, withStepUp(w.env, time.Now()), "db.service", "stop", p.Token); code(err) != proto.CodeConflict {
		t.Errorf("dependent started between plan and apply: %v", err)
	}
	if len(w.fake.acts) != 0 {
		t.Errorf("executed despite conflict: %v", w.fake.acts)
	}
}

func TestParseShowMany(t *testing.T) {
	out := "Id=a.service\nLoadState=loaded\nActiveState=active\n\nId=b.target\nLoadState=not-found\nActiveState=inactive\n"
	ps, err := ParseShowMany([]byte(out), 2)
	if err != nil || len(ps) != 2 || ps[0].ID != "a.service" || ps[1].LoadState != "not-found" {
		t.Fatalf("%v %+v", err, ps)
	}
	if _, err := ParseShowMany([]byte(out), 3); err == nil {
		t.Error("count mismatch accepted")
	}
	if _, err := ParseShow([]byte("Id=a.service\nRefuseManualStop=yes\nRefuseManualStop=no\n")); err == nil {
		t.Error("repeated property accepted")
	}
}

// Findings from the M2-2 security review: second-order effects get the
// matching checks.
func TestSecondOrderEffects(t *testing.T) {
	// Stop fires OnSuccess=: the triggered unit's own start graph is checked.
	db := svc("db.service", "active")
	db.Deps["OnSuccess"] = []string{"notify.service"}
	notify := svc("notify.service", "inactive")
	notify.Deps["Requires"] = []string{"reboot.target"}
	w := newWriteEnv(t, db, notify)
	if _, err := w.plan(t, "db.service", "stop"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "reboot.target") {
		t.Errorf("OnSuccess start graph: %v", err)
	}
	// ... and its Conflicts= with a running protected unit.
	notify2 := svc("notify.service", "inactive")
	notify2.Deps["Conflicts"] = []string{"ssh.socket"}
	w = newWriteEnv(t, db, notify2)
	if _, err := w.plan(t, "db.service", "stop"); code(err) != proto.CodePolicyDenied {
		t.Errorf("OnSuccess conflict with protected unit: %v", err)
	}
	// Isolate job mode on a trigger.
	iso := svc("iso.service", "active")
	iso.Deps["OnFailure"] = []string{"custom.target"}
	iso.OnFailureJobMode = "isolate"
	w = newWriteEnv(t, iso)
	for _, a := range []string{"stop", "start", "restart"} {
		if _, err := w.plan(t, "iso.service", a); code(err) != proto.CodePolicyDenied {
			t.Errorf("%s with OnFailureJobMode=isolate: %v", a, err)
		}
	}
	// Restart: a dependent restarted by propagation gets the start checks.
	base := svc("base.service", "active")
	base.Deps["RequiredBy"] = []string{"app.service"}
	app := svc("app.service", "active")
	app.Actions["StartLimitAction"] = "reboot"
	w = newWriteEnv(t, base, app)
	if _, err := w.plan(t, "base.service", "restart"); code(err) != proto.CodePolicyDenied {
		t.Errorf("restarted dependent with StartLimitAction=reboot: %v", err)
	}
	if _, err := w.plan(t, "base.service", "stop"); err != nil {
		t.Errorf("plain stop should not apply start checks to the dependent: %v", err)
	}
	// Start: a running conflicted unit's own stop propagation is walked.
	front := svc("front.service", "inactive")
	front.Deps["Conflicts"] = []string{"legacy.service"}
	legacy := svc("legacy.service", "active")
	legacy.Deps["RequiredBy"] = []string{"sshd.service"}
	w = newWriteEnv(t, front, legacy, svc("sshd.service", "active"))
	if _, err := w.plan(t, "front.service", "start"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "sshd.service") {
		t.Errorf("conflict stop propagates to sshd: %v", err)
	}
	// Non-.service protected names: Conflicts=ssh.socket, and a protected socket PartOf the unit.
	c := svc("c.service", "inactive")
	c.Deps["Conflicts"] = []string{"ssh.socket"}
	w = newWriteEnv(t, c)
	if _, err := w.plan(t, "c.service", "start"); code(err) != proto.CodePolicyDenied {
		t.Errorf("Conflicts=ssh.socket: %v", err)
	}
	host := svc("host.service", "active")
	host.Deps["ConsistsOf"] = []string{"iscsid.socket"}
	sock := svc("iscsid.socket", "active")
	w = newWriteEnv(t, host, sock)
	if _, err := w.plan(t, "host.service", "stop"); code(err) != proto.CodePolicyDenied {
		t.Errorf("protected socket stopped with the unit: %v", err)
	}
}

// Inside apply (root handle present) the state query never uses the
// unprivileged runner, whose children share hc-api's uid (security review
// finding 1); elsewhere it does.
func TestStateQueryPathSelection(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("root path touched the unprivileged runner: %v", r)
		}
	}()
	rootOnly := &Env{Runner: nil, RootExec: new(runner.Runner).RootExec()}
	if _, err := runShow(context.Background(), rootOnly, "demo.service"); err == nil {
		t.Fatal("empty root runner succeeded")
	}
	unpriv := &Env{Runner: new(runner.Runner)}
	if _, err := runShow(context.Background(), unpriv, "demo.service"); err == nil {
		t.Fatal("empty runner succeeded")
	}
}

// A panic after the intent is journaled still records an outcome (unknown),
// so the operation is not left "pending" until the next broker restart; the
// panic itself propagates to the server, which contains it.
func TestPanicAfterIntentRecordsOutcome(t *testing.T) {
	w := newWriteEnv(t, svc("demo.service", "inactive"))
	w.fake.actFn = func(servicepolicy.Action, *UnitProps) (runner.Result, error) { panic("boom") }
	p, err := w.plan(t, "demo.service", "start")
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic swallowed")
			}
		}()
		_, _ = w.apply(t, w.env, "demo.service", "start", p.Token)
	}()
	entries, err := w.env.Writes.Read()
	if err != nil || len(entries) != 1 || entries[0].Final == nil || entries[0].Final.Outcome != OutcomeUncertain {
		t.Fatalf("journal after panic: %v %+v", err, entries)
	}
	if w.env.Writes.Locked(lockDomain("demo.service")) {
		t.Fatal("lock held after panic")
	}
}

// Re-review items: StopWhenUnneeded= dependencies, uninspectable services,
// template instances of VPN rules.
func TestReReviewItems(t *testing.T) {
	x := svc("x.service", "active")
	x.Deps["Requires"] = []string{"sshd.service"}
	y := svc("sshd.service", "active")
	y.StopWhenUnneeded = true
	w := newWriteEnv(t, x, y)
	if _, err := w.plan(t, "x.service", "stop"); code(err) != proto.CodePolicyDenied {
		t.Errorf("StopWhenUnneeded protected dependency: %v", err)
	}
	y2 := svc("helper.service", "active")
	y2.StopWhenUnneeded = true
	x2 := svc("x.service", "active")
	x2.Deps["Requires"] = []string{"helper.service"}
	w = newWriteEnv(t, x2, y2)
	if p, err := w.plan(t, "x.service", "stop"); err != nil || len(p.AlsoAffected) != 1 || p.AlsoAffected[0] != "helper.service" {
		t.Errorf("StopWhenUnneeded listed: %v %+v", err, p)
	}
	s := svc("s.service", "inactive")
	s.Deps["Wants"] = []string{`foo\x2dbar.service`}
	w = newWriteEnv(t, s)
	if _, err := w.plan(t, "s.service", "start"); code(err) != proto.CodePolicyDenied {
		t.Errorf("uninspectable service dependency: %v", err)
	}
	for _, n := range []string{"nebula@home.service", "ipsec@x.service"} {
		if !servicepolicy.NameProtection(n).Protected {
			t.Errorf("%s not protected", n)
		}
	}
}
