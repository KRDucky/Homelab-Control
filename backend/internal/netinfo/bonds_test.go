// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package netinfo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCollectBondsLACP(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"sys/class/net/bonding_masters":                         "bond0\n",
		"sys/class/net/bond0/bonding/mode":                      "802.3ad 4\n",
		"sys/class/net/bond0/bonding/slaves":                    "enp1s0 enp2s0\n",
		"sys/class/net/bond0/bonding/miimon":                    "100\n",
		"sys/class/net/bond0/bonding/lacp_rate":                 "fast 1\n",
		"sys/class/net/bond0/bonding/xmit_hash_policy":          "layer3+4 1\n",
		"sys/class/net/bond0/bonding/ad_aggregator":             "1\n",
		"sys/class/net/bond0/bonding/ad_partner_mac":            "00:00:00:00:00:00\n",
		"sys/class/net/bond0/operstate":                         "up\n",
		"sys/class/net/bond0/ifindex":                           "4\n",
		"sys/class/net/enp1s0/bonding_slave/state":              "active\n",
		"sys/class/net/enp1s0/bonding_slave/mii_status":         "up\n",
		"sys/class/net/enp1s0/bonding_slave/link_failure_count": "2\n",
		"sys/class/net/enp1s0/bonding_slave/ad_aggregator_id":   "1\n",
		"sys/class/net/enp1s0/speed":                            "1000\n",
		"sys/class/net/enp2s0/bonding_slave/state":              "backup\n",
		"sys/class/net/enp2s0/bonding_slave/mii_status":         "down\n",
		"sys/class/net/enp2s0/speed":                            "-1\n",
		"sys/class/net/lo/operstate":                            "unknown\n",
		"run/systemd/netif/links/4":                             "ADMIN_STATE=configured\nNETWORK_FILE=/run/systemd/network/10-netplan-bond0.network\n",
	})
	snap, err := CollectBonds(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.DriverLoaded || len(snap.Bonds) != 1 {
		t.Fatalf("snap: %+v", snap)
	}
	b := snap.Bonds[0]
	if b.Mode != "802.3ad" || !b.LACP || b.LACPRate != "fast" || b.XmitHashPolicy != "layer3+4" || b.PartnerMAC != "" || b.MiiMon != "100" {
		t.Fatalf("bond: %+v", b)
	}
	if len(b.Slaves) != 2 || b.Slaves[0].LinkFailures != 2 || b.Slaves[0].Speed != "1000" || b.Slaves[1].Speed != "" || b.Slaves[1].MiiStatus != "down" || b.Slaves[0].AggregatorID != "1" {
		t.Fatalf("slaves: %+v", b.Slaves)
	}
	if b.Renderer.ID != "systemd-networkd" || b.Renderer.Via != "netplan" || b.Renderer.Confidence != "observed" {
		t.Fatalf("renderer: %+v", b.Renderer)
	}
	if len(snap.Stacks) != 1 || snap.Stacks[0] != "systemd-networkd" {
		t.Fatalf("stacks: %v", snap.Stacks)
	}
}

func TestCollectBondsActiveBackupNetworkManagerAndUnknown(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"sys/class/net/bonding_masters":            "bond1 bond2\n",
		"sys/class/net/bond1/bonding/mode":         "active-backup 1\n",
		"sys/class/net/bond1/bonding/slaves":       "eth0\n",
		"sys/class/net/bond1/bonding/active_slave": "eth0\n",
		"sys/class/net/bond1/bonding/lacp_rate":    "slow 0\n",
		"sys/class/net/bond1/ifindex":              "7\n",
		"sys/class/net/eth0/bonding_slave/state":   "active\n",
		"run/NetworkManager/devices/7":             "[device]\nmanaged=true\n",
		"sys/class/net/bond2/bonding/mode":         "balance-rr 0\n",
		"sys/class/net/bond2/ifindex":              "8\n",
		"run/systemd/netif/links/8":                "ADMIN_STATE=unmanaged\n",
	})
	snap, err := CollectBonds(context.Background(), root)
	if err != nil || len(snap.Bonds) != 2 {
		t.Fatalf("err=%v %+v", err, snap)
	}
	b1, b2 := snap.Bonds[0], snap.Bonds[1]
	if b1.LACP || b1.LACPRate != "" || b1.ActiveSlave != "eth0" || b1.Renderer.ID != "NetworkManager" {
		t.Fatalf("bond1: %+v", b1)
	}
	if b2.Renderer.ID != "unknown" || b2.Renderer.Confidence != "unknown" || len(b2.Slaves) != 0 {
		t.Fatalf("an unmanaged networkd link is not networkd: %+v", b2)
	}
}

func TestCollectBondsIfupdownAndNoDriver(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"sys/class/net/bond0/bonding/mode": "balance-alb 6\n",
		"sys/class/net/bond0/ifindex":      "3\n",
		"run/network/ifstate.bond0":        "bond0\n",
	})
	snap, err := CollectBonds(context.Background(), root)
	if err != nil || snap.DriverLoaded || len(snap.Bonds) != 1 || snap.Bonds[0].Renderer.ID != "ifupdown" {
		t.Fatalf("err=%v %+v", err, snap)
	}
	empty, err := CollectBonds(context.Background(), t.TempDir())
	if err != nil || empty.DriverLoaded || len(empty.Bonds) != 0 {
		t.Fatalf("no sysfs: err=%v %+v", err, empty)
	}
}

func TestValidIface(t *testing.T) {
	for _, ok := range []string{"bond0", "enp1s0f1", "br-lan.10"} {
		if !validIface(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", "eth0:1", "has space", "sixteencharsxxxx"} {
		if validIface(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
