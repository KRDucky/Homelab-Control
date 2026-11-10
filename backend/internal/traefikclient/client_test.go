// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package traefikclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testToken = "fixture-token-0123456789abcdef0123456789"

func fixtureModel() Model {
	return Model{Name: "demo", Host: "demo.localhost", Port: 8080, Entrypoint: "web", Protocol: "http", UpstreamScheme: "http"}
}
func TestModelValidation(t *testing.T) {
	m := fixtureModel()
	p, e := m.Labels()
	if e != nil || p.Labels["traefik.http.routers.demo.tls"] != "false" {
		t.Fatal(p, e)
	}
	for _, bad := range []Model{{}, {Name: "x.rule", Host: "demo.localhost", Port: 80, Entrypoint: "web", Protocol: "http", UpstreamScheme: "http"}} {
		if _, e := bad.Labels(); e == nil {
			t.Fatal("accepted invalid model")
		}
	}
	for _, modify := range []func(*Model){func(m *Model) { m.Host = "x`) || Host(`evil.test" }, func(m *Model) { m.Port = 0 }, func(m *Model) { m.Port = 65536 }, func(m *Model) { m.TLS = &TLS{} }, func(m *Model) { m.Protocol = "https" }, func(m *Model) { m.UpstreamScheme = "tcp" }} {
		b := m
		modify(&b)
		if _, e := b.Labels(); e == nil {
			t.Fatal("invalid model accepted", b)
		}
	}
	m.Protocol = "https"
	m.TLS = &TLS{CertificateResolver: "staging"}
	m.UpstreamScheme = "https"
	p, e = m.Labels()
	if e != nil || p.Labels["traefik.http.routers.demo.tls.certresolver"] != "staging" {
		t.Fatal(p, e)
	}
}
func TestClientFixedContractAndRedaction(t *testing.T) {
	calls := []string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Error("missing helper auth")
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/inventory":
			io := `{"schemaVersion":1,"routers":[{"name":"demo","provider":"file","status":"enabled","password":"secret"}],"services":[],"middlewares":[],"token":"secret"}`
			w.Write([]byte(io))
		case "/v1/diagnostics":
			w.Write([]byte(`{"schemaVersion":1,"status":"available","checks":[{"resource":"routers","status":"available","hint":"secret"},{"resource":"services","status":"available"},{"resource":"middlewares","status":"unavailable","code":"dns_failed","hint":"secret"}]}`))
		case "/v1/labels":
			var m Model
			json.NewDecoder(r.Body).Decode(&m)
			p, _ := m.Labels()
			json.NewEncoder(w).Encode(p)
		default:
			t.Error("unexpected helper path")
		}
	}))
	defer ts.Close()
	c, e := New(ts.URL, testToken)
	if e != nil {
		t.Fatal(e)
	}
	inv, e := c.Inventory(context.Background())
	b, _ := json.Marshal(inv)
	if e != nil || strings.Contains(string(b), "secret") {
		t.Fatal(string(b), e)
	}
	d, e := c.Diagnostics(context.Background())
	b, _ = json.Marshal(d)
	if e != nil || d.Status != "degraded" || strings.Contains(string(b), "secret") {
		t.Fatal(string(b), e)
	}
	if _, e = c.Labels(context.Background(), fixtureModel()); e != nil {
		t.Fatal(e)
	}
	if strings.Join(calls, ",") != "GET /v1/inventory,GET /v1/diagnostics,POST /v1/labels" {
		t.Fatal(calls)
	}
}
func TestClientRejectsBadResponsesAndRedirects(t *testing.T) {
	leaked := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer destination.Close()
	for _, body := range []string{`{}`, `{"schemaVersion":9,"routers":[],"services":[],"middlewares":[]}`, strings.Repeat("x", 4*1024*1024+1)} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(body))
		}))
		c, _ := New(ts.URL, testToken)
		if _, e := c.Inventory(context.Background()); e != ErrUnavailable {
			t.Fatal("bad response accepted", e)
		}
		ts.Close()
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer ts.Close()
	c, _ := New(ts.URL, testToken)
	if _, e := c.Inventory(context.Background()); e != ErrUnavailable || leaked {
		t.Fatal("redirect followed", e)
	}
}
func TestClientRejectsInjectedLabelsAndCancelledRequests(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"labels":{"evil":"true"}}`))
	}))
	defer ts.Close()
	c, _ := New(ts.URL, testToken)
	if _, e := c.Labels(context.Background(), fixtureModel()); e != ErrUnavailable {
		t.Fatal("untrusted labels accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.Inventory(ctx); e != ErrUnavailable {
		t.Fatal("cancellation ignored")
	}
}
func TestConfigurationAndPrivateSecret(t *testing.T) {
	for _, url := range []string{"file:///tmp/x", "http://u:p@host", "http://host/path", "http://host/?x=1"} {
		if _, e := New(url, testToken); e == nil {
			t.Fatal(url)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	os.WriteFile(path, []byte(testToken+"\n"), 0600)
	if tok, e := LoadToken(path); e != nil || tok != testToken {
		t.Fatal(e)
	}
	link := filepath.Join(dir, "link")
	os.Symlink(path, link)
	if _, e := LoadToken(link); e == nil {
		t.Fatal("symlink accepted")
	}
	if _, e := New("http://helper", testToken+"\n"); e == nil {
		t.Fatal("header injection accepted")
	}
}
