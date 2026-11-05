// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
)

// Regression tests for the M2-3 review findings (probe 7): systemctl works
// from the files on disk, and disable removes every link to the unit. The
// fake controller's Change is systemctlModel, which reproduces the measured
// systemctl behaviour independently of the review code.

func linksUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(filepath.Join(root, "/etc/systemd/system"), func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type()&os.ModeSymlink != 0 {
			rel, _ := filepath.Rel(root, p)
			out = append(out, "/"+rel)
		}
		return nil
	})
	return out
}

func TestEnablementRefusesStaleLoadedView(t *testing.T) {
	cases := map[string]func(e *eenv){
		// A drop-in written after systemd loaded the unit: enable would honour
		// its [Install] (probe 7 A1'), `systemctl show` does not list it.
		"new drop-in": func(e *eenv) {
			writeUnit(t, e.files.root, "/etc/systemd/system/demo.service.d/zz.conf", "[Install]\nWantedBy=extra.target\n")
		},
		// A new /etc fragment shadowing the loaded vendor one (probe 7 A4').
		"new fragment": func(e *eenv) {
			writeUnit(t, e.files.root, "/usr/lib/systemd/system/demo.service", wantedByMulti)
			e.files.props["demo.service"].FragmentPath = "/usr/lib/systemd/system/demo.service"
		},
		// A drop-in systemd has loaded that is gone from disk.
		"removed drop-in": func(e *eenv) {
			e.files.props["demo.service"].DropInPaths = []string{"/etc/systemd/system/demo.service.d/gone.conf"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnablementEnv(t)
			before := linksUnder(t, e.files.root)
			mutate(e)
			_, err := e.eplan(t, "demo.service", "enable")
			if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "differ from the ones systemd has loaded") {
				t.Fatalf("want a stale-view refusal, got %v", err)
			}
			if len(e.files.changes) != 0 || strings.Join(linksUnder(t, e.files.root), ",") != strings.Join(before, ",") {
				t.Fatal("something changed")
			}
		})
	}
}

// A drop-in that appears between plan and apply (without a reload) must not
// be applied: the plan described other links.
func TestEnablementDropInAfterPlanIsRefusedAtApply(t *testing.T) {
	e := newEnablementEnv(t)
	p, err := e.eplan(t, "demo.service", "enable")
	if err != nil {
		t.Fatal(err)
	}
	writeUnit(t, e.files.root, "/etc/systemd/system/demo.service.d/zz.conf", "[Install]\nWantedBy=extra.target\n")
	_, err = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token)
	if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "Nothing was done") {
		t.Fatalf("apply after a new drop-in: %v", err)
	}
	if len(e.files.changes) != 0 || len(linksUnder(t, e.files.root)) != 0 {
		t.Fatalf("changed: %v %v", e.files.changes, linksUnder(t, e.files.root))
	}
}

// With PID 1 and the disk in agreement, a drop-in's [Install] is reviewed and
// the links systemctl creates are exactly the planned ones.
func TestEnablementLoadedDropInIsPlanned(t *testing.T) {
	e := newEnablementEnv(t)
	writeUnit(t, e.files.root, "/etc/systemd/system/demo.service.d/zz.conf", "[Install]\nWantedBy=extra.target\n")
	e.files.props["demo.service"].DropInPaths = []string{"/etc/systemd/system/demo.service.d/zz.conf"}
	e.files.reload()
	p, err := e.eplan(t, "demo.service", "enable")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.Links, ",") != "/etc/systemd/system/extra.target.wants/demo.service,/etc/systemd/system/multi-user.target.wants/demo.service" {
		t.Fatalf("links: %v", p.Links)
	}
	out, err := e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token)
	if err != nil || out.Outcome != OutcomeSucceeded || len(out.Unexpected) != 0 {
		t.Fatalf("%+v %v", out, err)
	}
	for _, l := range out.Links {
		if !l.Present || !l.PointsToUnit || l.Target != "/etc/systemd/system/demo.service" {
			t.Fatalf("link read-back: %+v", l)
		}
	}
	if strings.Join(linksUnder(t, e.files.root), ",") != strings.Join(p.Links, ",") {
		t.Fatalf("on disk: %v", linksUnder(t, e.files.root))
	}
}

func TestEnablementSharedDropIns(t *testing.T) {
	// A type-level drop-in without [Install] (Fedora ships one) is fine.
	e := newEnablementEnv(t)
	writeUnit(t, e.files.root, "/usr/lib/systemd/system/service.d/10-timeout-abort.conf", "[Service]\nTimeoutStopFailureMode=abort\n")
	e.files.props["demo.service"].DropInPaths = []string{"/usr/lib/systemd/system/service.d/10-timeout-abort.conf"}
	e.files.reload()
	if _, err := e.eplan(t, "demo.service", "enable"); err != nil {
		t.Fatalf("type-level drop-in without [Install]: %v", err)
	}
	// With [Install] it is ambiguous (systemctl ignores it, probe 7 A2/A3).
	e = newEnablementEnv(t)
	writeUnit(t, e.files.root, "/etc/systemd/system/demo-.service.d/x.conf", "[Install]\nWantedBy=extra.target\n")
	e.files.props["demo.service"].DropInPaths = []string{"/etc/systemd/system/demo-.service.d/x.conf"}
	e.files.reload()
	if _, err := e.eplan(t, "demo.service", "enable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "systemctl ignores") {
		t.Fatalf("prefix drop-in with [Install]: %v", err)
	}
	// A shared drop-in adding Conflicts= with SSH applies at boot.
	e = newEnablementEnv(t)
	writeUnit(t, e.files.root, "/etc/systemd/system/service.d/c.conf", "[Unit]\nConflicts=ssh.service\n")
	e.files.props["demo.service"].DropInPaths = []string{"/etc/systemd/system/service.d/c.conf"}
	e.files.reload()
	if _, err := e.eplan(t, "demo.service", "enable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "conflicts with ssh.service") {
		t.Fatalf("shared Conflicts=ssh: %v", err)
	}
}

// [Unit] settings on disk that the next boot uses but PID 1 has not loaded.
func TestEnablementDiskUnitHazards(t *testing.T) {
	cases := map[string]struct{ file, want string }{
		"conflicts ssh":      {"[Unit]\nConflicts=ssh.service\n[Service]\nExecStart=/bin/true\n[Install]\nWantedBy=multi-user.target\n", "conflicts with ssh.service"},
		"failure reboot":     {"[Unit]\nFailureAction=reboot\n[Service]\nExecStart=/bin/true\n[Install]\nWantedBy=multi-user.target\n", "emergency action"},
		"continued conflict": {"[Unit]\nConflicts=a.service \\\n  ssh.service\n[Service]\nExecStart=/bin/true\n[Install]\nWantedBy=multi-user.target\n", "line continuation"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnablementEnv(t)
			writeUnit(t, e.files.root, "/etc/systemd/system/demo.service", c.file)
			e.files.reload()
			if _, err := e.eplan(t, "demo.service", "enable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
	// A continued value elsewhere must not be read as a new section.
	e := newEnablementEnv(t)
	writeUnit(t, e.files.root, "/etc/systemd/system/demo.service",
		"[Service]\nExecStart=/bin/echo \\\n[Install]\nAlias=sshd.service\n[Install]\nWantedBy=multi-user.target\n")
	e.files.reload()
	p, err := e.eplan(t, "demo.service", "enable")
	if err != nil || len(p.Aliases) != 0 {
		t.Fatalf("continuation read as a section: %+v %v", p, err)
	}
}

// systemctl disable removes links its [Install] does not describe (probe 7 B):
// such units are refused, and nothing is removed.
func TestDisableRefusesUndescribedLinks(t *testing.T) {
	extra := map[string]string{
		"other target":     "/etc/systemd/system/other.target.wants/demo.service",
		"requires":         "/etc/systemd/system/web.service.requires/demo.service",
		"manual alias":     "/etc/systemd/system/demo-manual.service",
		"renamed in wants": "/etc/systemd/system/multi-user.target.wants/renamed.service",
		"deep":             "/etc/systemd/system/deep/er/demo.service",
	}
	for name, l := range extra {
		t.Run(name, func(t *testing.T) {
			e := newEnablementEnv(t)
			e.files.setLinks("demo.service", true, -1)
			e.files.state["demo.service"] = "enabled"
			full := filepath.Join(e.files.root, l)
			_ = os.MkdirAll(filepath.Dir(full), 0o755)
			if err := os.Symlink("/etc/systemd/system/demo.service", full); err != nil {
				t.Fatal(err)
			}
			e.files.reload() // the links exist and systemd has loaded them
			before := linksUnder(t, e.files.root)
			_, err := e.eplan(t, "demo.service", "disable")
			if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), l) || !strings.Contains(err.Error(), "would remove them too") {
				t.Fatalf("got %v", err)
			}
			if len(e.files.changes) != 0 || strings.Join(linksUnder(t, e.files.root), ",") != strings.Join(before, ",") {
				t.Fatal("changed")
			}
		})
	}
	// Enabling a disabled unit with a stray link to it is refused too.
	e := newEnablementEnv(t)
	_ = os.MkdirAll(filepath.Join(e.files.root, "/etc/systemd/system/other.target.wants"), 0o755)
	_ = os.Symlink("/etc/systemd/system/demo.service", filepath.Join(e.files.root, "/etc/systemd/system/other.target.wants/demo.service"))
	if _, err := e.eplan(t, "demo.service", "enable"); code(err) != proto.CodePolicyDenied {
		t.Fatalf("stray link on enable: %v", err)
	}
	// Runtime links are refused.
	e = newEnablementEnv(t)
	_ = os.MkdirAll(filepath.Join(e.files.root, "/run/systemd/system/multi-user.target.wants"), 0o755)
	_ = os.Symlink("/etc/systemd/system/demo.service", filepath.Join(e.files.root, "/run/systemd/system/multi-user.target.wants/demo.service"))
	if _, err := e.eplan(t, "demo.service", "enable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "/run/systemd/system") {
		t.Fatalf("runtime link: %v", err)
	}
	// A planned link that points somewhere else is not the unit's.
	e = newEnablementEnv(t)
	e.files.state["demo.service"] = "enabled"
	_ = os.MkdirAll(filepath.Join(e.files.root, "/etc/systemd/system/multi-user.target.wants"), 0o755)
	_ = os.Symlink("/usr/lib/systemd/system/demo.service", filepath.Join(e.files.root, "/etc/systemd/system/multi-user.target.wants/demo.service"))
	e.files.reload()
	if _, err := e.eplan(t, "demo.service", "disable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "not to the unit file") {
		t.Fatalf("foreign target: %v", err)
	}
}

// The read-back is a fresh scan: links systemctl creates beyond the plan, or
// that point elsewhere, are reported and the outcome is not "succeeded".
func TestEnablementReadBackScansLinks(t *testing.T) {
	e := newEnablementEnv(t)
	p, err := e.eplan(t, "demo.service", "enable")
	if err != nil {
		t.Fatal(err)
	}
	e.files.changeFn = func(action, unit string) (runner.Result, error) {
		for _, l := range []string{"/etc/systemd/system/multi-user.target.wants/demo.service", "/etc/systemd/system/surprise.target.wants/demo.service"} {
			_ = os.MkdirAll(filepath.Dir(filepath.Join(e.files.root, l)), 0o755)
			_ = os.Symlink("/etc/systemd/system/demo.service", filepath.Join(e.files.root, l))
		}
		e.files.state[unit] = "enabled"
		return runner.Result{}, nil
	}
	out, err := e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token)
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome == OutcomeSucceeded || strings.Join(out.Unexpected, ",") != "/etc/systemd/system/surprise.target.wants/demo.service" ||
		!strings.Contains(out.Message, "not reviewed") || len(out.Recovery) == 0 {
		t.Fatalf("extra link not reported: %+v", out)
	}

	e = newEnablementEnv(t)
	p, err = e.eplan(t, "demo.service", "enable")
	if err != nil {
		t.Fatal(err)
	}
	e.files.changeFn = func(action, unit string) (runner.Result, error) {
		l := filepath.Join(e.files.root, "/etc/systemd/system/multi-user.target.wants/demo.service")
		_ = os.MkdirAll(filepath.Dir(l), 0o755)
		_ = os.Symlink("/dev/null", l)
		e.files.state[unit] = "enabled"
		return runner.Result{}, nil
	}
	out, err = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token)
	if err != nil || out.Outcome == OutcomeSucceeded || out.Links[0].PointsToUnit || out.Links[0].Target != "/dev/null" {
		t.Fatalf("foreign link target accepted: %+v %v", out, err)
	}
}

// Recovery describes what it observed against the intent, and retries nothing.
func TestEnablementRecoveryDescribesObservedState(t *testing.T) {
	e := newEnablementEnv(t)
	e.files.setLinks("demo.service", true, -1)
	e.files.state["demo.service"] = "enabled"
	obs, desc := InspectUnit(e.env)(writesRecord(enablementOpName, "enable", "demo.service", `{"id":"demo.service","unit_file_state":"disabled"}`))
	if len(obs) == 0 || !strings.Contains(desc, "enabled at boot (before the change: disabled; requested: enable; changed, not verified)") ||
		!strings.Contains(desc, "1 link(s)") {
		t.Fatalf("description: %q", desc)
	}
	if len(e.files.changes) != 0 {
		t.Fatal("recovery executed something")
	}
}

func writesRecord(op, action, target, observed string) writes.Record {
	return writes.Record{Op: op, Action: action, Target: target, Observed: []byte(observed)}
}
