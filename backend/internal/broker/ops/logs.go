// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	. "homelab-control/backend/internal/broker/types"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"homelab-control/backend/internal/broker/proto"
)

// services.unit.logs is the first broker op that takes caller arguments
// (M2 PR-1b). It is read-only. Review rules (ADR 0008 amendment):
//   - args are strictly decoded: {unit, lines}; anything else is rejected;
//   - unit must match a conservative grammar AND be a unit systemd currently
//     lists (no free-form names reach journalctl);
//   - argv is fixed except for the validated values, passed as --unit=NAME and
//     --lines=N (never as separate, flag-like arguments);
//   - journalctl runs as the service user, with only the journal reader group
//     added, so it can read the system journal but nothing else new.

const (
	maxLogMessage = 4 << 10
	maxLogTotal   = 1 << 20
)

type logsArgs struct {
	Unit  string `json:"unit"`
	Lines int    `json:"lines"`
}

// ValidateLogsArgs checks decoded arguments (shared with hc-api for early 400s).
func ValidateLogsArgs(unit string, lines int) error {
	if !LogsUnitRE.MatchString(unit) { // types.LogsUnitRE
		return &proto.Error{Code: proto.CodeBadRequest, Message: "unit name is not valid"}
	}
	if lines < 1 || lines > MaxLogLines {
		return &proto.Error{Code: proto.CodeBadRequest, Message: "lines must be between 1 and " + strconv.Itoa(MaxLogLines)}
	}
	return nil
}

// logsAuditArgs records which unit and how many lines were requested; only
// values that pass validation are echoed into the audit log.
func logsAuditArgs(raw json.RawMessage) string {
	var a logsArgs
	if proto.DecodeArgs(raw, &a) != nil || ValidateLogsArgs(a.Unit, a.Lines) != nil {
		return "invalid args"
	}
	return "unit=" + a.Unit + " lines=" + strconv.Itoa(a.Lines)
}

func unitLogs(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a logsArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be {\"unit\": string, \"lines\": integer}"}
	}
	if err := ValidateLogsArgs(a.Unit, a.Lines); err != nil {
		return nil, err
	}
	if env.Runner == nil || !env.Runner.Available("journalctl") || !env.Runner.Available("systemctl") {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "journalctl/systemctl are not installed; this host does not appear to use systemd"}
	}
	if fi, err := os.Stat(env.Roots.Path("/run/systemd/system")); err != nil || !fi.IsDir() {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "systemd is not running as the init system on this host"}
	}
	if env.JournalGID == 0 {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the journal reader group (systemd-journal) does not exist on this host"}
	}
	// Existence: the unit must be one systemd lists right now.
	lu, err := env.Runner.Run(ctx, "systemctl", "list-units", "--all", "--output=json", "--no-pager", "--type="+unitTypes)
	if err != nil {
		return nil, runErr(err)
	}
	if lu.ExitCode != 0 {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl list-units failed: " + clean(lu.Stderr, 200)}
	}
	units, err := ParseListUnits(lu.Stdout)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "unexpected systemctl output"}
	}
	known := false
	for _, u := range units {
		if u.Name == a.Unit {
			known = true
			break
		}
	}
	if !known {
		return nil, &proto.Error{Code: proto.CodeNotFound, Message: "no such unit"}
	}
	res, err := env.Runner.RunWithGroups(ctx, []uint32{env.JournalGID}, "journalctl",
		"--unit="+a.Unit, "--lines="+strconv.Itoa(a.Lines), "--output=json",
		"--output-fields=MESSAGE,PRIORITY,_PID,SYSLOG_IDENTIFIER", "--no-pager", "--quiet")
	if err != nil {
		return nil, journalErr(err)
	}
	if res.ExitCode != 0 {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "journalctl failed: " + clean(res.Stderr, 200)}
	}
	out := ParseJournal(res.Stdout)
	out.Unit, out.Requested = a.Unit, a.Lines
	out.Source = "journalctl --unit --output=json (run as homelab-control with the systemd-journal group)"
	return out, nil
}

func journalErr(err error) error {
	pe := runErr(err).(*proto.Error)
	pe.Message = strings.ReplaceAll(pe.Message, "systemctl", "journalctl")
	return pe
}

type journalEntry struct {
	Realtime   string          `json:"__REALTIME_TIMESTAMP"`
	Message    json.RawMessage `json:"MESSAGE"`
	Priority   string          `json:"PRIORITY"`
	PID        string          `json:"_PID"`
	Identifier string          `json:"SYSLOG_IDENTIFIER"`
}

// ParseJournal converts journalctl JSON lines into bounded, sanitised entries.
// Malformed lines are skipped and counted; messages are capped at 4 KiB and the
// total at 1 MiB (older entries are dropped first).
func ParseJournal(b []byte) UnitLogs {
	var out UnitLogs
	out.Entries = []LogEntry{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var je journalEntry
		if err := json.Unmarshal(line, &je); err != nil {
			out.Skipped++
			continue
		}
		e := LogEntry{Message: messageText(je.Message)}
		if us, err := strconv.ParseInt(je.Realtime, 10, 64); err == nil && us > 0 {
			t := time.UnixMicro(us).UTC()
			e.Time = &t
		}
		if p, err := strconv.Atoi(je.Priority); err == nil && p >= 0 && p <= 7 {
			e.Priority = &p
		}
		if pid, err := strconv.Atoi(je.PID); err == nil && pid > 0 {
			e.PID = pid
		}
		e.Identifier = clean(je.Identifier, 64)
		if len(e.Message) > maxLogMessage {
			e.Message, e.Truncated = truncUTF8(e.Message, maxLogMessage), true
		}
		out.Entries = append(out.Entries, e)
	}
	if sc.Err() != nil {
		out.Skipped++
	}
	total := 0
	for i := len(out.Entries) - 1; i >= 0; i-- {
		total += len(out.Entries[i].Message) + 64
		if total > maxLogTotal {
			out.Entries = out.Entries[i+1:]
			out.Truncated = true
			break
		}
	}
	return out
}

// messageText handles MESSAGE as a string, as a byte array (non-UTF-8 data)
// or as null; control characters other than tab are removed.
func messageText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var bs []byte
		var ints []int
		if json.Unmarshal(raw, &ints) == nil {
			for _, v := range ints {
				if v >= 0 && v <= 255 {
					bs = append(bs, byte(v))
				}
			}
			s = strings.ToValidUTF8(string(bs), "�")
		}
	}
	s = strings.ToValidUTF8(s, "�")
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\t') || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func truncUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
