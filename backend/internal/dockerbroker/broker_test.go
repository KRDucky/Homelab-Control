// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package dockerbroker

import (
	"context"
	"encoding/json"
	"homelab-control/backend/internal/composediscovery"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "fixture-metadata-test-credential-0123456789"

type provider struct {
	calls atomic.Int32
	err   error
	wait  bool
}

func (p *provider) Snapshot(ctx context.Context) (composediscovery.Snapshot, error) {
	p.calls.Add(1)
	if p.wait {
		<-ctx.Done()
		return composediscovery.Snapshot{}, ctx.Err()
	}
	return composediscovery.Snapshot{Source: "docker-rootless", Services: []composediscovery.Service{{ID: strings.Repeat("a", 64), Compose: true, Project: "notes", Service: "web", Container: "notes-web-1", State: "running", Ports: []composediscovery.Port{{Port: 8080, Protocol: "tcp"}, {Port: 53, Protocol: "udp"}}, Networks: []string{"private"}, Labels: map[string]string{"traefik.enable": "true", "traefik.http.middlewares.auth.basicauth.users": "SECRET-HASH", "unrelated": "PRIVATE"}}}}, p.err
}
func start(t *testing.T, p composediscovery.Provider, uid uint32) (*Client, string) {
	t.Helper()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "metadata.sock")
	l, e := Listen(path)
	if e != nil {
		t.Fatal(e)
	}
	server, e := NewServer(p, testToken, uid)
	if e != nil {
		t.Fatal(e)
	}
	go server.Serve(l)
	t.Cleanup(func() { server.Close(); l.Close() })
	client, e := NewClient(path, testToken, uint32(os.Getuid()), composediscovery.Context{Networks: []string{}, Entrypoints: []string{}, Resolvers: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	return client, path
}
func TestUnixBrokerContractSanitizationAndFixedReads(t *testing.T) {
	p := &provider{}
	client, _ := start(t, p, uint32(os.Getuid()))
	s, e := client.Snapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Services) != 1 || len(s.Services[0].Ports) != 1 || s.Services[0].Ports[0].Port != 8080 || !s.Services[0].HiddenTraefikSettings || s.Services[0].ReplicaCount != 1 {
		t.Fatal(s)
	}
	data, _ := json.Marshal(composediscovery.View(s))
	for _, secret := range []string{"SECRET", "PRIVATE", "basicauth", "unrelated"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("sensitive metadata leaked")
		}
	}
	if p.calls.Load() != 1 {
		t.Fatal(p.calls.Load())
	}
}
func TestBrokerAuthenticationPeerAndAllowlist(t *testing.T) {
	p := &provider{}
	client, path := start(t, p, uint32(os.Getuid()))
	for _, tt := range []struct {
		method, path, token string
		code                int
	}{{"GET", SnapshotPath, "", 403}, {"GET", SnapshotPath, "wrong", 403}, {"POST", SnapshotPath, testToken, 405}, {"GET", "/containers/json", testToken, 404}, {"GET", SnapshotPath + "?url=http://docker/exec", testToken, 400}, {"GET", "/v1/health", testToken, 200}} {
		req, _ := http.NewRequest(tt.method, "http://metadata"+tt.path, nil)
		req.Header.Set("Authorization", "Bearer "+tt.token)
		req.Header.Set("X-Scopes", "docker.compose.read")
		req.Header.Set("X-User", "admin")
		r, e := client.http.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != tt.code {
			t.Fatal(tt, r.StatusCode)
		}
	}
	if p.calls.Load() != 0 {
		t.Fatal("unauthorized or non-metadata request invoked provider")
	}
	wrong, _ := NewClient(path, "wrong-token-value-0123456789abcdef", uint32(os.Getuid()), composediscovery.Context{})
	if _, e := wrong.Snapshot(context.Background()); e != ErrUnavailable {
		t.Fatal(e)
	}
	rejected := &provider{}
	bad, _ := start(t, rejected, uint32(os.Getuid()+1))
	if _, e := bad.Snapshot(context.Background()); e != ErrUnavailable || rejected.calls.Load() != 0 {
		t.Fatal("peer identity bypass")
	}
}
func TestBrokerStrictSchemaRedactionAndBounds(t *testing.T) {
	s, _ := (&provider{}).Snapshot(context.Background())
	e, err := FromSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(e)
	for _, payload := range []string{`null`, `{}`, `{"schemaVersion":1,"schemaVersion":1,"source":"docker-rootless","services":[]}`, string(b) + `{}`, strings.Replace(string(b), `"schemaVersion":1`, `"schemaVersion":2`, 1), strings.Replace(string(b), `"source":"docker-rootless"`, `"source":"docker-rootless","environment":["SECRET"]`, 1), strings.Repeat("x", MaxBytes+1), strings.Replace(string(b), `"traefik.enable":"true"`, `"traefik.http.middlewares.auth.basicauth.users":"SECRET"`, 1)} {
		if _, err := Decode([]byte(payload)); err == nil {
			t.Fatal("invalid broker payload accepted")
		}
	}
	e.Services = append(e.Services, e.Services[0])
	b, _ = json.Marshal(e)
	if _, err := Decode(b); err == nil {
		t.Fatal("duplicate IDs accepted")
	}
}
func TestBrokerUnavailableTimeoutAndSocketFailures(t *testing.T) {
	p := &provider{err: ErrUnavailable}
	c, _ := start(t, p, uint32(os.Getuid()))
	if _, e := c.Snapshot(context.Background()); e != ErrUnavailable {
		t.Fatal(e)
	}
	p2 := &provider{wait: true}
	c, _ = start(t, p2, uint32(os.Getuid()))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := c.Snapshot(ctx); e != ErrUnavailable {
		t.Fatal(e)
	}
	if _, e := NewClient("tcp://127.0.0.1:1", testToken, 0, composediscovery.Context{}); e == nil {
		t.Fatal("TCP accepted")
	}
	if _, e := NewClient("/missing/metadata.sock", testToken, 0, composediscovery.Context{}); e == nil {
		t.Fatal("missing socket accepted")
	}
	path := filepath.Join(t.TempDir(), "present")
	os.WriteFile(path, []byte("must remain"), 0600)
	if _, e := Listen(path); e == nil {
		t.Fatal("existing file replaced")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "must remain" {
		t.Fatal("existing file modified")
	}
}
func TestDockerAdapterThroughBrokerUsesOnlyContainerList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine.sock")
	l, e := net.Listen("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	os.Chmod(path, 0600)
	var calls atomic.Int32
	fixture, e := os.ReadFile("../composediscovery/testdata/docker-containers.json")
	if e != nil {
		t.Fatal(e)
	}
	engine := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.RequestURI() != "/v1.53/containers/json?all=true&size=false" {
			t.Error("unexpected Docker request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixture)
	})}
	go engine.Serve(l)
	defer engine.Close()
	adapter, e := composediscovery.NewDocker(path, uint32(os.Getuid()), composediscovery.Context{})
	if e != nil {
		t.Fatal(e)
	}
	client, _ := start(t, adapter, uint32(os.Getuid()))
	s, e := client.Snapshot(context.Background())
	if e != nil || len(s.Services) != 3 || calls.Load() != 1 {
		t.Fatal(s, e)
	}
	b, _ := json.Marshal(composediscovery.View(s))
	if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "UNTRUSTED") {
		t.Fatal("fixture secrets leaked")
	}
}
func TestClientRejectsOversizedAndUnknownResponse(t *testing.T) {
	for _, body := range []string{strings.Repeat("x", MaxBytes+1), `{"schemaVersion":1,"source":"docker-rootless","services":[],"token":"SECRET"}`} {
		path := filepath.Join(t.TempDir(), "bad.sock")
		l, e := net.Listen("unix", path)
		if e != nil {
			t.Fatal(e)
		}
		os.Chmod(path, 0600)
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
		})}
		go server.Serve(l)
		c, e := NewClient(path, testToken, uint32(os.Getuid()), composediscovery.Context{})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = c.Snapshot(context.Background()); e != ErrUnavailable {
			t.Fatal(e)
		}
		server.Close()
	}
}

func TestPrivateSocketPathsAndRoutingConfig(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0755)
	if _, e := Listen(filepath.Join(dir, "refused.sock")); e == nil {
		t.Fatal("unsafe parent permissions accepted")
	}
	os.Chmod(dir, 0700)
	l, e := Listen(filepath.Join(dir, "broker.sock"))
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	if _, e := NewClient(filepath.Join(dir, "broker.sock"), testToken, uint32(os.Getuid()+1), composediscovery.Context{}); e == nil {
		t.Fatal("wrong socket owner accepted")
	}
	link := filepath.Join(dir, "link.sock")
	os.Symlink(filepath.Join(dir, "broker.sock"), link)
	if _, e := NewClient(link, testToken, uint32(os.Getuid()), composediscovery.Context{}); e == nil {
		t.Fatal("socket symlink accepted")
	}
	file := filepath.Join(dir, "routing.json")
	good := `{"schemaVersion":1,"routing":{"networks":["private"],"entrypoints":["web"],"resolvers":[],"dockerProvider":false}}`
	os.WriteFile(file, []byte(good), 0400)
	if _, e := LoadRouting(file); e != nil {
		t.Fatal(e)
	}
	os.Chmod(file, 0600)
	for _, body := range []string{good + `{}`, strings.Replace(good, `"web"`, `"UPPER_INVALID"`, 1), strings.Replace(good, `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1)} {
		os.WriteFile(file, []byte(body), 0600)
		if _, e := LoadRouting(file); e == nil {
			t.Fatal("invalid routing accepted")
		}
	}
}

func TestRejectReplicaCountClaimsOutsideSchema(t *testing.T) {
	s, _ := (&provider{}).Snapshot(context.Background())
	e, err := FromSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	e.Services[0].ReplicaCount = -1
	b, _ := json.Marshal(e)
	if _, err = Decode(b); err == nil {
		t.Fatal("negative count accepted")
	}
}
