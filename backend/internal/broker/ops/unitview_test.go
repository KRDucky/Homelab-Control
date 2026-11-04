// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"homelab-control/backend/internal/broker/proto"
	. "homelab-control/backend/internal/broker/types"
)

func TestViewArgsStrictAndValidated(t *testing.T) {
	env := &Env{Roots: Roots{Root: t.TempDir()}}
	for _, h := range []func(context.Context, *Env, json.RawMessage) (any, error){unitDetail, unitContents} {
		for _, raw := range []string{`{}`, `{"unit":"x.service","extra":1}`, `{"unit":1}`, `[1]`, `{"unit":"-x.service"}`,
			`{"unit":"x.service;id"}`, `{"unit":"a/b.service"}`, `{"unit":"x.device"}`, `{"unit":"../x.service"}`, `{"unit":"x*.service"}`} {
			if _, err := h(context.Background(), env, json.RawMessage(raw)); code(err) != proto.CodeBadRequest {
				t.Errorf("%s: %v", raw, err)
			}
		}
		// Valid args on a host without systemd: unavailable, no exec attempt.
		if _, err := h(context.Background(), env, json.RawMessage(`{"unit":"ssh.service"}`)); code(err) != proto.CodeUnavailable {
			t.Errorf("no systemd: %v", err)
		}
	}
	for _, h := range []func(context.Context, *Env, json.RawMessage) (any, error){timersList, socketsList} {
		if _, err := h(context.Background(), env, json.RawMessage(`{"x":1}`)); code(err) != proto.CodeBadRequest {
			t.Errorf("args accepted: %v", err)
		}
	}
	if viewAuditArgs(json.RawMessage(`{"unit":"getty@.service"}`)) != "unit=getty@.service" || viewAuditArgs(json.RawMessage(`{"unit":"x;y"}`)) != "invalid args" {
		t.Error("view audit args")
	}
}

func TestDetailPropsNeverRequestSecrets(t *testing.T) {
	for _, p := range detailProps {
		for _, bad := range []string{"Environment", "Exec", "Credential", "Password", "Token", "Secret"} {
			if strings.Contains(p, bad) && !strings.HasPrefix(p, "ExecMain") {
				t.Errorf("detail requests %s", p)
			}
		}
	}
	for _, a := range DetailShowArgs("x.service") {
		if !argOK(a) {
			t.Errorf("argument %q would be refused by the runner", a)
		}
	}
}

// argOK mirrors the runner's argument grammar (a second check that the
// fixed argv is valid).
func argOK(a string) bool { return len(a) <= 128 && !strings.ContainsAny(a, " ;$`|&<>\n") }

func detailBlock(kv string) map[string][]string { return ParseShowBlocks([]byte(kv))[0] }

func TestBuildDetailDoesNotInventState(t *testing.T) {
	// A loaded, failed service with restarts and a recorded exit.
	d := BuildDetail(UnitDetail{Requested: "demo.service", Kind: "unit", InRuntime: true}, detailBlock(
		"Id=demo.service\nNames=demo.service\nLoadState=loaded\nActiveState=failed\nSubState=failed\nResult=exit-code\n"+
			"FragmentPath=/etc/systemd/system/demo.service\nNRestarts=3\nExecMainCode=1\nExecMainStatus=2\n"+
			"ExecMainStartTimestamp=Wed 2026-11-04 06:00:00 UTC\nExecMainExitTimestamp=Wed 2026-11-04 06:00:01 UTC\n"+
			"Requires=a.service b.mount\nWantedBy=multi-user.target\nAfter=network.target\n"), nil)
	if d.Restarts == nil || *d.Restarts != 3 || d.LastExit == nil || d.LastExit.Code != "exited" || d.LastExit.Status != 2 ||
		d.Dependencies["Requires"][1] != "b.mount" || d.Dependencies["WantedBy"][0] != "multi-user.target" || d.Kind != "unit" {
		t.Fatalf("%+v", d)
	}
	// Never ran: no last exit is invented, and it says so.
	d = BuildDetail(UnitDetail{Requested: "idle.service", Kind: "unit", InRuntime: true}, detailBlock(
		"Id=idle.service\nLoadState=loaded\nActiveState=inactive\nNRestarts=0\nExecMainStartTimestamp=\nExecMainExitTimestamp=\n"), nil)
	if d.LastExit != nil || !strings.Contains(strings.Join(d.Notes, " "), "no record of the main process") {
		t.Fatalf("never ran: %+v", d)
	}
	// Alias: the canonical ID and its installed state are reported.
	en := "enabled"
	d = BuildDetail(UnitDetail{Requested: "sshd.service", Kind: "unit", InRuntime: true, BootState: "alias"}, detailBlock(
		"Id=ssh.service\nNames=ssh.service sshd.service\nLoadState=loaded\nActiveState=active\n"), &en)
	if d.Kind != "alias" || d.ID != "ssh.service" || d.BootState != "enabled" {
		t.Fatalf("alias: %+v", d)
	}
	// Installed but not loaded: systemd loaded it only to answer.
	d = BuildDetail(UnitDetail{Requested: "cold.service", Kind: "unit", InRuntime: false}, detailBlock(
		"Id=cold.service\nLoadState=loaded\nActiveState=inactive\n"), nil)
	if !d.LoadedForQuery || !strings.Contains(strings.Join(d.Notes, " "), "only to answer") {
		t.Fatalf("not loaded: %+v", d)
	}
	// Referenced but missing; non-services have no restart counter.
	d = BuildDetail(UnitDetail{Requested: "gone.mount", Kind: "unit", InRuntime: true}, detailBlock(
		"Id=gone.mount\nLoadState=not-found\nActiveState=inactive\nNRestarts=0\n"), nil)
	if d.Restarts != nil || d.LastExit != nil || !strings.Contains(strings.Join(d.Notes, " "), "no unit file") {
		t.Fatalf("missing: %+v", d)
	}
	// Generated (SysV) and masked units are labelled.
	d = BuildDetail(UnitDetail{Requested: "old.service", Kind: "unit", InRuntime: true}, detailBlock(
		"Id=old.service\nLoadState=loaded\nSourcePath=/etc/init.d/old\nFragmentPath=/run/systemd/generator.late/old.service\n"), nil)
	if !d.Generated {
		t.Fatalf("generated: %+v", d)
	}
	d = BuildDetail(UnitDetail{Requested: "m.service", Kind: "unit", InRuntime: true}, detailBlock("Id=m.service\nLoadState=masked\n"), nil)
	if !strings.Contains(strings.Join(d.Notes, " "), "masked") {
		t.Fatalf("masked: %+v", d)
	}
	// Dependency lists are capped.
	many := "Id=x.target\nLoadState=loaded\nWants="
	for i := 0; i < 80; i++ {
		many += "u" + strings.Repeat("a", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".service "
	}
	d = BuildDetail(UnitDetail{Requested: "x.target", Kind: "unit", InRuntime: true}, detailBlock(many+"\n"), nil)
	if len(d.Dependencies["Wants"]) != 64 || !d.DependenciesTruncated {
		t.Fatalf("cap: %d %v", len(d.Dependencies["Wants"]), d.DependenciesTruncated)
	}
}

func TestRedactLine(t *testing.T) {
	cases := map[string]string{
		"Environment=PASSWORD=hunter2 LOG_LEVEL=debug":     "Environment=PASSWORD=[redacted] LOG_LEVEL=[redacted]",
		`Environment="DB_URL=postgres://u:p@h/db"`:         "Environment=DB_URL=[redacted]",
		"SetCredential=api:supersecret":                    "SetCredential=[redacted]",
		"SetCredentialEncrypted=api: k6iUCUh0RJCQyvL8k8q1": "SetCredentialEncrypted=[redacted]",
		"ApiToken=abc": "ApiToken=[redacted]",
		"ExecStart=/usr/bin/app --password=hunter2 --port=8080":    "ExecStart=/usr/bin/app --password=[redacted] --port=8080",
		"ExecStart=/usr/bin/app --token hunter2 -v":                "ExecStart=/usr/bin/app --token [redacted] -v",
		"ExecStartPre=/usr/bin/x api_key=abc":                      "ExecStartPre=/usr/bin/x api_key=[redacted]",
		"ExecStart=/usr/bin/curl https://user:pw@example.invalid/": "ExecStart=/usr/bin/curl https://[redacted]@example.invalid/",
		"Description=fixture":                                      "Description=fixture",
		"ExecStart=/bin/true":                                      "ExecStart=/bin/true",
		"# Environment=X=1":                                        "# Environment=X=1",
		"PrivateTmp=yes":                                           "PrivateTmp=yes",
		"EnvironmentFile=/etc/default/app":                         "EnvironmentFile=/etc/default/app",
	}
	for in, want := range cases {
		got, _ := redactLine(in)
		if got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// Output captured from systemd 259 (docs/evidence/m2/m2-3/probes/probe1.out).
const catOut = `# Warning: hc-probe-c.service changed on disk, the version systemd has loaded is outdated.
# Run 'systemctl daemon-reload' to reload units.
# /etc/systemd/system/hc-probe-c.service
[Service]
ExecStart=/bin/sleep 1000
Environment=SECRET=hunter2
[Install]
WantedBy=multi-user.target

# /etc/systemd/system/hc-probe-c.service.d/secret.conf
`

func TestParseCat(t *testing.T) {
	c, err := ParseCat("hc-probe-c.service", []byte(catOut),
		`Failed to open "/etc/systemd/system/hc-probe-c.service.d/secret.conf": Permission denied`+"\n", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Files) != 2 || c.Files[0].Path != "/etc/systemd/system/hc-probe-c.service" || !c.Files[1].Unreadable ||
		len(c.Unreadable) != 1 || c.Redactions != 1 || len(c.Notes) != 2 {
		t.Fatalf("%+v", c)
	}
	if strings.Contains(c.Files[0].Text, "hunter2") {
		t.Fatal("secret not redacted")
	}
	// systemd ≤ 255 (probe 6): no header for the unreadable file, different wording.
	old := "# /etc/systemd/system/hc-p6.service\n[Service]\nExecStart=/bin/sleep 1000\n"
	c, err = ParseCat("hc-p6.service", []byte(old), "Failed to cat /etc/systemd/system/hc-p6.service.d/secret.conf: Permission denied\n", 1)
	if err != nil || len(c.Files) != 2 || !c.Files[1].Unreadable || c.Files[1].Path != "/etc/systemd/system/hc-p6.service.d/secret.conf" {
		t.Fatalf("systemd 255 format: %+v %v", c, err)
	}
	if _, err := ParseCat("x.service", nil, "No files found for x.service.\n", 1); code(err) != proto.CodeNotFound {
		t.Fatalf("no files: %v", err)
	}
	big := "# /etc/systemd/system/big.service\n" + strings.Repeat("Description=x\n", maxContentLines+10)
	c, _ = ParseCat("big.service", []byte(big), "", 0)
	if !c.Truncated || strings.Count(c.Files[0].Text, "\n") >= maxContentLines {
		t.Fatalf("not truncated: %v", c.Truncated)
	}
	c, _ = ParseCat("ctl.service", []byte("# /etc/systemd/system/ctl.service\nDescription=a\x1b[31mb\x07\n"), "", 0)
	if strings.ContainsAny(c.Files[0].Text, "\x1b\x07") {
		t.Fatal("control characters kept")
	}
}

// Captured from systemd 259 (probe1.out), including null fields.
const timersJSON = `[{"next":1793774034845450,"left":1793774034845450,"last":0,"passed":0,"unit":"systemd-tmpfiles-clean.timer","activates":"systemd-tmpfiles-clean.service"},{"next":null,"left":null,"last":0,"passed":0,"unit":"dnf-makecache.timer","activates":null},{"next":1,"unit":"bad name.timer","activates":"x.service"}]`

func TestTimers(t *testing.T) {
	out, names, err := ParseTimerRows([]byte(timersJSON))
	if err != nil || len(out.Timers) != 2 || len(names) != 2 {
		t.Fatalf("%+v %v", out, err)
	}
	if out.Timers[0].Next == nil || out.Timers[0].Last != nil || out.Timers[1].Next != nil || out.Timers[1].Activates != "" {
		t.Fatalf("null handling: %+v", out.Timers)
	}
	show := "Id=systemd-tmpfiles-clean.timer\nActiveState=active\nSubState=waiting\nPersistent=no\n" +
		"TimersMonotonic={ OnUnitActiveUSec=1d ; next_elapse=0 }\nTimersMonotonic={ OnBootUSec=15min ; next_elapse=2h }\n"
	st := map[string]map[string][]string{}
	for _, b := range ParseShowBlocks([]byte(show)) {
		st[first(b, "Id")] = b
	}
	ApplyTimerStates(&out, st)
	tt := out.Timers[0]
	if strings.Join(tt.Schedule, "|") != "OnUnitActiveUSec=1d|OnBootUSec=15min" || tt.Persistent == nil || *tt.Persistent || tt.ActiveState != "active" {
		t.Fatalf("states: %+v", tt)
	}
	if out.Timers[1].ActiveState != "unknown" {
		t.Fatalf("unanswered timer must stay unknown: %+v", out.Timers[1])
	}
	if _, _, err := ParseTimerRows([]byte("not json")); code(err) != proto.CodeInternal {
		t.Fatal("bad json accepted")
	}
}

const socketsJSON = `[{"listen":"0.0.0.0:22","unit":"ssh.socket","activates":"ssh.service"},{"listen":"[::]:22","unit":"ssh.socket","activates":"ssh.service"},{"listen":"/run/ssh-unix-local/socket","unit":"sshd-unix-local.socket","activates":null}]`

func TestSockets(t *testing.T) {
	out, names, err := ParseSocketRows([]byte(socketsJSON))
	if err != nil || len(out.Sockets) != 2 || len(names) != 2 {
		t.Fatalf("%+v %v", out, err)
	}
	if out.Sockets[0].Unit != "ssh.socket" || len(out.Sockets[0].Listen) != 2 || out.Sockets[0].Activates[0] != "ssh.service" || out.Sockets[1].Activates != nil {
		t.Fatalf("%+v", out.Sockets)
	}
	ApplySocketStates(&out, map[string]map[string][]string{"ssh.socket": {"Id": {"ssh.socket"}, "ActiveState": {"active"}, "SubState": {"listening"}}})
	if out.Sockets[0].SubState != "listening" || out.Sockets[1].ActiveState != "unknown" {
		t.Fatalf("%+v", out.Sockets)
	}
}

func TestParseShowBlocksKeepsRepeatedKeys(t *testing.T) {
	b := ParseShowBlocks([]byte("Id=a.timer\nTimersCalendar={ OnCalendar=daily ; x }\nTimersCalendar={ OnCalendar=weekly ; y }\n\nId=b.timer\n"))
	if len(b) != 2 || len(b[0]["TimersCalendar"]) != 2 || schedule(b[0]["TimersCalendar"][1]) != "OnCalendar=weekly" {
		t.Fatalf("%v", b)
	}
	_ = UnitDetail{}
}
