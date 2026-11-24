// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package netinfo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// BondSlave is one port of a kernel bond.
type BondSlave struct {
	Name         string `json:"name"`
	State        string `json:"state"`     // active | backup (bonding_slave/state)
	MiiStatus    string `json:"miiStatus"` // up | down
	LinkFailures int    `json:"linkFailures"`
	Speed        string `json:"speed,omitempty"` // Mb/s from sysfs, "" when unknown
	AggregatorID string `json:"aggregatorId,omitempty"`
}

// Renderer is the network configuration stack observed to manage a bond.
type Renderer struct {
	// ID is systemd-networkd, NetworkManager, ifupdown or unknown.
	ID string `json:"id"`
	// Via names a generator in front of the renderer (netplan), if observed.
	Via string `json:"via,omitempty"`
	// Source is the runtime state file the observation came from.
	Source string `json:"source,omitempty"`
	// Confidence is observed (a runtime state file names the bond) or unknown.
	Confidence string `json:"confidence"`
}

// Bond is one kernel bonding device.
type Bond struct {
	Name      string `json:"name"`
	Mode      string `json:"mode"` // e.g. 802.3ad, active-backup
	LACP      bool   `json:"lacp"` // mode 802.3ad
	OperState string `json:"operState"`
	MiiMon    string `json:"miimonMs,omitempty"`
	// LACP-only fields (empty for other modes).
	LACPRate       string `json:"lacpRate,omitempty"`
	XmitHashPolicy string `json:"xmitHashPolicy,omitempty"`
	AggregatorID   string `json:"aggregatorId,omitempty"`
	// PartnerMAC is the LACP partner system; "" when none has answered.
	PartnerMAC  string      `json:"partnerMac,omitempty"`
	ActiveSlave string      `json:"activeSlave,omitempty"`
	Slaves      []BondSlave `json:"slaves"`
	Renderer    Renderer    `json:"renderer"`
}

// BondSnapshot is the read-only bonding inventory.
type BondSnapshot struct {
	// DriverLoaded is true when /sys/class/net/bonding_masters exists.
	DriverLoaded bool   `json:"driverLoaded"`
	Bonds        []Bond `json:"bonds"`
	// Stacks lists the network configuration stacks observed running.
	Stacks    []string `json:"stacks"`
	Truncated bool     `json:"truncated"`
}

const (
	maxBonds  = 64
	maxSlaves = 64
	maxIfaces = 4096
)

// CollectBonds reads kernel bonding state from sysfs under root and the
// runtime state files of the network configuration stacks. It never changes
// a bond and never loads the bonding module.
func CollectBonds(ctx context.Context, root string) (BondSnapshot, error) {
	snap := BondSnapshot{Bonds: []Bond{}, Stacks: observedStacks(root)}
	netDir := filepath.Join(root, "/sys/class/net")
	if _, err := os.Stat(filepath.Join(netDir, "bonding_masters")); err == nil {
		snap.DriverLoaded = true
	}
	names, err := readDirNames(netDir, maxIfaces+1)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return snap, nil
		}
		return snap, errors.New("cannot list network interfaces")
	}
	if len(names) > maxIfaces {
		names, snap.Truncated = names[:maxIfaces], true
	}
	sort.Strings(names)
	for _, n := range names {
		if err := ctx.Err(); err != nil {
			return snap, err
		}
		if !validIface(n) {
			continue
		}
		dir := filepath.Join(netDir, n)
		if fi, err := os.Stat(filepath.Join(dir, "bonding")); err != nil || !fi.IsDir() {
			continue
		}
		if len(snap.Bonds) >= maxBonds {
			snap.Truncated = true
			break
		}
		snap.Bonds = append(snap.Bonds, readBond(root, dir, n))
	}
	return snap, nil
}

func readBond(root, dir, name string) Bond {
	b := Bond{Name: name, Slaves: []BondSlave{}}
	b.Mode = firstField(sysAttr(dir, "bonding/mode"))
	b.LACP = b.Mode == "802.3ad"
	b.OperState = sysAttr(dir, "operstate")
	b.MiiMon = sysAttr(dir, "bonding/miimon")
	b.ActiveSlave = sysAttr(dir, "bonding/active_slave")
	if b.LACP {
		b.LACPRate = firstField(sysAttr(dir, "bonding/lacp_rate"))
		b.XmitHashPolicy = firstField(sysAttr(dir, "bonding/xmit_hash_policy"))
		b.AggregatorID = sysAttr(dir, "bonding/ad_aggregator")
		if mac := sysAttr(dir, "bonding/ad_partner_mac"); mac != "" && mac != "00:00:00:00:00:00" {
			b.PartnerMAC = mac
		}
	}
	slaves := strings.Fields(sysAttr(dir, "bonding/slaves"))
	if len(slaves) > maxSlaves {
		slaves = slaves[:maxSlaves]
	}
	netDir := filepath.Dir(dir)
	for _, s := range slaves {
		if !validIface(s) {
			continue
		}
		sd := filepath.Join(netDir, s)
		sl := BondSlave{Name: s}
		sl.State = sysAttr(sd, "bonding_slave/state")
		sl.MiiStatus = sysAttr(sd, "bonding_slave/mii_status")
		sl.LinkFailures, _ = strconv.Atoi(sysAttr(sd, "bonding_slave/link_failure_count"))
		if sp := sysAttr(sd, "speed"); sp != "" && sp != "-1" {
			sl.Speed = sp
		}
		if b.LACP {
			sl.AggregatorID = sysAttr(sd, "bonding_slave/ad_aggregator_id")
		}
		b.Slaves = append(b.Slaves, sl)
	}
	b.Renderer = detectRenderer(root, name, sysAttr(dir, "ifindex"))
	return b
}

// detectRenderer looks for runtime state files that name the bond. It never
// reads configuration files that may hold secrets (NetworkManager keyfiles,
// netplan YAML).
func detectRenderer(root, name, ifindex string) Renderer {
	if _, err := strconv.Atoi(ifindex); err == nil {
		link := filepath.Join("/run/systemd/netif/links", ifindex)
		if b, err := readSmall(filepath.Join(root, link), smallFileMax); err == nil {
			kv := parseEnvLines(string(b))
			if st := kv["ADMIN_STATE"]; st != "" && st != "unmanaged" {
				r := Renderer{ID: "systemd-networkd", Source: link, Confidence: "observed"}
				if strings.Contains(kv["NETWORK_FILE"], "netplan") {
					r.Via = "netplan"
				}
				return r
			}
		}
		dev := filepath.Join("/run/NetworkManager/devices", ifindex)
		if b, err := readSmall(filepath.Join(root, dev), smallFileMax); err == nil {
			kv := parseEnvLines(string(b))
			if kv["managed"] != "false" {
				r := Renderer{ID: "NetworkManager", Source: dev, Confidence: "observed"}
				if strings.Contains(kv["connection-uuid"], "netplan") || nmNetplan(root) {
					r.Via = "netplan"
				}
				return r
			}
		}
	}
	for _, p := range []string{"/run/network/ifstate." + name, "/run/network/ifstate"} {
		b, err := readSmall(filepath.Join(root, p), 64<<10)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			k, _, _ := strings.Cut(strings.TrimSpace(line), "=")
			if k == name || (strings.HasSuffix(p, "."+name) && strings.TrimSpace(line) != "") {
				return Renderer{ID: "ifupdown", Source: p, Confidence: "observed"}
			}
		}
	}
	return Renderer{ID: "unknown", Confidence: "unknown"}
}

func nmNetplan(root string) bool {
	names, err := readDirNames(filepath.Join(root, "/run/NetworkManager/system-connections"), 1024)
	if err != nil {
		return false
	}
	for _, n := range names {
		if strings.HasPrefix(n, "netplan-") {
			return true
		}
	}
	return false
}

// observedStacks reports which configuration stacks are running, from their
// runtime directories (no D-Bus, no process execution).
func observedStacks(root string) []string {
	out := []string{}
	checks := []struct{ id, path string }{
		{"systemd-networkd", "/run/systemd/netif/links"},
		{"NetworkManager", "/run/NetworkManager"},
		{"ifupdown", "/run/network"},
		{"netplan", "/run/netplan"},
	}
	for _, c := range checks {
		if fi, err := os.Stat(filepath.Join(root, c.path)); err == nil && fi.IsDir() {
			out = append(out, c.id)
		}
	}
	return out
}

func parseEnvLines(s string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && k != "" && !strings.HasPrefix(k, "#") {
			m[k] = v
		}
	}
	return m
}

func sysAttr(dir, rel string) string {
	b, err := readSmall(filepath.Join(dir, rel), 1024)
	if err != nil {
		return ""
	}
	return cleanText(string(b), 256)
}

func firstField(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

// validIface accepts kernel interface names (IFNAMSIZ-1, no slash or space).
func validIface(n string) bool {
	if n == "" || len(n) > 15 || n == "." || n == ".." {
		return false
	}
	for _, c := range n {
		if c <= ' ' || c == '/' || c == ':' || c == 0x7f {
			return false
		}
	}
	return true
}
