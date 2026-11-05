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
	. "homelab-control/backend/internal/broker/types"
)

// Runtime unit configuration (B-2, probe 11). The netplan entries below are
// exactly what netplan 1.1.2 writes on Ubuntu 24.04 (the GitHub runner and
// the ubuntu-2404-netplan image): a runtime unit, its runtime .wants link and
// a runtime drop-in overriding ExecStart= of systemd-networkd-wait-online.

func writeLink(t *testing.T, root, path, target string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, full); err != nil {
		t.Fatal(err)
	}
}

// netplanRuntime adds netplan's runtime entries (disk and loaded graph).
func netplanRuntime(t *testing.T, e *eenv) {
	t.Helper()
	r := e.files.root
	writeUnit(t, r, "/run/systemd/system/netplan-ovs-cleanup.service",
		"[Unit]\nDescription=OpenVSwitch configuration for cleanup\nDefaultDependencies=no\nWants=ovsdb-server.service\nWants=network.target\n[Service]\nType=oneshot\nExecStart=/usr/sbin/netplan apply --only-ovs-cleanup\n")
	writeLink(t, r, "/run/systemd/system/systemd-networkd.service.wants/netplan-ovs-cleanup.service", "/run/systemd/system/netplan-ovs-cleanup.service")
	writeUnit(t, r, "/run/systemd/system/systemd-networkd-wait-online.service.d/10-netplan.conf",
		"[Unit]\nConditionPathIsSymbolicLink=/run/systemd/generator/network-online.target.wants/systemd-networkd-wait-online.service\n[Service]\nExecStart=\nExecStart=/lib/systemd/systemd-networkd-wait-online -i eth0:degraded\n")
	addUnit(e.fake, UnitProps{ID: "netplan-ovs-cleanup.service", Names: []string{"netplan-ovs-cleanup.service"}, LoadState: "loaded",
		ActiveState: "inactive", SubState: "dead", FragmentPath: "/run/systemd/system/netplan-ovs-cleanup.service",
		Deps: map[string][]string{"Wants": {"network.target", "ovsdb-server.service"}}})
	addUnitFile(t, e, unit("systemd-networkd.service", map[string][]string{"Wants": {"netplan-ovs-cleanup.service"}}))
	wo := unit("systemd-networkd-wait-online.service", nil)
	wo.DropInPaths = []string{"/run/systemd/system/systemd-networkd-wait-online.service.d/10-netplan.conf"}
	addUnitFile(t, e, wo)
	writeLink(t, r, "/etc/systemd/system/multi-user.target.wants/systemd-networkd.service", "/usr/lib/systemd/system/systemd-networkd.service")
	e.files.reload()
}

// 1. Unrelated netplan runtime entries permit ordinary enable and disable.
func TestNetplanRuntimeEntriesDoNotBlockOrdinaryChanges(t *testing.T) {
	e := newEnablementEnv(t)
	netplanRuntime(t, e)
	p, err := e.eplan(t, "demo.service", "enable")
	if err != nil {
		t.Fatalf("enable with netplan runtime entries: %v", err)
	}
	if o, err := e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "enable", p.Token); err != nil || o.Outcome != OutcomeSucceeded {
		t.Fatalf("%+v %v", o, err)
	}
	p, err = e.eplan(t, "demo.service", "disable")
	if err != nil {
		t.Fatalf("disable with netplan runtime entries: %v", err)
	}
	if o, err := e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "disable", p.Token); err != nil || o.Outcome != OutcomeSucceeded {
		t.Fatalf("%+v %v", o, err)
	}
	// A unit that wants systemd-networkd-wait-online (with netplan's runtime
	// drop-in, which overrides no safety-relevant setting): allowed.
	e = newEnablementEnv(t, svc("demo.service", "inactive"))
	netplanRuntime(t, e)
	e.fake.mu.Lock()
	e.fake.units["systemd-networkd-wait-online.service"].ActiveState = "inactive"
	e.fake.mu.Unlock()
	pulls(e, "demo.service", "Wants", "systemd-networkd-wait-online.service")
	if _, err := e.eplan(t, "demo.service", "enable"); err != nil {
		t.Fatalf("enable of a unit wanting wait-online: %v", err)
	}
}

// 2. Runtime aliases, drop-ins, masks and unit files of units the review
// reaches are evaluated or refused (enable: the start walk).
func TestEnableRuntimeConfigurationOfEvaluatedUnits(t *testing.T) {
	cases := map[string]struct {
		setup func(t *testing.T, e *eenv)
		want  string
	}{
		"runtime mask on a pulled-in unit": {func(t *testing.T, e *eenv) {
			h := unit("helper.service", nil)
			h.LoadState, h.ActiveState, h.FragmentPath = "masked", "inactive", "/run/systemd/system/helper.service"
			addUnit(e.fake, h)
			writeUnit(t, e.files.root, "/usr/lib/systemd/system/helper.service", "[Unit]\nConflicts=ssh.service\n")
			writeLink(t, e.files.root, "/run/systemd/system/helper.service", "/dev/null")
			pulls(e, "demo.service", "Wants", "helper.service")
		}, "masked only until the next boot"},
		"runtime drop-in hides FailureAction=reboot": {func(t *testing.T, e *eenv) {
			h := unit("helper.service", nil)
			h.ActiveState = "inactive"
			h.DropInPaths = []string{"/run/systemd/system/helper.service.d/x.conf"}
			addUnit(e.fake, h) // loaded FailureAction: none (the drop-in)
			writeUnit(t, e.files.root, h.FragmentPath, "[Unit]\nFailureAction=reboot\n[Service]\nExecStart=/bin/true\n")
			writeUnit(t, e.files.root, "/run/systemd/system/helper.service.d/x.conf", "[Unit]\nFailureAction=none\n")
			pulls(e, "demo.service", "Wants", "helper.service")
		}, "at the next boot helper.service has FailureAction=reboot"},
		"runtime unit file shadows a persistent one": {func(t *testing.T, e *eenv) {
			h := unit("helper.service", nil)
			h.ActiveState, h.FragmentPath = "inactive", "/run/systemd/system/helper.service"
			addUnit(e.fake, h)
			writeUnit(t, e.files.root, "/run/systemd/system/helper.service", "[Service]\nExecStart=/bin/true\n")
			writeUnit(t, e.files.root, "/usr/lib/systemd/system/helper.service", "[Unit]\nConflicts=ssh.service\n")
			pulls(e, "demo.service", "Wants", "helper.service")
		}, "only until the next boot; at boot its persistent unit file applies"},
		"runtime alias over a persistent unit name": {func(t *testing.T, e *eenv) {
			h := unit("helper.service", nil, "other-name.service")
			h.ActiveState = "inactive"
			addUnit(e.fake, h)
			writeUnit(t, e.files.root, h.FragmentPath, "[Service]\nExecStart=/bin/true\n")
			writeLink(t, e.files.root, "/run/systemd/system/other-name.service", h.FragmentPath)
			writeUnit(t, e.files.root, "/usr/lib/systemd/system/other-name.service", "[Unit]\nConflicts=ssh.service\n")
			pulls(e, "demo.service", "Wants", "other-name.service")
		}, "only until the next boot (runtime alias)"},
		"unreadable persistent files of a unit with runtime drop-ins": {func(t *testing.T, e *eenv) {
			h := unit("helper.service", nil)
			h.ActiveState = "inactive"
			h.DropInPaths = []string{"/run/systemd/system/helper.service.d/x.conf"}
			addUnit(e.fake, h)
			writeUnit(t, e.files.root, h.FragmentPath, "[Unit]\nFailureAction=none\n")
			writeUnit(t, e.files.root, "/run/systemd/system/helper.service.d/x.conf", "[Service]\nNice=1\n")
			_ = os.Chmod(filepath.Join(e.files.root, h.FragmentPath), 0)
			pulls(e, "demo.service", "Wants", "helper.service")
		}, "cannot be evaluated"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnablementEnv(t, svc("demo.service", "inactive"))
			e.tt = t
			c.setup(t, e)
			links := linksUnder(t, e.files.root)
			_, err := e.eplan(t, "demo.service", "enable")
			if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want refusal containing %q", err, c.want)
			}
			if len(e.files.changes) != 0 || len(e.fake.acts) != 0 || strings.Join(linksUnder(t, e.files.root), ",") != strings.Join(links, ",") {
				t.Fatal("changed")
			}
		})
	}
	// A runtime drop-in that overrides no safety-relevant setting: allowed.
	e := newEnablementEnv(t, svc("demo.service", "inactive"))
	e.tt = t
	h := unit("helper.service", nil)
	h.ActiveState = "inactive"
	h.DropInPaths = []string{"/run/systemd/system/helper.service.d/x.conf"}
	addUnitFile(t, e, h)
	writeUnit(t, e.files.root, "/run/systemd/system/helper.service.d/x.conf", "[Service]\nExecStart=\nExecStart=/bin/true\n")
	pulls(e, "demo.service", "Wants", "helper.service")
	if _, err := e.eplan(t, "demo.service", "enable"); err != nil {
		t.Fatalf("harmless runtime drop-in: %v", err)
	}
}

// 3. Disable: a protected unit started at boot by something else only through
// runtime configuration (a runtime link, a runtime alias, a runtime-only
// unit, or files that cannot be evaluated) is not anchored; the persistent
// path is.
func TestDisableRuntimeAnchorsDoNotCount(t *testing.T) {
	type setup func(t *testing.T, e *eenv)
	wg := func(t *testing.T, e *eenv) {
		addUnitFile(t, e, unit("wg-quick@wg0.service", nil))
		pulls(e, "demo.service", "Wants", "wg-quick@wg0.service")
	}
	refused := map[string]setup{
		"runtime .wants link": func(t *testing.T, e *eenv) {
			writeLink(t, e.files.root, "/run/systemd/system/multi-user.target.wants/wg-quick@wg0.service", "/usr/lib/systemd/system/wg-quick@wg0.service")
			e.fake.mu.Lock()
			e.fake.units["multi-user.target"].Deps["Wants"] = []string{"wg-quick@wg0.service"} // loaded
			e.fake.mu.Unlock()
		},
		"runtime drop-in Wants=": func(t *testing.T, e *eenv) {
			writeUnit(t, e.files.root, "/run/systemd/system/multi-user.target.d/x.conf", "[Unit]\nWants=wg-quick@wg0.service\n")
			e.fake.mu.Lock()
			mu := e.fake.units["multi-user.target"]
			mu.Deps["Wants"] = []string{"wg-quick@wg0.service"}
			mu.DropInPaths = []string{"/run/systemd/system/multi-user.target.d/x.conf"}
			e.fake.mu.Unlock()
		},
		"anchor through a runtime alias": func(t *testing.T, e *eenv) {
			o := unit("other.service", nil)
			addUnitFile(t, e, o)
			pulls(e, "other.service", "Wants", "vpn-alias.service")
			writeLink(t, e.files.root, "/run/systemd/system/vpn-alias.service", "/usr/lib/systemd/system/wg-quick@wg0.service")
			e.fake.mu.Lock()
			w := e.fake.units["wg-quick@wg0.service"]
			w.Names = append(w.Names, "vpn-alias.service")
			e.fake.units["vpn-alias.service"] = w
			e.fake.mu.Unlock()
			writeLink(t, e.files.root, "/etc/systemd/system/multi-user.target.wants/other.service", o.FragmentPath)
		},
		"runtime-only anchor unit": func(t *testing.T, e *eenv) {
			o := unit("other.service", map[string][]string{"Wants": {"wg-quick@wg0.service"}})
			o.FragmentPath = "/run/systemd/system/other.service"
			addUnitFile(t, e, o)
			writeLink(t, e.files.root, "/etc/systemd/system/multi-user.target.wants/other.service", o.FragmentPath)
		},
		"anchor whose persistent file cannot be read": func(t *testing.T, e *eenv) {
			o := unit("other.service", map[string][]string{"Wants": {"wg-quick@wg0.service"}})
			addUnitFile(t, e, o)
			writeLink(t, e.files.root, "/etc/systemd/system/multi-user.target.wants/other.service", o.FragmentPath)
			_ = os.Chmod(filepath.Join(e.files.root, o.FragmentPath), 0)
		},
		"anchor with a specifier in its Wants=": func(t *testing.T, e *eenv) {
			o := unit("other.service", nil)
			addUnitFile(t, e, o)
			// A valid Wants= line, but another relevant key cannot be evaluated:
			// the unit's boot relations are uncertain, so it anchors nothing.
			writeUnit(t, e.files.root, o.FragmentPath, "[Unit]\nWants=wg-quick@wg0.service\nRequires=helper@%i.service\n")
			writeLink(t, e.files.root, "/etc/systemd/system/multi-user.target.wants/other.service", o.FragmentPath)
		},
	}
	for name, s := range refused {
		t.Run(name, func(t *testing.T) {
			e := disableEnv(t)
			wg(t, e)
			s(t, e)
			e.files.reload()
			links := linksUnder(t, e.files.root)
			_, err := e.eplan(t, "demo.service", "disable")
			// An unreadable unit file is refused earlier by the Requisite= disk
			// scan; both refusals fail closed.
			if code(err) != proto.CodePolicyDenied || !(strings.Contains(err.Error(), "wg-quick@wg0.service is started at boot only through it") ||
				(name == "anchor whose persistent file cannot be read" && strings.Contains(err.Error(), "could not be checked"))) {
				t.Fatalf("got %v", err)
			}
			unchanged(t, e, links, map[string]string{"demo.service": "active", "wg-quick@wg0.service": "active"})
		})
	}
	// The persistent path anchors: another enabled unit's persistent Wants=.
	e := disableEnv(t)
	wg(t, e)
	o := unit("other.service", map[string][]string{"Wants": {"wg-quick@wg0.service"}})
	addUnitFile(t, e, o)
	writeLink(t, e.files.root, "/etc/systemd/system/multi-user.target.wants/other.service", o.FragmentPath)
	e.files.reload()
	if _, err := e.eplan(t, "demo.service", "disable"); err != nil {
		t.Fatalf("persistent anchor: %v", err)
	}
}

// Disable: runtime configuration on a unit the disabled unit reaches.
func TestDisableRuntimeConfigurationOfReachedUnits(t *testing.T) {
	cases := map[string]struct {
		setup func(t *testing.T, e *eenv)
		want  string
	}{
		"runtime mask": {func(t *testing.T, e *eenv) {
			h := unit("helper.service", nil)
			h.LoadState = "masked"
			addUnit(e.fake, h)
			writeUnit(t, e.files.root, h.FragmentPath, "[Unit]\nWants=wg-quick@wg0.service\n")
			writeLink(t, e.files.root, "/run/systemd/system/helper.service", "/dev/null")
			pulls(e, "demo.service", "Wants", "helper.service")
		}, "masked only until the next boot"},
		"runtime unit file shadowing a persistent one": {func(t *testing.T, e *eenv) {
			h := unit("helper.service", nil)
			h.FragmentPath = "/run/systemd/system/helper.service"
			addUnit(e.fake, h)
			writeUnit(t, e.files.root, h.FragmentPath, "[Service]\nExecStart=/bin/true\n")
			writeUnit(t, e.files.root, "/usr/lib/systemd/system/helper.service", "[Unit]\nWants=wg-quick@wg0.service\n")
			pulls(e, "demo.service", "Wants", "helper.service")
		}, "its persistent unit file was not evaluated"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := disableEnv(t)
			c.setup(t, e)
			e.files.reload()
			links := linksUnder(t, e.files.root)
			_, err := e.eplan(t, "demo.service", "disable")
			if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
			unchanged(t, e, links, map[string]string{"demo.service": "active"})
		})
	}
	// A persistent mask on a reached unit is a boot-time fact: allowed.
	e := disableEnv(t)
	h := unit("helper.service", nil)
	h.LoadState = "masked"
	addUnit(e.fake, h)
	writeLink(t, e.files.root, "/etc/systemd/system/helper.service", "/dev/null")
	pulls(e, "demo.service", "Wants", "helper.service")
	if _, err := e.eplan(t, "demo.service", "disable"); err != nil {
		t.Fatalf("persistent mask: %v", err)
	}
}

// 5. A runtime change between preview and apply that makes the reached graph
// uncertain invalidates the plan.
func TestRuntimeChangeBetweenPreviewAndApply(t *testing.T) {
	e := disableEnv(t, unit("helper.service", nil))
	pulls(e, "demo.service", "Wants", "helper.service")
	p, err := e.eplan(t, "demo.service", "disable")
	if err != nil {
		t.Fatal(err)
	}
	// helper.service is masked at runtime (`systemctl mask --runtime`, which reloads).
	writeLink(t, e.files.root, "/run/systemd/system/helper.service", "/dev/null")
	e.fake.mu.Lock()
	e.fake.units["helper.service"].LoadState = "masked"
	e.fake.mu.Unlock()
	e.files.reload()
	links := linksUnder(t, e.files.root)
	_, err = e.eapply(t, withStepUp(e.env, time.Now()), "demo.service", "disable", p.Token)
	if code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "masked only until the next boot") || !strings.Contains(err.Error(), "Nothing was done") {
		t.Fatalf("apply after a runtime mask: %v", err)
	}
	unchanged(t, e, links, map[string]string{"demo.service": "active"})
}

// 6. Oversized relevant persistent configuration fails closed.
func TestRuntimeEvaluationBounds(t *testing.T) {
	e := disableEnv(t)
	addUnitFile(t, e, unit("wg-quick@wg0.service", nil))
	pulls(e, "demo.service", "Wants", "wg-quick@wg0.service")
	for i := 0; i <= maxLinkDirEntries; i++ {
		writeLink(t, e.files.root, "/etc/systemd/system/multi-user.target.wants/x"+strings.Repeat("y", 3)+string(rune('a'+i%26))+itoa(i)+".service", "/dev/null")
	}
	e.files.reload()
	if _, err := e.eplan(t, "demo.service", "disable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "within bounds") {
		t.Fatalf("oversized link directory: %v", err)
	}
}

func itoa(i int) string {
	s := ""
	for {
		s = string(rune('0'+i%10)) + s
		i /= 10
		if i == 0 {
			return s
		}
	}
}

// A unit masked only until the next boot whose persistent file conflicts with
// the enabled unit: invisible in the loaded graph, applies at boot.
func TestEnableRuntimeMaskedConflict(t *testing.T) {
	e := newEnablementEnv(t, svc("demo.service", "inactive"))
	e.tt = t
	writeUnit(t, e.files.root, "/usr/lib/systemd/system/ssh-extra.service", "[Unit]\nConflicts=demo.service\n[Service]\nExecStart=/bin/true\n")
	writeLink(t, e.files.root, "/run/systemd/system/ssh-extra.service", "/dev/null")
	e.files.reload()
	links := linksUnder(t, e.files.root)
	if _, err := e.eplan(t, "demo.service", "enable"); code(err) != proto.CodePolicyDenied || !strings.Contains(err.Error(), "ssh-extra.service is masked only until the next boot") {
		t.Fatalf("runtime-masked conflicting unit: %v", err)
	}
	if strings.Join(linksUnder(t, e.files.root), ",") != strings.Join(links, ",") || len(e.files.changes) != 0 {
		t.Fatal("changed")
	}
	// A runtime mask of an unrelated unit: allowed.
	e = newEnablementEnv(t, svc("demo.service", "inactive"))
	writeUnit(t, e.files.root, "/usr/lib/systemd/system/unrelated.service", "[Unit]\nConflicts=other.service\n")
	writeLink(t, e.files.root, "/run/systemd/system/unrelated.service", "/dev/null")
	e.files.reload()
	if _, err := e.eplan(t, "demo.service", "enable"); err != nil {
		t.Fatalf("unrelated runtime mask: %v", err)
	}
}
