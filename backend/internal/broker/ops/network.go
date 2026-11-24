// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/netinfo"
)

// M11 read-only networking (ADR 0008 Amendment 9, Proposed). Both ops read
// fixed files as root in-process; neither takes an argument, opens a socket,
// or changes the host. network.firewall.config also runs one fixed,
// unprivileged `systemctl show` over the closed netinfo.FirewallUnits list.

// networkPortsList is the host port-use map: PID 1's socket tables (the host
// network namespace, although the broker unit itself has PrivateNetwork=yes)
// plus owner attribution from /proc/<pid>/fd, comm and cgroup. With only
// CAP_SETUID/CAP_SETGID the broker reads the fd tables of uid-0 processes;
// other processes are counted as hidden and keep their socket's account.
func networkPortsList(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	root := env.Roots.Root
	if root == "" {
		root = "/"
	}
	pm, err := netinfo.CollectPorts(ctx, root)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the host socket tables could not be read"}
	}
	return pm, nil
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
	var units map[string]netinfo.UnitState
	if env.Runner != nil && env.Runner.Available("systemctl") {
		res, err := env.Runner.Run(ctx, "systemctl", firewallShowArgs()...)
		if err == nil && res.ExitCode == 0 {
			units = netinfo.ParseUnitStates(res.Stdout)
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	root := env.Roots.Root
	if root == "" {
		root = "/"
	}
	return netinfo.CollectFirewall(root, units), nil
}
