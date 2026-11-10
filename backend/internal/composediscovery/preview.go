// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"errors"
	"fmt"
	"homelab-control/backend/internal/traefikclient"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// SafeLabels excludes unrelated values and middleware/auth material; the proposal
// changes individual keys, never replaces a complete labels or service object.
var safeKey = regexp.MustCompile(`^traefik\.(?:enable|docker\.network|http\.routers\.[a-z][a-z0-9-]{0,62}\.(?:rule|entrypoints|service|tls|tls\.certresolver)|http\.services\.[a-z][a-z0-9-]{0,62}\.loadbalancer\.server\.(?:port|scheme))$`)

func SafeLabels(labels map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range labels {
		if safeKey.MatchString(k) && safeValue(k, v) {
			out[k] = v
		}
	}
	return out
}
func safeValue(k, v string) bool {
	if len(v) > 280 {
		return false
	}
	switch {
	case strings.HasSuffix(k, ".rule"):
		return hostRule.MatchString(v)
	case strings.HasSuffix(k, ".port"):
		n, err := strconv.Atoi(v)
		return err == nil && n >= 1 && n <= 65535
	case strings.HasSuffix(k, ".scheme"):
		return v == "http" || v == "https"
	case k == "traefik.enable" || strings.HasSuffix(k, ".tls"):
		return v == "true" || v == "false"
	default:
		return regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,128}$`).MatchString(v)
	}
}

type ServiceView struct {
	Service
	TraefikLabels         map[string]string `json:"traefikLabels"`
	HiddenTraefikSettings bool              `json:"hiddenTraefikSettings"`
}
type Catalog struct {
	Source   string        `json:"source"`
	Fixture  bool          `json:"fixture"`
	Services []ServiceView `json:"services"`
	Traefik  Context       `json:"traefik"`
}

func View(s Snapshot) Catalog {
	out := Catalog{Source: s.Source, Fixture: s.Fixture, Traefik: s.Traefik, Services: []ServiceView{}}
	for _, service := range s.Services {
		safe := SafeLabels(service.Labels)
		hidden := service.HiddenTraefikSettings
		for key := range service.Labels {
			if strings.HasPrefix(key, "traefik.") {
				if _, ok := safe[key]; !ok {
					hidden = true
				}
			}
		}
		copy := service
		copy.Labels = nil
		out.Services = append(out.Services, ServiceView{copy, safe, hidden})
	}
	return out
}

type Request struct {
	ConfirmUnverifiedPort bool                `json:"confirmUnverifiedPort,omitempty"`
	ServiceID             string              `json:"serviceId"`
	Network               string              `json:"network"`
	Model                 traefikclient.Model `json:"model"`
}
type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Configuration struct {
	Labels   map[string]string `json:"labels"`
	Networks []string          `json:"networks"`
}
type Change struct {
	Path   []string `json:"path"`
	Before *string  `json:"before"`
	After  string   `json:"after"`
}
type ProxySummary struct {
	Application         string `json:"application"`
	TargetService       string `json:"targetService"`
	Container           string `json:"container"`
	Hostname            string `json:"hostname"`
	InternalPort        int    `json:"internalPort"`
	Protocol            string `json:"protocol"`
	UpstreamScheme      string `json:"upstreamScheme"`
	CertificateResolver string `json:"certificateResolver"`
	Network             string `json:"network"`
	Entrypoint          string `json:"entrypoint"`
}
type Proposal struct {
	Summary            ProxySummary  `json:"summary"`
	SchemaVersion      int           `json:"schemaVersion"`
	Source             string        `json:"source"`
	Fixture            bool          `json:"fixture"`
	Project            string        `json:"project"`
	Service            string        `json:"service"`
	Existing           Configuration `json:"existing"`
	Proposed           Configuration `json:"proposed"`
	Changes            []Change      `json:"changes"`
	Warnings           []Issue       `json:"warnings"`
	Blockers           []Issue       `json:"blockers"`
	ApplyAvailable     bool          `json:"applyAvailable"`
	SourceFileVerified bool          `json:"sourceFileVerified"`
	PreservedFields    []string      `json:"preservedFields"`
}

var hostRule = regexp.MustCompile("^Host\\(`([a-z0-9.-]+)`\\)$")

func Build(s Snapshot, inventory traefikclient.Inventory, req Request) (Proposal, error) {
	labels, err := req.Model.Labels()
	if err != nil {
		return Proposal{}, err
	}
	var selected *Service
	for i := range s.Services {
		if s.Services[i].ID == req.ServiceID {
			if selected != nil {
				return Proposal{}, errors.New("ambiguous selection")
			}
			selected = &s.Services[i]
		}
	}
	if selected == nil {
		return Proposal{}, errors.New("unknown service selection")
	}
	if req.Network == "" || !slices.Contains(s.Traefik.Networks, req.Network) {
		return Proposal{}, errors.New("select a confirmed Traefik network")
	}
	if !slices.Contains(s.Traefik.Entrypoints, req.Model.Entrypoint) {
		return Proposal{}, errors.New("select a confirmed entrypoint")
	}
	if req.Model.TLS != nil && req.Model.TLS.CertificateResolver != "" && !slices.Contains(s.Traefik.Resolvers, req.Model.TLS.CertificateResolver) {
		return Proposal{}, errors.New("select a confirmed certificate resolver")
	}
	labels.Labels["traefik.docker.network"] = req.Network
	existing := SafeLabels(selected.Labels)
	proposed := maps.Clone(existing)
	out := Proposal{SchemaVersion: 1, Source: s.Source, Fixture: s.Fixture, Project: selected.Project, Service: selected.Service, Existing: Configuration{existing, slices.Clone(selected.Networks)}, Proposed: Configuration{proposed, slices.Clone(selected.Networks)}, Changes: []Change{}, Warnings: []Issue{}, Blockers: []Issue{}, PreservedFields: []string{"environment", "volumes", "commands", "application settings", "unrelated labels", "other networks"}}
	resolver := ""
	if req.Model.TLS != nil {
		resolver = req.Model.TLS.CertificateResolver
	}
	out.Summary = ProxySummary{selected.Project, selected.Service, selected.Container, req.Model.Host, req.Model.Port, req.Model.Protocol, req.Model.UpstreamScheme, resolver, req.Network, req.Model.Entrypoint}
	warn := func(code, message string) { out.Warnings = append(out.Warnings, Issue{code, message}) }
	block := func(code, message string) { out.Blockers = append(out.Blockers, Issue{code, message}) }
	if s.Fixture {
		warn("fixture", "Demonstration discovery data; these are not your Docker applications.")
	}
	warn("source_unverified", "Compose source is not loaded. This is a labels/network patch proposal, not a complete file diff.")
	if selected.UnsupportedDeployment {
		block("unsupported_deployment", "One-off or unsupported deployment configuration requires manual review.")
	}
	if selected.HiddenTraefikSettings {
		block("unsupported_existing", "Unsupported or sensitive Traefik settings are not exposed and require manual review.")
	}
	if !selected.Compose || selected.Project == "" || selected.Service == "" {
		block("not_compose", "Standalone containers are not supported by the Compose proposal.")
	}
	if selected.State != "running" {
		warn("not_running", "Selected container is not reported running.")
	}
	foundPort := false
	for _, p := range selected.Ports {
		if p.Protocol == "tcp" && p.Port == req.Model.Port {
			foundPort = true
		}
	}
	tcpCount := 0
	for _, p := range selected.Ports {
		if p.Protocol == "tcp" {
			tcpCount++
		}
	}
	if tcpCount == 0 && !req.ConfirmUnverifiedPort {
		return Proposal{}, errors.New("explicit unverified internal port confirmation required")
	}
	if selected.UnsupportedNetworking {
		block("unsupported_networking", "Host, none or shared-container networking is unsupported by this preview.")
	}
	if !foundPort && tcpCount == 0 && req.ConfirmUnverifiedPort {
		warn("port_unverified", "Internal port was explicitly supplied by the user and validated only as a number; verify it against the application configuration.")
		block("port_unverified", "Internal listening port is not established by Docker metadata; manual verification is required.")
	} else if !foundPort {
		block("port_missing", "Select a discovered internal TCP port; published host ports are not used.")
	}
	if !slices.Contains(selected.Networks, req.Network) {
		block("network_missing", "The application does not share the selected Traefik network. A separately approved network attachment is required.")
		out.Proposed.Networks = append(out.Proposed.Networks, req.Network)
		out.Changes = append(out.Changes, Change{Path: []string{"services", selected.Service, "networks", req.Network}, After: "attach existing network"})
	}
	if s.Traefik.DockerProvider == nil {
		block("provider_unknown", "Docker provider availability is unknown; labels may not be consumed.")
	} else if !*s.Traefik.DockerProvider {
		block("provider_disabled", "Traefik Docker provider is disabled. Preview only; these labels will not configure the current file-provider fixture.")
	}
	if req.Model.Protocol == "https" {
		warn("tls_review", "Incoming TLS is independent of upstream encryption. Review certificates and entrypoint TLS options before any future application.")
		if req.Model.TLS.CertificateResolver == "" {
			warn("certificate_default", "No resolver selected: existing/default certificate only; certificate validity is not established.")
		}
	}
	for _, router := range inventory.Routers {
		if strings.TrimSuffix(router.Name, "@"+router.Provider) == req.Model.Name {
			block("router_duplicate", "Router name already exists in Traefik inventory.")
		}
		if router.Rule == "" {
			warn("host_unverified", "Some live routers lack routing rules; hostname conflicts cannot be fully verified.")
			continue
		}
		m := hostRule.FindStringSubmatch(router.Rule)
		if len(m) == 2 && m[1] == req.Model.Host {
			block("hostname_duplicate", "Hostname already exists in Traefik inventory.")
		} else if len(m) == 0 {
			warn("complex_rule", "A live routing rule cannot be compared by this single-host model; review conflicts manually.")
		}
	}
	for _, service := range s.Services {
		for k, v := range service.Labels {
			if strings.HasPrefix(k, "traefik.http.routers.") && strings.HasSuffix(k, ".rule") {
				m := hostRule.FindStringSubmatch(v)
				if len(m) == 2 && m[1] == req.Model.Host && (service.ID != selected.ID || k != "traefik.http.routers."+req.Model.Name+".rule") {
					block("hostname_duplicate", "Another discovered service already has this hostname label.")
				}
				if k == "traefik.http.routers."+req.Model.Name+".rule" && service.ID != selected.ID {
					block("router_duplicate", "Another service already defines this router name.")
				}
			}
		}
	}
	for k := range selected.Labels {
		if strings.HasPrefix(k, "traefik.") {
			if _, ok := existing[k]; !ok {
				block("unsupported_existing", "Existing Traefik settings outside this preview's routing model need manual review; sensitive values are not exposed.")
				break
			}
		}
	}
	for _, other := range s.Services {
		if other.ID != selected.ID && other.Project == selected.Project && other.Service == selected.Service && selected.Compose {
			block("replicas_unsupported", "Multiple containers represent the selected Compose service. Replica configuration needs manual review.")
		}
	}
	for key := range existing {
		if strings.HasPrefix(key, "traefik.http.routers."+req.Model.Name+".") {
			if _, ok := labels.Labels[key]; !ok {
				block("tls_or_routing_conflict", "Existing router settings are outside the requested model and would remain active. Manual review is required.")
			}
		}
	}
	keys := slices.Sorted(maps.Keys(labels.Labels))
	for _, key := range keys {
		value := labels.Labels[key]
		prior, ok := selected.Labels[key]
		if ok && prior != value {
			block("label_conflict", fmt.Sprintf("Existing routing label %s differs; no overwrite is authorized.", key))
		}
		if !ok || prior != value {
			var before *string
			if ok {
				v, visible := existing[key]
				if visible {
					before = &v
				}
			}
			out.Changes = append(out.Changes, Change{[]string{"services", selected.Service, "labels", key}, before, value})
		}
		out.Proposed.Labels[key] = value
	}
	return out, nil
}
