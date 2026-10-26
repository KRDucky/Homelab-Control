// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/broker/ops"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/brokerclient"
)

type fixture struct {
	sock  string
	srv   *Server
	audit *bytes.Buffer
}

func start(t *testing.T, allowed ...uint32) *fixture {
	t.Helper()
	dir, err := os.MkdirTemp("", "hcb") // short path: Unix socket paths are limited to 108 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "b.sock")
	buf := &bytes.Buffer{}
	if len(allowed) == 0 {
		allowed = []uint32{uint32(os.Getuid())}
	}
	srv := New(&ops.Env{Roots: ops.Roots{Root: dir}, Version: "test"}, audit.New(buf), allowed...)
	l, activated, err := Listener(sock)
	if err != nil || activated {
		t.Fatalf("listener: %v activated=%v", err, activated)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = srv.Serve(ctx, l); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return &fixture{sock: sock, srv: srv, audit: buf}
}

func call(t *testing.T, f *fixture, op string, scopes ...string) (json.RawMessage, error) {
	t.Helper()
	var out json.RawMessage
	err := brokerclient.New(f.sock).Call(context.Background(), op, proto.Actor{User: "tester", Scopes: scopes}, &out)
	return out, err
}

func code(err error) string {
	var pe *proto.Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	if err != nil {
		return err.Error()
	}
	return "ok"
}

func TestSocketMode(t *testing.T) {
	f := start(t)
	fi, err := os.Stat(f.sock)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o007 != 0 {
		t.Fatalf("socket accessible to others: %v", fi.Mode().Perm())
	}
}

func TestInfoAndUnknownOp(t *testing.T) {
	f := start(t)
	out, err := call(t, f, "broker.info")
	if err != nil || !strings.Contains(string(out), `"services.systemd.list"`) {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := call(t, f, "system.shell.exec"); code(err) != proto.CodeUnknownOp {
		t.Fatalf("unknown op: %v", err)
	}
	if !strings.Contains(f.audit.String(), `"outcome":"unknown_op"`) {
		t.Fatalf("unknown op not audited: %s", f.audit)
	}
}

func TestScopeRecheckedByBroker(t *testing.T) {
	f := start(t)
	if _, err := call(t, f, "legacy.webmin.status"); code(err) != proto.CodeForbidden {
		t.Fatalf("missing scope: %v", err)
	}
	if _, err := call(t, f, "legacy.webmin.status", "services.read"); code(err) != proto.CodeForbidden {
		t.Fatalf("wrong scope: %v", err)
	}
	if _, err := call(t, f, "legacy.webmin.status", "system.read"); err != nil {
		t.Fatalf("correct scope: %v", err)
	}
}

func TestPeerNotAllowed(t *testing.T) {
	f := start(t, 54321) // current uid not allowed
	if _, err := call(t, f, "broker.info"); code(err) != proto.CodeForbidden {
		t.Fatalf("foreign uid accepted: %v", err)
	}
	if !strings.Contains(f.audit.String(), "uid not allowed") {
		t.Fatal("peer rejection not audited")
	}
}

func rawSend(t *testing.T, sock string, payload []byte, announce uint32) proto.Response {
	t.Helper()
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], announce)
	c.Write(hdr[:])
	c.Write(payload)
	var resp proto.Response
	if err := proto.ReadFrame(c, &resp, proto.MaxResponse); err != nil {
		t.Fatalf("no response: %v", err)
	}
	return resp
}

func TestMalformedRequests(t *testing.T) {
	f := start(t)
	cases := map[string][]byte{
		"not json":        []byte("hello"),
		"unknown field":   []byte(`{"v":1,"op":"broker.info","actor":{"user":"x","scopes":[]},"sudo":true}`),
		"wrong version":   []byte(`{"v":2,"op":"broker.info","actor":{"user":"x","scopes":[]}}`),
		"trailing data":   []byte(`{"v":1,"op":"broker.info","actor":{"user":"x","scopes":[]}}{}`),
		"args to no-args": []byte(`{"v":1,"op":"legacy.webmin.status","args":{"path":"/etc/shadow"},"actor":{"user":"x","scopes":["system.read"]}}`),
	}
	for name, p := range cases {
		resp := rawSend(t, f.sock, p, uint32(len(p)))
		if resp.OK || resp.Error == nil || resp.Error.Code != proto.CodeBadRequest {
			t.Errorf("%s: %+v", name, resp)
		}
	}
	big := bytes.Repeat([]byte("a"), 100)
	if resp := rawSend(t, f.sock, big, proto.MaxRequest+1); resp.OK || resp.Error.Code != proto.CodeBadRequest {
		t.Errorf("oversized frame: %+v", resp)
	}
}

func TestSlowClientTimesOut(t *testing.T) {
	f := start(t)
	c, err := net.Dial("unix", f.sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(8 * time.Second))
	var resp proto.Response
	start := time.Now()
	err = proto.ReadFrame(c, &resp, proto.MaxResponse) // send nothing
	if time.Since(start) > 7*time.Second || (err == nil && resp.OK) {
		t.Fatalf("slow client held connection: %v %+v", err, resp)
	}
}

func TestPanickingOpContained(t *testing.T) {
	f := start(t)
	f.srv.registry["test.panic"] = ops.Op{Name: "test.panic", Timeout: time.Second, Handle: func(context.Context, *ops.Env, json.RawMessage) (any, error) {
		panic("boom")
	}}
	if _, err := call(t, f, "test.panic"); code(err) != proto.CodeInternal {
		t.Fatalf("panic not contained: %v", err)
	}
	if _, err := call(t, f, "broker.info"); err != nil {
		t.Fatalf("broker died after panic: %v", err)
	}
}

func TestBusy(t *testing.T) {
	f := start(t)
	f.srv.sem = make(chan struct{}, 1)
	release := make(chan struct{})
	f.srv.registry["test.block"] = ops.Op{Name: "test.block", Timeout: 5 * time.Second, Handle: func(context.Context, *ops.Env, json.RawMessage) (any, error) {
		<-release
		return "done", nil
	}}
	go call(t, f, "test.block")
	time.Sleep(200 * time.Millisecond)
	_, err := call(t, f, "broker.info")
	close(release)
	if code(err) != proto.CodeBusy {
		t.Fatalf("want busy, got %v", err)
	}
}

func TestClientUnavailable(t *testing.T) {
	err := brokerclient.New("/nonexistent/broker.sock").Call(context.Background(), "broker.info", proto.Actor{}, nil)
	if !errors.Is(err, brokerclient.ErrUnavailable) {
		t.Fatalf("%v", err)
	}
}

func TestAuditHasNoResults(t *testing.T) {
	f := start(t)
	if _, err := call(t, f, "broker.info"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.audit.String(), "services.systemd.list") {
		t.Fatalf("audit log contains result data: %s", f.audit)
	}
	if !strings.Contains(f.audit.String(), `"user":"tester"`) || !strings.Contains(f.audit.String(), "uid=") {
		t.Fatalf("audit lacks actor/peer: %s", f.audit)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// A result is withheld when its audit record cannot be written.
func TestAuditFailureWithholdsResult(t *testing.T) {
	f := start(t)
	f.srv.Audit = audit.New(failWriter{})
	out, err := call(t, f, "broker.info")
	if code(err) != proto.CodeUnavailable || len(out) != 0 {
		t.Fatalf("want unavailable and no result, got %v %s", err, out)
	}
}

// Argument-taking ops record validated arguments only, never raw input.
func TestOpArgsSummarisedInAudit(t *testing.T) {
	f := start(t)
	cl := brokerclient.New(f.sock)
	actor := proto.Actor{User: "tester", Scopes: []string{"services.logs"}}
	_ = cl.CallWithArgs(context.Background(), "services.unit.logs", map[string]any{"unit": "ssh.service", "lines": 5}, actor, nil)
	if !strings.Contains(f.audit.String(), `"detail":"services.unit.logs unit=ssh.service lines=5 `) {
		t.Fatalf("args not summarised: %s", f.audit)
	}
	evil := "$(id)\u001b[31m.service"
	err := cl.CallWithArgs(context.Background(), "services.unit.logs", map[string]any{"unit": evil, "lines": 5}, actor, nil)
	if code(err) != proto.CodeBadRequest {
		t.Fatalf("want bad_request, got %v", err)
	}
	if strings.Contains(f.audit.String(), "$(id)") || !strings.Contains(f.audit.String(), "services.unit.logs invalid args") {
		t.Fatalf("raw args reached the audit log: %s", f.audit)
	}
	// Without the scope the broker refuses before running anything.
	err = cl.CallWithArgs(context.Background(), "services.unit.logs", map[string]any{"unit": "ssh.service", "lines": 5}, proto.Actor{User: "tester", Scopes: []string{"services.read"}}, nil)
	if code(err) != proto.CodeForbidden {
		t.Fatalf("want forbidden, got %v", err)
	}
}

// TestRootExecHandleOnlyForRootExecOps proves W9 at runtime: the root-exec
// handle reaches an op only when the op is registered as root-exec, even if
// someone puts a handle into the shared Env.
func TestRootExecHandleOnlyForRootExecOps(t *testing.T) {
	f := start(t)
	f.srv.RootExec = new(runner.Runner).RootExec()
	f.srv.Env.RootExec = new(runner.Runner).RootExec() // must be ignored
	seen := map[string]bool{}
	var actor proto.Actor
	probe := func(name string) func(context.Context, *ops.Env, json.RawMessage) (any, error) {
		return func(_ context.Context, env *ops.Env, _ json.RawMessage) (any, error) {
			seen[name] = env.RootExec != nil
			actor = env.Actor
			return map[string]bool{"ok": true}, nil
		}
	}
	f.srv.registry = map[string]ops.Op{
		"t.none":      {Name: "t.none", Privilege: types.PrivNone, Timeout: time.Second, Handle: probe("t.none")},
		"t.rootread":  {Name: "t.rootread", Privilege: types.PrivRootRead, Timeout: time.Second, Handle: probe("t.rootread")},
		"t.unpriv":    {Name: "t.unpriv", Privilege: types.PrivUnprivilegedExec, Timeout: time.Second, Handle: probe("t.unpriv")},
		"t.rootexec":  {Name: "t.rootexec", Privilege: types.PrivRootExec, Timeout: time.Second, Handle: probe("t.rootexec")},
		"t.rootexec2": {Name: "t.rootexec2", Scope: "services.operate", Privilege: types.PrivRootExec, Timeout: time.Second, Handle: probe("t.rootexec2")},
	}
	for _, op := range []string{"t.none", "t.rootread", "t.unpriv", "t.rootexec"} {
		if _, err := call(t, f, op); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}
	if seen["t.none"] || seen["t.rootread"] || seen["t.unpriv"] || !seen["t.rootexec"] {
		t.Fatalf("root handle exposure: %v", seen)
	}
	if actor.User != "tester" {
		t.Fatalf("actor not passed per call: %+v", actor)
	}
	// The broker re-checks the write scope itself.
	if _, err := call(t, f, "t.rootexec2", "services.read", "services.logs"); code(err) != proto.CodeForbidden {
		t.Fatalf("root-exec op without services.operate: %v", err)
	}
	if seen["t.rootexec2"] {
		t.Fatal("handler ran without scope")
	}
}

// TestWriteOpsRefusedWithoutScopeEndToEnd goes through the real registry.
func TestWriteOpsRefusedWithoutScopeEndToEnd(t *testing.T) {
	f := start(t)
	for _, op := range []string{"services.unit.plan", "services.unit.apply"} {
		var out json.RawMessage
		err := brokerclient.New(f.sock).CallWithArgs(context.Background(), op,
			map[string]string{"unit": "demo.service", "action": "stop", "token": "x"},
			proto.Actor{User: "tester", Session: "s", Scopes: []string{"services.read", "services.logs", "audit.read"}}, &out)
		if code(err) != proto.CodeForbidden {
			t.Errorf("%s without services.operate: %v", op, err)
		}
	}
	if _, err := call(t, f, "operations.journal.list", "services.operate"); code(err) != proto.CodeForbidden {
		t.Errorf("journal without audit.read: %v", err)
	}
	if !strings.Contains(f.audit.String(), `"outcome":"forbidden"`) {
		t.Fatal("refusals not audited")
	}
}
