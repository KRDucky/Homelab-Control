// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
)

// Regression tests for the unreloaded-change gaps (probe 8, measured on
// systemd 249, 255 and 259): a change on disk that systemd has not loaded is
// used at the next boot, and in each measured case let an enabled fixture
// stop a stand-in protected service. The fake's loaded view (props and
// e.fake.units) is deliberately NOT updated by these edits, as in probe 8.

const gateMsg = "changed on disk after systemd last loaded its configuration"

func TestEnableRefusedAfterUnloadedChanges(t *testing.T) {
	cases := map[string]func(e *eenv){
		// G1: a new shared (prefix) drop-in adding Conflicts= with SSH.
		"new prefix drop-in": func(e *eenv) {
			writeUnit(t, e.files.root, "/etc/systemd/system/demo-.service.d/zz.conf", "[Unit]\nConflicts=ssh.service\n")
		},
		"new type-level drop-in": func(e *eenv) {
			writeUnit(t, e.files.root, "/usr/lib/systemd/system/service.d/zz.conf", "[Unit]\nConflicts=ssh.service\n")
		},
		// G2: the unit's own fragment now Wants= a unit that conflicts with SSH.
		"fragment edited": func(e *eenv) {
			writeUnit(t, e.files.root, "/etc/systemd/system/demo.service",
				"[Unit]\nWants=conflictor.service\n[Service]\nExecStart=/bin/true\n[Install]\nWantedBy=multi-user.target\n")
		},
		// G3: ANOTHER unit's new drop-in (a protected one) conflicts with this one.
		"protected unit's new drop-in": func(e *eenv) {
			writeUnit(t, e.files.root, "/etc/systemd/system/ssh.service.d/zz.conf", "[Unit]\nConflicts=demo.service\n")
		},
		"vendor unit replaced": func(e *eenv) {
			writeUnit(t, e.files.root, "/usr/lib/systemd/system/conflictor.service", "[Unit]\nConflicts=ssh.service\n")
		},
		"file removed": func(e *eenv) {
			writeUnit(t, e.files.root, "/etc/systemd/system/other.service", "[Service]\nExecStart=/bin/true\n")
			e.files.reload()
			_ = os.Remove(filepath.Join(e.files.root, "/etc/systemd/system/other.service"))
		},
		"foreign link": func(e *eenv) {
			l := filepath.Join(e.files.root, "/etc/systemd/system/multi-user.target.wants/conflictor.service")
			_ = os.MkdirAll(filepath.Dir(l), 0o755)
			_ = os.Symlink("/usr/lib/systemd/system/conflictor.service", l)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnablementEnv(t)
			mutate(e)
			before := linksUnder(t, e.files.root)
			_, err := e.eplan(t, "demo.service", "enable")
			if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), gateMsg) {
				t.Fatalf("want the unloaded-change refusal, got %v", err)
			}
			if len(e.files.changes) != 0 || strings.Join(linksUnder(t, e.files.root), ",") != strings.Join(before, ",") {
				t.Fatal("something changed")
			}
			// After the harness reloads, the same files are reviewable again
			// (the gate is about unloaded changes, not about the content).
			e.files.reload()
			if _, err := e.eplan(t, "demo.service", "enable"); err != nil && strings.Contains(err.Error(), gateMsg) {
				t.Fatalf("still refused as unloaded after a reload: %v", err)
			}
		})
	}
}

// An edit between preview and apply is refused at apply; nothing runs.
func TestEnableEditBetweenPreviewAndApply(t *testing.T) {
	for name, path := range map[string]string{
		"protected unit drop-in": "/etc/systemd/system/ssh.service.d/zz.conf",
		"shared drop-in":         "/etc/systemd/system/service.d/zz.conf",
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnablementEnv(t)
			p, err := e.eplan(t, "demo.service", "enable")
			if err != nil {
				t.Fatal(err)
			}
			writeUnit(t, e.files.root, path, "[Unit]\nConflicts=demo.service\n")
			_, err = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token)
			if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), gateMsg) || !strings.Contains(err.Error(), "Nothing was done") {
				t.Fatalf("apply after an edit: %v", err)
			}
			if len(e.files.changes) != 0 || len(linksUnder(t, e.files.root)) != 0 {
				t.Fatal("changed")
			}
		})
	}
}

// Homelab Control's own journaled changes do not block the next enable; a
// change made elsewhere afterwards does, even in a directory it touched.
func TestEnableGateExcusesOwnJournaledChanges(t *testing.T) {
	e := newEnablementEnv(t, svc("demo.service", "inactive"), svc("two.service", "inactive"), svc("three.service", "inactive"))
	for _, u := range []string{"demo.service", "two.service"} {
		p, err := e.eplan(t, u, "enable")
		if err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		if o, err := e.eapply(t, withStepUp(e.env, time.Now()), u, "enable", p.Token); err != nil || o.Outcome != OutcomeSucceeded {
			t.Fatalf("%s: %+v %v", u, o, err)
		}
	}
	// Disabling (undo) does not need the gate, and its own removal is excused.
	p, err := e.eplan(t, "two.service", "disable")
	if err != nil {
		t.Fatal(err)
	}
	if o, err := e.eapply(t, withStepUp(e.env, time.Now()), "two.service", "disable", p.Token); err != nil || o.Outcome != OutcomeSucceeded {
		t.Fatalf("disable: %+v %v", o, err)
	}
	if _, err := e.eplan(t, "three.service", "enable"); err != nil {
		t.Fatalf("own changes blocked the next enable: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	// A foreign removal in the directory the own changes touched.
	if err := os.Remove(filepath.Join(e.files.root, "/etc/systemd/system/multi-user.target.wants/demo.service")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.eplan(t, "three.service", "enable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "multi-user.target.wants") {
		t.Fatalf("foreign removal not detected: %v", err)
	}
}

// An interrupted change (no outcome with a window) is never excused.
func TestEnableGateDoesNotExcuseInterruptedChanges(t *testing.T) {
	e := newEnablementEnv(t, svc("demo.service", "inactive"), svc("two.service", "inactive"))
	p, err := e.eplan(t, "demo.service", "enable")
	if err != nil {
		t.Fatal(err)
	}
	e.files.changeFn = func(a, u string) (runner.Result, error) {
		l := filepath.Join(e.files.root, "/etc/systemd/system/multi-user.target.wants/demo.service")
		_ = os.MkdirAll(filepath.Dir(l), 0o755)
		_ = os.Symlink("/etc/systemd/system/demo.service", l)
		panic("helper crashed after the change")
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token)
	}()
	if _, err := e.eplan(t, "two.service", "enable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), gateMsg) {
		t.Fatalf("interrupted change excused: %v", err)
	}
}

func TestEnableGateFailsClosedWithoutLoadTime(t *testing.T) {
	e := newEnablementEnv(t)
	e.files.loadErr = errors.New("no answer")
	if _, err := e.eplan(t, "demo.service", "enable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "could not be determined") {
		t.Fatalf("enable without a load time: %v", err)
	}
	// Disable does not depend on it (undo stays possible).
	e.files.setLinks("demo.service", true, -1)
	e.files.state["demo.service"] = "enabled"
	if _, err := e.eplan(t, "demo.service", "disable"); err != nil {
		t.Fatalf("disable: %v", err)
	}
}

// Disable: Requisite= in unit files on disk, loaded or not.
func TestDisableRefusesRequisiteOnDisk(t *testing.T) {
	cases := map[string]struct {
		path, file string
		refused    bool
	}{
		"protected unit fragment": {"/usr/lib/systemd/system/sshd.service", "[Unit]\nRequisite=demo.service\n[Service]\nExecStart=/bin/true\n", true},
		"protected unit drop-in":  {"/etc/systemd/system/ssh.service.d/r.conf", "[Unit]\nRequisite=demo.service\n", true},
		"continued value":         {"/etc/systemd/system/ssh.service.d/r.conf", "[Unit]\nRequisite=a.service \\\n  demo.service\n", true},
		"shared drop-in":          {"/etc/systemd/system/service.d/r.conf", "[Unit]\nRequisite=demo.service\n", true},
		"prefix drop-in":          {"/etc/systemd/system/web-.service.d/r.conf", "[Unit]\nRequisite=demo.service\n", true},
		"unprotected unit":        {"/etc/systemd/system/web.service", "[Unit]\nRequisite=demo.service\n[Service]\nExecStart=/bin/true\n", false},
		"other section":           {"/etc/systemd/system/ssh.service.d/r.conf", "[Service]\nRequisite=demo.service\n", false},
		"names another unit":      {"/etc/systemd/system/ssh.service.d/r.conf", "[Unit]\nRequisite=demo2.service\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnablementEnv(t)
			e.files.setLinks("demo.service", true, -1)
			e.files.state["demo.service"] = "enabled"
			writeUnit(t, e.files.root, c.path, c.file)
			_, err := e.eplan(t, "demo.service", "disable")
			if c.refused != (code(err) == proto.CodePolicyDenied) {
				t.Fatalf("refused=%v: %v", c.refused, err)
			}
			if !c.refused && err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Manager timestamps as systemd 249, 255 and 259 print them (probe 8).
func TestParseManagerShow(t *testing.T) {
	v249 := "UnitsLoadStartTimestamp=Wed 2026-11-04 11:25:34.973341 UTC\nUnitsLoadStartTimestampMonotonic=25090489585\n"
	m, err := ParseManagerShow([]byte(v249))
	if err != nil || m.HasReloadStamp || m.BootMonotonic != 25090489585*time.Microsecond ||
		!m.BootRealtime.Equal(time.Date(2026, 11, 4, 11, 25, 34, 973341000, time.UTC)) {
		t.Fatalf("249: %+v %v", m, err)
	}
	noReload := "UnitsLoadStartTimestamp=Wed 2026-11-04 11:25:35.164385 UTC\nUnitsLoadStartTimestampMonotonic=25090680629\nUnitsLoadTimestampMonotonic=0\n"
	m, err = ParseManagerShow([]byte(noReload))
	if err != nil || !m.HasReloadStamp || !m.ReloadRealtime.IsZero() {
		t.Fatalf("259 before a reload: %+v %v", m, err)
	}
	reloaded := noReload[:strings.LastIndex(noReload, "UnitsLoadTimestampMonotonic")] +
		"UnitsLoadTimestamp=Wed 2026-11-04 11:26:10.751214 UTC\nUnitsLoadTimestampMonotonic=27090680629\n"
	m, err = ParseManagerShow([]byte(reloaded))
	if err != nil || m.ReloadMonotonic != 27090680629*time.Microsecond || m.ReloadRealtime.Second() != 10 {
		t.Fatalf("259 after a reload: %+v %v", m, err)
	}
	for _, bad := range []string{"", "UnitsLoadStartTimestamp=garbage\n", v249 + v249} {
		if _, err := ParseManagerShow([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// The threshold is the earliest of the realtime and monotonic forms, so a
// realtime clock step in either direction never moves it later.
func TestLoadThreshold(t *testing.T) {
	saved := uptime
	defer func() { uptime = saved }()
	root := Roots{Root: t.TempDir()}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	uptime = func() (time.Duration, error) { return 2 * time.Hour, nil }
	// Reloaded 1 h after boot (monotonic): loaded at now-1h.
	m := ManagerLoad{BootRealtime: now.Add(-2 * time.Hour), BootMonotonic: time.Second, HasReloadStamp: true,
		ReloadMonotonic: time.Hour}
	for name, real := range map[string]time.Time{
		"consistent":                       now.Add(-time.Hour),
		"clock stepped back after load":    now.Add(-time.Hour).Add(10 * time.Minute), // realtime later than truth? use the earlier
		"clock stepped forward after load": now.Add(-time.Hour).Add(-10 * time.Minute),
	} {
		m.ReloadRealtime = real
		th, err := loadThreshold(root, m, now)
		want := now.Add(-time.Hour)
		if real.Before(want) {
			want = real
		}
		if err != nil || !th.Equal(want.Add(-loadMargin)) {
			t.Errorf("%s: %v %v, want %v", name, th, err, want)
		}
	}
	// systemd 249 (no reload stamp): the generator directory's change time,
	// never earlier than the boot load.
	m = ManagerLoad{BootRealtime: time.Now().Add(-2 * time.Hour), BootMonotonic: time.Second}
	gen := root.Path(generatorDir)
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(gen)
	th, err := loadThreshold(root, m, time.Now())
	if err != nil || !th.Equal(ctimeOf(fi).Add(-loadMargin)) {
		t.Errorf("249: %v %v", th, err)
	}
}

// A foreign change made while an own change runs (inside its time window) is
// not excused: only the recorded link paths and their directories are.
func TestEnableGateForeignChangeDuringOwnChange(t *testing.T) {
	e := newEnablementEnv(t, svc("demo.service", "inactive"), svc("two.service", "inactive"))
	p, err := e.eplan(t, "demo.service", "enable")
	if err != nil {
		t.Fatal(err)
	}
	e.files.changeFn = func(a, u string) (runner.Result, error) {
		writeUnit(t, e.files.root, "/etc/systemd/system/ssh.service.d/zz.conf", "[Unit]\nConflicts=two.service\n")
		out := systemctlModel(e.files.root, a, u)
		e.files.state[u] = "enabled"
		return runner.Result{Stdout: []byte(out)}, nil
	}
	if o, err := e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token); err != nil || o.Outcome != OutcomeSucceeded {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := e.eplan(t, "two.service", "enable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "ssh.service.d") {
		t.Fatalf("foreign change inside the own window excused: %v", err)
	}
}
