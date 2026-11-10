// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package api

import (
	"context"
	"encoding/json"
	"errors"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/modules"
	"homelab-control/backend/internal/traefikclient"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const proxyPath = "/api/v1/networking/reverse-proxy/traefik"
const previewBody = `{"name":"demo","host":"demo.localhost","port":8080,"entrypoint":"web","protocol":"http","upstreamScheme":"http"}`

type fakeTraefik struct {
	calls int
	err   error
}

func (f *fakeTraefik) Inventory(context.Context) (traefikclient.Inventory, error) {
	f.calls++
	return traefikclient.Inventory{SchemaVersion: 1, Routers: []traefikclient.Resource{{Name: "demo", Provider: "file", Status: "enabled"}}, Services: []traefikclient.Resource{}, Middlewares: []traefikclient.Resource{}}, f.err
}
func (f *fakeTraefik) Diagnostics(context.Context) (traefikclient.Diagnostics, error) {
	f.calls++
	return traefikclient.Diagnostics{SchemaVersion: 1, Status: "available", Checks: []traefikclient.Check{}}, f.err
}
func (f *fakeTraefik) Labels(_ context.Context, m traefikclient.Model) (traefikclient.Preview, error) {
	f.calls++
	p, e := m.Labels()
	if f.err != nil {
		e = f.err
	}
	return p, e
}
func TestTraefikAuthorizationAndModule(t *testing.T) {
	for _, route := range []string{"inventory", "diagnostics", "labels"} {
		method, body := "GET", ""
		if route == "labels" {
			method, body = "POST", previewBody
		}
		h := newHarness(t)
		headers := map[string]string{"Origin": testOrigin, "Content-Type": "application/json"}
		if rr := h.do(method, proxyPath+"/"+route, body, headers); rr.Code != 401 {
			t.Fatal(rr.Code, rr.Body)
		}
		h.login("reader", "reader-password-123")
		headers["X-CSRF-Token"] = h.csrf
		if rr := h.do(method, proxyPath+"/"+route, body, headers); rr.Code != 403 {
			t.Fatal(rr.Code, rr.Body)
		}
	}
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead})
	f := &fakeTraefik{}
	h.srv.WithTraefik(f)
	if rr := h.do("GET", proxyPath+"/inventory", "", nil); rr.Code != 403 {
		t.Fatal(rr.Code)
	}
	for _, origin := range []string{"http://evil.test", "null"} {
		if rr := h.do("GET", proxyPath+"/inventory", "", map[string]string{"Origin": origin, "X-CSRF-Token": h.csrf}); rr.Code != 403 {
			t.Fatal(rr.Code)
		}
	}
	if rr := h.do("POST", proxyPath+"/labels", previewBody, map[string]string{"X-CSRF-Token": h.csrf, "Content-Type": "application/json"}); rr.Code != 403 {
		t.Fatal(rr.Code)
	}
	mods, _ := modules.Parse([]byte(`{"version":1,"disabled":["networking"]}`))
	h.srv.WithModules(mods)
	if rr := h.do("GET", proxyPath+"/inventory", "", packagesHeaders(h)); rr.Code != 404 {
		t.Fatal(rr.Code)
	}
	if f.calls != 0 {
		t.Fatal("unauthorized helper access")
	}
}
func TestTraefikReadsPreviewValidationAndAudit(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead})
	f := &fakeTraefik{}
	h.srv.WithTraefik(f)
	headers := packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	for _, route := range []string{"inventory", "diagnostics"} {
		if rr := h.do("GET", proxyPath+"/"+route, "", headers); rr.Code != 200 {
			t.Fatal(rr.Code, rr.Body)
		}
	}
	if rr := h.do("POST", proxyPath+"/labels", previewBody, headers); rr.Code != 200 || !strings.Contains(rr.Body.String(), "traefik.http.routers.demo") {
		t.Fatal(rr.Code, rr.Body)
	}
	for _, body := range []string{`{}`, strings.Replace(previewBody, `"port":8080`, `"port":0`, 1), strings.Replace(previewBody, `"name":"demo"`, `"name":"demo","command":"apply"`, 1), previewBody + `{}`, strings.Repeat("x", 4097)} {
		if rr := h.do("POST", proxyPath+"/labels", body, headers); rr.Code != 400 {
			t.Fatal(rr.Code, rr.Body)
		}
	}
	if f.calls != 3 {
		t.Fatal("invalid request reached helper", f.calls)
	}
	if strings.Contains(h.audit.String(), "demo.localhost") || !strings.Contains(h.audit.String(), "networking.traefik.labels.preview") {
		t.Fatal("audit leak or missing event")
	}
	f.err = errors.New("secret credential")
	if rr := h.do("GET", proxyPath+"/inventory", "", headers); rr.Code != 502 || strings.Contains(rr.Body.String(), "secret") {
		t.Fatal(rr.Code, rr.Body)
	}
	h.srv.audit = nil
	if rr := h.do("GET", proxyPath+"/inventory", "", headers); rr.Code != 503 {
		t.Fatal(rr.Code)
	}
}
func TestTraefikCapabilitiesAndRevocation(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead})
	get := func() Capability {
		rr := h.do("GET", "/api/v1/capabilities", "", nil)
		var res capabilitiesResponse
		json.Unmarshal(rr.Body.Bytes(), &res)
		for _, c := range res.Capabilities {
			if c.ID == "traefik" {
				return c
			}
		}
		return Capability{}
	}
	if c := get(); c.Status != CapUnavailable {
		t.Fatal(c)
	}
	h.srv.WithTraefik(&fakeTraefik{})
	if c := get(); c.Status != CapAvailable {
		t.Fatal(c)
	}
	h.users.Users[len(h.users.Users)-1].Scopes = nil
	h.srv.users = auth.NewStore(h.users)
	if rr := h.do("GET", proxyPath+"/inventory", "", packagesHeaders(h)); rr.Code != http.StatusForbidden {
		t.Fatal("revocation ignored", rr.Code)
	}
	if strings.Contains(auth.DefaultScopes, auth.ScopeNetworkingProxyRead) {
		t.Fatal("scope granted by default")
	}
	mods, _ := modules.Parse([]byte(`{"version":1,"disabled":["networking"]}`))
	h.srv.WithModules(mods)
	if c := get(); c.ID != "" {
		t.Fatal("disabled capability visible", c)
	}
}

func TestTraefikCoreToHTTPHelper(t *testing.T) {
	calls := 0
	helper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture-helper-token-0123456789abcdef" {
			t.Error("helper credential missing")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/inventory" || r.Method != "GET" {
			t.Error("unexpected proxy destination")
		}
		w.Write([]byte(`{"schemaVersion":1,"routers":[],"services":[],"middlewares":[],"credential":"private"}`))
	}))
	defer helper.Close()
	client, err := traefikclient.New(helper.URL, "fixture-helper-token-0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead})
	h.srv.WithTraefik(client)
	if rr := h.do("GET", proxyPath+"/inventory", "", packagesHeaders(h)); rr.Code != 200 || strings.Contains(rr.Body.String(), "credential") || strings.Contains(rr.Body.String(), "private") {
		t.Fatal(rr.Code, rr.Body)
	}
	if rr := h.do("HEAD", proxyPath+"/inventory", "", nil); rr.Code != 403 {
		t.Fatal("HEAD bypass", rr.Code)
	}
	if rr := h.do("GET", proxyPath+"/inventory?url=http://evil.test", "", packagesHeaders(h)); rr.Code != 400 {
		t.Fatal("destination input accepted", rr.Code)
	}
	if calls != 1 {
		t.Fatal("rejected requests reached helper", calls)
	}
}
