// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/dockerbroker"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const composeBody = `{"serviceId":"fixture-notes-web","network":"hc-traefik-test","model":{"name":"notes","host":"notes.example.com","port":8080,"entrypoint":"web","protocol":"http","upstreamScheme":"http"}}`

func TestComposeReadOnlyAuthorization(t *testing.T) {
	for _, scopes := range [][]string{{}, {auth.ScopeNetworkingProxyRead}, {auth.ScopeComposeRead}, {auth.ScopeNetworkingProxyRead, auth.ScopeComposeRead}} {
		h := harnessWith(t, nil, scopes)
		f := &fakeTraefik{}
		h.srv.WithTraefik(f).WithComposeDiscovery(composediscovery.Fixture{})
		headers := packagesHeaders(h)
		headers["Content-Type"] = "application/json"
		rr := h.do("POST", proxyPath+"/compose-preview", composeBody, headers)
		both := len(scopes) == 2
		if both && rr.Code != 200 {
			t.Fatal(rr.Code, rr.Body)
		}
		if !both && rr.Code != 403 {
			t.Fatal(scopes, rr.Code)
		}
		if !both && f.calls != 0 {
			t.Fatal("unauthorized helper request")
		}
		if both && !strings.Contains(rr.Body.String(), `"applyAvailable":false`) {
			t.Fatal(rr.Body)
		}
	}
}
func TestComposeUnavailableCSRFAndStrictModel(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeComposeRead})
	h.srv.WithTraefik(&fakeTraefik{})
	headers := packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	if rr := h.do("GET", proxyPath+"/applications", "", headers); rr.Code != 503 {
		t.Fatal(rr.Code)
	}
	h.srv.WithComposeDiscovery(composediscovery.Fixture{})
	if rr := h.do("GET", proxyPath+"/applications", "", nil); rr.Code != 403 {
		t.Fatal(rr.Code)
	}
	if rr := h.do("POST", proxyPath+"/compose-preview", composeBody, nil); rr.Code != 403 {
		t.Fatal(rr.Code)
	}
	for _, body := range []string{composeBody + `{}`, strings.Replace(composeBody, `"serviceId"`, `"apply":true,"serviceId"`, 1), strings.Replace(composeBody, `8080`, `0`, 1)} {
		if rr := h.do("POST", proxyPath+"/compose-preview", body, headers); rr.Code != 400 {
			t.Fatal(rr.Code, rr.Body)
		}
	}
	if rr := h.do("GET", proxyPath+"/applications", "", headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"fixture":true`) {
		t.Fatal(rr.Code, rr.Body)
	}
	if rr := h.do("POST", proxyPath+"/apply", composeBody, headers); rr.Code != 404 {
		t.Fatal("unexpected apply endpoint", rr.Code)
	}
}

type countingDiscovery struct {
	calls  int
	failed bool
}

func (p *countingDiscovery) Snapshot(ctx context.Context) (composediscovery.Snapshot, error) {
	p.calls++
	if p.failed {
		return composediscovery.Snapshot{}, errors.New("SECRET broker error")
	}
	return (composediscovery.Fixture{}).Snapshot(ctx)
}
func TestDiscoveryAuthorizationPrecedesProviderAndDoesNotRequireTraefik(t *testing.T) {
	for _, scopes := range [][]string{{auth.ScopeNetworkingProxyRead}, {auth.ScopeComposeRead}} {
		h := harnessWith(t, nil, scopes)
		p := &countingDiscovery{}
		h.srv.WithComposeDiscovery(p)
		rr := h.do("GET", proxyPath+"/applications", "", packagesHeaders(h))
		if len(scopes) > 0 && scopes[0] == auth.ScopeComposeRead {
			if rr.Code != 200 || p.calls != 1 {
				t.Fatal(rr.Code, p.calls)
			}
		} else if rr.Code != 403 || p.calls != 0 {
			t.Fatal(rr.Code, p.calls)
		}
		p.calls = 0
		if rr = h.do("GET", proxyPath+"/applications", "", nil); rr.Code != 403 || p.calls != 0 {
			t.Fatal("CSRF or scope bypass", rr.Code, p.calls)
		}
	}
	h := harnessWith(t, nil, []string{auth.ScopeComposeRead})
	p := &countingDiscovery{failed: true}
	h.srv.WithComposeDiscovery(p)
	rr := h.do("GET", proxyPath+"/applications", "", packagesHeaders(h))
	if rr.Code != 503 || strings.Contains(rr.Body.String(), "SECRET") {
		t.Fatal(rr.Code, rr.Body)
	}
}

type brokerFixture struct{ calls atomic.Int32 }

func (p *brokerFixture) Snapshot(ctx context.Context) (composediscovery.Snapshot, error) {
	p.calls.Add(1)
	s, e := (composediscovery.Fixture{}).Snapshot(ctx)
	s.Source = "docker-rootless"
	s.Fixture = false
	for i := range s.Services {
		s.Services[i].ID = fmt.Sprintf("%064x", i+1)
	}
	return s, e
}
func TestCOREMetadataBrokerIntegrationAuthorizationAndFailure(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "metadata.sock")
	listener, e := dockerbroker.Listen(path)
	if e != nil {
		t.Fatal(e)
	}
	source := &brokerFixture{}
	token := "metadata-integration-fixture-token-0123456789"
	server, e := dockerbroker.NewServer(source, token, uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	go server.Serve(listener)
	defer server.Close()
	snapshot, _ := (composediscovery.Fixture{}).Snapshot(context.Background())
	client, e := dockerbroker.NewClient(path, token, uint32(os.Getuid()), snapshot.Traefik)
	if e != nil {
		t.Fatal(e)
	}
	denied := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead})
	denied.srv.WithComposeDiscovery(client).WithTraefik(&fakeTraefik{})
	if rr := denied.do("GET", proxyPath+"/applications", "", packagesHeaders(denied)); rr.Code != 403 || source.calls.Load() != 0 {
		t.Fatal("unauthorized broker access", rr.Code)
	}
	h := harnessWith(t, nil, []string{auth.ScopeComposeRead, auth.ScopeNetworkingProxyRead})
	h.srv.WithComposeDiscovery(client).WithTraefik(&fakeTraefik{})
	headers := packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	if rr := h.do("GET", proxyPath+"/applications", "", headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"source":"docker-rootless"`) || strings.Contains(rr.Body.String(), token) {
		t.Fatal(rr.Code, rr.Body)
	}
	body := strings.Replace(composeBody, "fixture-notes-web", fmt.Sprintf("%064x", 1), 1)
	if rr := h.do("POST", proxyPath+"/compose-preview", body, headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"hostname":"notes.example.com"`) {
		t.Fatal(rr.Code, rr.Body)
	}
	calls := source.calls.Load()
	if rr := h.do("GET", proxyPath+"/applications", "", nil); rr.Code != 403 || source.calls.Load() != calls {
		t.Fatal("CSRF broker access")
	}
	server.Close()
	if rr := h.do("GET", proxyPath+"/applications", "", headers); rr.Code != 503 {
		t.Fatal(rr.Code)
	}
}

func TestTraefikFileApplyWritesGeneratedYAML(t *testing.T) {
	dir := t.TempDir()
	read := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeComposeRead})
	read.srv.WithTraefik(&fakeTraefik{}).WithComposeDiscovery(composediscovery.Fixture{})
	headers := packagesHeaders(read)
	headers["Content-Type"] = "application/json"
	if rr := read.do("POST", proxyPath+"/file-apply", composeBody, headers); rr.Code != 403 {
		t.Fatal("read scopes must not apply", rr.Code, rr.Body)
	}
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeNetworkingProxyApply, auth.ScopeComposeRead})
	h.srv.WithTraefik(&fakeTraefik{}).WithComposeDiscovery(composediscovery.Fixture{})
	headers = packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	if rr := h.do("POST", proxyPath+"/compose-preview", composeBody, headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"applyAvailable":false`) {
		t.Fatal("apply stays off without a dynamic dir", rr.Code, rr.Body)
	}
	if rr := h.do("POST", proxyPath+"/file-apply", composeBody, headers); rr.Code != 503 {
		t.Fatal("missing dir", rr.Code, rr.Body)
	}
	h.srv.WithTraefikDynamicDir(dir)
	if rr := h.do("POST", proxyPath+"/compose-preview", composeBody, headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"applyAvailable":true`) {
		t.Fatal(rr.Code, rr.Body)
	}
	if rr := h.do("POST", proxyPath+"/file-apply", composeBody, headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"wrote":"notes.yml"`) {
		t.Fatal(rr.Code, rr.Body)
	}
	b, err := os.ReadFile(filepath.Join(dir, "notes.yml"))
	if err != nil || !strings.Contains(string(b), "Host(`notes.example.com`)") || !strings.Contains(string(b), "url: http://web:8080") {
		t.Fatal(string(b), err)
	}
	if rr := h.do("POST", proxyPath+"/apply", composeBody, headers); rr.Code != 404 {
		t.Fatal("generic apply stays missing", rr.Code)
	}
}

type dockerProviderOn struct{}

func (dockerProviderOn) Snapshot(ctx context.Context) (composediscovery.Snapshot, error) {
	s, err := (composediscovery.Fixture{}).Snapshot(ctx)
	on := true
	s.Traefik.DockerProvider = &on
	return s, err
}

func TestTraefikComposeApplyPatchesLabels(t *testing.T) {
	dir := t.TempDir()
	compose := "name: example-notes\nservices:\n  web:\n    image: x\n"
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0644); err != nil {
		t.Fatal(err)
	}
	read := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeComposeRead})
	read.srv.WithTraefik(&fakeTraefik{}).WithComposeDiscovery(dockerProviderOn{}).WithComposeApplyDir(dir)
	headers := packagesHeaders(read)
	headers["Content-Type"] = "application/json"
	if rr := read.do("POST", proxyPath+"/compose-apply", composeBody, headers); rr.Code != 403 {
		t.Fatal("read scopes must not apply labels", rr.Code, rr.Body)
	}
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeNetworkingProxyApply, auth.ScopeComposeRead})
	h.srv.WithTraefik(&fakeTraefik{}).WithComposeDiscovery(dockerProviderOn{})
	headers = packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	if rr := h.do("POST", proxyPath+"/compose-apply", composeBody, headers); rr.Code != 503 {
		t.Fatal("missing compose dir", rr.Code, rr.Body)
	}
	h.srv.WithComposeApplyDir(dir)
	if rr := h.do("POST", proxyPath+"/compose-preview", composeBody, headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"labelApplyAvailable":true`) {
		t.Fatal(rr.Code, rr.Body)
	}
	if rr := h.do("POST", proxyPath+"/compose-apply", composeBody, headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"wrote":"compose.yaml"`) {
		t.Fatal(rr.Code, rr.Body)
	}
	b, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if err != nil || !strings.Contains(string(b), "Host(`notes.example.com`)") || !strings.Contains(string(b), "image: x") {
		t.Fatal(string(b), err)
	}
	if rr := h.do("POST", proxyPath+"/apply", composeBody, headers); rr.Code != 404 {
		t.Fatal("generic apply stays missing", rr.Code)
	}
}

type traefikPresent struct{}

func (traefikPresent) Snapshot(ctx context.Context) (composediscovery.Snapshot, error) {
	s, err := (composediscovery.Fixture{}).Snapshot(ctx)
	s.Services = append(s.Services, composediscovery.Service{
		ID: "fixture-traefik", Compose: true, Project: "hc-traefik-live", Service: "traefik",
		Container: "hc-traefik-fixture", State: "running", Ports: []composediscovery.Port{},
		Networks: []string{"hc-traefik-test"}, Labels: map[string]string{},
	})
	return s, err
}

func TestTraefikSetupWritesComposeWhenMissing(t *testing.T) {
	dir := t.TempDir()
	read := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeComposeRead})
	read.srv.WithComposeDiscovery(composediscovery.Fixture{}).WithTraefikSetupDir(dir)
	headers := packagesHeaders(read)
	headers["Content-Type"] = "application/json"
	if rr := read.do("POST", proxyPath+"/setup", "{}", headers); rr.Code != 403 {
		t.Fatal("read scopes must not write setup", rr.Code, rr.Body)
	}
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeNetworkingProxyApply, auth.ScopeComposeRead})
	h.srv.WithComposeDiscovery(composediscovery.Fixture{})
	headers = packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	if rr := h.do("GET", proxyPath+"/applications", "", headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"setupAvailable":false`) || !strings.Contains(rr.Body.String(), `"present":false`) {
		t.Fatal("setup stays off without a directory", rr.Code, rr.Body)
	}
	if rr := h.do("POST", proxyPath+"/setup", "{}", headers); rr.Code != 503 {
		t.Fatal("missing setup dir", rr.Code, rr.Body)
	}
	h.srv.WithTraefikSetupDir(dir)
	if rr := h.do("GET", proxyPath+"/applications", "", headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"setupAvailable":true`) {
		t.Fatal(rr.Code, rr.Body)
	}
	if rr := h.do("POST", proxyPath+"/setup", `{"deploy":true}`, headers); rr.Code != 400 {
		t.Fatal("unknown fields", rr.Code, rr.Body)
	}
	if rr := h.do("POST", proxyPath+"/setup", "{}", headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"started":false`) || !strings.Contains(rr.Body.String(), `"httpPort":"8880"`) {
		t.Fatal(rr.Code, rr.Body)
	}
	b, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if err != nil || !strings.Contains(string(b), "8880:8080") || strings.Contains(string(b), "'80:80'") {
		t.Fatal(string(b), err)
	}
}

func TestTraefikSetupConflictsWhenPresent(t *testing.T) {
	dir := t.TempDir()
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeNetworkingProxyApply, auth.ScopeComposeRead})
	h.srv.WithComposeDiscovery(traefikPresent{}).WithTraefikSetupDir(dir)
	headers := packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	if rr := h.do("GET", proxyPath+"/applications", "", headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"present":true`) || !strings.Contains(rr.Body.String(), "hc-traefik-fixture") || strings.Contains(rr.Body.String(), `"setupAvailable":true`) {
		t.Fatal(rr.Code, rr.Body)
	}
	if rr := h.do("POST", proxyPath+"/setup", "{}", headers); rr.Code != 409 || !strings.Contains(rr.Body.String(), "traefik_present") {
		t.Fatal(rr.Code, rr.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "compose.yaml")); !os.IsNotExist(err) {
		t.Fatal("existing Traefik must not write a second compose", err)
	}
}

// arcaneProviderOn returns the fixture snapshot with the Docker provider
// enabled and an Arcane manager container present.
type arcaneProviderOn struct{}

func (arcaneProviderOn) Snapshot(ctx context.Context) (composediscovery.Snapshot, error) {
	s, err := (composediscovery.Fixture{}).Snapshot(ctx)
	if err != nil {
		return s, err
	}
	on := true
	s.Traefik.DockerProvider = &on
	s.Services = append(s.Services, composediscovery.Service{
		ID: "fixture-arcane", Compose: true, Project: "hc-arcane", Service: "arcane",
		Container: "arcane", State: "running", Image: "ghcr.io/getarcaneapp/arcane:latest",
		PublishedPorts: []int{3552}, Ports: []composediscovery.Port{},
		Networks: []string{"hc-traefik-test"}, Labels: map[string]string{},
	})
	return s, nil
}

// arcaneApplyServer serves the slice-1 Arcane endpoints for one project.
type arcaneApplyServer struct {
	compose string
	putBody map[string]string
}

func (a *arcaneApplyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/environments/0/projects":
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []map[string]string{{"id": "p1", "name": "example-notes"}}})
	case r.Method == http.MethodGet && r.URL.Path == "/api/environments/0/projects/p1/compose":
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "p1", "name": "example-notes", "composeFileName": "compose.yaml", "composeContent": a.compose}})
	case r.Method == http.MethodPut && r.URL.Path == "/api/environments/0/projects/p1":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		a.putBody = body
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "p1", "name": "example-notes", "composeFileName": "compose.yaml"}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestTraefikComposeApplyWritesThroughArcane(t *testing.T) {
	arcane := &arcaneApplyServer{compose: "name: example-notes\nservices:\n  web:\n    image: x\n"}
	srv := httptest.NewServer(arcane)
	defer srv.Close()
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeNetworkingProxyApply, auth.ScopeComposeRead})
	h.srv.WithTraefik(&fakeTraefik{}).WithComposeDiscovery(arcaneProviderOn{}).WithArcane("test-key", srv.URL, "0")
	headers := packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	rr := h.do("POST", proxyPath+"/compose-apply", composeBody, headers)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"via":"arcane"`) || !strings.Contains(rr.Body.String(), `"wrote":"compose.yaml"`) || !strings.Contains(rr.Body.String(), `"recreated":false`) {
		t.Fatal(rr.Code, rr.Body)
	}
	if arcane.putBody == nil || !strings.Contains(arcane.putBody["composeContent"], "traefik.enable: 'true'") {
		t.Fatalf("put body: %+v", arcane.putBody)
	}
}

func TestTraefikComposeApplyFallsBackToFileWhenArcaneHasNoProject(t *testing.T) {
	// Arcane exists but owns a different project; the local file apply must run.
	arcane := &arcaneApplyServer{compose: "name: unrelated\nservices:\n  api:\n    image: y\n"}
	srv := httptest.NewServer(arcane)
	defer srv.Close()
	dir := t.TempDir()
	compose := "name: example-notes\nservices:\n  web:\n    image: x\n"
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0644); err != nil {
		t.Fatal(err)
	}
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeNetworkingProxyApply, auth.ScopeComposeRead})
	h.srv.WithTraefik(&fakeTraefik{}).WithComposeDiscovery(arcaneProviderOn{}).WithArcane("test-key", srv.URL, "0").WithComposeApplyDir(dir)
	headers := packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	rr := h.do("POST", proxyPath+"/compose-apply", composeBody, headers)
	if rr.Code != 200 || strings.Contains(rr.Body.String(), `"via":"arcane"`) || !strings.Contains(rr.Body.String(), `"wrote":"compose.yaml"`) {
		t.Fatal(rr.Code, rr.Body)
	}
	if arcane.putBody != nil {
		t.Fatal("Arcane must not be written when it does not own the project")
	}
	b, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if err != nil || !strings.Contains(string(b), "traefik.enable") {
		t.Fatal("file fallback did not patch", string(b), err)
	}
}

func TestTraefikComposeApplyArcaneConflictIs409(t *testing.T) {
	arcane := &arcaneApplyServer{compose: "name: example-notes\nservices:\n  web:\n    image: x\n    labels:\n      traefik.enable: \"false\"\n"}
	srv := httptest.NewServer(arcane)
	defer srv.Close()
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeNetworkingProxyApply, auth.ScopeComposeRead})
	h.srv.WithTraefik(&fakeTraefik{}).WithComposeDiscovery(arcaneProviderOn{}).WithArcane("test-key", srv.URL, "0")
	headers := packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	rr := h.do("POST", proxyPath+"/compose-apply", composeBody, headers)
	if rr.Code != 409 || !strings.Contains(rr.Body.String(), "compose_apply_blocked") {
		t.Fatal(rr.Code, rr.Body)
	}
	if arcane.putBody != nil {
		t.Fatal("conflicting labels must not be written")
	}
}

func TestTraefikComposeApplyUsesConfiguredURLWithoutDetection(t *testing.T) {
	// -arcane-url is the source of truth: a configured URL enables the path even
	// when no Arcane container appears in the discovery snapshot.
	arcane := &arcaneApplyServer{compose: "name: example-notes\nservices:\n  web:\n    image: x\n"}
	srv := httptest.NewServer(arcane)
	defer srv.Close()
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeNetworkingProxyApply, auth.ScopeComposeRead})
	h.srv.WithTraefik(&fakeTraefik{}).WithComposeDiscovery(dockerProviderOn{}).WithArcane("test-key", srv.URL, "0")
	headers := packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	rr := h.do("POST", proxyPath+"/compose-apply", composeBody, headers)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"via":"arcane"`) {
		t.Fatal(rr.Code, rr.Body)
	}
	if arcane.putBody == nil {
		t.Fatal("configured URL must drive the Arcane path without docker detection")
	}
}

func TestTraefikComposeApplyDoesNotAutoEnableFromDetection(t *testing.T) {
	// An Arcane container is detected, but no ARCANE_API_KEY is configured:
	// detection must not enable the path; the file apply is the fallback.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("name: example-notes\nservices:\n  web:\n    image: x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeNetworkingProxyApply, auth.ScopeComposeRead})
	h.srv.WithTraefik(&fakeTraefik{}).WithComposeDiscovery(arcaneProviderOn{}).WithComposeApplyDir(dir)
	headers := packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	rr := h.do("POST", proxyPath+"/compose-apply", composeBody, headers)
	if rr.Code != 200 || strings.Contains(rr.Body.String(), `"via":"arcane"`) {
		t.Fatal(rr.Code, rr.Body)
	}
	b, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if err != nil || !strings.Contains(string(b), "traefik.enable") {
		t.Fatal("detection must not auto-enable Arcane; file apply expected", string(b), err)
	}
}
