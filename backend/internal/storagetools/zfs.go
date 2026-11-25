// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagetools

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// OpenZFS: the helper runs zpool and zfs with fixed arguments and these
// parsers decode their documented script output (-H: tab-separated, -p:
// exact numbers) and the `zpool status` report. `zpool status -j` would be
// simpler but exists only from OpenZFS 2.3; Ubuntu 22.04 and 24.04 ship 2.1
// and 2.2, and the text report is the same on all of them.

// Bounds on ZFS output.
const (
	MaxZFSBytes    = 512 << 10 // per command
	MaxZFSPools    = 64
	MaxZFSVdevs    = 512 // per pool
	MaxZFSDatasets = 2048
	maxZFSText     = 1024 // status/action/scan/errors text
)

// ZpoolListArgs, ZpoolStatusArgs and ZfsListArgs are the fixed templates.
func ZpoolListArgs() []string {
	return []string{"list", "-H", "-p", "-o", "name,size,alloc,free,frag,cap,health"}
}

func ZpoolStatusArgs() []string { return []string{"status", "-p"} }

func ZfsListArgs() []string {
	return []string{"list", "-H", "-p", "-o", "name,used,avail,refer,mountpoint,compression,type", "-t", "filesystem,volume"}
}

// ZFSReport is the OpenZFS view: pools with their vdev tree, and datasets.
type ZFSReport struct {
	Pools     []ZFSPool    `json:"pools"`
	Datasets  []ZFSDataset `json:"datasets"`
	Truncated bool         `json:"truncated,omitempty"`
}

// ZFSPool joins `zpool list` (capacity, health) with `zpool status` (the
// report lines and the vdev tree).
type ZFSPool struct {
	Name    string    `json:"name"`
	Health  string    `json:"health"`
	Size    *uint64   `json:"size,omitempty"`
	Alloc   *uint64   `json:"alloc,omitempty"`
	Free    *uint64   `json:"free,omitempty"`
	FragPct *uint64   `json:"frag_pct,omitempty"`
	CapPct  *uint64   `json:"cap_pct,omitempty"`
	State   string    `json:"state,omitempty"`
	Status  string    `json:"status,omitempty"`
	Action  string    `json:"action,omitempty"`
	Scan    string    `json:"scan,omitempty"`
	Errors  string    `json:"errors,omitempty"`
	Vdevs   []ZFSVdev `json:"vdevs"`
}

// ZFSVdev is one row of the `zpool status` config tree. Depth 0 is the pool
// itself; group rows (logs, cache, spares, special, dedup) have no state.
type ZFSVdev struct {
	Name  string  `json:"name"`
	Depth int     `json:"depth"`
	Group bool    `json:"group,omitempty"`
	State string  `json:"state,omitempty"`
	Read  *uint64 `json:"read_errors,omitempty"`
	Write *uint64 `json:"write_errors,omitempty"`
	Cksum *uint64 `json:"checksum_errors,omitempty"`
	Note  string  `json:"note,omitempty"` // text after the counters, e.g. "(resilvering)"
}

// ZFSDataset is one `zfs list` row.
type ZFSDataset struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Used        *uint64 `json:"used,omitempty"`
	Avail       *uint64 `json:"avail,omitempty"`
	Refer       *uint64 `json:"refer,omitempty"`
	Mountpoint  string  `json:"mountpoint,omitempty"`
	Compression string  `json:"compression,omitempty"`
}

// ParseZFS decodes the three command outputs into one report. A pool listed
// by `zpool list` but missing from the status report keeps its capacity and
// health with an empty tree.
func ParseZFS(list, status, datasets []byte) (ZFSReport, error) {
	for _, b := range [][]byte{list, status, datasets} {
		if len(b) > MaxZFSBytes {
			return ZFSReport{}, fmt.Errorf("zfs output exceeds %d bytes", MaxZFSBytes)
		}
	}
	rep := ZFSReport{Pools: []ZFSPool{}, Datasets: []ZFSDataset{}}
	index := map[string]int{}
	for _, line := range lines(list) {
		f := strings.Split(line, "\t")
		if len(f) != 7 || !validZFSName(f[0]) {
			return ZFSReport{}, fmt.Errorf("unexpected zpool list row")
		}
		if len(rep.Pools) >= MaxZFSPools {
			rep.Truncated = true
			break
		}
		index[f[0]] = len(rep.Pools)
		rep.Pools = append(rep.Pools, ZFSPool{Name: f[0], Size: num(f[1]), Alloc: num(f[2]), Free: num(f[3]),
			FragPct: num(f[4]), CapPct: num(f[5]), Health: clipText(f[6]), Vdevs: []ZFSVdev{}})
	}
	statuses, truncated := parseZpoolStatus(status)
	rep.Truncated = rep.Truncated || truncated
	for _, st := range statuses {
		i, ok := index[st.Name]
		if !ok {
			continue // the pool went away between the two commands
		}
		p := &rep.Pools[i]
		p.State, p.Status, p.Action, p.Scan, p.Errors, p.Vdevs = st.State, st.Status, st.Action, st.Scan, st.Errors, st.Vdevs
	}
	for _, line := range lines(datasets) {
		f := strings.Split(line, "\t")
		if len(f) != 7 || !validZFSName(f[0]) {
			return ZFSReport{}, fmt.Errorf("unexpected zfs list row")
		}
		if len(rep.Datasets) >= MaxZFSDatasets {
			rep.Truncated = true
			break
		}
		// mountpoint is a path, or "-" (volume), "none" or "legacy", kept as zfs reports it.
		mp := f[4]
		rep.Datasets = append(rep.Datasets, ZFSDataset{Name: f[0], Used: num(f[1]), Avail: num(f[2]), Refer: num(f[3]),
			Mountpoint: clipText(mp), Compression: clipText(f[5]), Type: clipText(f[6])})
	}
	return rep, nil
}

// parseZpoolStatus reads the `zpool status -p` report: per pool the
// "key: value" lines (values may continue on tab-indented lines) and the
// config tree, whose depth is two spaces per level after a leading tab.
func parseZpoolStatus(b []byte) ([]ZFSPool, bool) {
	var out []ZFSPool
	var cur *ZFSPool
	var key string
	inConfig, truncated := false, false
	for _, line := range lines(b) {
		trimmed := strings.TrimSpace(line)
		if k, v, ok := statusKey(line); ok {
			inConfig = k == "config"
			key = k
			switch k {
			case "pool":
				if len(out) >= MaxZFSPools {
					truncated = true
					cur = nil
					continue
				}
				out = append(out, ZFSPool{Name: v, Vdevs: []ZFSVdev{}})
				cur = &out[len(out)-1]
			case "state", "status", "action", "scan", "errors":
				if cur != nil {
					setStatusField(cur, k, v)
				}
			}
			continue
		}
		if cur == nil || trimmed == "" {
			continue
		}
		if !inConfig {
			// Continuation of a multi-line status/action/scan value.
			if key == "status" || key == "action" || key == "scan" {
				setStatusField(cur, key, getStatusField(cur, key)+" "+trimmed)
			}
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			continue
		}
		body := line[1:]
		if strings.HasPrefix(body, "NAME") && strings.Contains(body, "STATE") {
			continue
		}
		indent := len(body) - len(strings.TrimLeft(body, " "))
		f := strings.Fields(body)
		if len(f) == 0 {
			continue
		}
		if len(cur.Vdevs) >= MaxZFSVdevs {
			truncated = true
			continue
		}
		v := ZFSVdev{Name: clipText(f[0]), Depth: indent / 2}
		if len(f) == 1 {
			v.Group = true // logs, cache, spares, special, dedup
		} else {
			v.State = f[1]
			if len(f) >= 5 {
				v.Read, v.Write, v.Cksum = num(f[2]), num(f[3]), num(f[4])
				if len(f) > 5 {
					v.Note = clipText(strings.Join(f[5:], " "))
				}
			} else if len(f) > 2 {
				v.Note = clipText(strings.Join(f[2:], " "))
			}
		}
		cur.Vdevs = append(cur.Vdevs, v)
	}
	return out, truncated
}

// statusKey matches a report line "  pool: tank" (right-aligned key).
func statusKey(line string) (key, value string, ok bool) {
	if strings.HasPrefix(line, "\t") {
		return "", "", false
	}
	k, v, found := strings.Cut(strings.TrimLeft(line, " "), ":")
	if !found {
		return "", "", false
	}
	switch k {
	case "pool", "state", "status", "action", "scan", "config", "errors", "see", "remove", "checkpoint":
		return k, strings.TrimSpace(v), true
	}
	return "", "", false
}

func setStatusField(p *ZFSPool, k, v string) {
	v = clipText(v)
	switch k {
	case "state":
		p.State = v
	case "status":
		p.Status = v
	case "action":
		p.Action = v
	case "scan":
		p.Scan = v
	case "errors":
		p.Errors = v
	}
}

func getStatusField(p *ZFSPool, k string) string {
	switch k {
	case "status":
		return p.Status
	case "action":
		return p.Action
	case "scan":
		return p.Scan
	}
	return ""
}

// validZFSName accepts pool and dataset names (zfs(8) "Naming"), including
// snapshot and bookmark separators, without control characters.
func validZFSName(s string) bool {
	if s == "" || len(s) > 255 {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func num(s string) *uint64 {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return nil // "-" or any non-number: not reported
	}
	return &v
}

func clipText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxZFSText {
		return s[:maxZFSText]
	}
	return s
}

func lines(b []byte) []string {
	var out []string
	s := bufio.NewScanner(bytes.NewReader(b))
	s.Buffer(make([]byte, 4096), 64<<10)
	for s.Scan() {
		if strings.TrimSpace(s.Text()) != "" {
			out = append(out, s.Text())
		}
	}
	return out
}
