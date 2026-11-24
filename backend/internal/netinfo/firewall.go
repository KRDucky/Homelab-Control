// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package netinfo

import (
	"bufio"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// Firewall backend IDs.
const (
	FirewallFirewalld = "firewalld"
	FirewallUfw       = "ufw"
	FirewallNftables  = "nftables"
	FirewallIptables  = "iptables"
	FirewallNone      = "none"
	FirewallUnknown   = "unknown"
)

// FirewallUnits are the fixed systemd units whose state decides the active
// backend. The broker queries exactly these names.
var FirewallUnits = []string{
	"firewalld.service", "ufw.service", "nftables.service",
	"netfilter-persistent.service", "iptables.service", "ip6tables.service",
}

// UnitState is the part of `systemctl show` the detector uses.
type UnitState struct {
	LoadState     string `json:"loadState"`
	ActiveState   string `json:"activeState"`
	UnitFileState string `json:"unitFileState"`
}

// Backend is one firewall stack and whether it is present and active.
type Backend struct {
	ID        string `json:"id"`
	Installed bool   `json:"installed"`
	// Binary is the well-known path found by Stat ("" when absent).
	Binary string `json:"binary,omitempty"`
	// Unit is the systemd unit used for detection and its state.
	Unit        string `json:"unit,omitempty"`
	ActiveState string `json:"activeState,omitempty"`
	Enabled     string `json:"enabled,omitempty"` // unit-file state
	// Role is active, installed (present but not active) or absent.
	Role string `json:"role"`
}

// FirewallSnapshot is the read-only firewall view. Only the active backend's
// saved configuration is parsed; the live kernel ruleset is not read.
type FirewallSnapshot struct {
	Active       string    `json:"active"`
	ActiveReason string    `json:"activeReason"`
	AlsoActive   []string  `json:"alsoActive"`
	Backends     []Backend `json:"backends"`
	// Source is always "saved-configuration" in this checkpoint.
	Source    string         `json:"source"`
	Firewalld *FirewalldView `json:"firewalld,omitempty"`
	Ufw       *UfwView       `json:"ufw,omitempty"`
	Nftables  *NftablesView  `json:"nftables,omitempty"`
	Iptables  *IptablesView  `json:"iptables,omitempty"`
	Notes     []string       `json:"notes"`
}

const (
	configMaxBytes = 1 << 20
	maxRules       = 1024
	maxZones       = 64
	maxZoneItems   = 256
	ruleTextMax    = 512
)

var firewallBinaries = map[string][]string{
	FirewallFirewalld: {"/usr/bin/firewall-cmd", "/usr/sbin/firewall-cmd"},
	FirewallUfw:       {"/usr/sbin/ufw", "/usr/bin/ufw"},
	FirewallNftables:  {"/usr/sbin/nft", "/usr/bin/nft", "/sbin/nft"},
	FirewallIptables:  {"/usr/sbin/iptables", "/usr/bin/iptables", "/sbin/iptables"},
}

// ParseUnitStates parses `systemctl show --property=Id,LoadState,ActiveState,
// UnitFileState -- <units>` output: one block per unit separated by a blank
// line. Only names in FirewallUnits are kept. systemctl prints a block (with
// LoadState=not-found) even for a unit that does not exist, so output that
// lacks any of FirewallUnits is not what this parser expects: it returns nil
// (state unknown) rather than a partial map that would read as "inactive".
func ParseUnitStates(b []byte) map[string]UnitState {
	out := map[string]UnitState{}
	allowed := map[string]bool{}
	for _, u := range FirewallUnits {
		allowed[u] = true
	}
	for _, block := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n\n") {
		kv := parseEnvLines(block)
		id := kv["Id"]
		if !allowed[id] {
			continue
		}
		out[id] = UnitState{
			LoadState:     cleanText(kv["LoadState"], 32),
			ActiveState:   cleanText(kv["ActiveState"], 32),
			UnitFileState: cleanText(kv["UnitFileState"], 32),
		}
	}
	if len(out) != len(FirewallUnits) {
		return nil
	}
	return out
}

func active(u map[string]UnitState, name string) bool {
	s, ok := u[name]
	return ok && s.ActiveState == "active"
}

// CollectFirewall detects the active backend from the unit states (nil when
// systemd could not be queried) and installed binaries, then reads the saved
// configuration of that backend only. Precedence follows the front-end
// stacking: firewalld and ufw drive nftables/iptables themselves, so they win.
func CollectFirewall(root string, units map[string]UnitState) FirewallSnapshot {
	snap := FirewallSnapshot{Source: "saved-configuration", AlsoActive: []string{}, Notes: []string{}, Backends: []Backend{}}
	ufwEnabled := false
	if b, err := readConfig(root, "/etc/ufw/ufw.conf"); err == nil {
		ufwEnabled = strings.EqualFold(unquote(parseEnvLines(string(b))["ENABLED"]), "yes")
	}
	iptablesUnit := "netfilter-persistent.service"
	if !active(units, iptablesUnit) && (active(units, "iptables.service") || active(units, "ip6tables.service")) {
		iptablesUnit = "iptables.service"
		if !active(units, iptablesUnit) {
			iptablesUnit = "ip6tables.service"
		}
	}
	order := []struct {
		id, unit string
		isActive bool
	}{
		{FirewallFirewalld, "firewalld.service", active(units, "firewalld.service")},
		{FirewallUfw, "ufw.service", ufwEnabled && active(units, "ufw.service")},
		{FirewallNftables, "nftables.service", active(units, "nftables.service")},
		{FirewallIptables, iptablesUnit, active(units, iptablesUnit)},
	}
	for _, o := range order {
		be := Backend{ID: o.id, Unit: o.unit, Role: "absent"}
		for _, p := range firewallBinaries[o.id] {
			if fi, err := os.Stat(filepath.Join(root, p)); err == nil && fi.Mode().IsRegular() {
				be.Installed, be.Binary = true, p
				break
			}
		}
		if s, ok := units[o.unit]; ok && s.LoadState != "not-found" {
			be.ActiveState, be.Enabled = s.ActiveState, s.UnitFileState
		}
		switch {
		case o.isActive:
			be.Role = "active"
			if snap.Active == "" {
				snap.Active = o.id
			} else {
				snap.AlsoActive = append(snap.AlsoActive, o.id)
			}
		case be.Installed || be.ActiveState != "":
			be.Role = "installed"
		}
		snap.Backends = append(snap.Backends, be)
	}
	switch {
	case units == nil:
		snap.Active = FirewallUnknown
		snap.ActiveReason = "systemd could not be queried, so the active firewall service is unknown."
		return snap
	case snap.Active == "":
		snap.Active = FirewallNone
		snap.ActiveReason = "No firewall service (firewalld, ufw, nftables, netfilter-persistent or iptables) is active."
		snap.Notes = append(snap.Notes, "Rules loaded by other means (scripts, container engines) are not shown; this checkpoint does not read the live kernel ruleset.")
		return snap
	}
	if len(snap.AlsoActive) > 0 {
		snap.Notes = append(snap.Notes, "More than one firewall service is active ("+snap.Active+", "+strings.Join(snap.AlsoActive, ", ")+"). Their rules can conflict.")
	}
	switch snap.Active {
	case FirewallFirewalld:
		snap.ActiveReason = "firewalld.service is active."
		v := readFirewalld(root)
		snap.Firewalld = &v
	case FirewallUfw:
		snap.ActiveReason = "ufw is enabled in /etc/ufw/ufw.conf and ufw.service is active."
		v := readUfw(root)
		snap.Ufw = &v
	case FirewallNftables:
		snap.ActiveReason = "nftables.service is active."
		v := readNftables(root)
		snap.Nftables = &v
	case FirewallIptables:
		snap.ActiveReason = iptablesUnit + " is active."
		v := readIptables(root)
		snap.Iptables = &v
	}
	return snap
}

// readConfig reads a fixed configuration path without following a final
// symlink, requiring a regular file, bounded by configMaxBytes.
func readConfig(root, abs string) ([]byte, error) {
	f, err := os.OpenFile(filepath.Join(root, abs), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, configMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > configMaxBytes {
		return nil, errors.New("configuration file too large")
	}
	return b, nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// ---- ufw ------------------------------------------------------------------

// UfwRule is one user rule from ufw's "### tuple ###" records.
type UfwRule struct {
	Action    string `json:"action"` // allow | deny | reject | limit (with _log suffix kept)
	Direction string `json:"direction"`
	Interface string `json:"interface,omitempty"`
	Route     bool   `json:"route"`
	Proto     string `json:"proto"`
	To        string `json:"to"`
	ToPort    string `json:"toPort"`
	From      string `json:"from"`
	FromPort  string `json:"fromPort"`
	App       string `json:"app,omitempty"`
	Comment   string `json:"comment,omitempty"`
	IPv6      bool   `json:"ipv6"`
}

// UfwView is ufw's saved configuration.
type UfwView struct {
	Enabled        bool      `json:"enabled"`
	LogLevel       string    `json:"logLevel,omitempty"`
	DefaultInput   string    `json:"defaultInput,omitempty"`
	DefaultOutput  string    `json:"defaultOutput,omitempty"`
	DefaultForward string    `json:"defaultForward,omitempty"`
	IPv6           string    `json:"ipv6,omitempty"`
	Rules          []UfwRule `json:"rules"`
	Truncated      bool      `json:"truncated"`
	Unreadable     []string  `json:"unreadable"`
}

func readUfw(root string) UfwView {
	v := UfwView{Rules: []UfwRule{}, Unreadable: []string{}}
	if b, err := readConfig(root, "/etc/ufw/ufw.conf"); err == nil {
		kv := parseEnvLines(string(b))
		v.Enabled = strings.EqualFold(unquote(kv["ENABLED"]), "yes")
		v.LogLevel = cleanText(unquote(kv["LOGLEVEL"]), 32)
	} else {
		v.Unreadable = append(v.Unreadable, "/etc/ufw/ufw.conf")
	}
	if b, err := readConfig(root, "/etc/default/ufw"); err == nil {
		kv := parseEnvLines(string(b))
		v.DefaultInput = policyWord(unquote(kv["DEFAULT_INPUT_POLICY"]))
		v.DefaultOutput = policyWord(unquote(kv["DEFAULT_OUTPUT_POLICY"]))
		v.DefaultForward = policyWord(unquote(kv["DEFAULT_FORWARD_POLICY"]))
		v.IPv6 = cleanText(unquote(kv["IPV6"]), 8)
	} else {
		v.Unreadable = append(v.Unreadable, "/etc/default/ufw")
	}
	for _, f := range []struct {
		path string
		v6   bool
	}{{"/etc/ufw/user.rules", false}, {"/etc/ufw/user6.rules", true}} {
		b, err := readConfig(root, f.path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) || !f.v6 {
				v.Unreadable = append(v.Unreadable, f.path)
			}
			continue
		}
		rules, trunc := ParseUfwRules(b, f.v6, maxRules-len(v.Rules))
		v.Rules = append(v.Rules, rules...)
		v.Truncated = v.Truncated || trunc
	}
	return v
}

func policyWord(s string) string {
	switch strings.ToUpper(s) {
	case "ACCEPT", "DROP", "REJECT":
		return strings.ToLower(s)
	case "":
		return ""
	default:
		return "unrecognised"
	}
}

// ParseUfwRules parses "### tuple ###" lines of user.rules/user6.rules. The
// layout is: [route:]action proto dport dst sport src [dapp sapp] direction
// [comment=<hex>]. Malformed tuples are skipped. At most limit rules return.
func ParseUfwRules(b []byte, v6 bool, limit int) ([]UfwRule, bool) {
	out := []UfwRule{}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	for sc.Scan() {
		rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "### tuple ###")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		var comment string
		if n := len(f); n > 0 && strings.HasPrefix(f[n-1], "comment=") {
			if raw, err := hex.DecodeString(strings.TrimPrefix(f[n-1], "comment=")); err == nil {
				comment = cleanText(string(raw), 128)
			}
			f = f[:n-1]
		}
		if len(f) != 7 && len(f) != 9 {
			continue
		}
		if len(out) >= limit {
			return out, true
		}
		r := UfwRule{Proto: f[1], ToPort: f[2], To: f[3], FromPort: f[4], From: f[5], IPv6: v6, Comment: comment}
		r.Action, r.Route = strings.CutPrefix(f[0], "route:")
		dir := f[len(f)-1]
		if len(f) == 9 {
			if f[6] != "-" {
				r.App = f[6]
			} else if f[7] != "-" {
				r.App = f[7]
			}
		}
		r.Direction, r.Interface, _ = strings.Cut(dir, "_")
		for _, p := range []*string{&r.Action, &r.Proto, &r.ToPort, &r.To, &r.FromPort, &r.From, &r.App, &r.Direction, &r.Interface} {
			*p = cleanText(*p, 128)
		}
		out = append(out, r)
	}
	return out, false
}

// ---- firewalld ------------------------------------------------------------

// FirewalldZone is one zone from the permanent configuration.
type FirewalldZone struct {
	Name       string   `json:"name"`
	Default    bool     `json:"default"`
	Target     string   `json:"target"`
	Source     string   `json:"source"` // /etc (customised) or /usr/lib (shipped)
	Interfaces []string `json:"interfaces"`
	Sources    []string `json:"sources"`
	Services   []string `json:"services"`
	Ports      []string `json:"ports"`
	Masquerade bool     `json:"masquerade"`
	Forwards   []string `json:"forwardPorts"`
	RichRules  []string `json:"richRules"`
}

// FirewalldView is firewalld's permanent configuration.
type FirewalldView struct {
	DefaultZone string          `json:"defaultZone"`
	Zones       []FirewalldZone `json:"zones"`
	Truncated   bool            `json:"truncated"`
	Unreadable  []string        `json:"unreadable"`
}

type fwZoneXML struct {
	Target     string `xml:"target,attr"`
	Interfaces []struct {
		Name string `xml:"name,attr"`
	} `xml:"interface"`
	Sources []struct {
		Address string `xml:"address,attr"`
		Ipset   string `xml:"ipset,attr"`
		Mac     string `xml:"mac,attr"`
	} `xml:"source"`
	Services []struct {
		Name string `xml:"name,attr"`
	} `xml:"service"`
	Ports      []fwPortXML `xml:"port"`
	Masquerade *struct{}   `xml:"masquerade"`
	Forwards   []struct {
		Port   string `xml:"port,attr"`
		Proto  string `xml:"protocol,attr"`
		ToPort string `xml:"to-port,attr"`
		ToAddr string `xml:"to-addr,attr"`
	} `xml:"forward-port"`
	Rules []fwRuleXML `xml:"rule"`
}

type fwPortXML struct {
	Port  string `xml:"port,attr"`
	Proto string `xml:"protocol,attr"`
}

type fwAddrXML struct {
	Address string `xml:"address,attr"`
	Invert  string `xml:"invert,attr"`
}

type fwRuleXML struct {
	Family      string     `xml:"family,attr"`
	Source      *fwAddrXML `xml:"source"`
	Destination *fwAddrXML `xml:"destination"`
	Service     *struct {
		Name string `xml:"name,attr"`
	} `xml:"service"`
	Port     *fwPortXML `xml:"port"`
	Protocol *struct {
		Value string `xml:"value,attr"`
	} `xml:"protocol"`
	Accept *struct{} `xml:"accept"`
	Reject *struct{} `xml:"reject"`
	Drop   *struct{} `xml:"drop"`
	Log    *struct{} `xml:"log"`
}

func (r fwRuleXML) text() string {
	parts := []string{"rule"}
	if r.Family != "" {
		parts = append(parts, "family="+r.Family)
	}
	addr := func(kind string, a *fwAddrXML) {
		if a == nil {
			return
		}
		s := kind
		if a.Invert == "true" || a.Invert == "yes" {
			s += " NOT"
		}
		parts = append(parts, s+" address="+a.Address)
	}
	addr("source", r.Source)
	addr("destination", r.Destination)
	switch {
	case r.Service != nil:
		parts = append(parts, "service name="+r.Service.Name)
	case r.Port != nil:
		parts = append(parts, "port port="+r.Port.Port+" protocol="+r.Port.Proto)
	case r.Protocol != nil:
		parts = append(parts, "protocol value="+r.Protocol.Value)
	}
	if r.Log != nil {
		parts = append(parts, "log")
	}
	switch {
	case r.Accept != nil:
		parts = append(parts, "accept")
	case r.Reject != nil:
		parts = append(parts, "reject")
	case r.Drop != nil:
		parts = append(parts, "drop")
	}
	return cleanText(strings.Join(parts, " "), ruleTextMax)
}

// ParseFirewalldZone parses one zone XML file. The decoder does not resolve
// external entities (encoding/xml never does).
func ParseFirewalldZone(name string, b []byte) (FirewalldZone, error) {
	var x fwZoneXML
	if err := xml.Unmarshal(b, &x); err != nil {
		return FirewalldZone{}, errors.New("zone XML does not parse")
	}
	z := FirewalldZone{Name: name, Target: cleanText(x.Target, 32), Masquerade: x.Masquerade != nil,
		Interfaces: []string{}, Sources: []string{}, Services: []string{}, Ports: []string{}, Forwards: []string{}, RichRules: []string{}}
	if z.Target == "" {
		z.Target = "default"
	}
	add := func(dst *[]string, s string) {
		if s = cleanText(s, 256); s != "" && len(*dst) < maxZoneItems {
			*dst = append(*dst, s)
		}
	}
	for _, i := range x.Interfaces {
		add(&z.Interfaces, i.Name)
	}
	for _, s := range x.Sources {
		switch {
		case s.Address != "":
			add(&z.Sources, s.Address)
		case s.Ipset != "":
			add(&z.Sources, "ipset:"+s.Ipset)
		case s.Mac != "":
			add(&z.Sources, "mac:"+s.Mac)
		}
	}
	for _, s := range x.Services {
		add(&z.Services, s.Name)
	}
	for _, p := range x.Ports {
		add(&z.Ports, p.Port+"/"+p.Proto)
	}
	for _, f := range x.Forwards {
		s := f.Port + "/" + f.Proto + " → "
		if f.ToAddr != "" {
			s += f.ToAddr
		}
		if f.ToPort != "" {
			s += ":" + f.ToPort
		}
		add(&z.Forwards, s)
	}
	for _, r := range x.Rules {
		add(&z.RichRules, r.text())
	}
	return z, nil
}

func readFirewalld(root string) FirewalldView {
	v := FirewalldView{Zones: []FirewalldZone{}, Unreadable: []string{}}
	if b, err := readConfig(root, "/etc/firewalld/firewalld.conf"); err == nil {
		v.DefaultZone = cleanText(unquote(parseEnvLines(string(b))["DefaultZone"]), 64)
	} else {
		v.Unreadable = append(v.Unreadable, "/etc/firewalld/firewalld.conf")
	}
	if v.DefaultZone == "" {
		v.DefaultZone = "public" // firewalld's compiled-in default
	}
	// /etc overrides /usr/lib for a zone of the same name.
	files := map[string]string{}
	for _, dir := range []string{"/usr/lib/firewalld/zones", "/etc/firewalld/zones"} {
		names, err := readDirNames(filepath.Join(root, dir), 4096)
		if err != nil {
			continue
		}
		for _, n := range names {
			if z, ok := strings.CutSuffix(n, ".xml"); ok && validZone(z) {
				files[z] = dir + "/" + n
			}
		}
	}
	names := make([]string, 0, len(files))
	for z := range files {
		names = append(names, z)
	}
	sort.Strings(names)
	for _, z := range names {
		if len(v.Zones) >= maxZones {
			v.Truncated = true
			break
		}
		b, err := readConfig(root, files[z])
		if err != nil {
			v.Unreadable = append(v.Unreadable, files[z])
			continue
		}
		zone, err := ParseFirewalldZone(z, b)
		if err != nil {
			v.Unreadable = append(v.Unreadable, files[z])
			continue
		}
		zone.Default = z == v.DefaultZone
		zone.Source = "/usr/lib"
		if strings.HasPrefix(files[z], "/etc/") {
			zone.Source = "/etc"
		}
		v.Zones = append(v.Zones, zone)
	}
	return v
}

func validZone(z string) bool {
	if z == "" || len(z) > 64 {
		return false
	}
	for _, c := range z {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ---- nftables -------------------------------------------------------------

// NftChain is one chain of a saved nftables table.
type NftChain struct {
	Name     string   `json:"name"`
	Type     string   `json:"type,omitempty"`
	Hook     string   `json:"hook,omitempty"`
	Priority string   `json:"priority,omitempty"`
	Policy   string   `json:"policy,omitempty"`
	Rules    []string `json:"rules"`
}

// NftTable is one saved nftables table.
type NftTable struct {
	Family string     `json:"family"`
	Name   string     `json:"name"`
	Chains []NftChain `json:"chains"`
	Sets   []string   `json:"sets"`
}

// NftablesView is the saved nftables configuration file.
type NftablesView struct {
	File       string     `json:"file"`
	Tables     []NftTable `json:"tables"`
	Includes   []string   `json:"includes"`
	FlushAll   bool       `json:"flushRuleset"`
	Truncated  bool       `json:"truncated"`
	Unreadable []string   `json:"unreadable"`
}

var nftConfigPaths = []string{"/etc/nftables.conf", "/etc/sysconfig/nftables.conf"}

func readNftables(root string) NftablesView {
	v := NftablesView{Tables: []NftTable{}, Includes: []string{}, Unreadable: []string{}}
	for _, p := range nftConfigPaths {
		b, err := readConfig(root, p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			v.Unreadable = append(v.Unreadable, p)
			continue
		}
		v.File = p
		parsed := ParseNftables(b)
		parsed.File, parsed.Unreadable = p, v.Unreadable
		return parsed
	}
	return v
}

// ParseNftables summarises an nft script: tables, chains with their base
// hook and policy, and rule lines. It is a reader for display, not an nft
// implementation: includes are listed, not followed, and variables are not
// expanded.
func ParseNftables(b []byte) NftablesView {
	v := NftablesView{Tables: []NftTable{}, Includes: []string{}, Unreadable: []string{}}
	type frame struct{ kind string }
	var stack []frame
	var tbl *NftTable
	var ch *NftChain
	rules := 0
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(stripComment(raw))
		if line == "" {
			continue
		}
		depth := len(stack)
		f := strings.Fields(line)
		switch {
		case depth == 0 && f[0] == "flush" && len(f) >= 2 && f[1] == "ruleset":
			v.FlushAll = true
		case depth == 0 && f[0] == "include" && len(f) >= 2:
			if len(v.Includes) < 64 {
				v.Includes = append(v.Includes, cleanText(strings.Trim(f[1], `"`), 256))
			}
		case depth == 0 && f[0] == "table" && strings.HasSuffix(line, "{"):
			fam, name := "ip", ""
			args := strings.Fields(strings.TrimSuffix(line, "{"))[1:]
			if len(args) == 2 {
				fam, name = args[0], args[1]
			} else if len(args) == 1 {
				name = args[0]
			}
			v.Tables = append(v.Tables, NftTable{Family: cleanText(fam, 16), Name: cleanText(name, 64), Chains: []NftChain{}, Sets: []string{}})
			tbl = &v.Tables[len(v.Tables)-1]
		case depth == 1 && tbl != nil && f[0] == "chain" && len(f) >= 2 && strings.Contains(line, "{"):
			name, _, _ := strings.Cut(f[1], "{")
			tbl.Chains = append(tbl.Chains, NftChain{Name: cleanText(name, 64), Rules: []string{}})
			ch = &tbl.Chains[len(tbl.Chains)-1]
			// One-line chain: chain NAME { type ...; policy ...; rule; }
			if _, body, ok := strings.Cut(line, "{"); ok {
				if i := strings.LastIndex(body, "}"); i >= 0 {
					body = body[:i]
				}
				nftStatements(ch, body, &rules, &v.Truncated)
			}
		case depth == 1 && tbl != nil && (f[0] == "set" || f[0] == "map") && len(f) >= 2:
			if len(tbl.Sets) < 64 {
				tbl.Sets = append(tbl.Sets, cleanText(f[0]+" "+strings.TrimSuffix(f[1], "{"), 64))
			}
		case depth == 2 && ch != nil && stack[1].kind == "chain":
			nftStatements(ch, line, &rules, &v.Truncated)
		}
		for _, c := range line {
			switch c {
			case '{':
				kind := "block"
				if len(stack) == 0 && f[0] == "table" {
					kind = "table"
				} else if len(stack) == 1 && f[0] == "chain" {
					kind = "chain"
				}
				if len(stack) < 32 {
					stack = append(stack, frame{kind})
				}
			case '}':
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				if len(stack) < 2 {
					ch = nil
				}
				if len(stack) == 0 {
					tbl = nil
				}
			}
		}
		if len(v.Tables) >= maxZones {
			v.Truncated = true
			break
		}
	}
	return v
}

// nftStatements reads the ';'-separated statements of one chain line.
func nftStatements(ch *NftChain, line string, rules *int, truncated *bool) {
	for _, stmt := range strings.Split(line, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || stmt == "}" {
			continue
		}
		sf := strings.Fields(stmt)
		switch sf[0] {
		case "type":
			if len(sf) >= 2 {
				ch.Type = cleanText(sf[1], 16)
			}
			for i := 2; i+1 < len(sf); i++ {
				switch sf[i] {
				case "hook":
					ch.Hook = cleanText(sf[i+1], 32)
				case "priority":
					ch.Priority = cleanText(sf[i+1], 32)
				}
			}
		case "policy":
			if len(sf) >= 2 {
				ch.Policy = cleanText(sf[1], 16)
			}
		default:
			if strings.HasSuffix(stmt, "{") || strings.HasPrefix(stmt, "}") {
				continue
			}
			if *rules >= maxRules {
				*truncated = true
				continue
			}
			*rules++
			ch.Rules = append(ch.Rules, cleanText(stmt, ruleTextMax))
		}
	}
}

// stripComment drops a '#' comment that is not inside double quotes.
func stripComment(s string) string {
	in := false
	for i, c := range s {
		switch c {
		case '"':
			in = !in
		case '#':
			if !in {
				return s[:i]
			}
		}
	}
	return s
}

// ---- iptables (saved rules) -------------------------------------------------

// IptablesChain is one chain in an iptables-save file.
type IptablesChain struct {
	Name   string   `json:"name"`
	Policy string   `json:"policy"` // ACCEPT/DROP for built-ins, "-" for user chains
	Rules  []string `json:"rules"`
}

// IptablesTable is one *table section.
type IptablesTable struct {
	Name   string          `json:"name"`
	Chains []IptablesChain `json:"chains"`
}

// IptablesFile is one saved rules file.
type IptablesFile struct {
	Path   string          `json:"path"`
	IPv6   bool            `json:"ipv6"`
	Tables []IptablesTable `json:"tables"`
}

// IptablesView is the saved iptables configuration.
type IptablesView struct {
	Files      []IptablesFile `json:"files"`
	Truncated  bool           `json:"truncated"`
	Unreadable []string       `json:"unreadable"`
}

var iptablesFiles = []struct {
	path string
	v6   bool
}{
	{"/etc/iptables/rules.v4", false}, {"/etc/iptables/rules.v6", true},
	{"/etc/sysconfig/iptables", false}, {"/etc/sysconfig/ip6tables", true},
}

func readIptables(root string) IptablesView {
	v := IptablesView{Files: []IptablesFile{}, Unreadable: []string{}}
	budget := maxRules
	for _, f := range iptablesFiles {
		b, err := readConfig(root, f.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			v.Unreadable = append(v.Unreadable, f.path)
			continue
		}
		tables, used, trunc := ParseIptablesSave(b, budget)
		budget -= used
		v.Truncated = v.Truncated || trunc
		v.Files = append(v.Files, IptablesFile{Path: f.path, IPv6: f.v6, Tables: tables})
	}
	return v
}

// ParseIptablesSave parses iptables-save output: *table, :CHAIN POLICY
// [counters], -A CHAIN rule..., COMMIT. At most limit rules are kept.
func ParseIptablesSave(b []byte, limit int) (tables []IptablesTable, rules int, truncated bool) {
	tables = []IptablesTable{}
	var t *IptablesTable
	chainIdx := map[string]int{}
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "*"):
			if len(tables) >= 8 {
				return tables, rules, true
			}
			tables = append(tables, IptablesTable{Name: cleanText(line[1:], 32), Chains: []IptablesChain{}})
			t = &tables[len(tables)-1]
			chainIdx = map[string]int{}
		case line == "COMMIT":
			t = nil
		case t != nil && strings.HasPrefix(line, ":"):
			f := strings.Fields(line[1:])
			if len(f) >= 2 && len(t.Chains) < 256 {
				chainIdx[f[0]] = len(t.Chains)
				t.Chains = append(t.Chains, IptablesChain{Name: cleanText(f[0], 64), Policy: cleanText(f[1], 16), Rules: []string{}})
			}
		case t != nil && strings.HasPrefix(line, "-A "):
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			i, ok := chainIdx[f[1]]
			if !ok {
				continue
			}
			if rules >= limit {
				truncated = true
				continue
			}
			rules++
			t.Chains[i].Rules = append(t.Chains[i].Rules, cleanText(strings.Join(f[2:], " "), ruleTextMax))
		}
	}
	return tables, rules, truncated
}
