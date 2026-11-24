// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package netinfo is the read-only M11 networking inventory: the host-wide
// port-use map, kernel bonding (LACP) detection, and the saved configuration
// of the active firewall backend. It only reads bounded files under a root
// prefix (tests point it at a fixture tree). It never executes a program,
// never opens a socket, never loads a module and never changes the host.
package netinfo

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Owner is one process that holds a listening socket.
type Owner struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	// Unit is the systemd unit (or scope) from the process cgroup, when known.
	Unit string `json:"unit,omitempty"`
	// Container is the short container ID from a docker/libpod/containerd
	// cgroup, when the process runs in one.
	Container string `json:"container,omitempty"`
}

// Socket is one listening (TCP) or bound, unconnected (UDP) socket in the
// network namespace of PID 1.
type Socket struct {
	Proto   string `json:"proto"` // tcp | tcp6 | udp | udp6
	Address string `json:"address"`
	Port    int    `json:"port"`
	// Exposure is "loopback" (127.0.0.0/8, ::1), "all" (0.0.0.0, ::) or
	// "address" (one specific address).
	Exposure string `json:"exposure"`
	UID      int    `json:"uid"`
	// Account is the passwd name of UID ("" when it is not in /etc/passwd).
	Account string  `json:"account,omitempty"`
	Owners  []Owner `json:"owners"`
	inodes  []uint64
}

// PortMap is the read-only host port-use map.
type PortMap struct {
	Sockets   []Socket `json:"sockets"`
	Truncated bool     `json:"truncated"`
	// Namespace says which network namespace was read.
	Namespace string `json:"namespace"`
	// ProcessesScanned and ProcessesHidden count processes whose open files
	// were (not) readable for owner attribution.
	ProcessesScanned int `json:"processesScanned"`
	ProcessesHidden  int `json:"processesHidden"`
	// Sources lists the kernel tables that were read ("tcp", "udp6", ...).
	Sources []string `json:"sources"`
	Notes   []string `json:"notes"`
}

const (
	procNetMaxBytes  = 8 << 20
	procNetMaxLine   = 1024
	maxSockets       = 4096
	maxProcs         = 32768
	maxFDsPerProc    = 65536
	maxOwnersPerSock = 16
	passwdMaxBytes   = 1 << 20
	smallFileMax     = 4096
)

// socket states of interest (include/net/tcp_states.h).
const (
	stateListen = 0x0A
	stateClose  = 0x07
)

// ParseProcNet parses one /proc/net/{tcp,tcp6,udp,udp6} table and returns the
// listening TCP sockets or bound, unconnected UDP sockets. proto is the table
// name. The table is bounded: overflow sets truncated instead of failing. A
// malformed header or data line fails the parse.
func ParseProcNet(r io.Reader, proto string) (socks []Socket, truncated bool, err error) {
	v6 := strings.HasSuffix(proto, "6")
	udp := strings.HasPrefix(proto, "udp")
	if proto != "tcp" && proto != "tcp6" && proto != "udp" && proto != "udp6" {
		return nil, false, fmt.Errorf("unknown socket table %q", proto)
	}
	sc := bufio.NewScanner(io.LimitReader(r, procNetMaxBytes+1))
	sc.Buffer(make([]byte, 0, procNetMaxLine), procNetMaxLine)
	size := 0
	first := true
	for sc.Scan() {
		line := sc.Text()
		size += len(line) + 1
		if size > procNetMaxBytes {
			return socks, true, nil
		}
		if first {
			first = false
			if !strings.Contains(line, "local_address") {
				return nil, false, errors.New("socket table: unexpected header")
			}
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) < 10 {
			return nil, false, errors.New("socket table: short line")
		}
		st, err := strconv.ParseUint(f[3], 16, 8)
		if err != nil {
			return nil, false, errors.New("socket table: bad state")
		}
		if udp {
			if st != stateClose || !zeroRemote(f[2]) {
				continue
			}
		} else if st != stateListen {
			continue
		}
		addr, port, err := parseHexAddr(f[1], v6)
		if err != nil {
			return nil, false, err
		}
		uid, err := strconv.Atoi(f[7])
		if err != nil || uid < 0 {
			return nil, false, errors.New("socket table: bad uid")
		}
		inode, err := strconv.ParseUint(f[9], 10, 64)
		if err != nil {
			return nil, false, errors.New("socket table: bad inode")
		}
		if len(socks) >= maxSockets {
			return socks, true, nil
		}
		socks = append(socks, Socket{
			Proto: proto, Address: addr.String(), Port: port, Exposure: exposure(addr),
			UID: uid, Owners: []Owner{}, inodes: []uint64{inode},
		})
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, false, errors.New("socket table: line too long")
		}
		return nil, false, fmt.Errorf("socket table: %w", err)
	}
	if first {
		return nil, false, errors.New("socket table: empty")
	}
	return socks, false, nil
}

func zeroRemote(s string) bool {
	h, p, ok := strings.Cut(s, ":")
	return ok && strings.Trim(h, "0") == "" && strings.Trim(p, "0") == ""
}

// parseHexAddr decodes the kernel's "ADDR:PORT" field. The address words are
// printed in host byte order (seq_printf of __be32 words), so they are
// converted back with the native byte order; the port is a plain hex number.
func parseHexAddr(s string, v6 bool) (netip.Addr, int, error) {
	h, p, ok := strings.Cut(s, ":")
	want := 8
	if v6 {
		want = 32
	}
	if !ok || len(h) != want {
		return netip.Addr{}, 0, errors.New("socket table: bad address")
	}
	raw, err := hex.DecodeString(h)
	if err != nil {
		return netip.Addr{}, 0, errors.New("socket table: bad address")
	}
	port, err := strconv.ParseUint(p, 16, 16)
	if err != nil {
		return netip.Addr{}, 0, errors.New("socket table: bad port")
	}
	var b [16]byte
	for i := 0; i < len(raw); i += 4 {
		w := binary.NativeEndian.Uint32(raw[i : i+4])
		binary.BigEndian.PutUint32(b[i:i+4], w)
	}
	if !v6 {
		return netip.AddrFrom4([4]byte{b[0], b[1], b[2], b[3]}), int(port), nil
	}
	a := netip.AddrFrom16(b)
	if a.Is4In6() {
		a = a.Unmap()
	}
	return a, int(port), nil
}

func exposure(a netip.Addr) string {
	switch {
	case a.IsLoopback():
		return "loopback"
	case a.IsUnspecified():
		return "all"
	default:
		return "address"
	}
}

// ParsePasswd maps uid to account name from bounded passwd(5) content.
// Malformed lines are skipped; the first name for a uid wins.
func ParsePasswd(r io.Reader) map[int]string {
	out := map[int]string{}
	sc := bufio.NewScanner(io.LimitReader(r, passwdMaxBytes))
	sc.Buffer(make([]byte, 0, 1024), 4096)
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		if len(f) < 3 || f[0] == "" || strings.HasPrefix(f[0], "#") {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil || uid < 0 {
			continue
		}
		if _, ok := out[uid]; !ok && len(f[0]) <= 64 {
			out[uid] = cleanText(f[0], 64)
		}
	}
	return out
}

// UnitFromCgroup returns the systemd unit or scope and a short container ID
// from /proc/<pid>/cgroup content. The unified (0::) line wins; on a hybrid
// or v1 host the name=systemd line is used.
func UnitFromCgroup(b []byte) (unit, container string) {
	var path string
	for _, line := range strings.Split(string(b), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "0" && parts[1] == "" {
			path = parts[2]
			break
		}
		if parts[1] == "name=systemd" && path == "" {
			path = parts[2]
		}
	}
	if path == "" {
		return "", ""
	}
	elems := strings.Split(strings.Trim(path, "/"), "/")
	for i := len(elems) - 1; i >= 0; i-- {
		e := elems[i]
		if container == "" {
			container = containerID(e)
			if container == "" && i > 0 && (elems[i-1] == "docker" || elems[i-1] == "libpod") && isHex(e) && len(e) >= 12 {
				container = e[:12]
			}
		}
		if unit == "" && (strings.HasSuffix(e, ".service") || strings.HasSuffix(e, ".scope") || strings.HasSuffix(e, ".socket")) {
			unit = cleanText(e, 256)
		}
	}
	return unit, container
}

func containerID(e string) string {
	for _, pre := range []string{"docker-", "libpod-", "cri-containerd-", "crio-"} {
		if rest, ok := strings.CutPrefix(e, pre); ok {
			id := strings.TrimSuffix(rest, ".scope")
			if len(id) >= 12 && isHex(id) {
				return id[:12]
			}
		}
	}
	return ""
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// CollectPorts reads the socket tables of PID 1's network namespace (the host
// namespace, also from a process that runs in a private one) under root and
// attributes each socket to the processes whose open files reference its
// inode. A process whose fd table cannot be read is counted as hidden; its
// sockets still show the owning account.
func CollectPorts(ctx context.Context, root string) (PortMap, error) {
	pm := PortMap{Sockets: []Socket{}, Notes: []string{}, Sources: []string{}, Namespace: "host (network namespace of PID 1)"}
	var all []Socket
	for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
		f, err := os.Open(filepath.Join(root, "/proc/1/net", proto))
		if errors.Is(err, os.ErrNotExist) {
			continue // e.g. IPv6 disabled
		}
		if err != nil {
			return pm, fmt.Errorf("cannot read the %s socket table", proto)
		}
		socks, trunc, err := ParseProcNet(f, proto)
		f.Close()
		if err != nil {
			return pm, err
		}
		pm.Sources = append(pm.Sources, proto)
		pm.Truncated = pm.Truncated || trunc
		all = append(all, socks...)
	}
	if len(pm.Sources) == 0 {
		return pm, errors.New("no socket table is readable")
	}
	all = mergeSockets(all)
	if len(all) > maxSockets {
		all, pm.Truncated = all[:maxSockets], true
	}
	if err := attributeOwners(ctx, root, all, &pm); err != nil {
		return pm, err
	}
	if b, err := readSmall(filepath.Join(root, "/etc/passwd"), passwdMaxBytes); err == nil {
		names := ParsePasswd(strings.NewReader(string(b)))
		for i := range all {
			all[i].Account = names[all[i].UID]
		}
	}
	pm.Sockets = all
	if pm.ProcessesHidden > 0 {
		pm.Notes = append(pm.Notes, fmt.Sprintf("%d processes could not be inspected; sockets they hold show the owning account only.", pm.ProcessesHidden))
	}
	return pm, nil
}

// mergeSockets folds rows with the same protocol, address and port (for
// example SO_REUSEPORT listeners) into one, keeping every inode, and sorts.
func mergeSockets(in []Socket) []Socket {
	idx := map[string]int{}
	out := make([]Socket, 0, len(in))
	for _, s := range in {
		k := s.Proto + "|" + s.Address + "|" + strconv.Itoa(s.Port)
		if i, ok := idx[k]; ok {
			out[i].inodes = append(out[i].inodes, s.inodes...)
			continue
		}
		idx[k] = len(out)
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		if out[i].Proto != out[j].Proto {
			return out[i].Proto < out[j].Proto
		}
		return out[i].Address < out[j].Address
	})
	return out
}

func attributeOwners(ctx context.Context, root string, socks []Socket, pm *PortMap) error {
	want := map[uint64]int{}
	for i, s := range socks {
		for _, ino := range s.inodes {
			want[ino] = i
		}
	}
	if len(want) == 0 {
		return nil
	}
	procDir := filepath.Join(root, "/proc")
	ents, err := os.ReadDir(procDir)
	if err != nil {
		return errors.New("cannot list processes")
	}
	seen := 0
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if seen >= maxProcs {
			pm.Truncated = true
			break
		}
		seen++
		if err := ctx.Err(); err != nil {
			return err
		}
		pdir := filepath.Join(procDir, e.Name())
		fds, err := readDirNames(filepath.Join(pdir, "fd"), maxFDsPerProc)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) { // exited between list and read
				pm.ProcessesHidden++
			}
			continue
		}
		pm.ProcessesScanned++
		var hit map[int]bool
		for _, fd := range fds {
			l, err := os.Readlink(filepath.Join(pdir, "fd", fd))
			if err != nil {
				continue
			}
			ino, ok := socketInode(l)
			if !ok {
				continue
			}
			if i, ok := want[ino]; ok {
				if hit == nil {
					hit = map[int]bool{}
				}
				hit[i] = true
			}
		}
		if len(hit) == 0 {
			continue
		}
		o := Owner{PID: pid}
		if b, err := readSmall(filepath.Join(pdir, "comm"), 64); err == nil {
			o.Command = cleanText(string(b), 32)
		}
		if b, err := readSmall(filepath.Join(pdir, "cgroup"), smallFileMax); err == nil {
			o.Unit, o.Container = UnitFromCgroup(b)
		}
		for i := range hit {
			if len(socks[i].Owners) < maxOwnersPerSock && !hasPID(socks[i].Owners, pid) {
				socks[i].Owners = append(socks[i].Owners, o)
			}
		}
	}
	for i := range socks {
		sort.Slice(socks[i].Owners, func(a, b int) bool { return socks[i].Owners[a].PID < socks[i].Owners[b].PID })
	}
	return nil
}

func hasPID(owners []Owner, pid int) bool {
	for _, o := range owners {
		if o.PID == pid {
			return true
		}
	}
	return false
}

func socketInode(l string) (uint64, bool) {
	rest, ok := strings.CutPrefix(l, "socket:[")
	if !ok || !strings.HasSuffix(rest, "]") {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSuffix(rest, "]"), 10, 64)
	return n, err == nil
}

func readDirNames(dir string, limit int) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.Readdirnames(limit)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return names, nil
}

// readSmall reads at most limit bytes of a file; longer content is cut.
func readSmall(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}

// cleanText drops control characters, trims and caps s at max bytes on a
// rune boundary.
func cleanText(s string, max int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut]
}
