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

// Compatibility matrix for the universal platform registry: each platform is
// optional, rootful daemons are explicit, and references never cross platforms.

func composeRegistration(t *testing.T, id string, kind platform.Kind, mode platform.Mode, p composediscovery.Provider, ack bool) platform.Registration {
	t.Helper()
	dist := platform.DistDockerEngine
	var warnings []platform.Issue
	if kind == platform.KindPodman {
		dist, warnings = platform.DistPodman, podman.Warnings(mode)
	}
	a, err := composeadapter.New(p, composeadapter.Options{Kind: kind, Distribution: dist, Mode: mode, Auth: platform.AuthFixture, Transport: "fixture", EndpointLabel: "Demonstration " + string(kind), Warnings: warnings})
	if err != nil {
		t.Fatal(err)
	}
	return platform.Registration{ID: id, DisplayName: id, ConnectionRef: "fixture:" + id, Adapter: a, AcknowledgeRootEquivalent: ack}
}

func k3sRegistration(t *testing.T) platform.Registration {
	t.Helper()
	k3s, err := kubernetes.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	return platform.Registration{ID: "k3s-fixture", DisplayName: "K3s (demonstration)", ConnectionRef: "fixture:k3s", Adapter: k3s}
}

type compatList struct {
	Platforms []struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		Mode string `json:"mode"`
	} `json:"platforms"`
	AutoSelected *string `json:"autoSelected"`
}

func listPlatforms(t *testing.T, h *harness) compatList {
	t.Helper()
	rr := h.do("GET", "/api/v1/platforms", "", packagesHeaders(h))
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body)
	}
	var out compatList
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRootfulDockerAndPodmanRequireAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		kind  platform.Kind
		scope string
		p     composediscovery.Provider
	}{{platform.KindDocker, auth.ScopeComposeRead, composediscovery.Fixture{}}, {platform.KindPodman, auth.ScopePodmanRead, podman.Fixture{}}} {
		id := string(tc.kind) + "-root"
		if _, err := platform.NewRegistry(composeRegistration(t, id, tc.kind, platform.ModeRootful, tc.p, false)); err == nil {
			t.Fatal(tc.kind, "rootful registration accepted without root-equivalence acknowledgement")
		}
		r, err := platform.NewRegistry(composeRegistration(t, id, tc.kind, platform.ModeRootful, tc.p, true))
		if err != nil {
			t.Fatal(tc.kind, err)
		}
		h := harnessWith(t, nil, []string{auth.ScopePlatformsRead, tc.scope})
		h.srv.WithPlatforms(r)
		got := listPlatforms(t, h)
		if len(got.Platforms) != 1 || got.Platforms[0].Mode != "rootful" || got.Platforms[0].Kind != string(tc.kind) {
			t.Fatal(tc.kind, got)
		}
		if rr := h.do("GET", "/api/v1/platforms/"+id+"/inventory", "", packagesHeaders(h)); rr.Code != 200 {
			t.Fatal(tc.kind, rr.Code, rr.Body)
		}
	}
	// Rootful Podman keeps the provider warning but not the per-user network one.
	codes := map[string]bool{}
	for _, w := range podman.Warnings(platform.ModeRootful) {
		codes[w.Code] = true
	}
	if !codes["podman_provider"] || codes["podman_rootless_network"] {
		t.Fatal(codes)
	}
}

func TestSinglePlatformInstallationsNeedNoOtherPlatform(t *testing.T) {
	for _, tc := range []struct {
		reg   func() platform.Registration
		scope string
	}{
		{func() platform.Registration {
			return composeRegistration(t, "docker-local", platform.KindDocker, platform.ModeRootless, composediscovery.Fixture{}, false)
		}, auth.ScopeComposeRead},
		{func() platform.Registration {
			return composeRegistration(t, "podman-local", platform.KindPodman, platform.ModeRootless, podman.Fixture{}, false)
		}, auth.ScopePodmanRead},
		{func() platform.Registration { return k3sRegistration(t) }, auth.ScopeKubernetesRead},
	} {
		reg := tc.reg()
		r, err := platform.NewRegistry(reg)
		if err != nil {
			t.Fatal(err)
		}
		// Only this platform's family scope: no Docker, Podman or Kubernetes
		// permission for an absent platform is needed.
		h := harnessWith(t, nil, []string{auth.ScopePlatformsRead, tc.scope})
		h.srv.WithPlatforms(r)
		got := listPlatforms(t, h)
		if len(got.Platforms) != 1 || got.Platforms[0].ID != reg.ID || got.AutoSelected == nil || *got.AutoSelected != reg.ID {
			t.Fatal(reg.ID, got)
		}
		if rr := h.do("GET", "/api/v1/platforms/"+reg.ID+"/inventory", "", packagesHeaders(h)); rr.Code != 200 {
			t.Fatal(reg.ID, rr.Code, rr.Body)
		}
	}
	// A Kubernetes-only installation has no Compose discovery at all; the
	// legacy Docker endpoint reports unavailable instead of failing startup.
	r, err := platform.NewRegistry(k3sRegistration(t))
	if err != nil {
		t.Fatal(err)
	}
	h := harnessWith(t, nil, []string{auth.ScopePlatformsRead, auth.ScopeKubernetesRead, auth.ScopeComposeRead})
	h.srv.WithPlatforms(r)
	if rr := h.do("GET", "/api/v1/platforms/docker-local/inventory", "", packagesHeaders(h)); rr.Code != 404 {
		t.Fatal("absent Docker platform must be unknown", rr.Code)
	}
}

func TestReferencesNeverCrossPlatforms(t *testing.T) {
	all := []string{auth.ScopeNetworkingProxyRead, auth.ScopePlatformsRead, auth.ScopeComposeRead, auth.ScopePodmanRead, auth.ScopeKubernetesRead}
	h := harnessWith(t, nil, all)
	f := &fakeTraefik{}
	h.srv.WithPlatforms(testRegistry(t)).WithTraefik(f)
	dw, _ := inventoryKeys(t, h, "docker-fixture")
	pw, _ := inventoryKeys(t, h, "podman-fixture")
	kw, ks := inventoryKeys(t, h, "k3s-fixture")
	model := `"model":{"name":"notes","host":"notes.example.test","port":80,"entrypoint":"web","protocol":"http","upstreamScheme":"http"}`
	for name, body := range map[string]string{
		"docker ref on k3s":    `{"platform":"k3s-fixture","target":"routing.preview.kubernetes-ingress","workload":"` + dw["example-notes-web-1"] + `","service":"` + ks["notes"] + `",` + model + `}`,
		"k3s ref on podman":    `{"platform":"podman-fixture","target":"routing.preview.compose-labels","workload":"` + kw["notes"] + `","network":"hc-traefik-test",` + model + `}`,
		"podman ref on docker": `{"platform":"docker-fixture","target":"routing.preview.compose-labels","workload":"` + pw["wiki_app_1"] + `","network":"hc-traefik-test",` + model + `}`,
		"docker ref on podman": `{"platform":"podman-fixture","target":"routing.preview.compose-labels","workload":"` + dw["example-notes-web-1"] + `","network":"hc-traefik-test",` + model + `}`,
	} {
		rr := h.do("POST", proxyPath+"/platform-preview", body, platformPreviewHeaders(h))
		if rr.Code != 400 {
			t.Fatal(name, rr.Code, rr.Body)
		}
		if strings.Contains(rr.Body.String(), "labels") && strings.Contains(rr.Body.String(), "traefik.http") {
			t.Fatal(name, "preview produced despite foreign reference")
		}
	}
}

func TestPlatformRoutesAreReadOnly(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopePlatformsRead, auth.ScopeComposeRead})
	h.srv.WithPlatforms(testRegistry(t))
	headers := platformPreviewHeaders(h)
	for _, method := range []string{"PUT", "PATCH", "DELETE"} {
		for _, path := range []string{"/api/v1/platforms", "/api/v1/platforms/docker-fixture", "/api/v1/platforms/docker-fixture/inventory", proxyPath + "/platform-preview"} {
			if rr := h.do(method, path, `{}`, headers); rr.Code != 404 && rr.Code != 405 {
				t.Fatal(method, path, rr.Code)
			}
		}
	}
	for _, path := range []string{proxyPath + "/platform-apply", proxyPath + "/platform-deploy", "/api/v1/platforms/docker-fixture/apply"} {
		if rr := h.do("POST", path, `{}`, headers); rr.Code != 404 && rr.Code != 405 {
			t.Fatal(path, rr.Code)
		}
	}
}
