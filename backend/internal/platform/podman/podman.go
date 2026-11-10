// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Package podman parses Podman's native libpod container list into the shared
// Compose snapshot. It deliberately does not assume Docker semantics: libpod
// field names, states, port ranges, pods, infra containers and podman-compose
// labels are handled explicitly. There is no live transport in this package;
// a bounded host-side reader needs its own reviewed plan and approval.
package podman

import (
	"context"
	_ "embed"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/platform"
)

// ListPath is the only libpod endpoint a future transport may request.
const ListPath = "/v5.0.0/libpod/containers/json?all=true"
const maxBytes = 4 << 20

var (
	nameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	idRE   = regexp.MustCompile(`^[a-f0-9]{64}$`)
	keyRE  = regexp.MustCompile(`^traefik\.[a-zA-Z0-9_.-]{1,240}$`)
)

type libpodPort struct {
	HostIP        string `json:"host_ip"`
	ContainerPort int    `json:"container_port"`
	HostPort      int    `json:"host_port"`
	Range         int    `json:"range"`
	Protocol      string `json:"protocol"`
}

type libpodContainer struct {
	ID       string            `json:"Id"`
	Names    []string          `json:"Names"`
	State    string            `json:"State"`
	Labels   map[string]string `json:"Labels"`
	Ports    []libpodPort      `json:"Ports"`
	Networks []string          `json:"Networks"`
	Pod      string            `json:"Pod"`
	PodName  string            `json:"PodName"`
	IsInfra  bool              `json:"IsInfra"`
}

// Libpod states differ from Docker's; unknown states fail closed.
var states = map[string]string{"created": "created", "configured": "created", "initialized": "created", "running": "running",
	"paused": "paused", "stopping": "running", "stopped": "exited", "exited": "exited", "removing": "removing"}

// Parse validates a libpod container list. Labels other than safe Traefik
// routing labels are dropped; values are never echoed in errors.
func Parse(raw []byte, source string, routing composediscovery.Context) (composediscovery.Snapshot, error) {
	var rows []libpodContainer
	if platform.DecodeBounded(raw, maxBytes, 8, &rows) != nil || rows == nil || len(rows) > 500 {
		return composediscovery.Snapshot{}, platform.ErrUnavailable
	}
	out := composediscovery.Snapshot{Source: source, Fixture: source == "podman-fixture", Services: []composediscovery.Service{}, Traefik: routing}
	seen := map[string]bool{}
	for _, row := range rows {
		if !idRE.MatchString(row.ID) || seen[row.ID] || len(row.Names) != 1 || len(row.Labels) > 512 || len(row.Ports) > 64 || len(row.Networks) > 64 {
			return composediscovery.Snapshot{}, platform.ErrUnavailable
		}
		seen[row.ID] = true
		// Pod infra (pause) containers are an implementation detail of pods,
		// never an application workload.
		if row.IsInfra {
			continue
		}
		state, ok := states[row.State]
		if !ok || !nameRE.MatchString(row.Names[0]) {
			return composediscovery.Snapshot{}, platform.ErrUnavailable
		}
		s := composediscovery.Service{ID: row.ID, Container: row.Names[0], State: state, Ports: []composediscovery.Port{}, Networks: []string{}, Labels: map[string]string{}}
		project, service, err := composeIdentity(row.Labels)
		if err != nil {
			return composediscovery.Snapshot{}, err
		}
		s.Compose = project != "" && service != ""
		if s.Compose {
			s.Project, s.Service = project, service
		}
		for k, v := range row.Labels {
			if len(k) > 256 || len(v) > 4096 {
				return composediscovery.Snapshot{}, platform.ErrUnavailable
			}
			if strings.HasPrefix(k, "traefik.") {
				safe := composediscovery.SafeLabels(map[string]string{k: v})
				if _, ok := safe[k]; ok && keyRE.MatchString(k) {
					s.Labels[k] = v
				} else {
					s.HiddenTraefikSettings = true
				}
			}
		}
		portSeen := map[string]bool{}
		for _, p := range row.Ports {
			n := max(p.Range, 1)
			if p.ContainerPort < 1 || n > 256 || p.ContainerPort+n-1 > 65535 || (p.Protocol != "tcp" && p.Protocol != "udp" && p.Protocol != "sctp") {
				return composediscovery.Snapshot{}, platform.ErrUnavailable
			}
			// host_port/host_ip are published host bindings and deliberately ignored.
			for port := p.ContainerPort; port < p.ContainerPort+n; port++ {
				key := strconv.Itoa(port) + "/" + p.Protocol
				if !portSeen[key] {
					portSeen[key] = true
					s.Ports = append(s.Ports, composediscovery.Port{Port: port, Protocol: p.Protocol})
				}
			}
		}
		if len(s.Ports) > 256 {
			return composediscovery.Snapshot{}, platform.ErrUnavailable
		}
		slices.SortFunc(s.Ports, func(a, b composediscovery.Port) int {
			if a.Port == b.Port {
				return strings.Compare(a.Protocol, b.Protocol)
			}
			return a.Port - b.Port
		})
		for _, n := range row.Networks {
			if !nameRE.MatchString(n) || slices.Contains(s.Networks, n) {
				return composediscovery.Snapshot{}, platform.ErrUnavailable
			}
			s.Networks = append(s.Networks, n)
		}
		slices.Sort(s.Networks)
		// Pod members share the pod's network namespace, and rootless containers
		// without a named network (pasta/slirp4netns) cannot share a network with
		// Traefik. Neither is a supported label-routing target.
		if row.Pod != "" || len(s.Networks) == 0 {
			s.UnsupportedNetworking = true
		}
		out.Services = append(out.Services, s)
	}
	slices.SortFunc(out.Services, func(a, b composediscovery.Service) int { return strings.Compare(a.ID, b.ID) })
	composediscovery.CountReplicas(out.Services)
	return out, nil
}

// podman-compose writes io.podman.compose.* and usually com.docker.compose.*;
// docker-compose against the Podman socket writes only com.docker.compose.*.
// If both are present they must agree.
func composeIdentity(labels map[string]string) (string, string, error) {
	pp, ps := labels["io.podman.compose.project"], labels["io.podman.compose.service"]
	dp, ds := labels["com.docker.compose.project"], labels["com.docker.compose.service"]
	if (pp != "" || ps != "") && (dp != "" || ds != "") && (pp != dp || ps != ds) {
		return "", "", platform.ErrUnavailable
	}
	project, service := pp, ps
	if project == "" && service == "" {
		project, service = dp, ds
	}
	if (project != "" || service != "") && (!nameRE.MatchString(project) || !nameRE.MatchString(service)) {
		return "", "", platform.ErrUnavailable
	}
	return project, service, nil
}

//go:embed testdata/libpod-containers.json
var fixtureRows []byte

// Fixture is labeled demonstration data parsed through the real parser.
type Fixture struct{}

func (Fixture) Snapshot(context.Context) (composediscovery.Snapshot, error) {
	disabled := false
	return Parse(fixtureRows, "podman-fixture", composediscovery.Context{Networks: []string{"hc-traefik-test"}, Entrypoints: []string{"web"}, Resolvers: []string{}, DockerProvider: &disabled})
}

// Warnings are Podman-specific caveats attached to every Podman preview.
func Warnings(mode platform.Mode) []platform.Issue {
	w := []platform.Issue{{Code: "podman_provider", Message: "Traefik reads Podman labels through its Docker provider pointed at a Podman API socket; that endpoint must be configured and reviewed separately."}}
	if mode == platform.ModeRootless {
		w = append(w, platform.Issue{Code: "podman_rootless_network", Message: "Rootless Podman networks are per-user; Traefik must run under the same user and share a named network."})
	}
	return w
}
