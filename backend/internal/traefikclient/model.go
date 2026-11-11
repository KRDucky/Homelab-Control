// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Package traefikclient implements the fixed, read-only extension contract.
package traefikclient

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var pathPrefix = regexp.MustCompile(`^/[A-Za-z0-9._~/-]{0,200}$`)
var backendName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

type TLS struct {
	CertificateResolver string `json:"certificateResolver,omitempty"`
}
type Model struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Entrypoint     string `json:"entrypoint"`
	Protocol       string `json:"protocol"`
	UpstreamScheme string `json:"upstreamScheme"`
	TLS            *TLS   `json:"tls,omitempty"`
	Path           string `json:"path,omitempty"`
}
type Preview struct {
	Labels map[string]string `json:"labels"`
}
type FilePreview struct {
	File map[string]any `json:"file"`
	YAML string         `json:"yaml"`
}

func (m Model) Rule() string {
	host := fmt.Sprintf("Host(`%s`)", m.Host)
	if m.Path == "" {
		return host
	}
	return host + " && PathPrefix(`" + m.Path + "`)"
}

// Labels is also used to verify helper output cannot smuggle arbitrary labels.
func (m Model) Labels() (Preview, error) {
	bad := errors.New("invalid Traefik preview model")
	if !identifier.MatchString(m.Name) || !identifier.MatchString(m.Entrypoint) || m.Port < 1 || m.Port > 65535 || len(m.Host) > 253 || !strings.Contains(m.Host, ".") {
		return Preview{}, bad
	}
	for _, part := range strings.Split(m.Host, ".") {
		if !dnsLabel.MatchString(part) {
			return Preview{}, bad
		}
	}
	if (m.Protocol != "http" && m.Protocol != "https") || (m.UpstreamScheme != "http" && m.UpstreamScheme != "https") {
		return Preview{}, bad
	}
	if m.Protocol == "http" && m.TLS != nil {
		return Preview{}, bad
	}
	if m.Protocol == "https" && (m.TLS == nil || (m.TLS.CertificateResolver != "" && !identifier.MatchString(m.TLS.CertificateResolver))) {
		return Preview{}, bad
	}
	if m.Path != "" && (strings.Contains(m.Path, "..") || !pathPrefix.MatchString(m.Path)) {
		return Preview{}, bad
	}
	router := "traefik.http.routers." + m.Name
	service := "traefik.http.services." + m.Name
	out := Preview{Labels: map[string]string{
		"traefik.enable": "true", router + ".rule": m.Rule(),
		router + ".entrypoints": m.Entrypoint, router + ".service": m.Name, router + ".tls": strconv.FormatBool(m.Protocol == "https"),
		service + ".loadbalancer.server.port": strconv.Itoa(m.Port), service + ".loadbalancer.server.scheme": m.UpstreamScheme,
	}}
	if m.TLS != nil && m.TLS.CertificateResolver != "" {
		out.Labels[router+".tls.certresolver"] = m.TLS.CertificateResolver
	}
	return out, nil
}

// File is a Traefik file-provider fragment. backend is the Docker DNS name.
func (m Model) File(backend string) (FilePreview, error) {
	if _, err := m.Labels(); err != nil {
		return FilePreview{}, err
	}
	if !backendName.MatchString(backend) {
		return FilePreview{}, errors.New("invalid Traefik preview model")
	}
	router := map[string]any{
		"rule":        m.Rule(),
		"entryPoints": []string{m.Entrypoint},
		"service":     m.Name,
	}
	tlsLines := []string{}
	if m.Protocol == "https" {
		tls := map[string]any{}
		if m.TLS != nil && m.TLS.CertificateResolver != "" {
			tls["certResolver"] = m.TLS.CertificateResolver
			tlsLines = []string{"      tls:", "        certResolver: " + m.TLS.CertificateResolver}
		} else {
			tlsLines = []string{"      tls: {}"}
		}
		router["tls"] = tls
	}
	url := fmt.Sprintf("%s://%s:%d", m.UpstreamScheme, backend, m.Port)
	file := map[string]any{
		"http": map[string]any{
			"routers": map[string]any{m.Name: router},
			"services": map[string]any{
				m.Name: map[string]any{
					"loadBalancer": map[string]any{
						"servers": []any{map[string]any{"url": url}},
					},
				},
			},
		},
	}
	lines := []string{
		"http:",
		"  routers:",
		"    " + m.Name + ":",
		"      rule: '" + m.Rule() + "'",
		"      entryPoints:",
		"        - " + m.Entrypoint,
		"      service: " + m.Name,
	}
	lines = append(lines, tlsLines...)
	lines = append(lines,
		"  services:",
		"    "+m.Name+":",
		"      loadBalancer:",
		"        servers:",
		"          - url: "+url,
	)
	return FilePreview{File: file, YAML: strings.Join(lines, "\n")}, nil
}
