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
}
type Preview struct {
	Labels map[string]string `json:"labels"`
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
	router := "traefik.http.routers." + m.Name
	service := "traefik.http.services." + m.Name
	out := Preview{Labels: map[string]string{
		"traefik.enable": "true", router + ".rule": fmt.Sprintf("Host(`%s`)", m.Host),
		router + ".entrypoints": m.Entrypoint, router + ".service": m.Name, router + ".tls": strconv.FormatBool(m.Protocol == "https"),
		service + ".loadbalancer.server.port": strconv.Itoa(m.Port), service + ".loadbalancer.server.scheme": m.UpstreamScheme,
	}}
	if m.TLS != nil && m.TLS.CertificateResolver != "" {
		out.Labels[router+".tls.certresolver"] = m.TLS.CertificateResolver
	}
	return out, nil
}
