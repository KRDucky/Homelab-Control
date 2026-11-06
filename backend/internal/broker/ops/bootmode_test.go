// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"strings"
	"testing"

	"homelab-control/backend/internal/broker/proto"
)

// Final safety review (2026-11-05): the enable review is about the start at
// the NEXT boot, so what is running now must not decide it. Each case below
// was measured through the product in disposable containers on systemd 249,
// 255 and 259 (dev/disposable/boot-repro.sh): before the correction it was
// allowed and the protected stand-in did not run after the reboot.

const prot = "homelab-control-hctest-vpn.service" // a protected-name stand-in

func inactive(u UnitProps) UnitProps {
	u.ActiveState, u.SubState = "inactive", "dead"
	return u
}

// enableEnv is demo.service (disabled, inactive) plus the given units with
// persistent files.
func enableEnv(t *testing.T, extra ...UnitProps) *eenv {
	t.Helper()
	e := newEnablementEnv(t)
	e.tt = t
	for _, u := range extra {
		addUnitFile(t, e, u)
	}
	e.files.reload()
	return e
}

func refused(t *testing.T, e *eenv, unit, action, want string) {
	t.Helper()
	links := linksUnder(t, e.files.root)
	_, err := e.eplan(t, unit, action)
	if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want a refusal containing %q", err, want)
	}
	if got := strings.Join(linksUnder(t, e.files.root), ","); got != strings.Join(links, ",") || len(e.files.changes) != 0 || len(e.fake.acts) != 0 {
		t.Fatalf("something changed: %s %v %v", got, e.files.changes, e.fake.acts)
	}
}

// E1: demo → mid (running now, not started at boot otherwise) → y, which
// conflicts with a protected unit. Refused whether mid runs or not; allowed
// when mid is started at boot anyway (the conflict is not caused by demo).
func TestEnableDoesNotTrustRunningDependencies(t *testing.T) {
	for _, state := range []string{"active", "inactive"} {
		t.Run(state, func(t *testing.T) {
			mid := unit("mid.service", map[string][]string{"Wants": {"y.service"}})
			mid.ActiveState = state
			e := enableEnv(t, mid, inactive(unit("y.service", map[string][]string{"Conflicts": {prot}})), unit(prot, nil))
			pulls(e, "demo.service", "Wants", "mid.service")
			refused(t, e, "demo.service", "enable", "would stop "+prot)
		})
	}
	mid := unit("mid.service", map[string][]string{"Wants": {"y.service"}})
	e := enableEnv(t, mid, inactive(unit("y.service", map[string][]string{"Conflicts": {prot}})), unit(prot, nil))
	pulls(e, "demo.service", "Wants", "mid.service")
	pulls(e, "multi-user.target", "Wants", "mid.service")
	if _, err := e.eplan(t, "demo.service", "enable"); err != nil {
		t.Fatalf("mid started at boot anyway: %v", err)
	}
}

// E2: a running dependency with FailureAction=reboot.
func TestEnableRunningDependencyWithEmergencyAction(t *testing.T) {
	x := unit("x.service", nil)
	x.Actions = map[string]string{"FailureAction": "reboot"}
	e := enableEnv(t, x)
	pulls(e, "demo.service", "Wants", "x.service")
	refused(t, e, "demo.service", "enable", "x.service sets FailureAction=reboot")
}

// E3: demo conflicts with z, which is stopped now but started at boot (the
// enabled protected unit BindsTo= it): the stop checks apply. A conflict
// with a unit that cannot run at the next boot (not started at boot, not
// running, not started by the change — like shutdown.target) is harmless.
func TestEnableConflictWithAUnitThatRunsAtBoot(t *testing.T) {
	z := inactive(unit("z.service", map[string][]string{"BoundBy": {prot}}))
	p := inactive(unit(prot, map[string][]string{"BindsTo": {"z.service"}}))
	e := enableEnv(t, z, p)
	pulls(e, "multi-user.target", "Wants", prot)
	pulls(e, "demo.service", "Conflicts", "z.service")
	refused(t, e, "demo.service", "enable", "would stop or restart "+prot)

	e = enableEnv(t, z, p) // nothing starts them at boot
	pulls(e, "demo.service", "Conflicts", "z.service")
	if _, err := e.eplan(t, "demo.service", "enable"); err != nil {
		t.Fatalf("conflict with a unit that does not run at boot: %v", err)
	}
	// In the loaded graph only (runtime or generated relation): may run at
	// boot, so it is checked as well.
	e = enableEnv(t, z, p)
	e.fake.mu.Lock()
	e.fake.units["multi-user.target"].Deps["Wants"] = append(e.fake.units["multi-user.target"].Deps["Wants"], prot)
	e.fake.mu.Unlock()
	pulls(e, "demo.service", "Conflicts", "z.service")
	refused(t, e, "demo.service", "enable", "would stop or restart "+prot)
}

// A conflict between two units the change itself starts (a is walked before
// the protected unit, which BindsTo= b): once the walk is complete, b may run
// at boot (the change starts it), so the stop checks apply to b and to the
// protected unit bound to it.
func TestEnableConflictBetweenUnitsTheChangeStarts(t *testing.T) {
	e := enableEnv(t, inactive(unit("a.service", map[string][]string{"Conflicts": {"b.service"}})),
		inactive(unit("b.service", map[string][]string{"BoundBy": {prot}})),
		inactive(unit(prot, map[string][]string{"BindsTo": {"b.service"}})))
	pulls(e, "demo.service", "Wants", "a.service", prot)
	refused(t, e, "demo.service", "enable", "would stop or restart "+prot)
}

// E5: socket activation: demo wants a socket whose service conflicts with a
// protected unit (Triggers= is followed for the boot review).
func TestEnableFollowsSocketActivation(t *testing.T) {
	e := enableEnv(t, inactive(unit("s.socket", map[string][]string{"Triggers": {"s.service"}})),
		inactive(unit("s.service", map[string][]string{"Conflicts": {prot}})), unit(prot, nil))
	pulls(e, "demo.service", "Wants", "s.socket")
	refused(t, e, "demo.service", "enable", "would stop "+prot)
}

// E7e / D7: a runtime drop-in with the same name hides a persistent drop-in
// until the next boot.
func TestRuntimeDropInHidingAPersistentDropIn(t *testing.T) {
	hidden := func(t *testing.T, e *eenv, id string) {
		e.fake.mu.Lock()
		e.fake.units[id].DropInPaths = []string{"/run/systemd/system/" + id + ".d/10-x.conf"}
		e.fake.mu.Unlock()
		if ip := e.files.props[id]; ip != nil {
			ip.DropInPaths = []string{"/run/systemd/system/" + id + ".d/10-x.conf"}
		}
		writeUnit(t, e.files.root, "/run/systemd/system/"+id+".d/10-x.conf", "[Unit]\nDescription=runtime\n")
		writeUnit(t, e.files.root, "/usr/lib/systemd/system/"+id+".d/10-x.conf", "[Unit]\nConflicts="+prot+"\nWants="+prot+"\n")
		e.files.reload()
	}
	e := enableEnv(t, inactive(unit("m.service", nil)))
	pulls(e, "demo.service", "Wants", "m.service")
	hidden(t, e, "m.service")
	refused(t, e, "demo.service", "enable", "hidden by a runtime drop-in")

	e = disableEnv(t, unit("m.service", nil), unit(prot, nil))
	pulls(e, "demo.service", "Wants", "m.service")
	hidden(t, e, "m.service")
	refused(t, e, "demo.service", "disable", "hidden by a runtime drop-in")

	e = disableEnv(t) // the unit's own drop-in
	hidden(t, e, "demo.service")
	refused(t, e, "demo.service", "disable", "hidden by a runtime drop-in")

	// A runtime drop-in alone (netplan's kind) is not refused.
	e = disableEnv(t, unit("m.service", nil))
	pulls(e, "demo.service", "Wants", "m.service")
	e.fake.mu.Lock()
	e.fake.units["m.service"].DropInPaths = []string{"/run/systemd/system/m.service.d/10-netplan.conf"}
	e.fake.mu.Unlock()
	writeUnit(t, e.files.root, "/run/systemd/system/m.service.d/10-netplan.conf", "[Service]\nExecStart=\nExecStart=/bin/true\n")
	e.files.reload()
	if _, err := e.eplan(t, "demo.service", "disable"); err != nil {
		t.Fatalf("runtime drop-in alone: %v", err)
	}
}

// D6: a unit disable reaches has a name that is a runtime alias over a
// persistent unit file (relations are shown by canonical ID).
func TestDisableRuntimeAliasOverAPersistentName(t *testing.T) {
	e := disableEnv(t, unit("bar.service", nil), unit(prot, nil))
	e.fake.mu.Lock()
	e.fake.units["bar.service"].Names = []string{"bar.service", "foo.service"}
	e.fake.units["foo.service"] = e.fake.units["bar.service"]
	e.fake.mu.Unlock()
	writeUnit(t, e.files.root, "/usr/lib/systemd/system/foo.service", "[Unit]\nWants="+prot+"\n")
	writeLink(t, e.files.root, "/run/systemd/system/foo.service", "/usr/lib/systemd/system/bar.service")
	pulls(e, "demo.service", "Wants", "bar.service")
	refused(t, e, "demo.service", "disable", "foo.service resolves to it only until the next boot")
}

// D1: the disable check does not depend on what is running: a running or a
// stopped intermediate gives the same refusal.
func TestDisableIndependentOfRunningState(t *testing.T) {
	for _, state := range []string{"active", "inactive"} {
		for _, pstate := range []string{"active", "inactive"} {
			mid := unit("mid.service", map[string][]string{"Wants": {prot}})
			mid.ActiveState = state
			p := unit(prot, nil)
			p.ActiveState = pstate
			e := disableEnv(t, mid, p)
			pulls(e, "demo.service", "Wants", "mid.service")
			refused(t, e, "demo.service", "disable", "started at boot only through it")
		}
	}
}

// The boot model is used only by the enable review: an M2-2 start now still
// relies on what is running now (a running dependency's own dependencies are
// already satisfied).
func TestStartNowKeepsItsModel(t *testing.T) {
	mid := unit("mid.service", map[string][]string{"Wants": {"y.service"}})
	e := enableEnv(t, mid, inactive(unit("y.service", map[string][]string{"Conflicts": {prot}})), unit(prot, nil))
	pulls(e, "demo.service", "Wants", "mid.service")
	d := e.fake.units["demo.service"]
	if _, err := assess(context.Background(), e.env, "start", *d); err != nil {
		t.Fatalf("M2-2 start with a running dependency: %v", err)
	}
}

// Units started by the change at boot are listed (also Homelab Control's
// own unloaded .wants links are followed).
func TestEnableListsBootStartsIncludingRunningOnes(t *testing.T) {
	e := enableEnv(t, unit("mid.service", nil))
	pulls(e, "demo.service", "Wants", "mid.service")
	p, err := e.eplan(t, "demo.service", "enable")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(p.AlsoStarted, ","), "mid.service") {
		t.Fatalf("starts %v", p.AlsoStarted)
	}
}

// Hidden persistent configuration on a unit the walk never evaluates: Z is
// started at boot anyway (so not walked); its persistent drop-in conflicts
// with (or binds to) a unit the change starts, but a same-name runtime drop-in
// hides it, so the loaded graph shows no ConflictedBy=/BoundBy= on that unit.
// At the next boot the persistent drop-in applies. Refused by the runtime scan.
func TestEnableHiddenReverseRelationOfAnUnwalkedUnit(t *testing.T) {
	for _, c := range []struct{ name, key, dir string }{
		{"Conflicts via system", "Conflicts", "/run/systemd/system/"},
		{"BindsTo via system.control", "BindsTo", "/run/systemd/system.control/"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := enableEnv(t, unit("z.service", nil), inactive(unit("y.service", nil)))
			pulls(e, "multi-user.target", "Wants", "z.service")
			pulls(e, "demo.service", "Wants", "y.service")
			e.fake.mu.Lock()
			e.fake.units["z.service"].DropInPaths = []string{c.dir + "z.service.d/50-x.conf"}
			e.fake.mu.Unlock()
			writeUnit(t, e.files.root, c.dir+"z.service.d/50-x.conf", "[Unit]\nDescription=runtime\n")
			writeUnit(t, e.files.root, "/etc/systemd/system/z.service.d/50-x.conf", "[Unit]\n"+c.key+"=y.service\n")
			e.files.reload()
			refused(t, e, "demo.service", "enable", "hidden")
		})
	}
	// A runtime unit file shadowing z's persistent file (PartOf= y): refused.
	e := enableEnv(t, unit("z.service", nil), inactive(unit("y.service", nil)))
	pulls(e, "multi-user.target", "Wants", "z.service")
	pulls(e, "demo.service", "Wants", "y.service")
	writeUnit(t, e.files.root, "/run/systemd/system/z.service", "[Service]\nExecStart=/bin/true\n")
	writeUnit(t, e.files.root, "/usr/lib/systemd/system/z.service", "[Unit]\nPartOf=y.service\n")
	e.files.reload()
	refused(t, e, "demo.service", "enable", "hidden by a runtime unit file")
	// A runtime drop-in on an unwalked unit that hides nothing: not refused.
	e = enableEnv(t, unit("z.service", nil), inactive(unit("y.service", nil)))
	pulls(e, "multi-user.target", "Wants", "z.service")
	pulls(e, "demo.service", "Wants", "y.service")
	writeUnit(t, e.files.root, "/run/systemd/system/z.service.d/50-x.conf", "[Unit]\nDescription=runtime\n")
	e.files.reload()
	if _, err := e.eplan(t, "demo.service", "enable"); err != nil {
		t.Fatalf("runtime drop-in hiding nothing: %v", err)
	}
}
