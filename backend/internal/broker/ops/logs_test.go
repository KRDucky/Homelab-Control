// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"homelab-control/backend/internal/broker/proto"
	. "homelab-control/backend/internal/broker/types"
)

func TestLogsUnitGrammar(t *testing.T) {
	good := []string{"ssh.service", "systemd-journald.service", "getty@tty1.service", "dev-hugepages.mount",
		"apt-daily.timer", "dbus.socket", "multi-user.target", "user@1000.service", "a:b_c.path"}
	for _, u := range good {
		if err := ValidateLogsArgs(u, 50); err != nil {
			t.Errorf("%q rejected: %v", u, err)
		}
	}
	bad := []string{"", "ssh", "-x.service", "--output=export.service", "ssh.service;id", "$(id).service",
		"a b.service", "a\n.service", "a\x00.service", "../etc.service", "a/b.service", "foo.device",
		"foo.slice", "foo.scope", "dev-disk-by\\x2duuid.mount", strings.Repeat("a", 101) + ".service",
		"ssh.service\n", "ssh.SERVICE"}
	for _, u := range bad {
		var pe *proto.Error
		if err := ValidateLogsArgs(u, 50); !errors.As(err, &pe) || pe.Code != proto.CodeBadRequest {
			t.Errorf("%q accepted", u)
		}
	}
	for _, n := range []int{0, -1, MaxLogLines + 1, 1 << 30} {
		if err := ValidateLogsArgs("ssh.service", n); err == nil {
			t.Errorf("lines %d accepted", n)
		}
	}
}

func TestLogsArgsStrictDecode(t *testing.T) {
	env := &Env{Roots: Roots{Root: t.TempDir()}}
	for _, raw := range []string{`{}`, `{"unit":"ssh.service"}`, `{"unit":"ssh.service","lines":10,"extra":1}`,
		`{"unit":["ssh.service"],"lines":10}`, `{"unit":"ssh.service","lines":"10"}`, `{"unit":"ssh.service","lines":10.5}`,
		`[1]`, `{"unit":"ssh.service","lines":10}{"x":1}`} {
		_, err := unitLogs(context.Background(), env, json.RawMessage(raw))
		var pe *proto.Error
		if !errors.As(err, &pe) || pe.Code != proto.CodeBadRequest {
			t.Errorf("%s: got %v", raw, err)
		}
	}
	// Valid args but no systemd tools: unavailable, never an exec attempt.
	_, err := unitLogs(context.Background(), env, json.RawMessage(`{"unit":"ssh.service","lines":10}`))
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeUnavailable {
		t.Fatalf("got %v", err)
	}
}

func TestParseJournal(t *testing.T) {
	in := strings.Join([]string{
		`{"__REALTIME_TIMESTAMP":"1760000000000000","MESSAGE":"Started OpenSSH","PRIORITY":"6","_PID":"1","SYSLOG_IDENTIFIER":"systemd"}`,
		`{"__REALTIME_TIMESTAMP":"1760000001000000","MESSAGE":[104,105,255,10,27,91,51,49,109],"PRIORITY":"3","_PID":"812"}`,
		`not json`,
		`{"__REALTIME_TIMESTAMP":"x","MESSAGE":null,"PRIORITY":"99","_PID":"-4","SYSLOG_IDENTIFIER":"evil\nid"}`,
		`{"MESSAGE":"` + strings.Repeat("é", 3000) + `"}`,
	}, "\n")
	got := ParseJournal([]byte(in))
	if len(got.Entries) != 4 || got.Skipped != 1 {
		t.Fatalf("%+v", got)
	}
	e0, e1, e2, e3 := got.Entries[0], got.Entries[1], got.Entries[2], got.Entries[3]
	if e0.Message != "Started OpenSSH" || e0.Priority == nil || *e0.Priority != 6 || e0.Time == nil || e0.Time.Unix() != 1760000000 || e0.Identifier != "systemd" {
		t.Errorf("e0 %+v", e0)
	}
	if e1.Message != "hi�[31m" || *e1.Priority != 3 || e1.PID != 812 {
		t.Errorf("e1 %q %+v", e1.Message, e1)
	}
	if e2.Priority != nil || e2.PID != 0 || e2.Time != nil || e2.Identifier != "evilid" || e2.Message != "" {
		t.Errorf("e2 %+v", e2)
	}
	if !e3.Truncated || len(e3.Message) > maxLogMessage || !strings.HasSuffix(e3.Message, "é") {
		t.Errorf("e3 truncated=%v len=%d", e3.Truncated, len(e3.Message))
	}
}

func TestParseJournalTotalCap(t *testing.T) {
	var b strings.Builder
	line := `{"MESSAGE":"` + strings.Repeat("x", 4000) + `"}` + "\n"
	for i := 0; i < 400; i++ {
		b.WriteString(line)
	}
	got := ParseJournal([]byte(b.String()))
	total := 0
	for _, e := range got.Entries {
		total += len(e.Message)
	}
	if !got.Truncated || total > maxLogTotal || len(got.Entries) == 0 {
		t.Fatalf("truncated=%v entries=%d total=%d", got.Truncated, len(got.Entries), total)
	}
}

// FuzzLogsArgs: no input may pass validation unless it matches the grammar
// and bounds, and decoding never panics.
func FuzzLogsArgs(f *testing.F) {
	for _, s := range []string{`{"unit":"ssh.service","lines":10}`, `{"unit":"-x","lines":1}`, `{"unit":"a.service","lines":500}`,
		`{"unit":"a.service\u0000","lines":5}`, `{"unit":"--output=export.service","lines":5}`, `{}`, `null`, `[]`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		var a logsArgs
		if proto.DecodeArgs(raw, &a) != nil {
			return
		}
		if ValidateLogsArgs(a.Unit, a.Lines) != nil {
			return
		}
		if !LogsUnitRE.MatchString(a.Unit) || a.Lines < 1 || a.Lines > MaxLogLines || strings.HasPrefix(a.Unit, "-") ||
			strings.ContainsAny(a.Unit, " /\\;$`\n\x00=") {
			t.Fatalf("accepted %q %d", a.Unit, a.Lines)
		}
		if len("--unit="+a.Unit) > 128 {
			t.Fatalf("argument too long for the runner: %q", a.Unit)
		}
	})
}
