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
	"strings"
	"unicode/utf8"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
)

// mergerfs.snapraid.status runs argv exactly "status". snapraid then reads
// its compiled default configuration (/etc/snapraid.conf). Callers cannot
// pass a path, --conf, or any other flag. The op does not run sync, diff,
// fix, scrub, or smart, and it does not return content-file paths or the
// scrub graph.

const (
	snapraidStatusMaxLines = 12
	snapraidStatusMaxRunes = 200
	snapraidStatusNote     = "This summary refers to the latest sync. Later modifications are not counted. The scrub graph is omitted. Content files are not read."
)

// snapraidStatusArgv is the only argument list this op may pass.
// TestSnapraidStatusArgvIsClosed pins it.
var snapraidStatusArgv = []string{"status"}

func snapraidStatus(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	stdout, stderr, exitCode, err := snapraidStatusRun(ctx, env)
	if err != nil {
		return nil, err
	}
	out := ParseSnapraidStatus(stdout, exitCode)
	if exitCode != 0 && len(out.Summary) == 0 {
		msg := clipSnapraidLine(stderr)
		if msg != "" && !strings.Contains(msg, "/") && !strings.Contains(msg, "\\") {
			out.Summary = []string{msg}
		}
	}
	return out, nil
}

func snapraidStatusRun(ctx context.Context, env *Env) ([]byte, string, int, error) {
	if env.SnapraidStatus != nil {
		stdout, stderr, exitCode, err := env.SnapraidStatus(ctx)
		if err != nil {
			var pe *proto.Error
			if errors.As(err, &pe) {
				return nil, "", 0, err
			}
			return nil, "", 0, snapraidRunErr(err)
		}
		return stdout, stderr, exitCode, nil
	}
	if env.Runner == nil || !env.Runner.Available("snapraid") {
		return nil, "", 0, &proto.Error{Code: proto.CodeUnavailable, Message: "snapraid is not installed"}
	}
	res, err := env.Runner.Run(ctx, "snapraid", snapraidStatusArgv...)
	if err != nil {
		return nil, "", 0, snapraidRunErr(err)
	}
	return res.Stdout, res.Stderr, res.ExitCode, nil
}

func snapraidRunErr(err error) error {
	switch {
	case errors.Is(err, runner.ErrTimeout):
		return &proto.Error{Code: proto.CodeTimeout, Message: "snapraid did not answer in time"}
	case errors.Is(err, runner.ErrTooLarge):
		return &proto.Error{Code: proto.CodeInternal, Message: "snapraid output exceeded the size limit"}
	default:
		msg := "snapraid could not be run"
		var ex *runner.ExecError
		if errors.As(err, &ex) && ex.Class != "" {
			msg += " (" + ex.Class + ")"
		}
		return &proto.Error{Code: proto.CodeInternal, Message: msg}
	}
}

// ParseSnapraidStatus keeps recognised prose lines from `snapraid status`.
// The per-disk table, the bar graph, and any other line (including file
// paths) are dropped. At most 12 lines are returned, each at most 200 runes.
func ParseSnapraidStatus(stdout []byte, exitCode int) SnapraidStatus {
	out := SnapraidStatus{
		ExitCode: exitCode,
		Summary:  []string{},
		Note:     snapraidStatusNote,
		Source:   "snapraid status (default configuration; no --conf)",
	}
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || snapraidGraph(line) {
			continue
		}
		keep, syncing, scrub := snapraidStatusPhrase(line)
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

func snapraidStatusPhrase(line string) (keep, syncing, scrub bool) {
	switch {
	case strings.Contains(line, "No sync is in progress"):
		return true, false, false
	case strings.Contains(line, "sync is in progress") || strings.HasPrefix(line, "Syncing"):
		return true, true, false
	case strings.Contains(line, "No error"):
		return true, false, false
	case strings.HasPrefix(line, "WARNING") || strings.HasPrefix(line, "DANGER") || strings.HasPrefix(line, "FATAL"):
		return true, false, false
	case strings.Contains(line, "oldest block was scrubbed"):
		return true, false, true
	case strings.Contains(line, "silent error"):
		return true, false, false
	case strings.Contains(line, "Nothing to do"):
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
