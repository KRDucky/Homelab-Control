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
	list := env.PortsSockets
	if list == nil && env.Runner != nil && env.Runner.Available("systemctl") {
		list = func(ctx context.Context) ([]byte, int, error) {
			res, err := env.Runner.Run(ctx, "systemctl", portsSocketsArgs...)
			return res.Stdout, res.ExitCode, err
		}
	}
	var units []netinfo.SocketUnit
	if list != nil {
		out, code, err := list(ctx)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil && code == 0 {
			units = socketUnits(out)
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

// socketUnits keeps the IP listen rows of `systemctl list-sockets` JSON with
// a valid .socket unit name. Bad output yields none (owners stay unknown).
func socketUnits(b []byte) []netinfo.SocketUnit {
	var rows []rawSocket
	if json.Unmarshal(b, &rows) != nil {
		return nil
	}
	var out []netinfo.SocketUnit
	for _, r := range rows {
		if !unitNameRE.MatchString(r.Unit) || !strings.HasSuffix(r.Unit, ".socket") || len(out) >= 1024 {
			continue
		}
		u := netinfo.SocketUnit{Listen: clean(r.Listen, 200), Type: clean(r.Type, 16), Unit: r.Unit}
		if r.Activates != nil && unitNameRE.MatchString(*r.Activates) {
			u.Activates = *r.Activates
		}
		out = append(out, u)
	}
	return out
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
