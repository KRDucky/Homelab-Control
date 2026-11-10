// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const dockerListPath = "/v1.53/containers/json?all=true&size=false"
const maxDockerBytes = 4 << 20

var ErrDockerUnavailable = errors.New("Docker discovery metadata unavailable or invalid")
var dockerName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var dockerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Docker is deliberately not wired to CLI/server startup. A separately reviewed
// CORE management deployment may inject it with WithComposeDiscovery. Socket
// ownership is not a read-only capability: the process must be isolated from UI
// and helper, and granted access only after reviewing that full daemon authority.
type Docker struct {
	client  *http.Client
	routing Context
}

// NewDocker accepts only an explicit administrator-selected local Unix socket.
// It never reads DOCKER_HOST, executes a CLI, proxies TCP, or discovers file paths.
// Constructing it performs no Docker request. The caller must separately verify
// the context/daemon is rootless; ownership alone does not prove rootless mode.
func NewDocker(socket string, ownerUID uint32, routing Context) (*Docker, error) {
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket || socket == "/var/run/docker.sock" || ownerUID == 0 {
		return nil, ErrDockerUnavailable
	}
	verify := func() error {
		st, e := os.Lstat(socket)
		if e != nil || st.Mode()&os.ModeSocket == 0 {
			return ErrDockerUnavailable
		}
		stat, ok := st.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != ownerUID || st.Mode().Perm()&0007 != 0 {
			return ErrDockerUnavailable
		}
		return nil
	}
	if verify() != nil {
		return nil, ErrDockerUnavailable
	}
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxConnsPerHost: 1, MaxResponseHeaderBytes: 16384, ResponseHeaderTimeout: 2 * time.Second, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if verify() != nil {
			return nil, ErrDockerUnavailable
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socket)
	}}
	return &Docker{client: &http.Client{Transport: tr, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrDockerUnavailable }}, routing: cloneRouting(routing)}, nil
}
func cloneRouting(c Context) Context {
	out := Context{Networks: append([]string{}, c.Networks...), Entrypoints: append([]string{}, c.Entrypoints...), Resolvers: append([]string{}, c.Resolvers...)}
	if c.DockerProvider != nil {
		v := *c.DockerProvider
		out.DockerProvider = &v
	}
	return out
}

type dockerContainer struct {
	ID     string `json:"Id"`
	Names  []string
	State  string
	Labels map[string]string
	Ports  []struct {
		PrivatePort int
		PublicPort  int
		Type        string
	}
	HostConfig      struct{ NetworkMode string }
	NetworkSettings struct{ Networks map[string]json.RawMessage }
}

func (d *Docker) Snapshot(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+dockerListPath, nil)
	if e != nil {
		return Snapshot{}, ErrDockerUnavailable
	}
	req.Header.Set("Accept", "application/json")
	resp, e := d.client.Do(req)
	if e != nil {
		return Snapshot{}, ErrDockerUnavailable
	}
	defer resp.Body.Close()
	media, _, mediaErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != 200 || resp.ContentLength > maxDockerBytes || mediaErr != nil || media != "application/json" {
		return Snapshot{}, ErrDockerUnavailable
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, maxDockerBytes+1))
	if e != nil || len(raw) > maxDockerBytes {
		return Snapshot{}, ErrDockerUnavailable
	}
	var rows []dockerContainer
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > 500 {
		return Snapshot{}, ErrDockerUnavailable
	}
	out := Snapshot{Source: "docker-rootless", Fixture: false, Services: []Service{}, Traefik: cloneRouting(d.routing)}
	seen := map[string]bool{}
	for _, row := range rows {
		if !dockerID.MatchString(row.ID) || seen[row.ID] || len(row.Names) != 1 || len(row.Labels) > 512 || len(row.Ports) > 256 || len(row.NetworkSettings.Networks) > 64 {
			return Snapshot{}, ErrDockerUnavailable
		}
		seen[row.ID] = true
		name := strings.TrimPrefix(row.Names[0], "/")
		if !dockerName.MatchString(name) {
			return Snapshot{}, ErrDockerUnavailable
		}
		switch row.State {
		case "created", "running", "paused", "restarting", "removing", "exited", "dead":
		default:
			return Snapshot{}, ErrDockerUnavailable
		}
		s := Service{ID: row.ID, Container: name, State: row.State, Ports: []Port{}, Networks: []string{}, Labels: map[string]string{}}
		project, service := row.Labels["com.docker.compose.project"], row.Labels["com.docker.compose.service"]
		if (project != "" || service != "") && (!dockerName.MatchString(project) || !dockerName.MatchString(service)) {
			return Snapshot{}, ErrDockerUnavailable
		}
		s.Compose = project != "" && service != ""
		if s.Compose {
			s.Project = project
			s.Service = service
		}
		// One-off Compose jobs are not safe service deployment targets. Retain their
		// identity, but mark as unsupported rather than silently treating them as apps.
		if oneoff := row.Labels["com.docker.compose.oneoff"]; oneoff != "" && !strings.EqualFold(oneoff, "false") {
			s.UnsupportedDeployment = true
		}
		for k, v := range row.Labels {
			if len(k) > 256 || len(v) > 4096 {
				return Snapshot{}, ErrDockerUnavailable
			}
			if strings.HasPrefix(k, "traefik.") {
				if safeKey.MatchString(k) && safeValue(k, v) {
					s.Labels[k] = v
				} else {
					s.HiddenTraefikSettings = true
				}
			}
		}
		if row.HostConfig.NetworkMode == "host" || row.HostConfig.NetworkMode == "none" || strings.HasPrefix(row.HostConfig.NetworkMode, "container:") {
			s.UnsupportedNetworking = true
		}
		// PublicPort is deliberately ignored. Only Docker's internal PrivatePort is used.
		portSeen := map[string]bool{}
		for _, p := range row.Ports {
			if p.PrivatePort < 1 || p.PrivatePort > 65535 || (p.Type != "tcp" && p.Type != "udp" && p.Type != "sctp") {
				return Snapshot{}, ErrDockerUnavailable
			}
			key := strconv.Itoa(p.PrivatePort) + "/" + p.Type
			if !portSeen[key] {
				s.Ports = append(s.Ports, Port{p.PrivatePort, p.Type})
				portSeen[key] = true
			}
		}
		slices.SortFunc(s.Ports, func(a, b Port) int {
			if a.Port == b.Port {
				return strings.Compare(a.Protocol, b.Protocol)
			}
			return a.Port - b.Port
		})
		for network := range row.NetworkSettings.Networks {
			if !dockerName.MatchString(network) {
				return Snapshot{}, ErrDockerUnavailable
			}
			s.Networks = append(s.Networks, network)
		}
		slices.Sort(s.Networks)
		out.Services = append(out.Services, s)
	}
	slices.SortFunc(out.Services, func(a, b Service) int { return strings.Compare(a.ID, b.ID) })
	CountReplicas(out.Services)
	return out, nil
}
