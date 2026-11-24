// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package netinfo

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func states(active ...string) map[string]UnitState {
	m := map[string]UnitState{}
	for _, u := range FirewallUnits {
		m[u] = UnitState{LoadState: "not-found", ActiveState: "inactive", UnitFileState: ""}
	}
	for _, u := range active {
		m[u] = UnitState{LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}
	}
	return m
}

func touchBin(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		writeFiles(t, root, map[string]string{p: "#!bin\n"})
	}
}

func TestParseUnitStatesKeepsOnlyFixedUnits(t *testing.T) {
	out := "Id=ufw.service\nLoadState=loaded\nActiveState=active\nUnitFileState=enabled\n\nId=evil.service\nActiveState=active\n\nId=nftables.service\nLoadState=not-found\nActiveState=inactive\nUnitFileState=\n"
	m := ParseUnitStates([]byte(out))
	if len(m) != 2 || m["ufw.service"].ActiveState != "active" || m["nftables.service"].LoadState != "not-found" {
		t.Fatalf("%+v", m)
	}
}

func TestDetectUfwActiveAndParseRules(t *testing.T) {
	root := t.TempDir()
	touchBin(t, root, "usr/sbin/ufw", "usr/sbin/nft", "usr/sbin/iptables")
	comment := hex.EncodeToString([]byte("web admin"))
	writeFiles(t, root, map[string]string{
		"etc/ufw/ufw.conf": "# comment\nENABLED=yes\nLOGLEVEL=low\n",
		"etc/default/ufw":  "IPV6=yes\nDEFAULT_INPUT_POLICY=\"DROP\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\nDEFAULT_FORWARD_POLICY=\"WEIRD\"\n",
		"etc/ufw/user.rules": "*filter\n### tuple ### allow tcp 22 0.0.0.0/0 any 192.168.1.0/24 in\n-A ufw-user-input -p tcp --dport 22 -j ACCEPT\n" +
			"### tuple ### allow tcp 443 0.0.0.0/0 any 0.0.0.0/0 Nginx%20Full - in comment=" + comment + "\n" +
			"### tuple ### route:deny any any 0.0.0.0/0 any 10.0.0.0/8 out_eth1\n" +
			"### tuple ### malformed\n",
		"etc/ufw/user6.rules": "### tuple ### limit tcp 22 ::/0 any ::/0 in\n",
	})
	snap := CollectFirewall(root, states("ufw.service"))
	if snap.Active != FirewallUfw || snap.Ufw == nil || snap.Nftables != nil || snap.Source != "saved-configuration" {
		t.Fatalf("snap: %+v", snap)
	}
	u := snap.Ufw
	if !u.Enabled || u.DefaultInput != "drop" || u.DefaultOutput != "accept" || u.DefaultForward != "unrecognised" || u.IPv6 != "yes" {
		t.Fatalf("defaults: %+v", u)
	}
	if len(u.Rules) != 4 {
		t.Fatalf("rules: %+v", u.Rules)
	}
	if r := u.Rules[0]; r.Action != "allow" || r.ToPort != "22" || r.From != "192.168.1.0/24" || r.Direction != "in" {
		t.Errorf("rule0 %+v", r)
	}
	if r := u.Rules[1]; r.App != "Nginx%20Full" || r.Comment != "web admin" {
		t.Errorf("rule1 %+v", r)
	}
	if r := u.Rules[2]; !r.Route || r.Action != "deny" || r.Direction != "out" || r.Interface != "eth1" {
		t.Errorf("rule2 %+v", r)
	}
	if r := u.Rules[3]; !r.IPv6 || r.Action != "limit" {
		t.Errorf("rule3 %+v", r)
	}
	roles := map[string]string{}
	for _, b := range snap.Backends {
		roles[b.ID] = b.Role
	}
	if fmt.Sprint(roles) != "map[firewalld:absent iptables:installed nftables:installed ufw:active]" {
		t.Errorf("roles: %v", roles)
	}
}

func TestUfwServiceActiveButDisabledIsNotActive(t *testing.T) {
	root := t.TempDir()
	touchBin(t, root, "usr/sbin/ufw")
	writeFiles(t, root, map[string]string{"etc/ufw/ufw.conf": "ENABLED=no\n"})
	snap := CollectFirewall(root, states("ufw.service"))
	if snap.Active != FirewallNone || snap.Ufw != nil || len(snap.Notes) == 0 {
		t.Fatalf("ufw with ENABLED=no must not be active: %+v", snap)
	}
}

func TestFirewalldWinsOverNftablesAndNotesConflict(t *testing.T) {
	root := t.TempDir()
	touchBin(t, root, "usr/bin/firewall-cmd", "usr/sbin/nft")
	writeFiles(t, root, map[string]string{
		"etc/firewalld/firewalld.conf":       "DefaultZone=home\n",
		"usr/lib/firewalld/zones/public.xml": `<?xml version="1.0"?><zone><short>Public</short><service name="ssh"/></zone>`,
		"usr/lib/firewalld/zones/home.xml":   `<zone><service name="ssh"/></zone>`,
		"etc/firewalld/zones/home.xml": `<?xml version="1.0" encoding="utf-8"?>
<zone target="DROP"><interface name="enp1s0"/><source address="192.168.1.0/24"/><service name="ssh"/><service name="mdns"/>
<port protocol="tcp" port="8443"/><masquerade/><forward-port port="80" protocol="tcp" to-port="8080" to-addr="10.0.0.5"/>
<rule family="ipv4"><source address="10.0.0.0/8" invert="true"/><service name="http"/><log/><reject/></rule></zone>`,
		"etc/firewalld/zones/bad.xml":  `<zone><service`,
		"etc/firewalld/zones/../x.xml": "",
	})
	snap := CollectFirewall(root, states("firewalld.service", "nftables.service"))
	if snap.Active != FirewallFirewalld || fmt.Sprint(snap.AlsoActive) != "[nftables]" || len(snap.Notes) != 1 {
		t.Fatalf("snap: %+v", snap)
	}
	v := snap.Firewalld
	if v.DefaultZone != "home" || len(v.Zones) != 2 {
		t.Fatalf("zones: %+v", v)
	}
	home := v.Zones[0]
	if home.Name != "home" || !home.Default || home.Source != "/etc" || home.Target != "DROP" || !home.Masquerade ||
		fmt.Sprint(home.Services) != "[ssh mdns]" || fmt.Sprint(home.Ports) != "[8443/tcp]" ||
		fmt.Sprint(home.Forwards) != "[80/tcp → 10.0.0.5:8080]" ||
		fmt.Sprint(home.RichRules) != "[rule family=ipv4 source NOT address=10.0.0.0/8 service name=http log reject]" {
		t.Fatalf("home: %+v", home)
	}
	if v.Zones[1].Name != "public" || v.Zones[1].Source != "/usr/lib" || v.Zones[1].Target != "default" {
		t.Fatalf("public: %+v", v.Zones[1])
	}
	if fmt.Sprint(v.Unreadable) != "[/etc/firewalld/zones/bad.xml]" {
		t.Fatalf("unreadable: %v", v.Unreadable)
	}
}

func TestNftablesSavedConfig(t *testing.T) {
	root := t.TempDir()
	touchBin(t, root, "usr/sbin/nft")
	writeFiles(t, root, map[string]string{"etc/nftables.conf": `#!/usr/sbin/nft -f
flush ruleset
include "/etc/nftables.d/*.nft"
table inet filter {
	set blocked { type ipv4_addr; }
	chain input {
		type filter hook input priority filter; policy drop;
		ct state established,related accept # keep
		iif "lo" accept
		tcp dport { 22, 443 } accept
	}
	chain forward {
		type filter hook forward priority 0; policy drop;
	}
	chain output {
		type filter hook output priority 0; policy accept;
	}
}
table ip nat {
	chain postrouting { type nat hook postrouting priority 100; policy accept; }
}
`})
	snap := CollectFirewall(root, states("nftables.service"))
	v := snap.Nftables
	if snap.Active != FirewallNftables || v == nil || v.File != "/etc/nftables.conf" || !v.FlushAll || fmt.Sprint(v.Includes) != "[/etc/nftables.d/*.nft]" {
		t.Fatalf("snap: %+v / %+v", snap, v)
	}
	if len(v.Tables) != 2 || v.Tables[0].Family != "inet" || v.Tables[0].Name != "filter" || fmt.Sprint(v.Tables[0].Sets) != "[set blocked]" {
		t.Fatalf("tables: %+v", v.Tables)
	}
	in := v.Tables[0].Chains[0]
	if in.Name != "input" || in.Hook != "input" || in.Policy != "drop" || in.Type != "filter" || len(in.Rules) != 3 || in.Rules[2] != "tcp dport { 22, 443 } accept" {
		t.Fatalf("input: %+v", in)
	}
	if len(v.Tables[0].Chains) != 3 || v.Tables[0].Chains[1].Policy != "drop" {
		t.Fatalf("chains: %+v", v.Tables[0].Chains)
	}
	if nat := v.Tables[1].Chains; len(nat) != 1 || nat[0].Name != "postrouting" || nat[0].Type != "nat" || nat[0].Hook != "postrouting" || nat[0].Policy != "accept" {
		t.Fatalf("nat: %+v", v.Tables[1])
	}
}

func TestIptablesSavedRules(t *testing.T) {
	root := t.TempDir()
	touchBin(t, root, "usr/sbin/iptables")
	writeFiles(t, root, map[string]string{
		"etc/iptables/rules.v4": "# Generated by iptables-save\n*filter\n:INPUT DROP [0:0]\n:FORWARD DROP [0:0]\n:OUTPUT ACCEPT [10:20]\n:LOGDROP - [0:0]\n-A INPUT -i lo -j ACCEPT\n-A INPUT -p tcp -m tcp --dport 22 -j ACCEPT\n-A NOPE -j DROP\nCOMMIT\n*nat\n:PREROUTING ACCEPT [0:0]\nCOMMIT\n",
	})
	snap := CollectFirewall(root, states("netfilter-persistent.service"))
	v := snap.Iptables
	if snap.Active != FirewallIptables || v == nil || len(v.Files) != 1 || len(v.Files[0].Tables) != 2 {
		t.Fatalf("snap: %+v", snap)
	}
	f := v.Files[0].Tables[0]
	if f.Name != "filter" || len(f.Chains) != 4 || f.Chains[0].Policy != "DROP" || fmt.Sprint(f.Chains[0].Rules) != "[-i lo -j ACCEPT -p tcp -m tcp --dport 22 -j ACCEPT]" || f.Chains[3].Policy != "-" {
		t.Fatalf("filter: %+v", f)
	}
	_, used, trunc := ParseIptablesSave([]byte("*filter\n:INPUT ACCEPT [0:0]\n-A INPUT -j A\n-A INPUT -j B\nCOMMIT\n"), 1)
	if used != 1 || !trunc {
		t.Fatalf("limit: used=%d trunc=%v", used, trunc)
	}
}

func TestFirewallUnknownWithoutSystemdAndConfigSymlinkRefused(t *testing.T) {
	root := t.TempDir()
	snap := CollectFirewall(root, nil)
	if snap.Active != FirewallUnknown {
		t.Fatalf("nil units: %+v", snap)
	}
	writeFiles(t, root, map[string]string{"secret": "ENABLED=yes\n"})
	if err := os.MkdirAll(filepath.Join(root, "etc/ufw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "secret"), filepath.Join(root, "etc/ufw/ufw.conf")); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig(root, "/etc/ufw/ufw.conf"); err == nil {
		t.Fatal("a symlinked configuration file must not be followed")
	}
}
