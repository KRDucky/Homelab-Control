// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	. "homelab-control/backend/internal/broker/types"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// PinnedWebmin is the upstream baseline in compat/webmin.lock; installed
// versions older than this miss fixes our audit relies on.
const PinnedWebmin = "2.670"

var safeName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func webminStatus(_ context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	dir := env.Roots.Path("/etc/webmin")
	st := WebminStatus{ConfigDir: "/etc/webmin", RPCCapable: []string{}, Checks: []Check{}}
	conf, err := readFixed(filepath.Join(dir, "miniserv.conf"), 256<<10)
	if err != nil {
		if isNotExist(err) {
			return st, nil
		}
		st.Notes = append(st.Notes, "miniserv.conf unreadable: "+clean(err.Error(), 200))
		return st, nil
	}
	st.Installed = true
	ms := parseKV(conf)
	if v, err := readFixed(filepath.Join(dir, "version"), 64); err == nil {
		st.Version = clean(string(v), 32)
	}
	if p, err := strconv.Atoi(ms["port"]); err == nil && p > 0 && p < 65536 {
		st.Port = p
	}
	tls := ms["ssl"] == "1"
	st.TLS = &tls
	st.Bind = clean(ms["bind"], 64)
	st.AllIfaces = st.Bind == "" || st.Bind == "*" || st.Bind == "0.0.0.0" || st.Bind == "::"
	st.Running = webminRunning(env, ms["pidfile"])

	cfg, _ := readFixed(filepath.Join(dir, "config"), 256<<10)
	gc := parseKV(cfg)

	// Usernames only (never hashes) from miniserv.users: "user:hash:..." lines.
	var users []string
	if ub, err := readFixed(filepath.Join(dir, "miniserv.users"), 1<<20); err == nil {
		for _, line := range strings.Split(string(ub), "\n") {
			name, _, _ := strings.Cut(line, ":")
			if safeName.MatchString(name) {
				users = append(users, name)
			}
		}
	}
	st.Users = len(users)
	for _, u := range users {
		// Mirrors webmin_user_can_rpc(): rpc=1 → yes, rpc=0 → no, unset →
		// root/admin/sysadm are treated as RPC-capable.
		acl, _ := readFixed(filepath.Join(dir, u+".acl"), 256<<10)
		rpc, set := parseKV(acl)["rpc"]
		if rpc == "1" || rpc == "2" || rpc == "3" || (!set && (u == "root" || u == "admin" || u == "sysadm")) {
			st.RPCCapable = append(st.RPCCapable, u)
		}
	}
	sort.Strings(st.RPCCapable)

	add := func(id, level, msg string) { st.Checks = append(st.Checks, Check{ID: id, Level: level, Message: msg}) }
	switch {
	case st.Version == "":
		add("version", "info", "Installed version could not be read.")
	case versionLess(st.Version, PinnedWebmin):
		add("version", "warn", "Webmin "+st.Version+" is older than the audited baseline "+PinnedWebmin+"; upgrade from your distribution or webmin.com.")
	default:
		add("version", "ok", "Webmin "+st.Version+" is at or above the audited baseline "+PinnedWebmin+".")
	}
	if tls {
		add("tls", "ok", "The legacy interface uses TLS.")
	} else {
		add("tls", "warn", "The legacy interface serves plain HTTP; logins can be intercepted on the network.")
	}
	if st.AllIfaces {
		add("bind", "warn", "The legacy interface listens on all network interfaces. Bind it to localhost or your tailnet if you only use it locally.")
	} else {
		add("bind", "ok", "The legacy interface listens only on "+st.Bind+".")
	}
	if gc["referers_none"] == "1" {
		add("referer", "ok", "Requests with unknown referers are refused (CSRF defence).")
	} else {
		add("referer", "warn", "Referer checking for unknown referers is off (referers_none is not 1).")
	}
	if len(st.RPCCapable) > 0 {
		add("rpc", "warn", "RPC-capable accounts can run arbitrary module functions as root: "+strings.Join(st.RPCCapable, ", ")+". Set rpc=0 unless you use Webmin clustering.")
	} else if st.Users > 0 {
		add("rpc", "ok", "No account can use Webmin RPC.")
	}
	return st, nil
}

// webminRunning checks the pidfile named in miniserv.conf. The path comes
// from a root-owned file but is still confined to /var or /run.
func webminRunning(env *Env, pidfile string) *bool {
	p := filepath.Clean(pidfile)
	if pidfile == "" || !filepath.IsAbs(p) || !(strings.HasPrefix(p, "/var/") || strings.HasPrefix(p, "/run/")) {
		return nil
	}
	b, err := readFixed(env.Roots.Path(p), 32)
	if err != nil {
		f := false
		return &f
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		f := false
		return &f
	}
	cmd, err := readFixed(env.Roots.Path("/proc/"+strconv.Itoa(pid)+"/cmdline"), 4096)
	ok := err == nil && strings.Contains(string(cmd), "miniserv.pl")
	return &ok
}

// versionLess compares dotted numeric versions ("2.105" < "2.670").
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y float64
		if i < len(pa) {
			x, _ = strconv.ParseFloat("0."+pa[i], 64) // Webmin minors are decimal fractions: 2.67 == 2.670
			if i == 0 {
				x, _ = strconv.ParseFloat(pa[i], 64)
			}
		}
		if i < len(pb) {
			y, _ = strconv.ParseFloat("0."+pb[i], 64)
			if i == 0 {
				y, _ = strconv.ParseFloat(pb[i], 64)
			}
		}
		if x != y {
			return x < y
		}
	}
	return false
}
