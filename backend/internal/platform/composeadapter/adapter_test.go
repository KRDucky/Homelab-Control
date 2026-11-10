// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composeadapter

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/platform"
	"homelab-control/backend/internal/platform/podman"
	"homelab-control/backend/internal/traefikclient"
)

func registry(t *testing.T, regs ...platform.Registration) *platform.Registry {
	t.Helper()
	r, err := platform.NewRegistry(regs...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func all(string) bool { return true }

func dockerFixture(t *testing.T) *Adapter {
	a, err := New(composediscovery.Fixture{}, Options{Kind: platform.KindDocker, Distribution: platform.DistDockerEngine, Mode: platform.ModeRootless, Auth: platform.AuthFixture, Transport: "fixture", EndpointLabel: "Demonstration Docker"})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

var model = traefikclient.Model{Name: "notes", Host: "notes.example.com", Port: 8080, Entrypoint: "web", Protocol: "http", UpstreamScheme: "http"}

func TestDockerInventoryKeepsNativeIdentityAndPlatform(t *testing.T) {
	r := registry(t, platform.Registration{ID: "docker-fixture", DisplayName: "Docker demo", ConnectionRef: "fixture:docker", Adapter: dockerFixture(t)})
	inv, view, err := r.Inventory(context.Background(), "docker-fixture", all)
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Fixture || view.Kind != platform.KindDocker || len(inv.Workloads) != 3 {
		t.Fatalf("%+v", inv)
	}
	for _, w := range inv.Workloads {
		if w.Ref.Platform != "docker-fixture" || w.Ref.Kind != "container" {
			t.Fatal(w.Ref)
		}
		if w.Ref.Name == "example-standalone" && !slices.Contains(w.Issues, "not_compose") {
			t.Fatal("standalone container not flagged")
		}
	}
	b, _ := json.Marshal(inv)
	if strings.Contains(string(b), "org.example.owner") {
		t.Fatal("non-routing label exposed")
	}
	if !inv.Capabilities.Has(platform.CapPreviewComposeLabel) || inv.Capabilities.Has(platform.CapPreviewIngress) {
		t.Fatal(inv.Capabilities)
	}
}

func TestDockerPreviewIsTheVerifiedComposeBuild(t *testing.T) {
	a := dockerFixture(t)
	r := registry(t, platform.Registration{ID: "docker-fixture", DisplayName: "Docker demo", ConnectionRef: "fixture:docker", Adapter: a})
	inv, _, _ := r.Inventory(context.Background(), "docker-fixture", all)
	var key string
	for _, w := range inv.Workloads {
		if w.Ref.UID == "fixture-notes-web" {
			key = w.Ref.Key()
		}
	}
	req := platform.PreviewRequest{Platform: "docker-fixture", Target: platform.CapPreviewComposeLabel, Workload: key, Network: "hc-traefik-test", Model: model}
	got, err := a.Preview(context.Background(), inv, req, traefikclient.Inventory{})
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := composediscovery.Fixture{}.Snapshot(context.Background())
	want, _ := composediscovery.Build(snap, traefikclient.Inventory{}, composediscovery.Request{ServiceID: "fixture-notes-web", Network: "hc-traefik-test", Model: model})
	if !reflect.DeepEqual(got.Compose, want) || got.ApplyAvailable || got.Deployable || got.Format != "compose-labels" {
		t.Fatal("platform preview diverged from the Phase 6/7 Compose preview")
	}
	if !slices.ContainsFunc(got.Blockers, func(i platform.Issue) bool { return i.Code == "provider_disabled" }) {
		t.Fatal(got.Blockers)
	}
	for _, bad := range []platform.PreviewRequest{
		{Target: platform.CapPreviewIngress, Workload: key, Network: "hc-traefik-test", Model: model},
		{Target: platform.CapPreviewComposeLabel, Workload: "docker-fixture/container/-/nope/nope", Network: "hc-traefik-test", Model: model},
		{Target: platform.CapPreviewComposeLabel, Workload: key, Service: "x", Network: "hc-traefik-test", Model: model},
	} {
		if _, err := a.Preview(context.Background(), inv, bad, traefikclient.Inventory{}); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestPodmanFixtureThroughSharedAdapter(t *testing.T) {
	a, err := New(podman.Fixture{}, Options{Kind: platform.KindPodman, Distribution: platform.DistPodman, Mode: platform.ModeRootless, Auth: platform.AuthFixture, Transport: "fixture", EndpointLabel: "Demonstration Podman", Warnings: podman.Warnings(platform.ModeRootless)})
	if err != nil {
		t.Fatal(err)
	}
	r := registry(t, platform.Registration{ID: "podman-fixture", DisplayName: "Podman demo", ConnectionRef: "fixture:podman", Adapter: a})
	inv, view, err := r.Inventory(context.Background(), "podman-fixture", func(s string) bool { return s == "podman.read" })
	if err != nil || view.Kind != platform.KindPodman {
		t.Fatal(err, view)
	}
	if _, _, err := r.Inventory(context.Background(), "podman-fixture", func(s string) bool { return s == "docker.compose.read" }); err == nil {
		t.Fatal("Docker scope granted Podman access")
	}
	if len(inv.Workloads) != 3 {
		t.Fatalf("infra container must be excluded: %d", len(inv.Workloads))
	}
	b, _ := json.Marshal(inv)
	if strings.Contains(string(b), "must-not-leak") || strings.Contains(string(b), "secret-hash") || strings.Contains(string(b), "18000") {
		t.Fatal("sensitive label or published host port exposed")
	}
	var wiki platform.Workload
	for _, w := range inv.Workloads {
		switch w.Ref.Name {
		case "wiki_app_1":
			wiki = w
		case "photos-web":
			if !w.HiddenRouting || !slices.Contains(w.Issues, "unsupported_networking") {
				t.Fatal("pod member must be unsupported with hidden routing settings", w)
			}
		case "music_api_1":
			if len(w.Ports) != 2 || w.Ports[1].Port != 5001 || w.State != "exited" {
				t.Fatal("libpod port range or state mapping", w)
			}
		}
	}
	m := model
	m.Name, m.Host, m.Port = "wiki", "wiki.example.com", 8000
	p, err := a.Preview(context.Background(), inv, platform.PreviewRequest{Target: platform.CapPreviewComposeLabel, Workload: wiki.Ref.Key(), Network: "hc-traefik-test", Model: m}, traefikclient.Inventory{})
	if err != nil {
		t.Fatal(err)
	}
	codes := []string{}
	for _, w := range p.Warnings {
		codes = append(codes, w.Code)
		if strings.Contains(w.Message, "Docker applications") {
			t.Fatal("Podman preview described as Docker")
		}
	}
	if !slices.Contains(codes, "podman_provider") || !slices.Contains(codes, "podman_rootless_network") {
		t.Fatal(codes)
	}
}

func TestAdapterOptionsValidated(t *testing.T) {
	for _, o := range []Options{{Kind: platform.KindKubernetes, Mode: platform.ModeRootless, Transport: "x"}, {Kind: platform.KindDocker, Mode: platform.ModeCluster, Transport: "x"}, {Kind: platform.KindDocker, Mode: platform.ModeRootless}} {
		if _, err := New(composediscovery.Fixture{}, o); err == nil {
			t.Errorf("accepted %+v", o)
		}
	}
	if _, err := New(nil, Options{Kind: platform.KindDocker, Mode: platform.ModeRootless, Transport: "x"}); err == nil {
		t.Error("nil provider accepted")
	}
}
