// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"context"
	"encoding/json"
	"homelab-control/backend/internal/traefikclient"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureDocker(t *testing.T, body string) *Docker {
	t.Helper()
	return &Docker{client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.RequestURI() != dockerListPath || r.Header.Get("Authorization") != "" {
			t.Fatal("unexpected Docker operation")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, routing: Context{Networks: []string{"hc-traefik-test"}, Entrypoints: []string{"web"}, Resolvers: []string{}}}
}
func dockerFixture(t *testing.T) string {
	t.Helper()
	b, e := os.ReadFile("testdata/docker-containers.json")
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestDockerRealisticMetadataSanitization(t *testing.T) {
	d := fixtureDocker(t, dockerFixture(t))
	s, e := d.Snapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if s.Fixture || s.Source != "docker-rootless" || len(s.Services) != 3 {
		t.Fatal(s)
	}
	service := s.Services[0]
	if !service.Compose || service.Project != "notes" || service.Service != "web" || !service.HiddenTraefikSettings {
		t.Fatal(service)
	}
	if !reflect.DeepEqual(service.Ports, []Port{{53, "udp"}, {8080, "tcp"}}) {
		t.Fatal(service.Ports)
	}
	if s.Services[1].Compose || !s.Services[2].UnsupportedDeployment {
		t.Fatal(s.Services)
	}
	b, _ := json.Marshal(View(s))
	for _, secret := range []string{"PRIVATE", "UNTRUSTED", "config_files", "Command", "IPAddress", "18080", "28080", "43000"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("metadata leakage", secret)
		}
	}
	s.Traefik.DockerProvider = new(bool)
	req := Request{ServiceID: service.ID, Network: "hc-traefik-test", Model: traefikclient.Model{Name: "new", Host: "new.example.com", Port: 8080, Entrypoint: "web", Protocol: "http", UpstreamScheme: "http"}}
	p, e := Build(s, traefikclient.Inventory{}, req)
	if e != nil || !has(p.Blockers, "unsupported_existing") || !has(p.Blockers, "provider_disabled") || p.ApplyAvailable {
		t.Fatal(p, e)
	}
	req.ConfirmUnverifiedPort = true
	req.ServiceID = s.Services[2].ID
	p, e = Build(s, traefikclient.Inventory{}, req)
	if e != nil || !has(p.Blockers, "unsupported_deployment") {
		t.Fatal(p, e)
	}
}
func TestDockerRejectsMalformedAndOversizedMetadata(t *testing.T) {
	original := dockerFixture(t)
	for name, body := range map[string]string{"null": "null", "object": "{}", "invalid-json": "[", "too-large": strings.Repeat(" ", maxDockerBytes+1), "bad-id": strings.Replace(original, strings.Repeat("a", 64), "../../bad", 1), "bad-name": strings.Replace(original, "notes-web-1", "bad\\nname", 1), "bad-port": strings.Replace(original, `"PrivatePort": 8080`, `"PrivatePort": 0`, 1), "incomplete-compose": strings.Replace(original, `"com.docker.compose.service": "web"`, `"com.docker.compose.service": ""`, 1), "duplicate-id": strings.Replace(original, `"Id": "`+strings.Repeat("b", 64)+`"`, `"Id": "`+strings.Repeat("a", 64)+`"`, 1)} {
		t.Run(name, func(t *testing.T) {
			if _, e := fixtureDocker(t, body).Snapshot(context.Background()); e != ErrDockerUnavailable {
				t.Fatal(e)
			}
		})
	}
}
func TestDockerDeadlineAndHTTPFailures(t *testing.T) {
	for _, status := range []int{301, 401, 403, 404, 500} {
		d := fixtureDocker(t, "[]")
		d.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("PRIVATE DAEMON ERROR"))}, nil
		})
		if _, e := d.Snapshot(context.Background()); e != ErrDockerUnavailable {
			t.Fatal(status, e)
		}
	}
	d := fixtureDocker(t, "[]")
	d.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, e := d.Snapshot(ctx); e != ErrDockerUnavailable {
		t.Fatal(e)
	}
}
func TestDockerUnixTransportAndConfiguration(t *testing.T) {
	// Local fake Unix daemon only; never MediaMouse's Docker socket.
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	if e = os.Chmod(socket, 0600); e != nil {
		t.Fatal(e)
	}
	seen := make(chan string, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Method + " " + r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "[]")
	})}
	go server.Serve(listener)
	defer server.Close()
	t.Setenv("DOCKER_HOST", "tcp://evil.invalid:2375")
	disabled := false
	d, e := NewDocker(socket, uint32(os.Getuid()), Context{DockerProvider: &disabled})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = d.Snapshot(context.Background()); e != nil {
		t.Fatal(e)
	}
	if <-seen != "GET "+dockerListPath {
		t.Fatal("unexpected operation")
	}
	for _, path := range []string{"tcp://host:2375", "relative", "/var/run/docker.sock", "/missing/docker.sock"} {
		if _, e = NewDocker(path, uint32(os.Getuid()), Context{}); e == nil {
			t.Fatal(path)
		}
	}
	if _, e = NewDocker(socket, 0, Context{}); e == nil {
		t.Fatal("root owner accepted")
	}
	link := socket + "-link"
	if e = os.Symlink(socket, link); e != nil {
		t.Fatal(e)
	}
	if _, e = NewDocker(link, uint32(os.Getuid()), Context{}); e == nil {
		t.Fatal("symlink accepted")
	}
	if e = os.Chmod(socket, 0606); e != nil {
		t.Fatal(e)
	}
	if _, e = d.Snapshot(context.Background()); e != ErrDockerUnavailable {
		t.Fatal("changed permissions not rejected", e)
	}
}

func TestDockerMultipleProjectsReplicasMissingPortsAndNetworking(t *testing.T) {
	var rows []map[string]any
	if json.Unmarshal([]byte(dockerFixture(t)), &rows) != nil {
		t.Fatal("fixture")
	}
	clone := func(v map[string]any) map[string]any {
		b, _ := json.Marshal(v)
		var out map[string]any
		json.Unmarshal(b, &out)
		return out
	}
	replica := clone(rows[0])
	replica["Id"] = strings.Repeat("d", 64)
	replica["Names"] = []string{"/notes-web-2"}
	rows = append(rows, replica)
	another := clone(rows[0])
	another["Id"] = strings.Repeat("e", 64)
	another["Names"] = []string{"/gallery-web-1"}
	another["Labels"] = map[string]string{"com.docker.compose.project": "gallery", "com.docker.compose.service": "web"}
	another["State"] = "exited"
	another["Ports"] = []any{}
	another["HostConfig"] = map[string]string{"NetworkMode": "host"}
	rows = append(rows, another)
	b, _ := json.Marshal(rows)
	s, e := fixtureDocker(t, string(b)).Snapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if s.Services[0].ReplicaCount != 2 || s.Services[3].ReplicaCount != 2 || s.Services[4].ReplicaCount != 1 || !s.Services[4].UnsupportedNetworking || len(s.Services[4].Ports) != 0 {
		t.Fatal(s.Services)
	}
	req := Request{ServiceID: s.Services[4].ID, Network: "hc-traefik-test", ConfirmUnverifiedPort: true, Model: traefikclient.Model{Name: "gallery", Host: "gallery.example.com", Port: 3000, Entrypoint: "web", Protocol: "http", UpstreamScheme: "http"}}
	p, e := Build(s, traefikclient.Inventory{}, req)
	if e != nil || !has(p.Blockers, "port_unverified") || !has(p.Blockers, "unsupported_networking") {
		t.Fatal(p, e)
	}
}
