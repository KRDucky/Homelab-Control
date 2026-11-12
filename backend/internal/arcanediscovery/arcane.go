// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Package arcanediscovery detects an Arcane manager container from the same
// Docker discovery snapshot the Traefik integration uses, and patches a
// project's Compose content through Arcane's published REST API. It is the
// slice-1 "via:arcane" path; the local file patch remains the fallback.
//
// It never touches the Docker socket and never imports Arcane: the client is
// stdlib-only HTTP against the operator-configured or detected URL.
package arcanediscovery

import (
	"fmt"
	"strings"

	"homelab-control/backend/internal/composediscovery"
)

// ArcanePublishedPort is the host-published HTTP port Arcane's API listens on.
const ArcanePublishedPort = 3552

// Detection is the result of scanning a discovery snapshot for Arcane.
type Detection struct {
	Present   bool   `json:"present"`
	Container string `json:"container,omitempty"`
	// URL is derived from a host-published 3552 port ("http://127.0.0.1:3552"),
	// empty when the container does not publish that port. The operator's
	// -arcane-url, when set, always wins over this value.
	URL string `json:"url,omitempty"`
}

// OfficialArcaneImage is true when an image reference identifies the Arcane
// manager: last path segment "arcane", or the getarcaneapp/arcane or
// getarcaneapp/manager repository.
func OfficialArcaneImage(image string) bool {
	image = strings.TrimSpace(image)
	if image == "" || len(image) > 512 {
		return false
	}
	if lower := strings.ToLower(image); strings.Contains(lower, "getarcaneapp/arcane") || strings.Contains(lower, "getarcaneapp/manager") {
		return true
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
	return strings.EqualFold(base, "arcane")
}

// Detect scans a discovery snapshot for the Arcane manager container and
// derives its API URL from a host-published 3552 port when present.
func Detect(snapshot composediscovery.Snapshot) Detection {
	for _, s := range snapshot.Services {
		if !isArcane(s) {
			continue
		}
		d := Detection{Present: true, Container: s.Container}
		for _, p := range s.PublishedPorts {
			if p == ArcanePublishedPort {
				d.URL = fmt.Sprintf("http://127.0.0.1:%d", p)
				break
			}
		}
		return d
	}
	return Detection{}
}

func isArcane(s composediscovery.Service) bool {
	if OfficialArcaneImage(s.Image) {
		return true
	}
	if strings.EqualFold(s.Container, "arcane") {
		return true
	}
	return s.Compose && strings.EqualFold(s.Service, "arcane")
}
