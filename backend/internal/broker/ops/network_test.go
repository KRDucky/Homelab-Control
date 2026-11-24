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

// The listing fixture is the real shape from the Ubuntu 26.04 guest
// (list-sockets-as-service-user.json, 2026-11-24): no "type" field.
func TestNetworkPortsListUsesSystemdSocketList(t *testing.T) {
	root := t.TempDir()
	write(t, root, "proc/1/net/tcp", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"+
		"   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 9 1 0 100 0 0 10 0\n", 0o644)
	write(t, root, "proc/1/net/udp", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops\n"+
		"   0: 00000000:0016 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 10 2 0 0\n", 0o644)
	listing := `[{"listen":"0.0.0.0:22","unit":"ssh.socket","activates":"ssh.service"},` +
		`{"listen":"[::]:22","unit":"ssh.socket","activates":"ssh.service"},` +
		`{"listen":"/run/ssh-unix-local/socket","unit":"sshd-unix-local.socket","activates":null},` +
		`{"listen":"0.0.0.0:22","unit":"bad name;.socket","activates":null},` +
		`{"listen":"0.0.0.0:23","unit":"a\\x2db.socket","activates":null}]`
	show := "Id=ssh.socket\nListen=0.0.0.0:22 (Stream)\nListen=[::]:22 (Stream)\n\nId=other.socket\nListen=0.0.0.0:22 (Datagram)\n"
	var calls [][]string
	env := &Env{Roots: Roots{Root: root}, PortsSystemctl: func(_ context.Context, args []string) ([]byte, int, error) {
		calls = append(calls, args)
		if args[0] == "list-sockets" {
			return []byte(listing), 0, nil
		}
		return []byte(show), 0, nil
	}}
	got, err := networkPortsList(context.Background(), env, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || strings.Join(calls[0], " ") != "list-sockets --all --output=json --no-pager" ||
		strings.Join(calls[1], " ") != "show --no-pager --property=Id,Listen -- ssh.socket" {
		t.Fatalf("argv: %q", calls)
	}
	pm := got.(netinfo.PortMap)
	var tcp, udp netinfo.Socket
	for _, s := range pm.Sockets {
		if s.Proto == "tcp" {
			tcp = s
		} else {
			udp = s
		}
	}
	if o := tcp.Owners; len(o) != 1 || o[0].Socket != "ssh.socket" || o[0].Unit != "ssh.service" || o[0].Source != netinfo.OwnerSystemdSocket {
		t.Fatalf("tcp owners: %+v", o)
	}
	if len(udp.Owners) != 0 {
		t.Fatalf("a Stream socket unit must not claim a UDP socket, nor an unlisted unit: %+v", udp.Owners)
	}
	// A failing listing is not an error: owners just stay unknown.
	for name, f := range map[string]func(context.Context, []string) ([]byte, int, error){
		"bad json": func(context.Context, []string) ([]byte, int, error) { return []byte("not json"), 0, nil },
		"exit 1":   func(context.Context, []string) ([]byte, int, error) { return []byte(listing), 1, nil },
		"show fails": func(_ context.Context, a []string) ([]byte, int, error) {
			if a[0] == "list-sockets" {
				return []byte(listing), 0, nil
			}
			return nil, 0, errors.New("boom")
		},
	} {
		env.PortsSystemctl = f
		got, err = networkPortsList(context.Background(), env, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, s := range got.(netinfo.PortMap).Sockets {
			if len(s.Owners) != 0 {
				t.Fatalf("%s: owners guessed: %+v", name, s.Owners)
			}
		}
	}
}
