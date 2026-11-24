// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"strings"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/netinfo"
)

// M11 read-only networking (ADR 0008 Amendment 9, Proposed). Both ops read
// fixed files as root in-process; neither takes an argument, opens a socket,
// or changes the host. network.firewall.config also runs one fixed,
// unprivileged `systemctl show` over the closed netinfo.FirewallUnits list.

// networkPortsList is the host port-use map: PID 1's socket tables (the host
// network namespace, although the broker unit itself has PrivateNetwork=yes)
// plus owner attribution. The broker holds only CAP_SETUID/CAP_SETGID, and the
// kernel's ptrace check on /proc/<pid>/fd needs the reader's capabilities to
// cover the target's, so fd tables are readable for almost no process (VM
// check 2026-11-24). Owners therefore also come from systemd's own socket
// list (one fixed unprivileged `systemctl list-sockets`) and, labelled
// inferred, from the socket's account when all its processes share one unit
// (/proc/<pid>/status and cgroup need no ptrace). CAP_SYS_PTRACE is not added.
func networkPortsList(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	root := env.Roots.Root
	if root == "" {
		root = "/"
	}
	run := env.PortsSystemctl
	if run == nil && env.Runner != nil && env.Runner.Available("systemctl") {
		run = func(ctx context.Context, args []string) ([]byte, int, error) {
			res, err := env.Runner.Run(ctx, "systemctl", args...)
			return res.Stdout, res.ExitCode, err
		}
	}
	var units []netinfo.SocketUnit
	if run != nil {
		units = systemdSocketUnits(ctx, run)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	pm, err := netinfo.CollectPorts(ctx, root, units)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the host socket tables could not be read"}
	}
	return pm, nil
}

// portsSocketsArgs is the fixed systemd socket listing (same argv as
// services.sockets.list).
var portsSocketsArgs = []string{"list-sockets", "--all", "--output=json", "--no-pager"}

// socketListenArgs is the fixed query for the listen type of socket units:
// list-sockets JSON has listen, unit and activates but no type (Ubuntu 26.04
// guest, 2026-11-24), and an IP:port alone does not say TCP or UDP. The
// names passed unitNameRE and end in .socket; they follow "--".
func socketListenArgs(units []string) []string {
	return append([]string{"show", "--no-pager", "--property=Id,Listen", "--"}, units...)
}

const maxSocketUnits = 256

// systemdSocketUnits returns the IP listen addresses of systemd socket units
// with their type (Stream = TCP, Datagram = UDP). Any failure yields none:
// owners then stay unidentified rather than guessed.
func systemdSocketUnits(ctx context.Context, run func(context.Context, []string) ([]byte, int, error)) []netinfo.SocketUnit {
	out, code, err := run(ctx, portsSocketsArgs)
	if err != nil || code != 0 {
		return nil
	}
	var rows []rawSocket
	if json.Unmarshal(out, &rows) != nil {
		return nil
	}
	activates := map[string]string{}
	var names []string
	for _, r := range rows {
		// The runner refuses '\\' (systemd escapes) in an argument, and one bad
		// name would fail the whole show; such units are skipped.
		if !unitNameRE.MatchString(r.Unit) || !strings.HasSuffix(r.Unit, ".socket") || strings.ContainsAny(r.Unit, "\\") || r.Unit[0] == '-' {
			continue
		}
		if _, _, ok := netinfo.ParseListen(r.Listen); !ok {
			continue
		}
		if _, seen := activates[r.Unit]; !seen {
			if len(names) >= maxSocketUnits {
				continue
			}
			names = append(names, r.Unit)
			activates[r.Unit] = ""
		}
		if r.Activates != nil && unitNameRE.MatchString(*r.Activates) {
			activates[r.Unit] = *r.Activates
		}
	}
	if len(names) == 0 {
		return nil
	}
	out, code, err = run(ctx, socketListenArgs(names))
	if err != nil || code != 0 {
		return nil
	}
	return parseSocketListen(out, activates)
}

// parseSocketListen reads `systemctl show --property=Id,Listen` blocks:
// "Id=ssh.socket" then one "Listen=0.0.0.0:22 (Stream)" line per address.
// Only units from the listing (activates) are kept.
func parseSocketListen(b []byte, activates map[string]string) []netinfo.SocketUnit {
	var res []netinfo.SocketUnit
	for _, block := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n\n") {
		var id string
		var listens []string
		for _, line := range strings.Split(block, "\n") {
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			switch k {
			case "Id":
				id = v
			case "Listen":
				listens = append(listens, v)
			}
		}
		act, known := activates[id]
		if !known {
			continue
		}
		for _, l := range listens {
			addr, typ, ok := strings.Cut(strings.TrimSpace(l), " (")
			typ = strings.TrimSuffix(typ, ")")
			if !ok || (typ != "Stream" && typ != "Datagram") || len(res) >= 1024 {
				continue
			}
			res = append(res, netinfo.SocketUnit{Listen: clean(addr, 200), Type: typ, Unit: id, Activates: act})
		}
	}
	return res
}

// firewallShowArgs is the fixed state query. Every name is a compile-time
// constant from netinfo.FirewallUnits and follows "--".
func firewallShowArgs() []string {
	args := []string{"show", "--no-pager", "--property=Id,LoadState,ActiveState,UnitFileState", "--"}
	return append(args, netinfo.FirewallUnits...)
}

// networkFirewallConfig detects the active firewall backend and returns its
// saved configuration. It does not read the live kernel ruleset: the broker
// has no CAP_NET_ADMIN and runs in a private network namespace.
func networkFirewallConfig(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	show := env.FirewallShow
	if show == nil && env.Runner != nil && env.Runner.Available("systemctl") {
		show = func(ctx context.Context) ([]byte, int, error) {
			res, err := env.Runner.Run(ctx, "systemctl", firewallShowArgs()...)
			return res.Stdout, res.ExitCode, err
		}
	}
	// nil units = systemd could not be asked: the backend is reported unknown.
	var units map[string]netinfo.UnitState
	if show != nil {
		out, code, err := show(ctx)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil && code == 0 {
			units = netinfo.ParseUnitStates(out)
		}
	}
	root := env.Roots.Root
	if root == "" {
		root = "/"
	}
	return netinfo.CollectFirewall(root, units), nil
}
