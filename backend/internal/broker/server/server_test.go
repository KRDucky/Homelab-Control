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
