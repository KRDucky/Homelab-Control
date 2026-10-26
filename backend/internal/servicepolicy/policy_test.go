// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package servicepolicy

import (
	"strings"
	"testing"
)

func TestProtectedNames(t *testing.T) {
	for _, n := range []string{
		"homelab-control.service", "homelab-control-broker.service", "ssh.service", "sshd.service",
		"sshd@0-1.2.3.4:22-5.6.7.8:9.service", "ssh@x.service", "sshd-keygen@rsa.service", "dbus.service",
		"dbus-broker.service", "polkit.service", "systemd-journald.service", "systemd-logind.service",
		"systemd-udevd.service", "systemd-networkd.service", "systemd-resolved.service", "systemd-timesyncd.service",
		"getty@tty1.service", "serial-getty@ttyS0.service", "container-getty@1.service", "console-getty.service",
		"autovt@tty3.service", "user@1000.service", "user-runtime-dir@1000.service", "NetworkManager.service",
		"NetworkManager-wait-online.service", "networking.service", "network.service", "wpa_supplicant.service",
		"wpa_supplicant@wlan0.service", "iwd.service", "wicked.service", "netplan-ovs-cleanup.service",
		"ifup@eth0.service", "dhcpcd.service", "display-manager.service", "gdm.service", "sddm.service",
		"lightdm.service", "firewalld.service", "ufw.service", "nftables.service", "netfilter-persistent.service",
		"tailscaled.service", "zerotier-one.service", "wg-quick@wg0.service", "cloudflared.service",
		"zfs-zed.service", "zfs-mount.service", "lvm2-monitor.service", "multipathd.service", "auditd.service",
		"emergency.service", "rescue.service", "reboot.service", "SSHD.service", "Ssh.service", "networkmanager.service",
	} {
		if p := NameProtection(n); !p.Protected || p.Reason == "" || p.Category == "" {
			t.Errorf("%s not protected: %+v", n, p)
		}
	}
}

func TestOrdinaryServicesNotProtected(t *testing.T) {
	for _, n := range []string{"nginx.service", "docker.service", "jellyfin.service", "hc-test-dummy.service",
		"cron.service", "smbd.service", "postgresql@16-main.service", "myapp@1.service", "sshguard-lite.service",
		"network-backup.service", "systemdfoo.service", "plex.service", "caddy.service"} {
		if p := NameProtection(n); p.Protected {
			t.Errorf("%s protected: %+v", n, p)
		}
	}
}

func TestNamesProtectionCoversAliasesAndUnitFiles(t *testing.T) {
	if !NamesProtection("innocent.service", "sshd.service").Protected {
		t.Error("alias not checked")
	}
	if !NamesProtection("copy.service", "/usr/lib/systemd/system/ssh.service").Protected {
		t.Error("unit file name not checked")
	}
	if NamesProtection("web.service", "", "/etc/systemd/system/web.service").Protected {
		t.Error("false positive")
	}
}

func TestValidUnitAndAction(t *testing.T) {
	for _, ok := range []string{"demo.service", "a.service", "getty@tty1.service", "x:y_z-1.service"} {
		if !ValidUnit(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "-a.service", "a", "a.socket", "foo@.service", "a..b.service", "a/b.service",
		"a\\x2d.service", "a b.service", strings.Repeat("a", 101) + ".service"} {
		if ValidUnit(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, a := range []string{"start", "stop", "restart"} {
		if !ValidAction(a) {
			t.Errorf("%s rejected", a)
		}
	}
	for _, a := range []string{"enable", "disable", "mask", "reload", "daemon-reload", "Start", ""} {
		if ValidAction(a) {
			t.Errorf("%s accepted", a)
		}
	}
}

func TestDangerousTargetsAndActions(t *testing.T) {
	for _, n := range []string{"reboot.target", "poweroff.target", "emergency.target", "rescue.target",
		"systemd-reboot.service", "suspend.target", "ctrl-alt-del.target", "kexec.target", "soft-reboot.target"} {
		if !DangerousTarget(n) {
			t.Errorf("%s", n)
		}
	}
	for _, n := range []string{"network.target", "multi-user.target", "nginx.service", "shutdown-ish.service"} {
		if DangerousTarget(n) {
			t.Errorf("%s flagged", n)
		}
	}
	if !SafeEmergencyAction("none") || !SafeEmergencyAction("") || SafeEmergencyAction("reboot") || SafeEmergencyAction("poweroff-force") {
		t.Error("emergency actions")
	}
}

// Rules cover every unit type of a protected name, plus the additions from the
// M2-2 security review.
func TestProtectionCoversOtherUnitTypesAndReviewAdditions(t *testing.T) {
	for _, n := range []string{"ssh.socket", "sshd.socket", "iscsid.socket", "multipathd.socket", "gdm.socket",
		"network.target", "dbus.socket", "systemd-journald.socket", "netctl@wlan0.service", "netctl-auto@wlan0.service",
		"openvpn@home.service", "openvpn-server@srv.service", "openvpn-client@c.service", "ovs-vswitchd.service",
		"ovsdb-server.service", "openvswitch-switch.service", "dm-event.service", "dm-event.socket", "sssd.service",
		"sssd-nss.socket", "nslcd.service", "winbind.service", "network-manager.service", "strongswan-starter.service",
		"ipsec.service", "lvm2.service", "zfs-import.target", "Ssh.Socket"} {
		if !NameProtection(n).Protected {
			t.Errorf("%s not protected", n)
		}
	}
	for _, n := range []string{"sshguard.service", "nginx.service", "web.socket", "ssh-agent.socket", "backup.timer"} {
		if p := NameProtection(n); p.Protected {
			t.Errorf("%s protected (%s)", n, p.Category)
		}
	}
}
