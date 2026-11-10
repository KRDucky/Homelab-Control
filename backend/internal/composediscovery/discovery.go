// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Package composediscovery is a read-only metadata contract. Its providers have no shell,
// Compose-file reader or mutation method. Docker transport is explicitly opt-in.
package composediscovery

import "context"

type Port struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}
type Service struct {
	HiddenTraefikSettings bool     `json:"-"`
	ReplicaCount          int      `json:"replicaCount,omitempty"`
	UnsupportedNetworking bool     `json:"unsupportedNetworking,omitempty"`
	UnsupportedDeployment bool     `json:"unsupportedDeployment,omitempty"`
	ID                    string   `json:"id"`
	Compose               bool     `json:"compose"`
	Project               string   `json:"project,omitempty"`
	Service               string   `json:"service,omitempty"`
	Container             string   `json:"container"`
	State                 string   `json:"state"`
	Ports                 []Port   `json:"ports"`
	Networks              []string `json:"networks"`
	// Labels stays server-side. Browser views contain safe routing labels only.
	Labels map[string]string `json:"-"`
}
type Context struct {
	Networks       []string `json:"networks"`
	Entrypoints    []string `json:"entrypoints"`
	Resolvers      []string `json:"resolvers"`
	DockerProvider *bool    `json:"dockerProvider"`
}
type Snapshot struct {
	Source   string    `json:"source"`
	Fixture  bool      `json:"fixture"`
	Services []Service `json:"services"`
	Traefik  Context   `json:"traefik"`
}

// Provider must perform bounded, authorized metadata reads only. Enabling live Docker
// transport needs separate approval; callers never supply socket paths or argv.
type Provider interface {
	Snapshot(context.Context) (Snapshot, error)
}
type Fixture struct{}

func (Fixture) Snapshot(context.Context) (Snapshot, error) {
	disabled := false
	return Snapshot{Source: "fixture", Fixture: true, Traefik: Context{Networks: []string{"hc-traefik-test"}, Entrypoints: []string{"web"}, Resolvers: []string{}, DockerProvider: &disabled}, Services: []Service{
		{ID: "fixture-notes-web", Compose: true, Project: "example-notes", Service: "web", Container: "example-notes-web-1", State: "running", Ports: []Port{{8080, "tcp"}}, Networks: []string{"hc-traefik-test"}, Labels: map[string]string{"org.example.owner": "preserved"}},
		{ID: "fixture-gallery-web", Compose: true, Project: "example-gallery", Service: "web", Container: "example-gallery-web-1", State: "running", Ports: []Port{{3000, "tcp"}}, Networks: []string{"gallery_default"}, Labels: map[string]string{}},
		{ID: "fixture-standalone", Compose: false, Container: "example-standalone", State: "running", Ports: []Port{{9090, "tcp"}}, Networks: []string{"hc-traefik-test"}, Labels: map[string]string{}},
	}}, nil
}

// CountReplicas reports observed containers per Compose project/service, not a
// desired Compose scale. Broker and CORE independently recompute this value.
func CountReplicas(services []Service) {
	counts := map[string]int{}
	for _, s := range services {
		if s.Compose {
			counts[s.Project+"\x00"+s.Service]++
		}
	}
	for i := range services {
		if services[i].Compose {
			services[i].ReplicaCount = counts[services[i].Project+"\x00"+services[i].Service]
		} else {
			services[i].ReplicaCount = 0
		}
	}
}
