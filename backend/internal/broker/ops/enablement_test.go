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
	"time"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/servicepolicy"
)

// fakeFiles is an in-memory unit-file view backed by real files and links in
// the fixture root, so the broker's own [Install] reader and link checks run
// against a filesystem.
type fakeFiles struct {
	mu      sync.Mutex
	root    string
	props   map[string]*InstallProps
	state   map[string]string
	changes []string
	// changeFn replaces the default (systemctl-like) behaviour.
	changeFn func(action, unit string) (runner.Result, error)
	isErr    error
}

func (f *fakeFiles) ShowInstall(_ context.Context, _ *Env, unit string) (InstallProps, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.props[unit]
	if !ok {
		return InstallProps{ID: unit, LoadState: "not-found"}, nil
	}
	return *p, nil
}

func (f *fakeFiles) IsEnabled(_ context.Context, _ *Env, unit string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isErr != nil {
		return "", f.isErr
	}
	if s, ok := f.state[unit]; ok {
		return s, nil
	}
	return "not-found", nil
}

// links as the review computes them, from the fixture's [Install] files.
func (f *fakeFiles) links(unit string) []string {
	p := f.props[unit]
	info, err := ReadInstall(Roots{Root: f.root}, p.FragmentPath, p.DropInPaths)
	if err != nil {
		return nil
	}
	var out []string
	for _, t := range info.WantedBy {
		out = append(out, systemdUnitDir+t+".wants/"+unit)
	}
	for _, a := range info.Alias {
		out = append(out, systemdUnitDir+a)
	}
	return out
}

func (f *fakeFiles) setLinks(unit string, present bool, only int) {
	for i, l := range f.links(unit) {
		if only >= 0 && i >= only {
			break
		}
		path := filepath.Join(f.root, l)
		if present {
			_ = os.MkdirAll(filepath.Dir(path), 0o755)
			_ = os.Symlink(f.props[unit].FragmentPath, path)
		} else {
			_ = os.Remove(path)
		}
	}
}

func (f *fakeFiles) Change(_ context.Context, env *Env, action, unit string) (runner.Result, error) {
	if env.RootExec == nil {
		return runner.Result{}, runner.ErrNotAllowed
	}
	if _, err := runner.RootArgv(changeTemplates[action], unit); err != nil {
		return runner.Result{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, action+" "+unit)
	if f.changeFn != nil {
		return f.changeFn(action, unit)
	}
	f.setLinks(unit, action == ActionEnable, -1)
	f.state[unit] = map[string]string{ActionEnable: "enabled", ActionDisable: "disabled"}[action]
	return runner.Result{Stdout: []byte("Created symlink '/etc/systemd/system/multi-user.target.wants/" + unit + "' → '/etc/systemd/system/" + unit + "'.\n")}, nil
}

type eenv struct {
	*wenv
	files *fakeFiles
	tt    *testing.T
}

// unitFile writes a fixture unit file under the fixture root.
func writeUnit(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const wantedByMulti = "[Unit]\nDescription=fixture\n[Service]\nExecStart=/bin/true\nEnvironment=SECRET=fixture-only\n[Install]\nWantedBy=multi-user.target\n"

// newEnablementEnv builds a broker env with one disabled unit demo.service.
func newEnablementEnv(t *testing.T, units ...UnitProps) *eenv {
	t.Helper()
	if len(units) == 0 {
		units = []UnitProps{svc("demo.service", "inactive")}
	}
	w := newWriteEnv(t, units...)
	w.env.Actor.Scopes = []string{"services.configure"}
	f := &fakeFiles{root: w.env.Roots.Root, props: map[string]*InstallProps{}, state: map[string]string{}}
	for _, u := range units {
		frag := u.FragmentPath
		f.props[u.ID] = &InstallProps{ID: u.ID, Names: u.Names, LoadState: u.LoadState, FragmentPath: frag}
		f.state[u.ID] = "disabled"
		writeUnit(t, f.root, frag, wantedByMulti)
	}
	w.env.Enablement = f
	return &eenv{wenv: w, files: f, tt: t}
}

func (e *eenv) eplan(t *testing.T, unit, action string) (EnablementPlan, error) {
	t.Helper()
	r, err := unitEnablementPlan(context.Background(), e.env, args(map[string]string{"unit": unit, "action": action}))
	if err != nil {
		return EnablementPlan{}, err
	}
	return r.(EnablementPlan), nil
}

func (e *eenv) eapply(t *testing.T, env *Env, unit, action, token string) (EnablementOutcome, error) {
	t.Helper()
	r, err := unitEnablementApply(context.Background(), env, args(map[string]string{"unit": unit, "action": action, "token": token}))
	if err != nil {
		return EnablementOutcome{}, err
	}
	return r.(EnablementOutcome), nil
}

func journalKinds(t *testing.T, path string) []string {
	t.Helper()
	b, _ := os.ReadFile(path)
	var kinds []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var ev struct {
			Event string `json:"event"`
		}
		if json.Unmarshal([]byte(l), &ev) == nil && strings.HasPrefix(ev.Event, "op.") {
			kinds = append(kinds, ev.Event)
		}
	}
	return kinds
}

func TestEnableFlowStepUpAndIndependentReadBack(t *testing.T) {
	e := newEnablementEnv(t)
	p, err := e.eplan(t, "demo.service", "enable")
	if err != nil {
		t.Fatal(err)
	}
	if p.Unit != "demo.service" || p.Risk != RiskDisruptive || !p.StepUpRequired || p.BootState != "disabled" ||
		len(p.Links) != 1 || p.Links[0] != "/etc/systemd/system/multi-user.target.wants/demo.service" {
		t.Fatalf("plan: %+v", p)
	}
	all := strings.Join(p.Consequences, " ")
	for _, want := range []string{"next boot", "does not start it now", "not reloaded", "no automatic rollback"} {
		if !strings.Contains(all, want) {
			t.Errorf("consequences lack %q: %v", want, p.Consequences)
		}
	}
	// No step-up, stale step-up: refused, nothing changed, token still usable.
	if _, err := e.eapply(t, e.env, "demo.service", "enable", p.Token); code(err) != proto.CodeStepUpRequired {
		t.Fatalf("no step-up: %v", err)
	}
	if _, err := e.eapply(t, withStepUp(e.env, time.Now().Add(-6*time.Minute)), "demo.service", "enable", p.Token); code(err) != proto.CodeStepUpRequired {
		t.Fatalf("stale step-up: %v", err)
	}
	if len(e.files.changes) != 0 {
		t.Fatal("changed without step-up")
	}
	out, err := e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token)
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != OutcomeSucceeded || out.BootState != "enabled" || out.BootStateBefore != "disabled" ||
		len(out.Links) != 1 || !out.Links[0].Present || len(out.Reported) != 1 || out.OperationID == "" {
		t.Fatalf("outcome: %+v", out)
	}
	if len(e.fake.acts) != 0 {
		t.Fatal("enablement must never start, stop or restart the unit")
	}
	if k := journalKinds(t, e.journal); strings.Join(k, ",") != "op.intent,op.outcome" {
		t.Fatalf("journal: %v", k)
	}
	b, _ := os.ReadFile(e.journal)
	if strings.Contains(string(b), "hcp1.") || strings.Contains(string(b), "fixture-only") {
		t.Fatal("journal contains a token or unit-file content")
	}
	// Replay: the token is single use.
	if _, err := e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token); code(err) != proto.CodeInvalidToken {
		t.Fatalf("replay: %v", err)
	}
	// Now disable it again: the opposite action is reviewed separately.
	d, err := e.eplan(t, "demo.service", "disable")
	if err != nil {
		t.Fatal(err)
	}
	out, err = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "disable", d.Token)
	if err != nil || out.Outcome != OutcomeSucceeded || out.BootState != "disabled" || out.Links[0].Present {
		t.Fatalf("disable: %+v %v", out, err)
	}
	if !strings.Contains(strings.Join(d.Consequences, " "), "does not stop it now") {
		t.Errorf("disable consequences: %v", d.Consequences)
	}
}

func TestEnablementTokensNeverCrossOperations(t *testing.T) {
	e := newEnablementEnv(t)
	e.env.Actor.Scopes = []string{"services.configure", "services.operate"}
	ep, err := e.eplan(t, "demo.service", "enable")
	if err != nil {
		t.Fatal(err)
	}
	sp, err := e.plan(t, "demo.service", "start")
	if err != nil {
		t.Fatal(err)
	}
	now := withStepUp(e.env, time.Now())
	// An enablement token cannot start the unit; a start token cannot enable it.
	if _, err := e.apply(t, now, "demo.service", "start", ep.Token); code(err) != proto.CodeInvalidToken {
		t.Fatalf("enablement token on start: %v", err)
	}
	if _, err := e.eapply(t, now, "demo.service", "enable", sp.Token); code(err) != proto.CodeInvalidToken {
		t.Fatalf("start token on enablement: %v", err)
	}
	// Action, target, actor and session substitutions.
	ep2, _ := e.eplan(t, "demo.service", "enable")
	if _, err := e.eapply(t, now, "demo.service", "disable", ep2.Token); code(err) != proto.CodeInvalidToken {
		t.Fatalf("action substitution: %v", err)
	}
	other := *now
	other.Actor.User = "mallory"
	if _, err := e.eapply(t, &other, "demo.service", "enable", ep2.Token); code(err) != proto.CodeInvalidToken {
		t.Fatalf("actor substitution: %v", err)
	}
	other = *now
	other.Actor.Session = "sess-b"
	if _, err := e.eapply(t, &other, "demo.service", "enable", ep2.Token); code(err) != proto.CodeInvalidToken {
		t.Fatalf("session substitution: %v", err)
	}
	if len(e.files.changes)+len(e.fake.acts) != 0 {
		t.Fatalf("something ran: %v %v", e.files.changes, e.fake.acts)
	}
}

func TestEnablementTokenExpiry(t *testing.T) {
	e := newEnablementEnv(t)
	p, _ := e.eplan(t, "demo.service", "enable")
	e.env.Writes.SetClock(func() time.Time { return time.Now().Add(writes.TokenTTL + time.Second) })
	if _, err := e.eapply(t, withStepUp(e.env, time.Now().Add(writes.TokenTTL)), "demo.service", "enable", p.Token); code(err) != proto.CodeInvalidToken {
		t.Fatalf("expired: %v", err)
	}
}

func TestEnablementStalePlansConflict(t *testing.T) {
	cases := map[string]func(e *eenv){
		"is-enabled changed": func(e *eenv) { e.files.state["demo.service"] = "enabled" },
		"[Install] gained an alias": func(e *eenv) {
			writeUnit(e.t(), e.files.root, "/etc/systemd/system/demo.service", wantedByMulti+"Alias=demo-alias.service\n")
		},
		"drop-in added": func(e *eenv) {
			writeUnit(e.t(), e.files.root, "/etc/systemd/system/demo.service.d/x.conf", "[Service]\nNice=1\n")
			e.files.props["demo.service"].DropInPaths = []string{"/etc/systemd/system/demo.service.d/x.conf"}
		},
		"run state changed": func(e *eenv) { e.fake.units["demo.service"].ActiveState = "active" },
		"fragment moved": func(e *eenv) {
			writeUnit(e.t(), e.files.root, "/usr/lib/systemd/system/demo.service", wantedByMulti)
			e.files.props["demo.service"].FragmentPath = "/usr/lib/systemd/system/demo.service"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnablementEnv(t)
			e.tt = t
			p, err := e.eplan(t, "demo.service", "enable")
			if err != nil {
				t.Fatal(err)
			}
			mutate(e)
			_, err = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token)
			if c := code(err); c != proto.CodeConflict && c != proto.CodePolicyDenied {
				t.Fatalf("stale plan: %v", err)
			}
			if len(e.files.changes) != 0 {
				t.Fatal("executed a stale plan")
			}
			if k := journalKinds(t, e.journal); len(k) != 0 {
				t.Fatalf("journal written for a refused change: %v", k)
			}
		})
	}
}

func TestEnablementRefusals(t *testing.T) {
	type tc struct {
		unit    string
		action  string
		file    string // [Install]-bearing content for the fragment ("" = default)
		mutate  func(e *eenv)
		code    string
		message string
	}
	sshAlias := svc("innocent.service", "inactive")
	sshAlias.Names = []string{"innocent.service", "sshd.service"}
	conflicting := svc("demo.service", "inactive")
	conflicting.Deps["Conflicts"] = []string{"ssh.service"}
	rebooting := svc("demo.service", "inactive")
	rebooting.Actions["FailureAction"] = "reboot"
	masked := svc("demo.service", "inactive")
	masked.LoadState = "masked"
	cases := map[string]tc{
		"protected by name":             {unit: "ssh.service", code: proto.CodePolicyDenied, message: "protected"},
		"broker unit":                   {unit: "homelab-control-broker.service", code: proto.CodePolicyDenied, message: "protected"},
		"alias resolves to protected":   {unit: "innocent.service", code: proto.CodePolicyDenied, message: "protected"},
		"install alias protected":       {file: "[Install]\nWantedBy=multi-user.target\nAlias=sshd.service\n", code: proto.CodePolicyDenied, message: "alias sshd.service"},
		"wanted by protected unit":      {file: "[Install]\nWantedBy=ssh.service\n", code: proto.CodePolicyDenied, message: "wanted by ssh.service"},
		"wanted by reboot":              {file: "[Install]\nWantedBy=reboot.target\n", code: proto.CodePolicyDenied, message: "reboot"},
		"Also=":                         {file: "[Install]\nWantedBy=multi-user.target\nAlso=other.service\n", code: proto.CodePolicyDenied, message: "Also="},
		"RequiredBy=":                   {file: "[Install]\nRequiredBy=multi-user.target\n", code: proto.CodePolicyDenied, message: "RequiredBy"},
		"UpheldBy=":                     {file: "[Install]\nUpheldBy=multi-user.target\n", code: proto.CodePolicyDenied, message: "UpheldBy"},
		"DefaultInstance=":              {file: "[Install]\nWantedBy=multi-user.target\nDefaultInstance=x\n", code: proto.CodePolicyDenied, message: "DefaultInstance"},
		"specifier":                     {file: "[Install]\nWantedBy=multi-user.target\nAlias=%N-x.service\n", code: proto.CodePolicyDenied, message: "specifiers"},
		"reset assignment":              {file: "[Install]\nWantedBy=\n", code: proto.CodePolicyDenied, message: "empty assignment"},
		"line continuation":             {file: "[Install]\nWantedBy=multi-user.target \\\n  graphical.target\n", code: proto.CodePolicyDenied, message: "continuation"},
		"unknown install key":           {file: "[Install]\nWantedBy=multi-user.target\nFooBar=1\n", code: proto.CodePolicyDenied, message: "not supported"},
		"no install targets":            {file: "[Service]\nExecStart=/bin/true\n", code: proto.CodePolicyDenied, message: "nothing to enable"},
		"template instance":             {unit: "getty@tty1.service", code: proto.CodeBadRequest},
		"template":                      {unit: "getty@.service", code: proto.CodeBadRequest},
		"socket unit":                   {unit: "demo.socket", code: proto.CodeBadRequest},
		"injection":                     {unit: "demo.service;reboot", code: proto.CodeBadRequest},
		"leading dash":                  {unit: "-demo.service", code: proto.CodeBadRequest},
		"mask is not an action":         {action: "mask", code: proto.CodeBadRequest},
		"enable --now is not an action": {action: "enable --now", code: proto.CodeBadRequest},
		"start is not an enablement":    {action: "start", code: proto.CodeBadRequest},
		"already enabled":               {mutate: func(e *eenv) { e.files.state["demo.service"] = "enabled" }, code: proto.CodePolicyDenied, message: "already enabled"},
		"already disabled":              {action: "disable", code: proto.CodePolicyDenied, message: "already disabled"},
		"static":                        {mutate: func(e *eenv) { e.files.state["demo.service"] = "static" }, code: proto.CodePolicyDenied, message: "no [Install] section"},
		"enabled-runtime":               {action: "disable", mutate: func(e *eenv) { e.files.state["demo.service"] = "enabled-runtime" }, code: proto.CodePolicyDenied, message: "runtime"},
		"linked":                        {mutate: func(e *eenv) { e.files.state["demo.service"] = "linked" }, code: proto.CodePolicyDenied, message: "linked"},
		"generated":                     {mutate: func(e *eenv) { e.files.state["demo.service"] = "generated" }, code: proto.CodePolicyDenied, message: "generated"},
		"indirect":                      {mutate: func(e *eenv) { e.files.state["demo.service"] = "indirect" }, code: proto.CodePolicyDenied, message: "indirectly"},
		"SysV source":                   {mutate: func(e *eenv) { e.files.props["demo.service"].SourcePath = "/etc/init.d/demo" }, code: proto.CodePolicyDenied, message: "init script"},
		"transient":                     {mutate: func(e *eenv) { e.files.props["demo.service"].Transient = true }, code: proto.CodePolicyDenied, message: "transient"},
		"generator directory": {mutate: func(e *eenv) {
			writeUnit(e.t(), e.files.root, "/run/systemd/generator/demo.service", wantedByMulti)
			e.files.props["demo.service"].FragmentPath = "/run/systemd/generator/demo.service"
		}, code: proto.CodePolicyDenied, message: "outside the unit directories"},
		"symlinked directory": {mutate: func(e *eenv) {
			writeUnit(e.t(), e.files.root, "/srv/elsewhere/demo.service", wantedByMulti)
			_ = os.MkdirAll(filepath.Join(e.files.root, "/usr/local/lib/systemd"), 0o755)
			_ = os.Symlink(filepath.Join(e.files.root, "/srv/elsewhere"), filepath.Join(e.files.root, "/usr/local/lib/systemd/system"))
			e.files.props["demo.service"].FragmentPath = "/usr/local/lib/systemd/system/demo.service"
		}, code: proto.CodePolicyDenied, message: "symbolic link"},
		"inconsistent links": {mutate: func(e *eenv) { e.files.setLinks("demo.service", true, -1) }, code: proto.CodePolicyDenied, message: "inconsistent"},
		"drop-in install alias protected": {mutate: func(e *eenv) {
			writeUnit(e.t(), e.files.root, "/etc/systemd/system/demo.service.d/alias.conf", "[Install]\nAlias=dbus-fake.service\n")
			e.files.props["demo.service"].DropInPaths = []string{"/etc/systemd/system/demo.service.d/alias.conf"}
		}, code: proto.CodePolicyDenied, message: "protected"},
		"drop-in outside dirs": {mutate: func(e *eenv) {
			e.files.props["demo.service"].DropInPaths = []string{"/etc/shadow.conf"}
		}, code: proto.CodePolicyDenied},
		"masked":            {mutate: func(e *eenv) { *e.fake.units["demo.service"] = masked }, code: proto.CodePolicyDenied, message: "masked"},
		"not found":         {unit: "nothing.service", code: proto.CodeNotFound},
		"boot conflict":     {mutate: func(e *eenv) { *e.fake.units["demo.service"] = conflicting }, code: proto.CodePolicyDenied, message: "ssh.service"},
		"reboot on failure": {mutate: func(e *eenv) { *e.fake.units["demo.service"] = rebooting }, code: proto.CodePolicyDenied, message: "reboot"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnablementEnv(t, svc("demo.service", "inactive"), sshAlias)
			e.tt = t
			if c.file != "" {
				writeUnit(t, e.files.root, "/etc/systemd/system/demo.service", c.file)
			}
			if c.mutate != nil {
				c.mutate(e)
			}
			if c.action == "" {
				c.action = "enable"
			}
			if c.unit == "" {
				c.unit = "demo.service"
			}
			_, err := e.eplan(t, c.unit, c.action)
			if code(err) != c.code {
				t.Fatalf("got %v, want %s", err, c.code)
			}
			if c.message != "" && !strings.Contains(err.Error(), c.message) {
				t.Errorf("message %q lacks %q", err.Error(), c.message)
			}
			if e.env.Writes.Outstanding() != 0 {
				t.Fatal("a token was issued for a refused change")
			}
		})
	}
}

// t returns the current subtest for mutate helpers.
func (e *eenv) t() *testing.T { return e.tt }

func TestMergedUsrLibIsAccepted(t *testing.T) {
	root := t.TempDir()
	writeUnit(t, root, "/usr/lib/systemd/system/demo.service", wantedByMulti)
	if err := os.Symlink("usr/lib", filepath.Join(root, "lib")); err != nil {
		t.Fatal(err)
	}
	info, err := ReadInstall(Roots{Root: root}, "/lib/systemd/system/demo.service", nil)
	if err != nil || len(info.WantedBy) != 1 {
		t.Fatalf("merged /lib: %+v %v", info, err)
	}
	// A /lib link elsewhere is a symlinked component.
	root2 := t.TempDir()
	writeUnit(t, root2, "/srv/x/systemd/system/demo.service", wantedByMulti)
	_ = os.Symlink("srv/x", filepath.Join(root2, "lib"))
	if _, err := ReadInstall(Roots{Root: root2}, "/lib/systemd/system/demo.service", nil); !IsUnsupported(err) {
		t.Fatalf("foreign /lib link: %v", err)
	}
}

func TestReadInstallPathSafety(t *testing.T) {
	root := t.TempDir()
	writeUnit(t, root, "/etc/systemd/system/demo.service", wantedByMulti)
	writeUnit(t, root, "/etc/systemd/system/big.service", "[Install]\nWantedBy=multi-user.target\n"+strings.Repeat("#", maxUnitFileBytes))
	if err := os.Symlink(filepath.Join(root, "etc/systemd/system/demo.service"), filepath.Join(root, "etc/systemd/system/link.service")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "etc/systemd/system/demo.service", "/etc/systemd/system/../systemd/system/demo.service",
		"/etc/systemd/system/demo.conf", "/etc/shadow", "/home/x/demo.service", "/etc/systemd/system/link.service",
		"/etc/systemd/system/big.service", "/etc/systemd/system/missing.service", "/etc/systemd/system//demo.service"} {
		if _, err := ReadInstall(Roots{Root: root}, bad, nil); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	many := make([]string, maxDropIns+1)
	for i := range many {
		many[i] = "/etc/systemd/system/demo.service.d/x.conf"
	}
	if _, err := ReadInstall(Roots{Root: root}, "/etc/systemd/system/demo.service", many); !IsUnsupported(err) {
		t.Errorf("too many drop-ins: %v", err)
	}
}

func TestParseInstall(t *testing.T) {
	var i InstallInfo
	err := ParseInstall([]byte("[Unit]\nWantedBy=ignored.target\n[Install]\n# comment\n; comment\nWantedBy = multi-user.target graphical.target\nAlias=a.service\n[Service]\nAlias=ignored\n"), &i)
	if err != nil || strings.Join(i.WantedBy, ",") != "multi-user.target,graphical.target" || strings.Join(i.Alias, ",") != "a.service" {
		t.Fatalf("%+v %v", i, err)
	}
	for _, bad := range []string{"[Install]\nWantedBy=a b;c\n", "[Install]\nAlias=\"x.service\"\n", "[Install]\nWantedBy=-.mount\n",
		"[Install\n", "[Install]\nnot an assignment\n", "[Install]\nWantedBy=a\\x2db.target\n"} {
		var j InstallInfo
		if err := ParseInstall([]byte(bad), &j); !IsUnsupported(err) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestEnablementLocks(t *testing.T) {
	e := newEnablementEnv(t)
	p, _ := e.eplan(t, "demo.service", "enable")
	unlock, _ := e.env.Writes.Lock(unitFilesLock)
	if _, err := e.eplan(t, "demo.service", "enable"); code(err) != proto.CodeBusy {
		t.Fatalf("plan with unit-files locked: %v", err)
	}
	if _, err := e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token); code(err) != proto.CodeBusy {
		t.Fatalf("apply with unit-files locked: %v", err)
	}
	unlock()
	// The service lock is shared with start/stop/restart.
	p, _ = e.eplan(t, "demo.service", "enable")
	unlock, _ = e.env.Writes.Lock(lockDomain("demo.service"))
	if _, err := e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token); code(err) != proto.CodeBusy {
		t.Fatalf("apply with service locked: %v", err)
	}
	unlock()
	if len(e.files.changes) != 0 {
		t.Fatal("ran while locked")
	}
	// Locks are released after an apply (also on refusal): a new review works.
	if _, err := e.eplan(t, "demo.service", "enable"); err != nil {
		t.Fatalf("locks leaked: %v", err)
	}
}

func TestEnablementConcurrentAppliesExecuteOnce(t *testing.T) {
	e := newEnablementEnv(t)
	p, _ := e.eplan(t, "demo.service", "enable")
	var wg sync.WaitGroup
	results := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token)
			results <- code(err)
		}()
	}
	wg.Wait()
	close(results)
	ok := 0
	for c := range results {
		if c == "" {
			ok++
		} else if c != proto.CodeInvalidToken && c != proto.CodeBusy {
			t.Errorf("unexpected %s", c)
		}
	}
	if ok != 1 || len(e.files.changes) != 1 {
		t.Fatalf("ok=%d changes=%v", ok, e.files.changes)
	}
}

func TestEnablementHonestOutcomes(t *testing.T) {
	two := "[Install]\nWantedBy=multi-user.target\nAlias=demo-alias.service\n"
	cases := map[string]struct {
		fn      func(f *fakeFiles) func(action, unit string) (runner.Result, error)
		isErr   bool
		outcome string
		message string
	}{
		"partial with error": {fn: func(f *fakeFiles) func(string, string) (runner.Result, error) {
			return func(a, u string) (runner.Result, error) {
				f.setLinks(u, true, 1)
				return runner.Result{ExitCode: 1, Stderr: "Failed to enable unit: File exists"}, nil
			}
		}, outcome: OutcomeFailed, message: "partly applied"},
		"nothing changed": {fn: func(f *fakeFiles) func(string, string) (runner.Result, error) {
			return func(a, u string) (runner.Result, error) {
				return runner.Result{ExitCode: 1, Stderr: "Access denied"}, nil
			}
		}, outcome: OutcomeFailed, message: "Nothing appears to have changed"},
		"success claimed, not observed": {fn: func(f *fakeFiles) func(string, string) (runner.Result, error) {
			return func(a, u string) (runner.Result, error) { return runner.Result{}, nil }
		}, outcome: OutcomeFailed, message: "no change is observed"},
		"timeout": {fn: func(f *fakeFiles) func(string, string) (runner.Result, error) {
			return func(a, u string) (runner.Result, error) { return runner.Result{}, runner.ErrTimeout }
		}, outcome: OutcomeUncertain, message: "did not finish"},
		"read-back fails": {isErr: true, outcome: OutcomeUncertain, message: "not verified"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnablementEnv(t)
			writeUnit(t, e.files.root, "/etc/systemd/system/demo.service", two)
			p, err := e.eplan(t, "demo.service", "enable")
			if err != nil || len(p.Links) != 2 {
				t.Fatalf("plan: %+v %v", p, err)
			}
			if c.fn != nil {
				e.files.changeFn = c.fn(e.files)
			} else {
				e.files.changeFn = func(a, u string) (runner.Result, error) {
					e.files.setLinks(u, true, -1)
					e.files.isErr = errors.New("is-enabled failed")
					return runner.Result{}, nil
				}
			}
			out, err := e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token)
			if err != nil {
				t.Fatal(err)
			}
			if out.Outcome != c.outcome || !strings.Contains(out.Message, c.message) || len(out.Recovery) == 0 {
				t.Fatalf("outcome: %+v", out)
			}
			if k := journalKinds(t, e.journal); strings.Join(k, ",") != "op.intent,op.outcome" {
				t.Fatalf("journal: %v", k)
			}
		})
	}
}

func TestEnablementNeedsRootJournalAndSession(t *testing.T) {
	e := newEnablementEnv(t)
	p, _ := e.eplan(t, "demo.service", "enable")
	noRoot := withStepUp(e.env, time.Now())
	noRoot.RootExec = nil
	if _, err := e.eapply(t, noRoot, "demo.service", "enable", p.Token); code(err) != proto.CodeForbidden {
		t.Fatalf("without root handle: %v", err)
	}
	noJournal := withStepUp(e.env, time.Now())
	noJournal.Writes = nil
	if _, err := e.eapply(t, noJournal, "demo.service", "enable", p.Token); code(err) != proto.CodeUnavailable {
		t.Fatalf("without journal: %v", err)
	}
	anon := *e.env
	anon.Actor.Session = ""
	if _, err := unitEnablementPlan(context.Background(), &anon, args(map[string]string{"unit": "demo.service", "action": "enable"})); code(err) != proto.CodeForbidden {
		t.Fatalf("plan without session: %v", err)
	}
	if len(e.files.changes) != 0 {
		t.Fatal("ran without root/journal")
	}
}

func TestEnablementInterruptedIsRecordedNotRetried(t *testing.T) {
	e := newEnablementEnv(t)
	p, _ := e.eplan(t, "demo.service", "enable")
	// The helper dies after the intent: simulate by panicking in Change.
	e.files.changeFn = func(a, u string) (runner.Result, error) { panic("killed") }
	func() {
		defer func() { _ = recover() }()
		_, _ = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token)
	}()
	if k := journalKinds(t, e.journal); strings.Join(k, ",") != "op.intent,op.outcome" {
		t.Fatalf("panic must still journal an outcome: %v", k)
	}
	// A bare intent (broker killed) is recovered as interrupted, with the
	// fresh boot state — never retried.
	rec := writes.Record{Op: enablementOpName, Action: "enable", Target: "demo.service", Actor: "alice", Risk: RiskDisruptive}
	if _, err := e.env.Writes.Intent(rec); err != nil {
		t.Fatal(err)
	}
	n, err := e.env.Writes.Recover(InspectUnit(e.env))
	if err != nil || n != 1 {
		t.Fatalf("recover: %d %v", n, err)
	}
	b, _ := os.ReadFile(e.journal)
	if !strings.Contains(string(b), "disabled at boot") {
		t.Fatalf("interrupted record lacks the boot state: %s", b)
	}
	if len(e.files.changes) != 1 {
		t.Fatalf("retried: %v", e.files.changes)
	}
}

func TestEnablementAuditArgsNeverContainToken(t *testing.T) {
	if got := enablementAuditArgs(args(map[string]string{"unit": "demo.service", "action": "enable", "token": "hcp1.secret.mac"})); got != "unit=demo.service action=enable" {
		t.Fatalf("%q", got)
	}
	if got := enablementAuditArgs(json.RawMessage(`{"unit":"demo.service","action":"mask"}`)); got != "invalid args" {
		t.Fatalf("%q", got)
	}
}

func TestEnablementStrictDecode(t *testing.T) {
	e := newEnablementEnv(t)
	for _, raw := range []string{`{}`, `{"unit":"demo.service"}`, `{"unit":"demo.service","action":"enable","now":true}`,
		`{"unit":["demo.service"],"action":"enable"}`, `[1]`, `{"unit":"demo.service","action":"enable"}{"x":1}`} {
		if _, err := unitEnablementPlan(context.Background(), e.env, json.RawMessage(raw)); code(err) != proto.CodeBadRequest {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func FuzzEnablementArgs(f *testing.F) {
	for _, s := range []string{`{"unit":"demo.service","action":"enable"}`, `{"unit":"-x.service","action":"disable"}`,
		`{"unit":"a@b.service","action":"enable"}`, `{"unit":"demo.service","action":"mask"}`, `{"unit":"d\u0000.service","action":"enable"}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		var a enablementArgs
		if proto.DecodeArgs(json.RawMessage(raw), &a) != nil || validEnablementArgs(a.Unit, a.Action) != nil {
			return
		}
		// Anything accepted must be a plain, non-instance .service name and
		// a valid argument for the root templates.
		if !servicepolicy.ValidEnablementUnit(a.Unit) || strings.HasPrefix(a.Unit, "-") || strings.ContainsAny(a.Unit, "@/\\ \n\x00") {
			t.Fatalf("accepted %q", a.Unit)
		}
		if _, err := runner.RootArgv(changeTemplates[a.Action], a.Unit); err != nil {
			t.Fatalf("root template refused accepted unit %q: %v", a.Unit, err)
		}
	})
}

func FuzzParseInstall(f *testing.F) {
	f.Add([]byte(wantedByMulti))
	f.Add([]byte("[Install]\nAlias=a.service b.service\nWantedBy=x.target\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		var i InstallInfo
		if ParseInstall(b, &i) != nil {
			return
		}
		for _, l := range [][]string{i.WantedBy, i.RequiredBy, i.UpheldBy, i.Alias, i.Also} {
			for _, n := range l {
				if !inspectable(n) || strings.ContainsAny(n, "%\\ ") {
					t.Fatalf("parsed unsafe name %q", n)
				}
			}
		}
	})
}

// Probe 4 (docs/evidence/m2/m2-3/probes/probe4-alias-collision.out): an
// Alias= naming another unit would replace that unit's definition at the
// next reload. Such aliases, foreign files at link paths and protected
// Requisite= dependents are refused.
func TestEnablementCollisionsAndRequisite(t *testing.T) {
	aliasFile := "[Install]\nWantedBy=multi-user.target\nAlias=victim.service\n"
	requisite := svc("demo.service", "inactive")
	requisite.Deps["RequisiteOf"] = []string{"sshd.service"}
	cases := map[string]struct {
		action, file string
		mutate       func(e *eenv)
		message      string
	}{
		"alias names an installed unit file": {file: aliasFile, mutate: func(e *eenv) { e.files.state["victim.service"] = "static" }, message: "already exists"},
		"alias names a loaded unit": {file: aliasFile, mutate: func(e *eenv) {
			v := svc("victim.service", "active")
			e.fake.units["victim.service"] = &v
		}, message: "already knows that name"},
		"regular file at the alias path": {file: aliasFile, mutate: func(e *eenv) {
			writeUnit(e.t(), e.files.root, "/etc/systemd/system/victim.service", "[Service]\nExecStart=/bin/true\n")
		}, message: "not a symbolic link"},
		"regular file at the wants path": {mutate: func(e *eenv) {
			writeUnit(e.t(), e.files.root, "/etc/systemd/system/multi-user.target.wants/demo.service", "x")
		}, message: "not a symbolic link"},
		"disable a requisite of SSH": {action: "disable", mutate: func(e *eenv) {
			*e.fake.units["demo.service"] = requisite
			e.files.state["demo.service"] = "enabled"
			e.files.setLinks("demo.service", true, -1)
		}, message: "requisite of sshd.service"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnablementEnv(t)
			e.tt = t
			if c.file != "" {
				writeUnit(t, e.files.root, "/etc/systemd/system/demo.service", c.file)
			}
			c.mutate(e)
			if c.action == "" {
				c.action = "enable"
			}
			_, err := e.eplan(t, "demo.service", c.action)
			if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), c.message) {
				t.Fatalf("got %v, want policy_denied containing %q", err, c.message)
			}
		})
	}
	// The alias is free again once the unit is gone: allowed.
	e := newEnablementEnv(t)
	writeUnit(t, e.files.root, "/etc/systemd/system/demo.service", aliasFile)
	if _, err := e.eplan(t, "demo.service", "enable"); err != nil {
		t.Fatalf("free alias refused: %v", err)
	}
}

// [Install] content can come from root-only drop-ins: refusals name the
// setting and line, never the value.
func TestInstallRefusalsDoNotEchoValues(t *testing.T) {
	for _, in := range []string{"[Install]\nTopSecretKey=hunter2-fixture\n", "[Install]\nWantedBy=hunter2-fixture;x\n",
		"[Install]\nAlias=hunter2-fixture.bogus\n"} {
		var i InstallInfo
		err := ParseInstall([]byte(in), &i)
		if err == nil || strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "TopSecret") {
			t.Errorf("%q → %v", in, err)
		}
	}
}

func TestParseIsEnabledAcrossVersions(t *testing.T) {
	for _, c := range []struct{ out, errout, want string }{
		{"enabled\n", "", "enabled"},
		{"not-found\n", "", "not-found"}, // systemd ≥ 255
		{"", "Failed to get unit file state for x.service: No such file or directory\n", "not-found"}, // 249 (probe 6)
		{"alias\n", "", "alias"},
	} {
		if got, err := ParseIsEnabled([]byte(c.out), c.errout); err != nil || got != c.want {
			t.Errorf("%q/%q → %q %v", c.out, c.errout, got, err)
		}
	}
	for _, bad := range [][2]string{{"", "Access denied"}, {"Enabled!\n", ""}, {"", ""}} {
		if _, err := ParseIsEnabled([]byte(bad[0]), bad[1]); code(err) != proto.CodeUnavailable {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
}
