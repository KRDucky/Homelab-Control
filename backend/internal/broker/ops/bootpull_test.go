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
)

// Blocker B-1 (probe 10, measured on systemd 249/255/259): after disabling a
// unit that pulls in a protected unit — directly, transitively or through an
// alias — the protected unit no longer starts at boot unless something else
// pulls it in (another enabled unit, or socket/timer activation). The fake
// loaded graph below models exactly those measured relations.

// disableEnv is demo.service, enabled, plus the given loaded units.
func disableEnv(t *testing.T, extra ...UnitProps) *eenv {
	t.Helper()
	e := newEnablementEnv(t, svc("demo.service", "active"))
	e.tt = t
	for _, u := range extra {
		addUnitFile(t, e, u)
	}
	e.files.setLinks("demo.service", true, -1)
	e.files.state["demo.service"] = "enabled"
	e.files.reload()
	return e
}

func unit(id string, deps map[string][]string, names ...string) UnitProps {
	return UnitProps{ID: id, Names: append([]string{id}, names...), LoadState: "loaded", ActiveState: "active",
		SubState: "running", Deps: deps, FragmentPath: "/usr/lib/systemd/system/" + id}
}

// addUnitFile registers u in the loaded graph and writes its persistent unit
// file (with its dependencies) and alias links, as a package would.
func addUnitFile(t *testing.T, e *eenv, u UnitProps) {
	t.Helper()
	addUnit(e.fake, u)
	body := "[Unit]\nDescription=fixture " + u.ID + "\n"
	for k, l := range u.Deps {
		if len(l) > 0 && k != "Triggers" {
			body += k + "=" + strings.Join(l, " ") + "\n"
		}
	}
	writeUnit(t, e.files.root, u.FragmentPath, body)
	for _, n := range u.Names {
		if n != u.ID {
			_ = os.Symlink(u.FragmentPath, filepath.Join(e.files.root, "/usr/lib/systemd/system", n))
		}
	}
}

// pulls adds a dependency to the loaded graph and to the unit's persistent
// file, then simulates the harness's daemon-reload.
func pulls(e *eenv, id string, key string, names ...string) {
	e.fake.mu.Lock()
	u := e.fake.units[id]
	u.Deps[key] = append(u.Deps[key], names...)
	frag := u.FragmentPath
	e.fake.mu.Unlock()
	if key != "Triggers" {
		b, _ := os.ReadFile(filepath.Join(e.files.root, frag))
		writeUnit(e.tt, e.files.root, frag, string(b)+"[Unit]\n"+key+"="+strings.Join(names, " ")+"\n")
	}
	e.files.reload()
}

// unpull removes all of a unit's dependencies of one kind (loaded graph and
// persistent file), then simulates the harness's daemon-reload.
func unpull(e *eenv, id, key string) {
	e.fake.mu.Lock()
	u := e.fake.units[id]
	u.Deps[key] = nil
	frag := u.FragmentPath
	e.fake.mu.Unlock()
	b, _ := os.ReadFile(filepath.Join(e.files.root, frag))
	var keep []string
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(l, key+"=") {
			keep = append(keep, l)
		}
	}
	writeUnit(e.tt, e.files.root, frag, strings.Join(keep, "\n"))
	e.files.reload()
}

// unchanged asserts that nothing ran and no link or runtime state changed.
func unchanged(t *testing.T, e *eenv, links []string, states map[string]string) {
	t.Helper()
	if len(e.files.changes) != 0 || len(e.fake.acts) != 0 {
		t.Fatalf("something ran: %v %v", e.files.changes, e.fake.acts)
	}
	if got := strings.Join(linksUnder(t, e.files.root), ","); got != strings.Join(links, ",") {
		t.Fatalf("links changed: %s", got)
	}
	for id, st := range states {
		if e.fake.units[id].ActiveState != st {
			t.Fatalf("%s state changed to %s", id, e.fake.units[id].ActiveState)
		}
	}
	if e.files.state["demo.service"] != "enabled" {
		t.Fatal("boot state changed")
	}
}

func TestDisableRefusedWhenOnlyItStartsAProtectedUnitAtBoot(t *testing.T) {
	cases := map[string]struct {
		extra   []UnitProps
		key     string
		target  string
		message string
	}{
		"Wants remote access":    {[]UnitProps{unit("wg-quick@wg0.service", nil)}, "Wants", "wg-quick@wg0.service", "wg-quick@wg0.service is started at boot only through it"},
		"Requires remote access": {[]UnitProps{unit("tailscaled.service", nil)}, "Requires", "tailscaled.service", "tailscaled.service is started at boot only through it"},
		"BindsTo SSH":            {[]UnitProps{unit("ssh.service", nil)}, "BindsTo", "ssh.service", "ssh.service is started at boot only through it"},
		"Upholds VPN":            {[]UnitProps{unit("openvpn-client@site.service", nil)}, "Upholds", "openvpn-client@site.service", "is protected"},
		// Transitive: demo → helper (unprotected) → tailscaled.
		"transitive": {[]UnitProps{unit("helper.service", map[string][]string{"Requires": {"tailscaled.service"}}), unit("tailscaled.service", nil)},
			"Wants", "helper.service", "demo.service → helper.service → tailscaled.service"},
		// Alias: demo wants myvpn.service, a name of tailscaled.service.
		"alias": {[]UnitProps{unit("tailscaled.service", nil, "myvpn.service")}, "Wants", "myvpn.service", "tailscaled.service is started at boot only through it"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := disableEnv(t, c.extra...)
			pulls(e, "demo.service", c.key, c.target)
			links := linksUnder(t, e.files.root)
			states := map[string]string{"demo.service": "active"}
			for _, u := range c.extra {
				states[u.ID] = "active"
			}
			_, err := e.eplan(t, "demo.service", "disable")
			if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), c.message) {
				t.Fatalf("got %v, want refusal containing %q", err, c.message)
			}
			unchanged(t, e, links, states)
		})
	}
}

func TestDisableAllowedWhenTheProtectedUnitIsStartedOtherwise(t *testing.T) {
	// Anchored: the boot target wants wg-quick@wg0 itself (another enabled unit).
	e := disableEnv(t, unit("wg-quick@wg0.service", nil), unit("other.service", map[string][]string{"Wants": {"wg-quick@wg0.service"}}))
	pulls(e, "demo.service", "Wants", "wg-quick@wg0.service")
	pulls(e, "multi-user.target", "Wants", "other.service")
	p, err := e.eplan(t, "demo.service", "disable")
	if err != nil {
		t.Fatalf("anchored: %v", err)
	}
	if !strings.Contains(strings.Join(p.Consequences, " "), "No other unit is started at boot only through it") {
		t.Fatalf("consequences: %v", p.Consequences)
	}
	// Socket activation: sockets.target → ssh.socket → Triggers ssh.service.
	e = disableEnv(t, unit("ssh.service", nil), unit("sockets.target", map[string][]string{"Wants": {"ssh.socket"}}),
		unit("ssh.socket", map[string][]string{"Triggers": {"ssh.service"}}))
	pulls(e, "demo.service", "Wants", "ssh.service")
	pulls(e, "multi-user.target", "Wants", "sockets.target")
	if _, err := e.eplan(t, "demo.service", "disable"); err != nil {
		t.Fatalf("socket-activated: %v", err)
	}
	// An unprotected unit started only through demo is allowed and listed.
	e = disableEnv(t, unit("helper.service", nil))
	pulls(e, "demo.service", "Wants", "helper.service")
	p, err = e.eplan(t, "demo.service", "disable")
	if err != nil || !strings.Contains(strings.Join(p.Consequences, " "), "helper.service") {
		t.Fatalf("unprotected: %+v %v", p.Consequences, err)
	}
	// Ordinary unit with default dependencies on a fresh host: allowed.
	e = disableEnv(t, unit("sysinit.target", map[string][]string{"Wants": {"systemd-journald.service"}}), unit("systemd-journald.service", nil))
	pulls(e, "multi-user.target", "Requires", "sysinit.target")
	pulls(e, "demo.service", "Requires", "sysinit.target")
	if _, err := e.eplan(t, "demo.service", "disable"); err != nil {
		t.Fatalf("default dependencies: %v", err)
	}
}

func TestDisableBootPullFailsClosed(t *testing.T) {
	// The boot target cannot be resolved.
	e := disableEnv(t)
	e.fake.mu.Lock()
	delete(e.fake.units, "default.target")
	e.fake.mu.Unlock()
	if _, err := e.eplan(t, "demo.service", "disable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "boot target") {
		t.Fatalf("no boot target: %v", err)
	}
	// A service that cannot be inspected.
	e = disableEnv(t)
	pulls(e, "demo.service", "Wants", `vpn\x2dsite.service`)
	if _, err := e.eplan(t, "demo.service", "disable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "cannot be inspected") {
		t.Fatalf("uninspectable: %v", err)
	}
	// The walk exceeds its budget.
	saved := maxBootWalk
	defer func() { maxBootWalk = saved }()
	maxBootWalk = 3
	e = disableEnv(t, unit("a.service", nil), unit("b.service", nil), unit("c.service", nil))
	pulls(e, "multi-user.target", "Wants", "a.service", "b.service", "c.service")
	if _, err := e.eplan(t, "demo.service", "disable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "more than 3 units") {
		t.Fatalf("budget: %v", err)
	}
}

// Changes between preview and apply invalidate the plan; nothing runs.
func TestDisableBootPullStalePlan(t *testing.T) {
	// The loaded graph changes: the other puller of wg-quick goes away.
	e := disableEnv(t, unit("wg-quick@wg0.service", nil))
	pulls(e, "demo.service", "Wants", "wg-quick@wg0.service")
	pulls(e, "multi-user.target", "Wants", "wg-quick@wg0.service")
	p, err := e.eplan(t, "demo.service", "disable")
	if err != nil {
		t.Fatal(err)
	}
	unpull(e, "multi-user.target", "Wants")
	links := linksUnder(t, e.files.root)
	_, err = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "disable", p.Token)
	if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "started at boot only through it") {
		t.Fatalf("apply after the graph changed: %v", err)
	}
	unchanged(t, e, links, map[string]string{"demo.service": "active"})
	// An unprotected unit that becomes started only through demo (its other
	// puller goes away; demo itself is unchanged) changes the digest.
	e = disableEnv(t, unit("helper.service", nil))
	pulls(e, "demo.service", "Wants", "helper.service")
	pulls(e, "multi-user.target", "Wants", "helper.service")
	p, err = e.eplan(t, "demo.service", "disable")
	if err != nil {
		t.Fatal(err)
	}
	unpull(e, "multi-user.target", "Wants")
	if _, err = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "disable", p.Token); code(err) != proto.CodeConflict {
		t.Fatalf("apply after a new boot-only unit: %v", err)
	}
	// A unit file written on disk after the review: the change gate.
	e = disableEnv(t)
	p, err = e.eplan(t, "demo.service", "disable")
	if err != nil {
		t.Fatal(err)
	}
	// Another unit's file (systemd has not loaded it): the boot graph on disk differs.
	writeUnit(t, e.files.root, "/etc/systemd/system/multi-user.target.d/zz.conf", "[Unit]\nWants=other.service\n")
	if _, err = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "disable", p.Token); code(err) != proto.CodePolicyDenied ||
		!strings.Contains(err.Error(), gateMsg) {
		t.Fatalf("apply after an on-disk change: %v", err)
	}
	if e.files.state["demo.service"] != "enabled" {
		t.Fatal("changed")
	}
	_ = os.RemoveAll(filepath.Join(e.files.root, "/etc/systemd/system/multi-user.target.d"))
}

// The loaded graph does not show Homelab Control's own --no-reload changes:
// after it disables one of two pullers, systemd still believes that puller
// starts the protected unit. Disabling the last puller must be refused.
func TestDisableBootPullSeesOwnUnloadedDisables(t *testing.T) {
	e := newEnablementEnv(t, svc("demo.service", "active"), svc("other.service", "active"))
	e.tt = t
	addUnitFile(t, e, unit("wg-quick@wg0.service", nil))
	pulls(e, "demo.service", "Wants", "wg-quick@wg0.service")
	pulls(e, "other.service", "Wants", "wg-quick@wg0.service")
	// Both are enabled by their [Install] links (persistent); the loaded
	// multi-user.target shows them too.
	e.fake.mu.Lock()
	e.fake.units["multi-user.target"].Deps["Wants"] = []string{"demo.service", "other.service"}
	e.fake.mu.Unlock()
	for _, u := range []string{"demo.service", "other.service"} {
		e.files.setLinks(u, true, -1)
		e.files.state[u] = "enabled"
	}
	e.files.reload()
	p, err := e.eplan(t, "other.service", "disable")
	if err != nil {
		t.Fatalf("first puller (the other still anchors it): %v", err)
	}
	if o, err := e.eapply(t, withStepUp(e.env, time.Now()), "other.service", "disable", p.Token); err != nil || o.Outcome != "succeeded" {
		t.Fatalf("%+v %v", o, err)
	}
	// systemd's loaded multi-user.target still Wants other.service (no reload).
	_, err = e.eplan(t, "demo.service", "disable")
	if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "wg-quick@wg0.service is started at boot only through it") {
		t.Fatalf("last puller after an own unloaded disable: %v", err)
	}
	// And an own unloaded enable of another puller anchors it again.
	p, err = e.eplan(t, "other.service", "enable")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.eapply(t, withStepUp(e.env, time.Now()), "other.service", "enable", p.Token); err != nil {
		t.Fatal(err)
	}
	e.fake.mu.Lock()
	e.fake.units["multi-user.target"].Deps["Wants"] = []string{"demo.service"} // loaded: other's link not known
	e.fake.mu.Unlock()
	if _, err := e.eplan(t, "demo.service", "disable"); err != nil {
		t.Fatalf("own unloaded enable of another puller not counted: %v", err)
	}
}

// A boot-graph walk that fails or times out at apply (revalidation) fails
// closed: nothing runs, no intent is journaled, links and states unchanged.
func TestDisableBootPullTimeoutAtApplyFailsClosed(t *testing.T) {
	e := disableEnv(t, unit("helper.service", nil))
	pulls(e, "demo.service", "Wants", "helper.service")
	p, err := e.eplan(t, "demo.service", "disable")
	if err != nil {
		t.Fatal(err)
	}
	links := linksUnder(t, e.files.root)
	e.fake.manyErr = runner.ErrTimeout
	_, err = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "disable", p.Token)
	if err == nil || !strings.Contains(err.Error(), "time") {
		t.Fatalf("apply with a timed-out walk: %v", err)
	}
	unchanged(t, e, links, map[string]string{"demo.service": "active"})
	for _, k := range journalKinds(t, e.journal) {
		if k == "op.intent" {
			t.Fatal("an intent was journaled although revalidation failed")
		}
	}
}
