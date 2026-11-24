// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package netinfo

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tcpHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

// hex4 renders an IPv4 address the way the kernel prints it (host order).
func hex4(a, b, c, d byte) string {
	return fmt.Sprintf("%08X", binary.NativeEndian.Uint32([]byte{a, b, c, d}))
}

func hex6(addr [16]byte) string {
	var s strings.Builder
	for i := 0; i < 16; i += 4 {
		fmt.Fprintf(&s, "%08X", binary.NativeEndian.Uint32(addr[i:i+4]))
	}
	return s.String()
}

func tcpLine(n int, local, remote, st string, uid int, inode uint64) string {
	return fmt.Sprintf("   %d: %s %s %s 00000000:00000000 00:00000000 00000000 %5d        0 %d 1 0000000000000000 100 0 0 10 0\n", n, local, remote, st, uid, inode)
}

func TestParseProcNetTCPListenOnly(t *testing.T) {
	in := tcpHeader +
		tcpLine(0, hex4(127, 0, 0, 1)+":0277", "00000000:0000", "0A", 0, 100) +
		tcpLine(1, hex4(0, 0, 0, 0)+":0016", "00000000:0000", "0A", 0, 101) +
		tcpLine(2, hex4(192, 168, 1, 5)+":0016", hex4(192, 168, 1, 9)+":D431", "01", 1000, 102)
	socks, trunc, err := ParseProcNet(strings.NewReader(in), "tcp")
	if err != nil || trunc {
		t.Fatalf("err=%v trunc=%v", err, trunc)
	}
	if len(socks) != 2 {
		t.Fatalf("established connections must be dropped: %+v", socks)
	}
	if socks[0].Address != "127.0.0.1" || socks[0].Port != 631 || socks[0].Exposure != "loopback" {
		t.Errorf("loopback: %+v", socks[0])
	}
	if socks[1].Address != "0.0.0.0" || socks[1].Port != 22 || socks[1].Exposure != "all" || socks[1].inodes[0] != 101 {
		t.Errorf("wildcard: %+v", socks[1])
	}
}

func TestParseProcNetIPv6AndMapped(t *testing.T) {
	var any6, lo6, mapped [16]byte
	lo6[15] = 1
	copy(mapped[:], []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 10, 0, 0, 7})
	in := tcpHeader +
		tcpLine(0, hex6(any6)+":0050", hex6(any6)+":0000", "0A", 33, 7) +
		tcpLine(1, hex6(lo6)+":1F90", hex6(any6)+":0000", "0A", 0, 8) +
		tcpLine(2, hex6(mapped)+":01BB", hex6(any6)+":0000", "0A", 0, 9)
	socks, _, err := ParseProcNet(strings.NewReader(in), "tcp6")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, s := range socks {
		got = append(got, fmt.Sprintf("%s:%d/%s", s.Address, s.Port, s.Exposure))
	}
	want := "[:::80/all ::1:8080/loopback 10.0.0.7:443/address]"
	if fmt.Sprint(got) != want {
		t.Fatalf("got %v want %s", got, want)
	}
}

func TestParseProcNetUDPBoundUnconnected(t *testing.T) {
	in := tcpHeader +
		tcpLine(0, hex4(0, 0, 0, 0)+":0044", "00000000:0000", "07", 0, 1) + // dhcp client: bound
		tcpLine(1, hex4(10, 0, 0, 2)+":9C40", hex4(8, 8, 8, 8)+":0035", "01", 0, 2) // connected: dropped
	socks, _, err := ParseProcNet(strings.NewReader(in), "udp")
	if err != nil || len(socks) != 1 || socks[0].Port != 68 {
		t.Fatalf("err=%v socks=%+v", err, socks)
	}
}

func TestParseProcNetFailsClosed(t *testing.T) {
	cases := map[string]string{
		"no header":   tcpLine(0, hex4(0, 0, 0, 0)+":0016", "00000000:0000", "0A", 0, 1),
		"short line":  tcpHeader + "  0: 00000000:0016 00000000:0000 0A\n",
		"bad address": tcpHeader + tcpLine(0, "ZZZZZZZZ:0016", "00000000:0000", "0A", 0, 1),
		"bad uid":     tcpHeader + strings.Replace(tcpLine(0, hex4(0, 0, 0, 0)+":0016", "00000000:0000", "0A", 0, 1), "    0        0 1", "   -1        0 1", 1),
		"empty":       "",
	}
	for name, in := range cases {
		if _, _, err := ParseProcNet(strings.NewReader(in), "tcp"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, _, err := ParseProcNet(strings.NewReader(tcpHeader), "raw"); err == nil {
		t.Error("unknown table accepted")
	}
}

func TestParseProcNetTruncatesAtCap(t *testing.T) {
	var b strings.Builder
	b.WriteString(tcpHeader)
	for i := 0; i < maxSockets+5; i++ {
		b.WriteString(tcpLine(i, hex4(0, 0, 0, 0)+fmt.Sprintf(":%04X", i%65535+1), "00000000:0000", "0A", 0, uint64(i+1)))
	}
	socks, trunc, err := ParseProcNet(strings.NewReader(b.String()), "tcp")
	if err != nil || !trunc || len(socks) != maxSockets {
		t.Fatalf("err=%v trunc=%v n=%d", err, trunc, len(socks))
	}
}

func TestUnitFromCgroup(t *testing.T) {
	cases := []struct{ in, unit, ctr string }{
		{"0::/system.slice/ssh.service\n", "ssh.service", ""},
		{"0::/system.slice/docker-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.scope\n", "docker-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.scope", "0123456789ab"},
		{"0::/user.slice/user-1000.slice/user@1000.service/app.slice/syncthing.service\n", "syncthing.service", ""},
		{"12:pids:/\n1:name=systemd:/system.slice/nginx.service\n", "nginx.service", ""},
		{"1:name=systemd:/docker/0123456789abcdef0123\n", "", "0123456789ab"},
		{"garbage", "", ""},
	}
	for _, c := range cases {
		u, ctr := UnitFromCgroup([]byte(c.in))
		if u != c.unit || ctr != c.ctr {
			t.Errorf("%q: got %q %q", c.in, u, ctr)
		}
	}
}

func TestParsePasswd(t *testing.T) {
	m := ParsePasswd(strings.NewReader("root:x:0:0::/root:/bin/sh\n#c:x:5\nbad\nwww-data:x:33:33::/var/www:/usr/sbin/nologin\ndup:x:33:33::/:/\n"))
	if m[0] != "root" || m[33] != "www-data" || len(m) != 2 {
		t.Fatalf("%v", m)
	}
}

// writeProc builds a fixture /proc with one PID-1 socket table and processes
// whose fd symlinks point at socket inodes.
func writeProc(t *testing.T, root string, tcp string, procs map[int][]string, cgroups map[int]string) {
	t.Helper()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "proc/1/net"), 0o755))
	must(os.WriteFile(filepath.Join(root, "proc/1/net/tcp"), []byte(tcp), 0o644))
	for pid, links := range procs {
		d := filepath.Join(root, "proc", fmt.Sprint(pid))
		must(os.MkdirAll(filepath.Join(d, "fd"), 0o755))
		must(os.WriteFile(filepath.Join(d, "comm"), []byte(fmt.Sprintf("proc%d\n", pid)), 0o644))
		if cg, ok := cgroups[pid]; ok {
			must(os.WriteFile(filepath.Join(d, "cgroup"), []byte(cg), 0o644))
		}
		for i, l := range links {
			must(os.Symlink(l, filepath.Join(d, "fd", fmt.Sprint(i))))
		}
	}
	must(os.MkdirAll(filepath.Join(root, "etc"), 0o755))
	must(os.WriteFile(filepath.Join(root, "etc/passwd"), []byte("root:x:0:0::/:/bin/sh\nsvc:x:998:998::/:/bin/false\n"), 0o644))
}

func TestCollectPortsAttributesOwners(t *testing.T) {
	root := t.TempDir()
	tcp := tcpHeader +
		tcpLine(0, hex4(0, 0, 0, 0)+":0016", "00000000:0000", "0A", 0, 500) +
		tcpLine(1, hex4(0, 0, 0, 0)+":0016", "00000000:0000", "0A", 0, 501) + // SO_REUSEPORT twin
		tcpLine(2, hex4(127, 0, 0, 1)+":1F90", "00000000:0000", "0A", 998, 600)
	writeProc(t, root, tcp, map[int][]string{
		1:   {"socket:[500]"},
		200: {"/dev/null", "socket:[501]", "pipe:[9]"},
		300: {"socket:[999]"},
	}, map[int]string{200: "0::/system.slice/ssh.service\n"})
	pm, err := CollectPorts(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pm.Sockets) != 2 {
		t.Fatalf("twins must merge: %+v", pm.Sockets)
	}
	ssh := pm.Sockets[0]
	if ssh.Port != 22 || len(ssh.Owners) != 2 || ssh.Owners[0].PID != 1 || ssh.Owners[1].Unit != "ssh.service" || ssh.Owners[1].Command != "proc200" || ssh.Account != "root" {
		t.Fatalf("ssh row: %+v", ssh)
	}
	web := pm.Sockets[1]
	if web.Port != 8080 || len(web.Owners) != 0 || web.Account != "svc" {
		t.Fatalf("unattributed row keeps account only: %+v", web)
	}
	if pm.ProcessesScanned != 3 || fmt.Sprint(pm.Sources) != "[tcp]" {
		t.Fatalf("scan counts: %+v", pm)
	}
}

func TestCollectPortsCountsHiddenProcesses(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every fd table")
	}
	root := t.TempDir()
	tcp := tcpHeader + tcpLine(0, hex4(0, 0, 0, 0)+":0050", "00000000:0000", "0A", 33, 70)
	writeProc(t, root, tcp, map[int][]string{40: {"socket:[70]"}}, nil)
	if err := os.Chmod(filepath.Join(root, "proc/40/fd"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "proc/40/fd"), 0o755) })
	pm, err := CollectPorts(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if pm.ProcessesHidden != 1 || len(pm.Notes) != 1 || len(pm.Sockets[0].Owners) != 0 {
		t.Fatalf("hidden: %+v", pm)
	}
}

func TestCollectPortsNoTables(t *testing.T) {
	if _, err := CollectPorts(context.Background(), t.TempDir()); err == nil {
		t.Fatal("no socket table must be an error, not an empty map")
	}
}

func TestCollectPortsHonoursCancel(t *testing.T) {
	root := t.TempDir()
	writeProc(t, root, tcpHeader+tcpLine(0, hex4(0, 0, 0, 0)+":0016", "00000000:0000", "0A", 0, 5), map[int][]string{9: {"socket:[5]"}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CollectPorts(ctx, root); err == nil {
		t.Fatal("cancelled scan returned a map")
	}
}
