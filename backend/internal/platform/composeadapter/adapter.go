// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Package composeadapter adapts the existing, verified Compose discovery
// providers (Docker Metadata Broker client, direct rootless Docker reader,
// fixture, and the Podman libpod parser) to the platform contract. It adds no
// transport: it only normalizes composediscovery.Snapshot values and reuses
// composediscovery.Build for label previews, so Phase 6/7 behaviour is kept.
package composeadapter

import (
	"context"
	"maps"
	"slices"
	"strings"

	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/platform"
	"homelab-control/backend/internal/routingwarnings"
	"homelab-control/backend/internal/traefikclient"
)

type Options struct {
	Kind          platform.Kind
	Distribution  platform.Distribution
	Mode          platform.Mode
	Auth          platform.AuthMechanism
	Transport     string
	EndpointLabel string
	// Warnings are platform-specific caveats added to every preview (for
	// example Podman rootless networking).
	Warnings []platform.Issue
}

type Adapter struct {
	provider composediscovery.Provider
	opts     Options
}

func New(p composediscovery.Provider, o Options) (*Adapter, error) {
	if p == nil || (o.Kind != platform.KindDocker && o.Kind != platform.KindPodman) || o.Transport == "" {
		return nil, platform.ErrInvalid
	}
	if o.Mode != platform.ModeRootless && o.Mode != platform.ModeRootful {
		return nil, platform.ErrInvalid
	}
	return &Adapter{provider: p, opts: o}, nil
}

func (a *Adapter) Descriptor() platform.Descriptor {
	return platform.Descriptor{Kind: a.opts.Kind, Distribution: a.opts.Distribution, Mode: a.opts.Mode, Auth: a.opts.Auth,
		Endpoint: platform.Endpoint{Transport: a.opts.Transport, Label: a.opts.EndpointLabel}, Fixture: a.opts.Auth == platform.AuthFixture}
}

func (a *Adapter) Declared() []platform.Capability {
	return []platform.Capability{platform.CapWorkloads, platform.CapPorts, platform.CapNetworks, platform.CapHealth, platform.CapPreviewComposeLabel}
}

func (a *Adapter) Inventory(ctx context.Context, id string) (platform.Inventory, error) {
	snap, err := a.provider.Snapshot(ctx)
	if err != nil || snap.Fixture != (a.opts.Auth == platform.AuthFixture) {
		return platform.Inventory{}, platform.ErrUnavailable
	}
	return FromSnapshot(id, a.opts.Distribution, snap), nil
}

// FromSnapshot normalizes a Compose snapshot. Each container keeps its native
// identity (kind "container", Docker/Podman ID); the Compose project and
// service are retained as Group and Component, never assumed to exist.
func FromSnapshot(id string, dist platform.Distribution, snap composediscovery.Snapshot) platform.Inventory {
	inv := platform.Inventory{Platform: id, Fixture: snap.Fixture, Distribution: dist, Workloads: []platform.Workload{}, Services: []platform.Service{},
		Networks: []platform.Network{}, Namespaces: []string{}, Entrypoints: slices.Clone(snap.Traefik.Entrypoints), Resolvers: slices.Clone(snap.Traefik.Resolvers)}
	catalog := composediscovery.View(snap)
	networks := map[string]bool{}
	for _, n := range snap.Traefik.Networks {
		networks[n] = true
	}
	all := map[string]bool{}
	for _, s := range catalog.Services {
		w := platform.Workload{Ref: platform.Ref{Platform: id, Kind: "container", Namespace: s.Project, Name: s.Container, UID: s.ID},
			Group: s.Project, Component: s.Service.Service, State: s.State, Replicas: 1, Ports: []platform.Port{}, Networks: slices.Clone(s.Networks),
			Services: []platform.Ref{}, RoutingLabels: s.TraefikLabels, HiddenRouting: s.HiddenTraefikSettings, Issues: []string{}}
		if s.State == "running" {
			w.ReadyReplicas = 1
		}
		for _, p := range s.Ports {
			w.Ports = append(w.Ports, platform.Port{Port: p.Port, Protocol: p.Protocol})
		}
		if !s.Compose {
			w.Issues = append(w.Issues, "not_compose")
		}
		if s.ReplicaCount > 1 {
			w.Issues = append(w.Issues, "replicas_unsupported")
		}
		if s.UnsupportedNetworking {
			w.Issues = append(w.Issues, "unsupported_networking")
		}
		if s.UnsupportedDeployment {
			w.Issues = append(w.Issues, "unsupported_deployment")
		}
		inv.Workloads = append(inv.Workloads, w)
		for _, n := range s.Networks {
			all[n] = true
		}
	}
	for n := range networks {
		all[n] = true
	}
	for _, n := range slices.Sorted(maps.Keys(all)) {
		inv.Networks = append(inv.Networks, platform.Network{Ref: platform.Ref{Platform: id, Kind: "network", Name: n, UID: n}, TraefikAttached: networks[n]})
	}
	var available *bool
	detail := "Traefik Docker provider availability is unknown."
	if snap.Traefik.DockerProvider != nil {
		v := *snap.Traefik.DockerProvider
		available = &v
		detail = "Reported by administrator routing configuration, not by Traefik itself."
	}
	inv.Routing = []platform.RoutingProvider{{Kind: "traefik-docker-labels", Available: available, Classes: []string{}, Detail: detail}}
	inv.Capabilities.Detected = []platform.Capability{platform.CapWorkloads, platform.CapPorts, platform.CapNetworks, platform.CapHealth, platform.CapPreviewComposeLabel}
	inv.Capabilities.Unsupported = []platform.Unsupported{
		{Capability: platform.CapServices, Reason: "Compose platforms have no separate service objects; ports come from container metadata."},
		{Capability: platform.CapPreviewIngress, Reason: "Kubernetes resources are not generated for container platforms."},
	}
	return inv
}

// Preview delegates to the verified composediscovery.Build, with the selection
// resolved against CORE's validated inventory first.
func (a *Adapter) Preview(ctx context.Context, inv platform.Inventory, req platform.PreviewRequest, routers traefikclient.Inventory) (platform.Preview, error) {
	if req.Target != platform.CapPreviewComposeLabel || req.Service != "" {
		return platform.Preview{}, platform.ErrUnsupported
	}
	var selected *platform.Workload
	for i := range inv.Workloads {
		if inv.Workloads[i].Ref.Key() == req.Workload {
			selected = &inv.Workloads[i]
		}
	}
	if selected == nil {
		return platform.Preview{}, platform.ErrInvalid
	}
	snap, err := a.provider.Snapshot(ctx)
	if err != nil {
		return platform.Preview{}, platform.ErrUnavailable
	}
	proposal, err := composediscovery.Build(snap, routers, composediscovery.Request{ConfirmUnverifiedPort: req.ConfirmUnverifiedPort, ServiceID: selected.Ref.UID, Network: req.Network, Model: req.Model})
	if err != nil {
		return platform.Preview{}, platform.ErrInvalid
	}
	proposal.Warnings = routingwarnings.Consolidate(proposal.Warnings, routers)
	out := platform.Preview{SchemaVersion: 1, Target: req.Target, Format: "compose-labels", Workload: selected.Ref, Compose: proposal,
		Warnings: []platform.Issue{}, Blockers: []platform.Issue{}, Validation: platform.BaseValidation()}
	for _, w := range proposal.Warnings {
		out.Warnings = append(out.Warnings, platform.Issue{Code: w.Code, Message: w.Message})
	}
	for _, b := range proposal.Blockers {
		out.Blockers = append(out.Blockers, platform.Issue{Code: b.Code, Message: b.Message})
	}
	out.Warnings = append(out.Warnings, a.opts.Warnings...)
	if a.opts.Kind == platform.KindPodman {
		for i := range out.Warnings {
			out.Warnings[i].Message = strings.ReplaceAll(out.Warnings[i].Message, "Docker applications", "Podman applications")
		}
	}
	return out, nil
}
