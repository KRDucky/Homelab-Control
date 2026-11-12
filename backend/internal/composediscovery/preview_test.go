// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"context"
	"encoding/json"
	"homelab-control/backend/internal/traefikclient"
	"reflect"
	"strings"
	"testing"
)

func fixtureRequest() (Snapshot, Request) {
	s, _ := (Fixture{}).Snapshot(context.Background())
	return s, Request{ServiceID: "fixture-notes-web", Network: "hc-traefik-test", Model: traefikclient.Model{Name: "notes", Host: "notes.example.com", Port: 8080, Entrypoint: "web", Protocol: "http", UpstreamScheme: "http"}}
}
func has(issues []Issue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}
func TestPreviewOnlyPreservesInputs(t *testing.T) {
	s, r := fixtureRequest()
	before, _ := json.Marshal(s.Services[0].Labels)
	p, e := Build(s, traefikclient.Inventory{}, r)
	if e != nil {
		t.Fatal(e)
	}
	after, _ := json.Marshal(s.Services[0].Labels)
	if string(before) != string(after) || p.ApplyAvailable || p.SourceFileVerified {
		t.Fatal("mutation or false assurance")
	}
	if !has(p.Blockers, "provider_disabled") || p.Proposed.Labels["traefik.docker.network"] != r.Network || len(p.Changes) != 8 {
		t.Fatal(p)
	}
	if !reflect.DeepEqual(p.Existing.Networks, p.Proposed.Networks) {
		t.Fatal(p)
	}
	for _, c := range p.Changes {
		if len(c.Path) != 4 || c.Path[2] != "labels" {
			t.Fatal("replaces unrelated configuration")
		}
	}
}
func TestValidationAndBlockers(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Request)
	}{{"hostname", func(r *Request) { r.Model.Host = "a`evil.com" }}, {"port", func(r *Request) { r.Model.Port = 0 }}, {"tls", func(r *Request) { r.Model.Protocol = "https" }}, {"http-tls", func(r *Request) { r.Model.TLS = &traefikclient.TLS{} }}, {"network", func(r *Request) { r.Network = "unconfirmed" }}, {"resolver", func(r *Request) {
		r.Model.Protocol = "https"
		r.Model.TLS = &traefikclient.TLS{CertificateResolver: "unknown"}
	}}, {"selection", func(r *Request) { r.ServiceID = "missing" }}} {
		t.Run(test.name, func(t *testing.T) {
			s, r := fixtureRequest()
			test.change(&r)
			if _, e := Build(s, traefikclient.Inventory{}, r); e == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
	s, r := fixtureRequest()
	r.ServiceID = "fixture-gallery-web"
	p, e := Build(s, traefikclient.Inventory{}, r)
	if e != nil || !has(p.Blockers, "port_missing") || !has(p.Blockers, "network_missing") {
		t.Fatal(p, e)
	}
	r.ServiceID = "fixture-standalone"
	p, _ = Build(s, traefikclient.Inventory{}, r)
	if !has(p.Blockers, "not_compose") {
		t.Fatal(p)
	}
}

func TestBuildBlocksTraefikProxy(t *testing.T) {
	s, r := fixtureRequest()
	s.Services = append(s.Services, Service{
		ID: "fixture-traefik", Compose: true, Project: "hc-traefik-live", Service: "traefik",
		Container: "hc-traefik-fixture", State: "running", Ports: []Port{{8080, "tcp"}},
		Networks: []string{"hc-traefik-test"}, Labels: map[string]string{},
	})
	r.ServiceID = "fixture-traefik"
	p, err := Build(s, traefikclient.Inventory{}, r)
	if err != nil || !has(p.Blockers, "traefik_proxy") {
		t.Fatal(p, err)
	}
}
func TestConflictsTLSAndRedaction(t *testing.T) {
	s, r := fixtureRequest()
	s.Services[0].Labels["traefik.http.routers.notes.rule"] = "Host(`old.example.com`)"
	s.Services[0].Labels["traefik.http.middlewares.auth.basicauth.users"] = "PRIVATE-HASH"
	s.Services[0].Labels["traefik.http.routers.notes.tls.certresolver"] = "PRIVATE-HASH WITH SPACE"
	r.Model.Protocol = "https"
	r.Model.TLS = &traefikclient.TLS{}
	p, e := Build(s, traefikclient.Inventory{Routers: []traefikclient.Resource{{Name: "notes@file", Provider: "file", Rule: "Host(`notes.example.com`)"}}}, r)
	if e != nil {
		t.Fatal(e)
	}
	for _, code := range []string{"router_duplicate", "hostname_duplicate", "label_conflict", "unsupported_existing"} {
		if !has(p.Blockers, code) {
			t.Fatal(code, p)
		}
	}
	if p.Proposed.Labels["traefik.http.routers.notes.tls"] != "true" || p.Proposed.Labels["traefik.http.services.notes.loadbalancer.server.scheme"] != "http" {
		t.Fatal(p)
	}
	b, _ := json.Marshal([]any{p, View(s)})
	if strings.Contains(string(b), "PRIVATE-HASH") || strings.Contains(string(b), "org.example.owner") {
		t.Fatal("leaked unrelated or sensitive label")
	}
}

func TestHostAndPathPrefixIsHostnameDuplicate(t *testing.T) {
	s, r := fixtureRequest()
	r.Model.Host = "app.isolated.test"
	p, err := Build(s, traefikclient.Inventory{Routers: []traefikclient.Resource{
		{Name: "app-http@file", Provider: "file", Rule: "Host(`app.isolated.test`) && PathPrefix(`/app`)"},
	}}, r)
	if err != nil || !has(p.Blockers, "hostname_duplicate") {
		t.Fatal(p, err)
	}
	p, err = Build(s, traefikclient.Inventory{Routers: []traefikclient.Resource{
		{Name: "hc-api@file", Provider: "file", Rule: "PathPrefix(`/api`)"},
	}}, r)
	if err != nil || has(p.Blockers, "hostname_duplicate") || !has(p.Warnings, "complex_rule") {
		t.Fatal(p, err)
	}
}

func TestFileYAMLWhenDockerProviderDisabled(t *testing.T) {
	s, r := fixtureRequest()
	r.Model.Path = "/notes"
	p, err := Build(s, traefikclient.Inventory{}, r)
	if err != nil || !has(p.Blockers, "provider_disabled") || !has(p.Warnings, "file_provider") {
		t.Fatal(p, err)
	}
	if p.FileYAML == "" || !strings.Contains(p.FileYAML, "PathPrefix(`/notes`)") || !strings.Contains(p.FileYAML, "url: http://") {
		t.Fatal(p.FileYAML)
	}
	if p.ApplyAvailable {
		t.Fatal("apply leaked")
	}
}
