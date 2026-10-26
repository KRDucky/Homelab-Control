// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package servicepolicy is the built-in protected-unit policy for systemd
// service operations (owner decision D-5, option A). It is pure code: no
// process execution, no I/O. The broker enforces it authoritatively, together
// with checks that need the unit's observed properties (aliases, templates,
// RefuseManualStop, dependencies). hc-api uses NameProtection only to label
// units and disable controls in the UI; a UI or API bypass changes nothing,
// because the broker re-checks every name.
//
// The policy is fixed in code for M2-2: there are no user-editable overrides.
// Changing it requires review (ADR 0011).
package servicepolicy

import (
	"regexp"
	"strings"
)

// Action is one approved service verb.
type Action string

// The verbs authorized for M2-2. Nothing else (enable, disable, mask, reload,
// daemon-reload, …) is accepted anywhere.
const (
	Start   Action = "start"
	Stop    Action = "stop"
	Restart Action = "restart"
)

// ValidAction reports whether a is one of the approved verbs.
func ValidAction(a string) bool {
	switch Action(a) {
	case Start, Stop, Restart:
		return true
	}
	return false
}

// UnitRE is the grammar for a unit that a write operation may target: a
// .service unit, no leading '-', no systemd escapes ('\'), no path separators,
// at most 100 characters before the suffix. Uninstantiated templates
// ("name@.service") are rejected separately by ValidUnit.
var UnitRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.@-]{0,99}\.service$`)

// ValidUnit reports whether name is an acceptable write target name.
func ValidUnit(name string) bool {
	return UnitRE.MatchString(name) && !strings.Contains(name, "@.service") && !strings.Contains(name, "..")
}

// Protection explains why a unit is protected.
type Protection struct {
	Protected bool   `json:"protected"`
	Category  string `json:"category,omitempty"`
	Reason    string `json:"reason,omitempty"`
	// Matched is the name (alias or template base) that matched a rule.
	Matched string `json:"matched,omitempty"`
}

type rule struct {
	match    string // lower-case
	prefix   bool
	category string
}

// Categories and their user-facing reasons.
var reasons = map[string]string{
	"self":         "Homelab Control's own units: stopping them would end the session that is managing them.",
	"remote":       "Remote login (SSH): stopping or restarting it can lock you out of the host.",
	"ipc":          "D-Bus and polkit: core system message bus; stopping it breaks logins and most services.",
	"systemd":      "Core systemd infrastructure (journal, login, udev, networkd, resolved, power and boot units).",
	"console":      "Console login (getty) and boot splash: needed for local recovery access.",
	"session":      "User-session infrastructure: stopping it ends logged-in sessions.",
	"network":      "Network management and configuration: stopping it can disconnect the host.",
	"firewall":     "Firewall: stopping or restarting it can expose or block the host.",
	"remoteaccess": "Remote-access tunnels (Tailscale, WireGuard, ZeroTier, Cloudflare Tunnel): stopping one can lock you out.",
	"display":      "Display managers: stopping one ends graphical sessions.",
	"storage":      "Storage infrastructure (ZFS, LVM, md, multipath, iSCSI): storage changes are out of scope for service operations.",
	"security":     "Security auditing (auditd).",
	"power":        "Power, reboot and recovery-mode units: host shutdown and reboot are not authorized.",
	"identity":     "Network login and identity services (SSSD, LDAP, Winbind): stopping them can prevent logins.",
}

// rules is the fixed deny-list. Matching is case-insensitive against the unit
// base name (without ".service"), every alias, and every template base
// ("getty@" for "getty@tty1.service").
var rules = []rule{
	{"homelab-control", true, "self"},

	{"ssh", false, "remote"}, {"ssh@", false, "remote"}, {"sshd", true, "remote"},
	{"dropbear", true, "remote"}, {"openssh", true, "remote"},

	{"dbus", true, "ipc"}, {"polkit", true, "ipc"}, {"polkitd", false, "ipc"},

	{"systemd-", true, "systemd"}, {"init", false, "systemd"},
	{"emergency", false, "power"}, {"rescue", false, "power"},
	{"reboot", false, "power"}, {"poweroff", false, "power"}, {"halt", false, "power"},
	{"kexec", false, "power"},

	{"getty@", false, "console"}, {"serial-getty@", false, "console"},
	{"container-getty@", false, "console"}, {"console-getty", false, "console"},
	{"autovt@", false, "console"}, {"plymouth", true, "console"},

	{"user@", false, "session"}, {"user-runtime-dir@", false, "session"},
	{"accounts-daemon", false, "session"},

	{"networkmanager", true, "network"}, {"networking", false, "network"}, {"network", false, "network"},
	{"wpa_supplicant", true, "network"}, {"iwd", false, "network"}, {"connman", true, "network"},
	{"wicked", true, "network"}, {"netplan-", true, "network"}, {"ifup@", false, "network"},
	{"ifupdown", true, "network"}, {"dhcpcd", true, "network"}, {"dhclient", true, "network"},
	{"resolvconf", true, "network"}, {"network-manager", false, "network"}, {"netctl", true, "network"},
	{"ovs-vswitchd", false, "network"}, {"ovsdb-server", false, "network"}, {"openvswitch", true, "network"},

	{"firewalld", false, "firewall"}, {"ufw", false, "firewall"}, {"nftables", false, "firewall"},
	{"iptables", false, "firewall"}, {"ip6tables", false, "firewall"}, {"ebtables", false, "firewall"},
	{"netfilter-persistent", false, "firewall"}, {"shorewall", true, "firewall"},

	{"tailscaled", false, "remoteaccess"}, {"zerotier-one", false, "remoteaccess"},
	{"wg-quick@", false, "remoteaccess"}, {"netbird", false, "remoteaccess"},
	{"cloudflared", true, "remoteaccess"}, {"openvpn", true, "remoteaccess"},
	{"strongswan", true, "remoteaccess"}, {"ipsec", true, "remoteaccess"}, {"nebula", true, "remoteaccess"},

	{"display-manager", false, "display"}, {"gdm", false, "display"}, {"gdm3", false, "display"},
	{"sddm", false, "display"}, {"lightdm", false, "display"}, {"lxdm", false, "display"},
	{"xdm", false, "display"}, {"ly", false, "display"}, {"greetd", false, "display"},
	{"slim", false, "display"}, {"nodm", false, "display"},

	{"zfs-", true, "storage"}, {"zfs", false, "storage"}, {"lvm2-", true, "storage"},
	{"mdmonitor", true, "storage"}, {"mdadm", true, "storage"}, {"multipathd", false, "storage"},
	{"iscsid", false, "storage"}, {"iscsi", false, "storage"}, {"open-iscsi", false, "storage"},
	{"blk-availability", false, "storage"}, {"dm-event", false, "storage"}, {"lvm2", false, "storage"},

	{"auditd", false, "security"},

	{"sssd", true, "identity"}, {"nslcd", false, "identity"}, {"winbind", false, "identity"},
	{"nscd", false, "identity"}, {"slapd", false, "identity"},
}

// unitTypes are the suffixes stripped before matching, so that a rule covers
// every unit type of the same name (ssh.service, ssh.socket; network.target).
var unitTypes = []string{".service", ".socket", ".target", ".path", ".timer", ".mount", ".automount",
	".swap", ".slice", ".scope", ".device"}

// candidates returns the strings a unit name is matched as: the base name
// (without its unit type) and, for template instances, the template base
// ending in '@'.
func candidates(name string) []string {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, t := range unitTypes {
		if strings.HasSuffix(n, t) {
			n = strings.TrimSuffix(n, t)
			break
		}
	}
	out := []string{n}
	if i := strings.IndexByte(n, '@'); i >= 0 {
		out = append(out, n[:i+1])
	}
	return out
}

// NameProtection applies the deny-list to one unit name.
func NameProtection(name string) Protection {
	for _, c := range candidates(name) {
		for _, r := range rules {
			if (r.prefix && strings.HasPrefix(c, r.match)) || (!r.prefix && c == r.match) {
				return Protection{Protected: true, Category: r.category, Reason: reasons[r.category], Matched: name}
			}
		}
	}
	return Protection{}
}

// NamesProtection applies the deny-list to a unit's canonical ID, every alias
// (systemd "Names") and the basename of its unit file. Any match protects the
// unit: an alias cannot be used to reach a protected unit.
func NamesProtection(names ...string) Protection {
	for _, n := range names {
		if n == "" {
			continue
		}
		if i := strings.LastIndexByte(n, '/'); i >= 0 { // unit file path
			n = n[i+1:]
		}
		if p := NameProtection(n); p.Protected {
			return p
		}
	}
	return Protection{}
}

// DangerousTarget reports whether a unit pulled in or triggered by a start
// would shut down, reboot, suspend or isolate the host.
func DangerousTarget(name string) bool {
	n := strings.ToLower(name)
	base, _, _ := strings.Cut(n, ".")
	switch base {
	case "reboot", "poweroff", "halt", "kexec", "soft-reboot", "shutdown", "final", "exit",
		"emergency", "rescue", "suspend", "hibernate", "hybrid-sleep", "suspend-then-hibernate",
		"sleep", "ctrl-alt-del",
		"systemd-reboot", "systemd-poweroff", "systemd-halt", "systemd-kexec", "systemd-soft-reboot",
		"systemd-suspend", "systemd-hibernate", "systemd-hybrid-sleep", "systemd-suspend-then-hibernate":
		return true
	}
	return false
}

// SafeEmergencyAction reports whether a systemd *Action= property value (for
// example FailureAction=, SuccessAction=) cannot reboot or power off the host.
func SafeEmergencyAction(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || v == "none"
}
