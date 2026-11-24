// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/netinfo"
)

// Graphify review 2026-11-24: the M11 broker ops had no direct tests beyond
// the registry snapshot.

func TestNetworkOpsRefuseArguments(t *testing.T) {
	for _, h := range []func(context.Context, *Env, json.RawMessage) (any, error){networkPortsList, networkFirewallConfig} {
		_, err := h(context.Background(), &Env{Roots: Roots{Root: t.TempDir()}}, json.RawMessage(`{"path":"/etc/shadow"}`))
		var pe *proto.Error
		if !errors.As(err, &pe) || pe.Code != proto.CodeBadRequest {
			t.Fatalf("arguments accepted: %v", err)
		}
	}
}

func TestNetworkPortsListReadsPID1Tables(t *testing.T) {
	root := t.TempDir()
	write(t, root, "proc/1/net/tcp", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"+
		"   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 4242 1 0 100 0 0 10 0\n", 0o644)
	if err := os.MkdirAll(filepath.Join(root, "proc/77/fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[4242]", filepath.Join(root, "proc/77/fd/3")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "proc/77/comm", "sshd\n", 0o644)
	write(t, root, "proc/77/cgroup", "0::/system.slice/ssh.service\n", 0o644)
	got, err := networkPortsList(context.Background(), &Env{Roots: Roots{Root: root}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pm := got.(netinfo.PortMap)
	if len(pm.Sockets) != 1 || pm.Sockets[0].Port != 22 || len(pm.Sockets[0].Owners) != 1 || pm.Sockets[0].Owners[0].Unit != "ssh.service" {
		t.Fatalf("%+v", pm)
	}
}

func TestNetworkPortsListUnavailableWithoutTables(t *testing.T) {
	_, err := networkPortsList(context.Background(), &Env{Roots: Roots{Root: t.TempDir()}}, nil)
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeUnavailable || strings.Contains(pe.Message, "/proc") {
		t.Fatalf("want a generic unavailable error, got %v", err)
	}
}

func firewallShowFixture(active string) string {
	var b strings.Builder
	for _, u := range netinfo.FirewallUnits {
		state := "inactive"
		if u == active {
			state = "active"
		}
		b.WriteString("Id=" + u + "\nLoadState=loaded\nActiveState=" + state + "\nUnitFileState=enabled\n\n")
	}
	return b.String()
}

func TestNetworkFirewallConfigUsesFixedShowAndActiveBackend(t *testing.T) {
	root := t.TempDir()
	write(t, root, "etc/nftables.conf", "table inet filter {\n\tchain input {\n\t\ttype filter hook input priority 0; policy drop;\n\t}\n}\n", 0o644)
	called := 0
	env := &Env{Roots: Roots{Root: root}, FirewallShow: func(context.Context) ([]byte, int, error) {
		called++
		return []byte(firewallShowFixture("nftables.service")), 0, nil
	}}
	got, err := networkFirewallConfig(context.Background(), env, nil)
	if err != nil || called != 1 {
		t.Fatalf("err=%v called=%d", err, called)
	}
	snap := got.(netinfo.FirewallSnapshot)
	if snap.Active != netinfo.FirewallNftables || snap.Nftables == nil || snap.Nftables.Tables[0].Chains[0].Policy != "drop" {
		t.Fatalf("%+v", snap)
	}
	// The argv is fixed: show, no pager, four properties, then the closed unit list.
	args := firewallShowArgs()
	if strings.Join(args[:4], " ") != "show --no-pager --property=Id,LoadState,ActiveState,UnitFileState --" || strings.Join(args[4:], " ") != strings.Join(netinfo.FirewallUnits, " ") {
		t.Fatalf("argv: %v", args)
	}
}

func TestNetworkFirewallConfigUnknownWhenSystemdFails(t *testing.T) {
	for name, env := range map[string]*Env{
		"no runner":   {Roots: Roots{Root: t.TempDir()}},
		"exit status": {Roots: Roots{Root: t.TempDir()}, FirewallShow: func(context.Context) ([]byte, int, error) { return []byte(firewallShowFixture("ufw.service")), 1, nil }},
		"exec error":  {Roots: Roots{Root: t.TempDir()}, FirewallShow: func(context.Context) ([]byte, int, error) { return nil, 0, errors.New("boom") }},
		"short answer": {Roots: Roots{Root: t.TempDir()}, FirewallShow: func(context.Context) ([]byte, int, error) {
			return []byte("Id=ufw.service\nActiveState=inactive\n"), 0, nil
		}},
	} {
		got, err := networkFirewallConfig(context.Background(), env, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if a := got.(netinfo.FirewallSnapshot).Active; a != netinfo.FirewallUnknown {
			t.Errorf("%s: active=%q, want unknown", name, a)
		}
	}
}

func TestNetworkFirewallConfigHonoursCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	env := &Env{Roots: Roots{Root: t.TempDir()}, FirewallShow: func(context.Context) ([]byte, int, error) {
		cancel()
		return nil, -1, context.Canceled
	}}
	if _, err := networkFirewallConfig(ctx, env, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
