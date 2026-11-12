// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import "strings"

// Instance is a detected official Traefik proxy container. HomeControl CORE,
// plugin, and browser images are not Traefik.
type Instance struct {
	Present   bool   `json:"present"`
	Running   bool   `json:"running"`
	Container string `json:"container,omitempty"`
	Project   string `json:"project,omitempty"`
	Service   string `json:"service,omitempty"`
}

// OfficialTraefikImage is true when the last path segment of an image
// reference is exactly "traefik" (official image). Custom names such as
// traefik-core or traefik/whoami are rejected.
func OfficialTraefikImage(image string) bool {
	image = strings.TrimSpace(image)
	if image == "" || len(image) > 512 {
		return false
	}
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	base := image
	if i := strings.LastIndex(image, "/"); i >= 0 {
		base = image[i+1:]
	}
	if i := strings.LastIndex(base, ":"); i >= 0 {
		base = base[:i]
	}
	return strings.EqualFold(base, "traefik")
}

// IsTraefikProxy reports an official Traefik container or a Compose service
// named "traefik". Labelled applications are not Traefik.
func IsTraefikProxy(s Service) bool {
	if s.OfficialTraefik {
		return true
	}
	return s.Compose && strings.EqualFold(s.Service, "traefik")
}

// DetectInstance picks one Traefik proxy from discovery, preferring running.
func DetectInstance(services []Service) Instance {
	var found []Service
	for _, s := range services {
		if IsTraefikProxy(s) {
			found = append(found, s)
		}
	}
	if len(found) == 0 {
		return Instance{}
	}
	pick := found[0]
	for _, s := range found {
		if s.State == "running" {
			pick = s
			break
		}
	}
	return Instance{
		Present:   true,
		Running:   pick.State == "running",
		Container: pick.Container,
		Project:   pick.Project,
		Service:   pick.Service,
	}
}

// AttachInstance sets Traefik.Instance from discovered containers.
func AttachInstance(s *Snapshot) {
	if s == nil {
		return
	}
	s.Traefik.Instance = DetectInstance(s.Services)
}
