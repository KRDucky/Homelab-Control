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
	"sync"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/broker/ops"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/modules"
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
	f.srv.registry["test.panic"] = ops.Op{Name: "test.panic", Admission: ops.AdmissionControlPlan, Timeout: time.Second, Handle: func(context.Context, *ops.Env, json.RawMessage) (any, error) {
		panic("boom")
	}}
	if _, err := call(t, f, "test.panic"); code(err) != proto.CodeInternal {
		t.Fatalf("panic not contained: %v", err)
	}
	if _, err := call(t, f, "broker.info"); err != nil {
		t.Fatalf("broker died after panic: %v", err)
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

func TestAuditRecordsExecErrnoClass(t *testing.T) {
	f := start(t)
	saved := f.srv.registry["broker.info"]
	f.srv.registry["broker.info"] = ops.Op{
		Name: "broker.info", Admission: ops.AdmissionInfo, Timeout: time.Second,
		Handle: func(context.Context, *ops.Env, json.RawMessage) (any, error) {
			return nil, &proto.Error{Code: proto.CodeInternal, Message: "systemctl could not be run (EACCES)"}
		},
	}
	t.Cleanup(func() { f.srv.registry["broker.info"] = saved })
	_, err := call(t, f, "broker.info")
	if code(err) != proto.CodeInternal {
		t.Fatalf("%v", err)
	}
	got := f.audit.String()
	if !strings.Contains(got, `"outcome":"internal"`) || !strings.Contains(got, "errno=EACCES") {
		t.Fatalf("audit missing errno class: %s", got)
	}
	if strings.Contains(got, "/usr/bin") || strings.Contains(got, "permission denied") || strings.Contains(got, "TOKEN") {
		t.Fatalf("audit leaked: %s", got)
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
		"t.none":      {Name: "t.none", Privilege: types.PrivNone, Timeout: time.Second, Admission: ops.AdmissionControlPlan, Handle: probe("t.none")},
		"t.rootread":  {Name: "t.rootread", Privilege: types.PrivRootRead, Timeout: time.Second, Admission: ops.AdmissionControlPlan, Handle: probe("t.rootread")},
		"t.unpriv":    {Name: "t.unpriv", Privilege: types.PrivUnprivilegedExec, Timeout: time.Second, Admission: ops.AdmissionControlPlan, Handle: probe("t.unpriv")},
		"t.rootexec":  {Name: "t.rootexec", Privilege: types.PrivRootExec, Timeout: time.Second, Admission: ops.AdmissionControlPlan, Handle: probe("t.rootexec")},
		"t.rootexec2": {Name: "t.rootexec2", Scope: "services.operate", Privilege: types.PrivRootExec, Timeout: time.Second, Admission: ops.AdmissionControlPlan, Handle: probe("t.rootexec2")},
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

// M2-3 module registry: ops of a disabled module are refused (before the
// scope check or any handler) and omitted from broker.info; core ops and
// other modules keep working.
func TestDisabledModuleOpsRefused(t *testing.T) {
	f := start(t)
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["services"]}`))
	if err != nil {
		t.Fatal(err)
	}
	f.srv.Env.Modules = set
	for _, op := range []string{"services.systemd.list", "services.unit.logs", "services.unit.plan", "services.unit.apply",
		"services.enablement.plan", "services.enablement.apply", "services.timers.list", "services.unit.detail", "services.unit.contents"} {
		if _, err := call(t, f, op, "services.read", "services.logs", "services.operate", "services.configure"); code(err) != proto.CodeModuleDisabled {
			t.Errorf("%s: %v", op, err)
		}
	}
	if !strings.Contains(f.audit.String(), `"outcome":"module_disabled"`) {
		t.Fatalf("refusal not audited: %s", f.audit)
	}
	out, err := call(t, f, "broker.info")
	if err != nil || strings.Contains(string(out), `"services.`) || !strings.Contains(string(out), `"disabled_modules":["services"]`) {
		t.Fatalf("broker.info: %v %s", err, out)
	}
	if _, err := call(t, f, "legacy.webmin.status", "system.read"); err != nil {
		t.Fatalf("core op affected: %v", err)
	}
	// Fail closed: an invalid setting disables every module.
	f.srv.Env.Modules = modules.AllDisabled("test")
	if _, err := call(t, f, "services.systemd.list", "services.read"); code(err) != proto.CodeModuleDisabled {
		t.Fatalf("fail-closed: %v", err)
	}
}

func TestDisabledStorageModuleRefusesProbe(t *testing.T) {
	f := start(t)
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["storage"]}`))
	if err != nil {
		t.Fatal(err)
	}
	f.srv.Env.Modules = set
	if _, err := call(t, f, "storage.probe", "storage.probe"); code(err) != proto.CodeModuleDisabled {
		t.Fatalf("storage.probe with disabled module: %v", err)
	}
	if !strings.Contains(f.audit.String(), `"outcome":"module_disabled"`) {
		t.Fatalf("disabled storage probe was not audited: %s", f.audit.String())
	}
	out, err := call(t, f, "broker.info")
	if err != nil || strings.Contains(string(out), `"storage.probe"`) || !strings.Contains(string(out), `"disabled_modules":["storage"]`) {
		t.Fatalf("broker.info did not hide disabled storage op: %v %s", err, out)
	}
}

// M2-3: the broker re-checks services.configure; services.operate (or any
// other scope) never reaches the enablement ops, and unit contents need
// services.files (no other scope implies it).
func TestEnablementOpsNeedConfigureScopeEndToEnd(t *testing.T) {
	f := start(t)
	for _, op := range []string{"services.enablement.plan", "services.enablement.apply"} {
		var out json.RawMessage
		err := brokerclient.New(f.sock).CallWithArgs(context.Background(), op,
			map[string]string{"unit": "demo.service", "action": "enable", "token": "x"},
			proto.Actor{User: "tester", Session: "s", Scopes: []string{"services.read", "services.logs", "services.operate", "audit.read"}}, &out)
		if code(err) != proto.CodeForbidden {
			t.Errorf("%s without services.configure: %v", op, err)
		}
	}
	var out json.RawMessage
	err := brokerclient.New(f.sock).CallWithArgs(context.Background(), "services.unit.contents", map[string]string{"unit": "demo.service"},
		proto.Actor{User: "tester", Scopes: []string{"system.read", "services.read", "hardware.read", "services.logs", "audit.read",
			"services.operate", "services.configure"}}, &out)
	if code(err) != proto.CodeForbidden || !strings.Contains(err.Error(), "services.files") {
		t.Errorf("contents with every scope but services.files: %v", err)
	}
}

// M2-6: the broker re-checks host.settings.apply; preview, operate and
// configure never reach the host-settings ops.
func TestHostSettingsOpsNeedApplyScopeEndToEnd(t *testing.T) {
	f := start(t)
	for _, op := range []string{"host.settings.plan", "host.settings.apply"} {
		var out json.RawMessage
		err := brokerclient.New(f.sock).CallWithArgs(context.Background(), op,
			map[string]string{"static_hostname": "nas-01", "token": "x"},
			proto.Actor{User: "tester", Session: "s", Scopes: []string{
				"system.read", "host.settings.preview", "services.operate", "services.configure", "audit.read",
			}}, &out)
		if code(err) != proto.CodeForbidden {
			t.Errorf("%s without host.settings.apply: %v", op, err)
		}
	}
}

// startBlockingOps registers op in the given class and runs n instances,
// waiting until each handler has started (its admission slots are held).
// release unblocks them.
func startBlockingOps(t *testing.T, f *fixture, name string, admission ops.AdmissionClass, n int) (release func()) {
	t.Helper()
	started := make(chan struct{}, n)
	releaseCh := make(chan struct{})
	var once sync.Once
	f.srv.registry[name] = ops.Op{Name: name, Admission: admission, Timeout: 5 * time.Second,
		Handle: func(ctx context.Context, _ *ops.Env, _ json.RawMessage) (any, error) {
			started <- struct{}{}
			select {
			case <-releaseCh:
				return "done", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}}
	for i := 0; i < n; i++ {
		go call(t, f, name)
	}
	for i := 0; i < n; i++ {
		<-started
	}
	return func() { once.Do(func() { close(releaseCh) }) }
}

func TestClassCapacities(t *testing.T) {
	f := start(t)
	want := map[ops.AdmissionClass]int{
		ops.AdmissionControlPlan:  1,
		ops.AdmissionControlApply: 1,
		ops.AdmissionServicesRead: 3,
		ops.AdmissionSystemRead:   1,
		ops.AdmissionPackages:     1,
		ops.AdmissionProbe:        1,
		ops.AdmissionInfo:         1,
	}
	for cls, wantCap := range want {
		if f.srv.ClassCaps[cls] != wantCap {
			t.Errorf("%s: ClassCaps = %d, want %d", cls, f.srv.ClassCaps[cls], wantCap)
		}
		if got := cap(f.srv.classSem[cls]); got != wantCap {
			t.Errorf("%s: sem cap = %d, want %d", cls, got, wantCap)
		}
	}
	sum := 0
	for cls, wantCap := range want {
		if cls == ops.AdmissionInfo {
			continue
		}
		sum += wantCap
	}
	if sum != f.srv.MaxInFlight {
		t.Fatalf("expensive caps sum to %d, want %d", sum, f.srv.MaxInFlight)
	}
	// Every class referenced by a registered op has a configured semaphore.
	seen := map[ops.AdmissionClass]bool{}
	for _, op := range ops.Registry() {
		seen[op.Admission] = true
	}
	for cls := range seen {
		if _, ok := f.srv.classSem[cls]; !ok {
			t.Errorf("class %q has no configured semaphore", cls)
		}
		if cap(f.srv.classSem[cls]) == 0 {
			t.Errorf("class %q has zero capacity", cls)
		}
	}
}

func TestClassRefusesWhenFull(t *testing.T) {
	f := start(t)
	release := startBlockingOps(t, f, "test.pkg", ops.AdmissionPackages, 1)
	defer release()
	if _, err := call(t, f, "test.pkg"); code(err) != proto.CodeBusy {
		t.Fatalf("2nd package op: want busy, got %v", err)
	}
	if !strings.Contains(f.audit.String(), "gate=class") {
		t.Fatalf("class busy not audited: %s", f.audit.String())
	}
}

func TestCrossClassIsolation(t *testing.T) {
	f := start(t)
	pkgRelease := startBlockingOps(t, f, "test.pkg", ops.AdmissionPackages, 1)
	defer pkgRelease()
	// The services-read class is independent: admitted while packages is full.
	svcRelease := startBlockingOps(t, f, "test.svc", ops.AdmissionServicesRead, 1)
	defer svcRelease()
	if _, err := call(t, f, "test.pkg"); code(err) != proto.CodeBusy {
		t.Fatalf("2nd package op: want busy, got %v", err)
	}
}

func TestBrokerInfoUnderSaturation(t *testing.T) {
	f := start(t)
	f.srv.sem = make(chan struct{}, 1) // global backstop full
	release := startBlockingOps(t, f, "test.block", ops.AdmissionControlPlan, 1)
	defer release()
	// broker.info has a dedicated slot and does not take the global slot.
	if _, err := call(t, f, "broker.info"); err != nil {
		t.Fatalf("broker.info under saturation: %v", err)
	}
}

func TestGlobalCapStillRefuses(t *testing.T) {
	f := start(t)
	f.srv.sem = make(chan struct{}, 1) // shrink the global backstop
	release := startBlockingOps(t, f, "test.block", ops.AdmissionControlPlan, 1)
	defer release()
	f.srv.registry["test.block2"] = ops.Op{Name: "test.block2", Admission: ops.AdmissionControlApply, Timeout: time.Second,
		Handle: func(context.Context, *ops.Env, json.RawMessage) (any, error) { return "ok", nil }}
	if _, err := call(t, f, "test.block2"); code(err) != proto.CodeBusy {
		t.Fatalf("global cap: want busy, got %v", err)
	}
	if !strings.Contains(f.audit.String(), "gate=global") {
		t.Fatalf("global busy not audited: %s", f.audit.String())
	}
}

// A class slot taken before a failed global admission must be released
// immediately, so the class is not leaked.
func TestClassSlotReleasedWhenGlobalFull(t *testing.T) {
	f := start(t)
	f.srv.sem = make(chan struct{}, 1) // single global slot
	release := startBlockingOps(t, f, "test.block", ops.AdmissionControlPlan, 1)
	defer release()
	f.srv.registry["test.pkg"] = ops.Op{Name: "test.pkg", Admission: ops.AdmissionPackages, Timeout: time.Second,
		Handle: func(context.Context, *ops.Env, json.RawMessage) (any, error) { return "ok", nil }}
	if _, err := call(t, f, "test.pkg"); code(err) != proto.CodeBusy {
		t.Fatalf("package op with global full: want busy, got %v", err)
	}
	// The busy response is written only after the slot release, so this is a
	// deterministic observation: the package class must be empty again.
	if n := len(f.srv.classSem[ops.AdmissionPackages]); n != 0 {
		t.Fatalf("package slot leaked after global-busy: %d held", n)
	}
}

// A control plan is admitted while every read class and the info slot are
// saturated: control has its own capacity and is never starved by reads.
func TestControlAdmittedUnderReadSaturation(t *testing.T) {
	f := start(t)
	rel1 := startBlockingOps(t, f, "test.svc", ops.AdmissionServicesRead, 3)
	defer rel1()
	rel2 := startBlockingOps(t, f, "test.sys", ops.AdmissionSystemRead, 1)
	defer rel2()
	rel3 := startBlockingOps(t, f, "test.pkg", ops.AdmissionPackages, 1)
	defer rel3()
	rel4 := startBlockingOps(t, f, "test.prb", ops.AdmissionProbe, 1)
	defer rel4()
	rel5 := startBlockingOps(t, f, "test.info", ops.AdmissionInfo, 1)
	defer rel5()
	f.srv.registry["test.plan"] = ops.Op{Name: "test.plan", Admission: ops.AdmissionControlPlan, Timeout: time.Second,
		Handle: func(context.Context, *ops.Env, json.RawMessage) (any, error) { return "ok", nil }}
	if _, err := call(t, f, "test.plan"); err != nil {
		t.Fatalf("control plan under read saturation: %v", err)
	}
}

// The info slot is one, so a second concurrent broker.info is refused.
func TestInfoSlotRefusesSecond(t *testing.T) {
	f := start(t)
	release := startBlockingOps(t, f, "test.info", ops.AdmissionInfo, 1)
	defer release()
	if _, err := call(t, f, "broker.info"); code(err) != proto.CodeBusy {
		t.Fatalf("2nd info request: want busy, got %v", err)
	}
}
