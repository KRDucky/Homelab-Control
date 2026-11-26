// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"homelab-control/backend/internal/broker/proto"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/mergerfsapply"
)

// mergerfs.snapraid.status asks the opt-in root helper (hc-mergerfs-apply)
// for `snapraid status`, argv exactly "status", so snapraid reads its
// default configuration (/etc/snapraid.conf). It runs in the helper, not here,
// because SnapRAID takes its lock file next to the first content file for
// status as well (cmdline/snapraid.c: only devices, smart and probe skip the
// lock) and content files are root-only; this broker is read-only and runs
// programs as the service user. Callers cannot pass a path, --conf, or any
// other flag. The op does not run sync, diff, fix, scrub, or smart, and it
// returns no content-file path and no scrub graph.

const (
	snapraidStatusMaxLines = 16
	snapraidStatusMaxDisks = 64
	snapraidStatusMaxRunes = 200
	snapraidStatusNote     = "This report is read from SnapRAID's content file and refers to the latest sync. Changes since then are not counted. The scrub graph and file paths are omitted."
	snapraidLockedLine     = "Another SnapRAID command (sync, scrub or fix) holds the array lock, so status could not be read now."
)

func snapraidStatus(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	stdout, exitCode, cut, err := snapraidStatusRun(ctx, env)
	if err != nil {
		return nil, err
	}
	out := ParseSnapraidStatus(stdout, exitCode)
	if cut {
		// The helper stopped reading at its output cap: what was parsed is a
		// prefix of the report, not all of it.
		out.Truncated = true
	}
	return out, nil
}

// snapraidStatusRun returns snapraid's text, its exit code, and whether the
// helper cut the text at its output cap.
func snapraidStatusRun(ctx context.Context, env *Env) ([]byte, int, bool, error) {
	if env.SnapraidStatus != nil {
		stdout, exitCode, err := env.SnapraidStatus(ctx)
		if err != nil {
			var pe *proto.Error
			if errors.As(err, &pe) {
				return nil, 0, false, err
			}
			return nil, 0, false, snapraidRunErr(err)
		}
		return stdout, exitCode, false, nil
	}
	res, err := env.callMergerFS(ctx, mergerfsapply.Request{Version: mergerfsapply.ProtocolVersion, Action: mergerfsapply.ActionStatus})
	if err != nil {
		return nil, 0, false, snapraidRunErr(err)
	}
	if res.Output == "" && !res.OK {
		return nil, 0, false, &proto.Error{Code: proto.CodeInternal, Message: "snapraid status could not be run by the helper"}
	}
	return []byte(res.Output), res.ExitCode, res.OutputTruncated, nil
}

func snapraidRunErr(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &proto.Error{Code: proto.CodeTimeout, Message: "snapraid status did not answer in time"}
	case errors.Is(err, syscall.ENOENT), errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, errHelperNoReply):
		return &proto.Error{Code: proto.CodeUnavailable, Message: "the MergerFS helper is not enabled, so SnapRAID status cannot be read"}
	default:
		return &proto.Error{Code: proto.CodeInternal, Message: "the MergerFS helper did not return SnapRAID status"}
	}
}

// snapraidDiskRow is one row of the status table (cmdline/status.c): files,
// fragmented files, excess fragments, wasted GB ("-" when unknown), used GB,
// free GB and use% ("-" before the first sync), and the disk name. Sizes are
// whole GB in current SnapRAID but may carry a decimal (as wasted does), so
// every size column accepts one. The totals row has no name and does not match.
var snapraidDiskRow = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(\d+)\s+(-|-?\d+(?:\.\d+)?)\s+(\d+(?:\.\d+)?)\s+(-|\d+(?:\.\d+)?)\s+(-|\d+%)\s+([^\s/\\:]+)$`)

// ParseSnapraidStatus keeps the per-disk table and SnapRAID's summary
// sentences (cmdline/status.c) from `snapraid status`. The bar graph and any
// other line (including file paths) are dropped. At most 16 summary lines and
// 64 disks are returned, each line at most 200 runes.
func ParseSnapraidStatus(stdout []byte, exitCode int) SnapraidStatus {
	out := SnapraidStatus{
		ExitCode: exitCode,
		Summary:  []string{},
		Disks:    []SnapraidDisk{},
		Note:     snapraidStatusNote,
		Source:   "snapraid status (default configuration; no --conf)",
	}
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if m := snapraidDiskRow.FindStringSubmatch(line); m != nil {
			if len(out.Disks) >= snapraidStatusMaxDisks {
				out.Truncated = true
				continue
			}
			out.Disks = append(out.Disks, snapraidDisk(m))
			continue
		}
		if snapraidGraph(line) {
			continue
		}
		keep, syncing, scrub := snapraidStatusPhrase(line)
		if strings.Contains(line, "is already in use") {
			// SnapRAID prints two such lines (the lock file, then itself).
			if out.Locked {
				continue
			}
			out.Locked = true
			line, keep = snapraidLockedLine, true
		}
		if !keep {
			continue
		}
		if syncing {
			out.SyncInProgress = true
		}
		clipped := clipSnapraidLine(redactSnapraidPaths(line))
		if clipped == "" {
			continue
		}
		if scrub && out.ScrubAge == "" {
			out.ScrubAge = clipped
		}
		if len(out.Summary) >= snapraidStatusMaxLines {
			out.Truncated = true
			continue
		}
		out.Summary = append(out.Summary, clipped)
	}
	if sc.Err() != nil {
		out.Truncated = true
	}
	return out
}

func snapraidDisk(m []string) SnapraidDisk {
	u := func(s string) uint64 { n, _ := strconv.ParseUint(s, 10, 64); return n }
	f := func(s string) float64 { n, _ := strconv.ParseFloat(s, 64); return n }
	d := SnapraidDisk{Name: m[8], Files: u(m[1]), Fragmented: u(m[2]), ExcessFragments: u(m[3]), UsedGB: f(m[5])}
	if m[4] != "-" {
		if f, err := strconv.ParseFloat(m[4], 64); err == nil {
			d.WastedGB = &f
		}
	}
	if m[6] != "-" {
		n := f(m[6])
		d.FreeGB = &n
	}
	if m[7] != "-" {
		n := u(strings.TrimSuffix(m[7], "%"))
		d.UsePercent = &n
	}
	return d
}

// snapraidStatusPhrase recognises the sentences cmdline/status.c prints.
func snapraidStatusPhrase(line string) (keep, syncing, scrub bool) {
	switch {
	case strings.HasPrefix(line, "You have a sync in progress"):
		return true, true, false
	case strings.HasPrefix(line, "The oldest block was scrubbed"):
		return true, false, true
	case strings.HasPrefix(line, "WARNING!"), strings.HasPrefix(line, "DANGER!"),
		strings.HasPrefix(line, "No sync is in progress"),
		strings.HasPrefix(line, "The array is empty"),
		strings.HasSuffix(line, "of the array is not scrubbed."),
		strings.HasPrefix(line, "The full array was scrubbed"),
		strings.HasPrefix(line, "No error detected"),
		strings.HasPrefix(line, "You have a rehash in progress"),
		strings.HasPrefix(line, "No rehash is in progress"),
		strings.HasPrefix(line, "To fix them use the command"):
		return true, false, false
	default:
		return false, false, false
	}
}

// snapraidGraph reports lines that are the status bar graph or a rule of
// dashes. Disk names and prose contain other letters, so they are not graphs.
func snapraidGraph(line string) bool {
	if strings.Contains(line, "|") {
		return true
	}
	if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "===") {
		return true
	}
	for _, r := range line {
		switch r {
		case 'o', 'O', '%', '+', '-', '.', ' ':
			continue
		default:
			if r >= '0' && r <= '9' {
				continue
			}
			return false
		}
	}
	return true
}

// redactSnapraidPaths replaces any field that contains a slash. Status
// prose that names a content or data file must not leave the broker.
func redactSnapraidPaths(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.Contains(f, "/") || strings.Contains(f, "\\") {
			fields[i] = "[path]"
		}
	}
	return strings.Join(fields, " ")
}

func clipSnapraidLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if utf8.RuneCountInString(s) <= snapraidStatusMaxRunes {
		return s
	}
	i, n := 0, 0
	for i < len(s) && n < snapraidStatusMaxRunes {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		n++
	}
	return s[:i]
}
