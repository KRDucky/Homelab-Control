// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package kubernetes

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"homelab-control/backend/internal/platform"
	"homelab-control/backend/internal/traefikclient"
)

func all(string) bool { return true }

func fixtureInventory(t *testing.T) (*Adapter, platform.Inventory) {
	t.Helper()
	a, err := NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	r, err := platform.NewRegistry(platform.Registration{ID: "k3s-fixture", DisplayName: "K3s demo", ConnectionRef: "fixture:k3s", Adapter: a})
	if err != nil {
		t.Fatal(err)
	}
	inv, _, err := r.Inventory(context.Background(), "k3s-fixture", func(s string) bool { return s == "kubernetes.read" })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Inventory(context.Background(), "k3s-fixture", func(s string) bool { return s == "docker.compose.read" }); err == nil {
		t.Fatal("Docker scope granted cluster access")
	}
	return a, inv
}

func find(t *testing.T, inv platform.Inventory, kind, name string) (platform.Workload, platform.Service) {
	t.Helper()
	var w platform.Workload
	var s platform.Service
	for _, x := range inv.Workloads {
		if x.Ref.Kind == kind && x.Ref.Name == name {
			w = x
		}
	}
	for _, x := range inv.Services {
		if x.Ref.Name == name {
			s = x
		}
	}
	if w.Ref.UID == "" || s.Ref.UID == "" {
		t.Fatal("fixture resource missing", kind, name)
	}
	return w, s
}

func TestK3sDetectionAndNamespaceInventory(t *testing.T) {
	_, inv := fixtureInventory(t)
	if inv.Distribution != platform.DistK3s || inv.Version != "v1.31.4+k3s1" || !inv.Fixture {
		t.Fatal(inv.Distribution, inv.Version)
	}
	notes, svc := find(t, inv, "Deployment", "notes")
	if notes.Ref.Platform != "k3s-fixture" || notes.Ref.Namespace != "demo" || notes.State != "ready" || notes.Replicas != 2 || len(notes.Services) != 1 || notes.Services[0].UID != svc.Ref.UID {
		t.Fatalf("%+v", notes)
	}
	if svc.Ports[0].Port != 80 || svc.Ports[0].Target != "http" {
		t.Fatal(svc.Ports)
	}
	gallery, _ := find(t, inv, "Deployment", "gallery")
	if gallery.State != "unavailable" {
		t.Fatal(gallery.State)
	}
	db, _ := find(t, inv, "StatefulSet", "db")
	if db.Ref.Kind != "StatefulSet" {
		t.Fatal(db.Ref)
	}
	b, _ := json.Marshal(inv)
	for _, leak := range []string{"must-not-leak", "DB_PASSWORD", "last-applied", "example.invalid"} {
		if strings.Contains(string(b), leak) {
			t.Fatal("leaked", leak)
		}
	}
	c := inv.Capabilities
	if !c.Has(platform.CapPreviewIngress) || !c.Has(platform.CapPreviewIngressRoute) || c.Has(platform.CapPreviewHTTPRoute) || c.Has(platform.CapPreviewComposeLabel) {
		t.Fatal(c.Detected)
	}
}

var model = traefikclient.Model{Name: "notes", Host: "notes.example.test", Port: 80, Entrypoint: "websecure", Protocol: "https", UpstreamScheme: "http", TLS: &traefikclient.TLS{}}

func TestIngressAndIngressRoutePreviews(t *testing.T) {
	a, inv := fixtureInventory(t)
	notes, svc := find(t, inv, "Deployment", "notes")
	req := platform.PreviewRequest{Target: platform.CapPreviewIngress, Workload: notes.Ref.Key(), Service: svc.Ref.Key(), Model: model}
	p, err := a.Preview(context.Background(), inv, req, traefikclient.Inventory{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Blockers) != 0 || p.ApplyAvailable || p.Deployable || p.Format != "kubernetes-manifests" || len(p.Manifests) != 1 {
		t.Fatalf("%+v", p)
	}
	b, _ := json.Marshal(p.Manifests[0])
	got := string(b)
	for _, want := range []string{`"kind":"Ingress"`, `"ingressClassName":"traefik"`, `"host":"notes.example.test"`, `"number":80`, `"router.entrypoints":"websecure"`, `"namespace":"demo"`} {
		if !strings.Contains(strings.ReplaceAll(got, "traefik.ingress.kubernetes.io/", ""), want) {
			t.Fatal(want, got)
		}
	}
	if strings.Contains(got, "traefik.enable") || strings.Contains(got, "loadbalancer") {
		t.Fatal("Compose labels generated for Kubernetes")
	}
	req.Target = platform.CapPreviewIngressRoute
	req.Model.UpstreamScheme = "https"
	p, err = a.Preview(context.Background(), inv, req, traefikclient.Inventory{})
	if err != nil || len(p.Blockers) != 0 {
		t.Fatal(err, p.Blockers)
	}
	b, _ = json.Marshal(p.Manifests[0])
	if !strings.Contains(string(b), `"kind":"IngressRoute"`) || !strings.Contains(string(b), `"scheme":"https"`) || !strings.Contains(string(b), "Host(`notes.example.test`)") {
		t.Fatal(string(b))
	}
	// HTTPS upstream on a plain Ingress would need a Service change: blocked.
	req.Target = platform.CapPreviewIngress
	if p, _ = a.Preview(context.Background(), inv, req, traefikclient.Inventory{}); !hasCode(p.Blockers, "upstream_https_unsupported") {
		t.Fatal(p.Blockers)
	}
}

func hasCode(issues []platform.Issue, code string) bool {
	return slices.ContainsFunc(issues, func(i platform.Issue) bool { return i.Code == code })
}

func TestPreviewBlockersAndRejections(t *testing.T) {
	a, inv := fixtureInventory(t)
	notes, notesSvc := find(t, inv, "Deployment", "notes")
	_, dbSvc := find(t, inv, "StatefulSet", "db")
	base := platform.PreviewRequest{Target: platform.CapPreviewIngress, Workload: notes.Ref.Key(), Service: notesSvc.Ref.Key(), Model: model}
	check := func(name string, mut func(*platform.PreviewRequest), routers traefikclient.Inventory, code string) {
		t.Helper()
		req := base
		mut(&req)
		p, err := a.Preview(context.Background(), inv, req, routers)
		if err != nil || !hasCode(p.Blockers, code) {
			t.Errorf("%s: %v %+v", name, err, p.Blockers)
		}
	}
	check("existing ingress host", func(r *platform.PreviewRequest) { r.Model.Host = "wiki.example.test" }, traefikclient.Inventory{}, "hostname_duplicate")
	check("existing ingressroute host", func(r *platform.PreviewRequest) { r.Model.Host = "dash.example.test" }, traefikclient.Inventory{}, "hostname_duplicate")
	check("live traefik router", func(*platform.PreviewRequest) {}, traefikclient.Inventory{Routers: []traefikclient.Resource{{Name: "x@file", Rule: "Host(`notes.example.test`)"}}}, "hostname_duplicate")
	check("existing name", func(r *platform.PreviewRequest) { r.Model.Name = "wiki"; r.Model.Host = "new.example.test" }, traefikclient.Inventory{}, "name_duplicate")
	check("service not selecting", func(r *platform.PreviewRequest) { r.Service = dbSvc.Ref.Key(); r.Model.Port = 5432 }, traefikclient.Inventory{}, "service_not_selecting")
	check("container port instead of service port", func(r *platform.PreviewRequest) { r.Model.Port = 8080 }, traefikclient.Inventory{}, "port_missing")
	for name, mut := range map[string]func(*platform.PreviewRequest){
		"undetected gateway":     func(r *platform.PreviewRequest) { r.Target = platform.CapPreviewHTTPRoute },
		"compose labels target":  func(r *platform.PreviewRequest) { r.Target = platform.CapPreviewComposeLabel },
		"unknown workload":       func(r *platform.PreviewRequest) { r.Workload = "k3s-fixture/Deployment/demo/x/x" },
		"unconfirmed entrypoint": func(r *platform.PreviewRequest) { r.Model.Entrypoint = "admin" },
		"unconfirmed resolver":   func(r *platform.PreviewRequest) { r.Model.TLS = &traefikclient.TLS{CertificateResolver: "le"} },
		"unknown ingress class":  func(r *platform.PreviewRequest) { r.RouteClass = "nginx" },
		"docker network field":   func(r *platform.PreviewRequest) { r.Network = "bridge" },
	} {
		req := base
		mut(&req)
		if _, err := a.Preview(context.Background(), inv, req, traefikclient.Inventory{}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// gatewaySource builds a minimal vanilla Kubernetes cluster with Gateway API.
func gatewaySource(t *testing.T, extra map[string]string) *Adapter {
	t.Helper()
	src := &MapSource{Responses: map[string][]byte{}}
	a, err := New(src, Options{Namespaces: []string{"apps"}, Entrypoints: []string{"web"}, Auth: platform.AuthKubernetesBearer, Transport: "kubernetes-api", EndpointLabel: "Lab cluster"})
	if err != nil {
		t.Fatal(err)
	}
	src.Allowed = a.Paths()
	resp := map[string]string{
		"/version": `{"gitVersion":"v1.32.1"}`,
		"/apis":    `{"groups":[{"name":"apps","versions":[{"groupVersion":"apps/v1"}]},{"name":"gateway.networking.k8s.io","versions":[{"groupVersion":"gateway.networking.k8s.io/v1"}]}]}`,
		"/apis/gateway.networking.k8s.io/v1/gatewayclasses":             `{"items":[{"metadata":{"name":"traefik","uid":"11111111-1111-1111-1111-111111111111"},"spec":{"controllerName":"traefik.io/gateway-controller"}}]}`,
		"/apis/gateway.networking.k8s.io/v1/namespaces/apps/gateways":   `{"items":[{"metadata":{"name":"edge","namespace":"apps","uid":"22222222-2222-2222-2222-222222222222"},"spec":{"gatewayClassName":"traefik"}}]}`,
		"/apis/gateway.networking.k8s.io/v1/namespaces/apps/httproutes": `{"items":[]}`,
		"/apis/apps/v1/namespaces/apps/deployments":                     `{"items":[{"metadata":{"name":"api","namespace":"apps","uid":"33333333-3333-3333-3333-333333333333"},"spec":{"replicas":1,"template":{"metadata":{"labels":{"app":"api"}},"spec":{"containers":[{"name":"api","ports":[{"containerPort":9000}]}]}}},"status":{"readyReplicas":1}}]}`,
		"/apis/apps/v1/namespaces/apps/statefulsets":                    `{"items":[]}`,
		"/apis/apps/v1/namespaces/apps/daemonsets":                      `{"items":[]}`,
		"/api/v1/namespaces/apps/services":                              `{"items":[{"metadata":{"name":"api","namespace":"apps","uid":"44444444-4444-4444-4444-444444444444"},"spec":{"selector":{"app":"api"},"ports":[{"port":9000,"targetPort":9000}]}}]}`,
	}
	for k, v := range extra {
		resp[k] = v
	}
	for k, v := range resp {
		src.Responses[k] = []byte(v)
	}
	return a
}

func TestGatewayAPIDetectionOnVanillaKubernetes(t *testing.T) {
	a := gatewaySource(t, nil)
	inv, err := a.Inventory(context.Background(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	if inv.Distribution != platform.DistKubernetes || !inv.Capabilities.Has(platform.CapPreviewHTTPRoute) || inv.Capabilities.Has(platform.CapPreviewIngress) {
		t.Fatal(inv.Distribution, inv.Capabilities)
	}
	m := traefikclient.Model{Name: "api", Host: "api.example.test", Port: 9000, Entrypoint: "web", Protocol: "http", UpstreamScheme: "http"}
	p, err := a.Preview(context.Background(), inv, platform.PreviewRequest{Target: platform.CapPreviewHTTPRoute, Workload: inv.Workloads[0].Ref.Key(), Service: inv.Services[0].Ref.Key(), Model: m}, traefikclient.Inventory{})
	if err != nil || len(p.Blockers) != 0 {
		t.Fatal(err, p.Blockers)
	}
	b, _ := json.Marshal(p.Manifests[0])
	if !strings.Contains(string(b), `"kind":"HTTPRoute"`) || !strings.Contains(string(b), `"parentRefs":[{"name":"edge","namespace":"apps"}]`) || !hasCode(p.Warnings, "gateway_listener") {
		t.Fatal(string(b), p.Warnings)
	}
}

func TestKubernetesFailsClosed(t *testing.T) {
	for name, extra := range map[string]map[string]string{
		"bad version":      {"/version": `{"gitVersion":"1.32"}`},
		"duplicate key":    {"/version": `{"gitVersion":"v1.32.1","gitVersion":"v9.9.9"}`},
		"bad uid":          {"/api/v1/namespaces/apps/services": `{"items":[{"metadata":{"name":"api","namespace":"apps","uid":"x"},"spec":{}}]}`},
		"wrong namespace":  {"/api/v1/namespaces/apps/services": `{"items":[{"metadata":{"name":"api","namespace":"other","uid":"44444444-4444-4444-4444-444444444444"},"spec":{}}]}`},
		"bad service type": {"/api/v1/namespaces/apps/services": `{"items":[{"metadata":{"name":"api","namespace":"apps","uid":"44444444-4444-4444-4444-444444444444"},"spec":{"type":"Weird"}}]}`},
		"bad port":         {"/apis/apps/v1/namespaces/apps/deployments": `{"items":[{"metadata":{"name":"api","namespace":"apps","uid":"33333333-3333-3333-3333-333333333333"},"spec":{"template":{"spec":{"containers":[{"name":"a","ports":[{"containerPort":70000}]}]}}}}]}`},
		"missing response": {"/api/v1/namespaces/apps/services": ``},
	} {
		a := gatewaySource(t, extra)
		if _, err := a.Inventory(context.Background(), "lab"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The source refuses paths outside the adapter allowlist.
	src := MapSource{Allowed: []string{"/version"}, Responses: map[string][]byte{"/api/v1/secrets": []byte(`{}`)}}
	if _, err := src.Get(context.Background(), "/api/v1/secrets"); err == nil {
		t.Fatal("non-allowlisted path served")
	}
	for _, o := range []Options{{Transport: "x"}, {Transport: "x", Namespaces: []string{"kube-system"}}, {Transport: "x", Namespaces: []string{"Bad_NS"}}, {Transport: "x", Namespaces: []string{"a", "a"}}} {
		if _, err := New(MapSource{}, o); err == nil {
			t.Errorf("accepted options %+v", o)
		}
	}
	a := gatewaySource(t, nil)
	for _, p := range a.Paths() {
		if strings.Contains(p, "secret") || strings.Contains(p, "configmap") || strings.Contains(p, "/pods") || strings.Contains(p, "kube-system") {
			t.Fatal("adapter requests sensitive path", p)
		}
	}
}

// Identity is reported from the pod template only (observed, never desired),
// with container scope overriding pod scope, and invalid values fail closed.
func TestObservedIdentityFromPodTemplate(t *testing.T) {
	a, err := NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	inv, err := a.Inventory(context.Background(), "k3s-fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range inv.Workloads {
		switch w.Ref.Name {
		case "notes":
			if w.Identity != nil {
				t.Fatal("no securityContext must mean no reported identity", w.Identity)
			}
		case "gallery":
			o := w.Identity
			if o == nil || *o.UID != 3100 || *o.GID != 3000 || len(o.SupplementalGroups) != 2 || o.SupplementalGroups[1] != 5000 || o.Source != "kubernetes-pod-template" || o.Varies {
				t.Fatalf("%+v", o)
			}
		}
	}
	neg := int64(-1)
	big := int64(4294967295)
	one, two := int64(1), int64(2)
	for name, tc := range map[string]struct {
		pod *securityContext
		cs  []podContainer
		ok  bool
	}{
		"negative uid":             {&securityContext{RunAsUser: &neg}, nil, false},
		"reserved uid":             {&securityContext{RunAsGroup: &big}, nil, false},
		"negative group":           {&securityContext{SupplementalGroups: []int64{5, -3}}, nil, false},
		"container groups invalid": {nil, []podContainer{{SecurityContext: &securityContext{SupplementalGroups: []int64{5}}}}, false},
		"varying containers":       {&securityContext{RunAsUser: &one}, []podContainer{{SecurityContext: &securityContext{RunAsUser: &one}}, {SecurityContext: &securityContext{RunAsUser: &two}}}, true},
	} {
		o, err := observedIdentity(tc.pod, tc.cs)
		if (err == nil) != tc.ok {
			t.Fatal(name, err)
		}
		if name == "varying containers" && (!o.Varies || *o.UID != 1) {
			t.Fatal(name, o)
		}
	}
}
