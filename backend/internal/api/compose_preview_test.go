// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package api

import (
	"context"
	"errors"
	"fmt"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/dockerbroker"
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
