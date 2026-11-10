// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package api

import (
	"encoding/json"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/platform"
	"homelab-control/backend/internal/platform/composeadapter"
	"homelab-control/backend/internal/platform/kubernetes"
	"homelab-control/backend/internal/platform/podman"
)

func testRegistry(t *testing.T) *platform.Registry {
	t.Helper()
	docker, err := composeadapter.New(composediscovery.Fixture{}, composeadapter.Options{Kind: platform.KindDocker, Distribution: platform.DistDockerEngine, Mode: platform.ModeRootless, Auth: platform.AuthFixture, Transport: "fixture", EndpointLabel: "Demonstration Docker"})
	if err != nil {
		t.Fatal(err)
	}
	pod, err := composeadapter.New(podman.Fixture{}, composeadapter.Options{Kind: platform.KindPodman, Distribution: platform.DistPodman, Mode: platform.ModeRootless, Auth: platform.AuthFixture, Transport: "fixture", EndpointLabel: "Demonstration Podman", Warnings: podman.Warnings(platform.ModeRootless)})
	if err != nil {
		t.Fatal(err)
	}
	k3s, err := kubernetes.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	r, err := platform.NewRegistry(
		platform.Registration{ID: "docker-fixture", DisplayName: "Docker (demonstration)", ConnectionRef: "fixture:docker", Adapter: docker},
		platform.Registration{ID: "podman-fixture", DisplayName: "Podman (demonstration)", ConnectionRef: "fixture:podman", Adapter: pod},
		platform.Registration{ID: "k3s-fixture", DisplayName: "K3s (demonstration)", ConnectionRef: "fixture:k3s", Adapter: k3s})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type platformList struct {
	Platforms []struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	} `json:"platforms"`
	AutoSelected *string `json:"autoSelected"`
}

func TestPlatformListIsScopedPerFamily(t *testing.T) {
	cases := []struct {
		scopes []string
		code   int
		ids    string
		auto   string
	}{
		{nil, 403, "", ""},
		{[]string{auth.ScopeComposeRead, auth.ScopeKubernetesRead}, 403, "", ""},
		{[]string{auth.ScopePlatformsRead}, 200, "", ""},
		{[]string{auth.ScopePlatformsRead, auth.ScopeComposeRead}, 200, "docker-fixture", "docker-fixture"},
		{[]string{auth.ScopePlatformsRead, auth.ScopeKubernetesRead}, 200, "k3s-fixture", "k3s-fixture"},
		{[]string{auth.ScopePlatformsRead, auth.ScopeComposeRead, auth.ScopePodmanRead, auth.ScopeKubernetesRead}, 200, "docker-fixture,podman-fixture,k3s-fixture", ""},
	}
	for _, c := range cases {
		h := harnessWith(t, nil, c.scopes)
		h.srv.WithPlatforms(testRegistry(t))
		rr := h.do("GET", "/api/v1/platforms", "", packagesHeaders(h))
		if rr.Code != c.code {
			t.Fatal(c.scopes, rr.Code, rr.Body)
		}
		if c.code != 200 {
			continue
		}
		var out platformList
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, p := range out.Platforms {
			ids = append(ids, p.ID)
		}
		if strings.Join(ids, ",") != c.ids {
			t.Fatal(c.scopes, ids)
		}
		auto := ""
		if out.AutoSelected != nil {
			auto = *out.AutoSelected
		}
		if auto != c.auto {
			t.Fatal(c.scopes, auto)
		}
		if strings.Contains(rr.Body.String(), "fixture:docker") || strings.Contains(rr.Body.String(), "connectionRef") {
			t.Fatal("connection reference exposed")
		}
	}
}

func TestPlatformEndpointsFailClosed(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopePlatformsRead, auth.ScopeComposeRead})
	if rr := h.do("GET", "/api/v1/platforms", "", packagesHeaders(h)); rr.Code != 503 {
		t.Fatal("no registry must be unavailable", rr.Code)
	}
	h.srv.WithPlatforms(testRegistry(t))
	if rr := h.do("GET", "/api/v1/platforms", "", nil); rr.Code != 403 {
		t.Fatal("GET without CSRF token", rr.Code)
	}
	if rr := h.do("GET", "/api/v1/platforms/docker-fixture/inventory", "", nil); rr.Code != 403 {
		t.Fatal("inventory without CSRF token", rr.Code)
	}
	// Unknown and unauthorized platforms are indistinguishable.
	for _, id := range []string{"k3s-fixture", "nope", "UPPER"} {
		if rr := h.do("GET", "/api/v1/platforms/"+id+"/inventory", "", packagesHeaders(h)); rr.Code != 404 {
			t.Fatal(id, rr.Code)
		}
	}
	rr := h.do("GET", "/api/v1/platforms/docker-fixture/inventory", "", packagesHeaders(h))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"platform":"docker-fixture"`) || !strings.Contains(rr.Body.String(), `"capability":"routing.apply"`) {
		t.Fatal(rr.Code, rr.Body)
	}
	if !strings.Contains(h.audit.String(), "platform.docker.inventory.read") {
		t.Fatal("inventory read not audited")
	}
	if rr := h.do("GET", "/api/v1/platforms/docker-fixture/inventory?socket=/var/run/docker.sock", "", packagesHeaders(h)); rr.Code != 400 {
		t.Fatal("query parameters must be rejected", rr.Code)
	}
	for _, path := range []string{"/api/v1/platforms", "/api/v1/platforms/docker-fixture"} {
		if rr := h.do("POST", path, `{"socket":"/var/run/docker.sock"}`, map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "application/json"}); rr.Code != 404 && rr.Code != 405 {
			t.Fatal("registration API must not exist", path, rr.Code)
		}
	}
}

func platformPreviewHeaders(h *harness) map[string]string {
	headers := packagesHeaders(h)
	headers["Content-Type"] = "application/json"
	return headers
}

func inventoryKeys(t *testing.T, h *harness, id string) (map[string]string, map[string]string) {
	t.Helper()
	rr := h.do("GET", "/api/v1/platforms/"+id+"/inventory", "", packagesHeaders(h))
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body)
	}
	var out struct {
		Inventory platform.Inventory `json:"inventory"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	w, s := map[string]string{}, map[string]string{}
	for _, x := range out.Inventory.Workloads {
		w[x.Ref.Name] = x.Ref.Key()
	}
	for _, x := range out.Inventory.Services {
		s[x.Ref.Name] = x.Ref.Key()
	}
	return w, s
}

func TestPlatformPreviewPerFamily(t *testing.T) {
	all := []string{auth.ScopeNetworkingProxyRead, auth.ScopePlatformsRead, auth.ScopeComposeRead, auth.ScopePodmanRead, auth.ScopeKubernetesRead}
	h := harnessWith(t, nil, all)
	f := &fakeTraefik{}
	h.srv.WithPlatforms(testRegistry(t)).WithTraefik(f)

	dw, _ := inventoryKeys(t, h, "docker-fixture")
	body := `{"platform":"docker-fixture","target":"routing.preview.compose-labels","workload":"` + dw["example-notes-web-1"] + `","network":"hc-traefik-test","model":{"name":"notes","host":"notes.example.com","port":8080,"entrypoint":"web","protocol":"http","upstreamScheme":"http"}}`
	rr := h.do("POST", proxyPath+"/platform-preview", body, platformPreviewHeaders(h))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"format":"compose-labels"`) || !strings.Contains(rr.Body.String(), `"applyAvailable":false`) || !strings.Contains(rr.Body.String(), `"deployable":false`) {
		t.Fatal(rr.Code, rr.Body)
	}
	if !strings.Contains(h.audit.String(), "platform.docker.routing.preview") {
		t.Fatal("preview not audited")
	}

	kw, ks := inventoryKeys(t, h, "k3s-fixture")
	kbody := `{"platform":"k3s-fixture","target":"routing.preview.kubernetes-ingress","workload":"` + kw["notes"] + `","service":"` + ks["notes"] + `","model":{"name":"notes","host":"notes.example.test","port":80,"entrypoint":"web","protocol":"http","upstreamScheme":"http"}}`
	rr = h.do("POST", proxyPath+"/platform-preview", kbody, platformPreviewHeaders(h))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"kind":"Ingress"`) || strings.Contains(rr.Body.String(), "traefik.docker.network") || strings.Contains(rr.Body.String(), `"compose"`) {
		t.Fatal(rr.Code, rr.Body)
	}
	// Compose labels are refused for a Kubernetes platform.
	bad := strings.Replace(kbody, "routing.preview.kubernetes-ingress", "routing.preview.compose-labels", 1)
	if rr = h.do("POST", proxyPath+"/platform-preview", bad, platformPreviewHeaders(h)); rr.Code != 400 {
		t.Fatal(rr.Code)
	}
	// Strict request decoding: unknown fields (endpoint injection) are rejected.
	inject := strings.Replace(kbody, `"platform":`, `"endpoint":"https://evil.example","platform":`, 1)
	if rr = h.do("POST", proxyPath+"/platform-preview", inject, platformPreviewHeaders(h)); rr.Code != 400 {
		t.Fatal(rr.Code)
	}
	if rr = h.do("POST", proxyPath+"/apply", kbody, platformPreviewHeaders(h)); rr.Code != 404 {
		t.Fatal("unexpected apply endpoint", rr.Code)
	}
}

func TestPlatformPreviewRequiresEveryScope(t *testing.T) {
	full := []string{auth.ScopeNetworkingProxyRead, auth.ScopePlatformsRead, auth.ScopeKubernetesRead}
	for drop := range full {
		scopes := append(append([]string{}, full[:drop]...), full[drop+1:]...)
		h := harnessWith(t, nil, scopes)
		f := &fakeTraefik{}
		h.srv.WithPlatforms(testRegistry(t)).WithTraefik(f)
		body := `{"platform":"k3s-fixture","target":"routing.preview.kubernetes-ingress","workload":"x","service":"y","model":{"name":"notes","host":"notes.example.test","port":80,"entrypoint":"web","protocol":"http","upstreamScheme":"http"}}`
		rr := h.do("POST", proxyPath+"/platform-preview", body, platformPreviewHeaders(h))
		if rr.Code != 403 && rr.Code != 404 {
			t.Fatal(scopes, rr.Code)
		}
		if f.calls != 0 {
			t.Fatal("Traefik helper reached without full authorization", scopes)
		}
	}
}
