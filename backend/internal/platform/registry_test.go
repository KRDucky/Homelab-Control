// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fake struct {
	d   Descriptor
	inv Inventory
	err error
	n   int
}

func (f *fake) Descriptor() Descriptor { return f.d }
func (f *fake) Declared() []Capability {
	return []Capability{CapWorkloads, CapPorts, CapPreviewComposeLabel}
}
func (f *fake) Inventory(_ context.Context, id string) (Inventory, error) {
	f.n++
	inv := f.inv
	if inv.Platform == "" {
		inv.Platform = id
	}
	return inv, f.err
}

func dockerFake() *fake {
	return &fake{d: Descriptor{Kind: KindDocker, Distribution: DistDockerEngine, Mode: ModeRootless, Auth: AuthBrokerPeerToken, Endpoint: Endpoint{Transport: "unix-metadata-broker", Label: "Local rootless broker"}}}
}

func all(string) bool  { return true }
func none(string) bool { return false }

func TestRegistryValidatesAdministratorRegistrations(t *testing.T) {
	ok := Registration{ID: "docker-local", DisplayName: "Docker (rootless)", ConnectionRef: "broker:/private/socket", Adapter: dockerFake()}
	if _, err := NewRegistry(ok); err != nil {
		t.Fatal(err)
	}
	rootful := dockerFake()
	rootful.d.Mode = ModeRootful
	k8sNotCluster := dockerFake()
	k8sNotCluster.d.Kind = KindKubernetes
	fixtureMismatch := dockerFake()
	fixtureMismatch.d.Fixture = true
	unknownKind := dockerFake()
	unknownKind.d.Kind = "lxc"
	for name, regs := range map[string][]Registration{
		"bad id":                            {{ID: "Docker", DisplayName: "x", ConnectionRef: "r", Adapter: dockerFake()}},
		"duplicate id":                      {ok, ok},
		"duplicate endpoint":                {ok, {ID: "docker-two", DisplayName: "Docker two", ConnectionRef: ok.ConnectionRef, Adapter: dockerFake()}},
		"no connection ref":                 {{ID: "docker-x", DisplayName: "x", Adapter: dockerFake()}},
		"nil adapter":                       {{ID: "docker-x", DisplayName: "x", ConnectionRef: "r"}},
		"rootful unacknowledged":            {{ID: "docker-root", DisplayName: "Docker root", ConnectionRef: "r", Adapter: rootful}},
		"kubernetes without cluster mode":   {{ID: "k", DisplayName: "k", ConnectionRef: "r", Adapter: k8sNotCluster}},
		"fixture flag without fixture auth": {{ID: "dfx", DisplayName: "d", ConnectionRef: "r", Adapter: fixtureMismatch}},
		"unknown kind":                      {{ID: "lxc", DisplayName: "l", ConnectionRef: "r", Adapter: unknownKind}},
	} {
		if _, err := NewRegistry(regs...); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := NewRegistry(Registration{ID: "docker-root", DisplayName: "Docker root", ConnectionRef: "r", Adapter: rootful, AcknowledgeRootEquivalent: true}); err != nil {
		t.Fatal("acknowledged rootful registration rejected:", err)
	}
}

func TestVisibilityIsPlatformScopedAndHidesConnectionDetails(t *testing.T) {
	r, err := NewRegistry(Registration{ID: "docker-local", DisplayName: "Docker", ConnectionRef: "/run/user/1000/secret-socket-path", Adapter: dockerFake()})
	if err != nil {
		t.Fatal(err)
	}
	if v := r.Visible(none); len(v) != 0 {
		t.Fatal("platform visible without its scope")
	}
	if v := r.Visible(func(s string) bool { return s == "kubernetes.read" }); len(v) != 0 {
		t.Fatal("kubernetes scope revealed a Docker platform")
	}
	v := r.Visible(func(s string) bool { return s == "docker.compose.read" })
	if len(v) != 1 || v[0].Policy.ReadScope != "docker.compose.read" || v[0].Policy.Apply != "unavailable" || !strings.HasPrefix(v[0].Endpoint.Fingerprint, "sha256:") {
		t.Fatalf("%+v", v)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "secret-socket-path") || strings.Contains(string(b), "/run/user") {
		t.Fatal("connection reference leaked:", string(b))
	}
	if _, err := r.View("docker-local", none); !errors.Is(err, ErrNotFound) {
		t.Fatal("unauthorized lookup must look like a missing platform")
	}
	if _, err := r.View("nope", all); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestInventoryValidationFailsClosedAndTracksHealth(t *testing.T) {
	f := dockerFake()
	f.inv = Inventory{Workloads: []Workload{{Ref: Ref{Kind: "container", Name: "web", UID: "abc"}, State: "running"}}}
	r, _ := NewRegistry(Registration{ID: "docker-local", DisplayName: "Docker", ConnectionRef: "r", Adapter: f})
	// Workload refs without the platform ID are rejected.
	if _, view, err := r.Inventory(context.Background(), "docker-local", all); !errors.Is(err, ErrUnavailable) || view.Health.Status != "unavailable" {
		t.Fatal(err, view.Health)
	}
	f.inv.Workloads[0].Ref.Platform = "docker-local"
	f.inv.Capabilities.Detected = []Capability{CapWorkloads, CapApply}
	if _, _, err := r.Inventory(context.Background(), "docker-local", all); err == nil {
		t.Fatal("adapter claimed apply")
	}
	f.inv.Capabilities.Detected = []Capability{CapWorkloads, CapPreviewIngress}
	if _, _, err := r.Inventory(context.Background(), "docker-local", all); err == nil {
		t.Fatal("adapter claimed an undeclared capability")
	}
	f.inv.Capabilities.Detected = []Capability{CapWorkloads}
	inv, view, err := r.Inventory(context.Background(), "docker-local", all)
	if err != nil || view.Health.Status != "available" || inv.Workloads[0].Ports == nil || inv.Workloads[0].Issues == nil {
		t.Fatal(err, view.Health, inv)
	}
	if !strings.Contains(mustJSON(t, inv), `"capability":"routing.apply"`) {
		t.Fatal("apply must always be listed as unsupported")
	}
	// Duplicate references and fixture mismatches fail closed.
	f.inv.Workloads = append(f.inv.Workloads, f.inv.Workloads[0])
	if _, _, err := r.Inventory(context.Background(), "docker-local", all); err == nil {
		t.Fatal("duplicate ref accepted")
	}
	f.inv.Workloads = f.inv.Workloads[:1]
	f.inv.Fixture = true
	if _, _, err := r.Inventory(context.Background(), "docker-local", all); err == nil {
		t.Fatal("fixture data accepted from a live registration")
	}
	f.inv.Fixture = false
	f.err = errors.New("transport detail /run/user/1000")
	if _, view, err := r.Inventory(context.Background(), "docker-local", all); !errors.Is(err, ErrUnavailable) || strings.Contains(view.Health.Reason, "/run") {
		t.Fatal(err, view.Health)
	}
	before := f.n
	if _, _, err := r.Inventory(context.Background(), "docker-local", none); !errors.Is(err, ErrNotFound) || f.n != before {
		t.Fatal("unauthorized inventory reached the adapter")
	}
}

func TestAutoSelect(t *testing.T) {
	a := PublicView{ID: "a", Capabilities: Capabilities{Declared: []Capability{CapPreviewComposeLabel}}}
	b := PublicView{ID: "b", Capabilities: Capabilities{Declared: []Capability{CapPreviewIngress}}}
	c := PublicView{ID: "c", Capabilities: Capabilities{Declared: []Capability{CapWorkloads}}}
	if id, ok := AutoSelect([]PublicView{a, c}, PreviewCapabilities...); !ok || id != "a" {
		t.Fatal(id, ok)
	}
	if _, ok := AutoSelect([]PublicView{a, b}, PreviewCapabilities...); ok {
		t.Fatal("must not auto-select between two compatible platforms")
	}
	if _, ok := AutoSelect([]PublicView{c}, PreviewCapabilities...); ok {
		t.Fatal("selected incompatible platform")
	}
}

func TestDecodeBoundedRejectsDuplicatesDepthAndTrailing(t *testing.T) {
	var v map[string]any
	for _, in := range []string{`{"a":1,"a":2}`, `{"a":1}{}`, strings.Repeat("[", 40) + strings.Repeat("]", 40), ``, `{"a":` + strings.Repeat(`"x`, 10) + `}`} {
		if DecodeBounded([]byte(in), 1<<10, 16, &v) == nil {
			t.Errorf("accepted %q", in)
		}
	}
	if DecodeBounded([]byte(`{"a":1}`), 3, 16, &v) == nil {
		t.Error("size bound ignored")
	}
	if err := DecodeBounded([]byte(`{"a":{"b":[1,2]}}`), 1<<10, 16, &v); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
