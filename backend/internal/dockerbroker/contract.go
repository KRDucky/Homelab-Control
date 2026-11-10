// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
// Package dockerbroker implements a metadata-only service contract. It is not a
// Docker API proxy and accepts no endpoints, commands, paths or authorization claims.
package dockerbroker

import (
	"bytes"
	"encoding/json"
	"errors"
	"homelab-control/backend/internal/composediscovery"
	"io"
	"regexp"
	"slices"
	"strings"
)

const SnapshotPath = "/v1/compose/snapshot"
const MaxBytes = 2 << 20

var ErrUnavailable = errors.New("Docker metadata broker unavailable or invalid response")
var name = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var id = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Envelope struct {
	SchemaVersion int                            `json:"schemaVersion"`
	Source        string                         `json:"source"`
	Services      []composediscovery.ServiceView `json:"services"`
}

// Validate is applied on BOTH sides. Unknown JSON fields are rejected by Decode;
// sensitive/unsupported labels are rejected, never silently passed to a browser.
func Validate(e Envelope) ([]composediscovery.Service, error) {
	if e.SchemaVersion != 1 || e.Source != "docker-rootless" || e.Services == nil || len(e.Services) > 500 {
		return nil, ErrUnavailable
	}
	out := []composediscovery.Service{}
	seen := map[string]bool{}
	for _, v := range e.Services {
		s := v.Service
		if s.ReplicaCount < 0 || s.ReplicaCount > 500 {
			return nil, ErrUnavailable
		}
		if !id.MatchString(s.ID) || seen[s.ID] || !name.MatchString(s.Container) || s.Ports == nil || len(s.Ports) > 256 || s.Networks == nil || len(s.Networks) > 64 || v.TraefikLabels == nil || len(v.TraefikLabels) > 512 {
			return nil, ErrUnavailable
		}
		seen[s.ID] = true
		switch s.State {
		case "created", "running", "paused", "restarting", "removing", "exited", "dead":
		default:
			return nil, ErrUnavailable
		}
		if s.Compose {
			if !name.MatchString(s.Project) || !name.MatchString(s.Service) {
				return nil, ErrUnavailable
			}
		} else if s.Project != "" || s.Service != "" {
			return nil, ErrUnavailable
		}
		ports := map[int]bool{}
		for _, p := range s.Ports {
			if ports[p.Port] {
				return nil, ErrUnavailable
			}
			ports[p.Port] = true
			if p.Protocol != "tcp" || p.Port < 1 || p.Port > 65535 {
				return nil, ErrUnavailable
			}
		}
		nets := map[string]bool{}
		for _, n := range s.Networks {
			if !name.MatchString(n) || nets[n] {
				return nil, ErrUnavailable
			}
			nets[n] = true
		}
		safe := composediscovery.SafeLabels(v.TraefikLabels)
		if len(safe) != len(v.TraefikLabels) {
			return nil, ErrUnavailable
		}
		for k, value := range v.TraefikLabels {
			if safe[k] != value {
				return nil, ErrUnavailable
			}
		}
		s.Labels = safe
		s.HiddenTraefikSettings = v.HiddenTraefikSettings
		s.Ports = slices.Clone(s.Ports)
		s.Networks = slices.Clone(s.Networks)
		out = append(out, s)
	}
	composediscovery.CountReplicas(out)
	return out, nil
}
func FromSnapshot(s composediscovery.Snapshot) (Envelope, error) {
	if s.Fixture || s.Source != "docker-rootless" {
		return Envelope{}, ErrUnavailable
	}
	catalog := composediscovery.View(s)
	// Only internal TCP ports belong in this broker contract.
	for i := range catalog.Services {
		ports := []composediscovery.Port{}
		for _, p := range catalog.Services[i].Ports {
			if p.Protocol == "tcp" {
				ports = append(ports, p)
			}
		}
		catalog.Services[i].Ports = ports
	}
	e := Envelope{1, "docker-rootless", catalog.Services}
	clean, err := Validate(e)
	if err != nil {
		return Envelope{}, err
	}
	for i := range e.Services {
		e.Services[i].ReplicaCount = clean[i].ReplicaCount
	}
	return e, nil
}
func Decode(b []byte) ([]composediscovery.Service, error) {
	if len(b) > MaxBytes || !uniqueJSONKeys(b) {
		return nil, ErrUnavailable
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var e Envelope
	if dec.Decode(&e) != nil {
		return nil, ErrUnavailable
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return nil, ErrUnavailable
	}
	// Broker labels are public routing references only, never auth material.
	return Validate(e)
}
func validToken(token string) bool {
	if len(token) < 32 || len(token) > 256 {
		return false
	}
	for _, c := range token {
		if c < 33 || c > 126 {
			return false
		}
	}
	return !strings.ContainsAny(token, "\r\n")
}

// Reject duplicate fields and deep JSON before typed decoding. Two parsers must
// never disagree over which authorization/schema or routing field is authoritative.
func uniqueJSONKeys(b []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(b))
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 16 {
			return false
		}
		token, e := dec.Token()
		if e != nil {
			return false
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for dec.More() {
				key, e := dec.Token()
				if e != nil {
					return false
				}
				k, ok := key.(string)
				if !ok || keys[k] {
					return false
				}
				keys[k] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, e := dec.Token()
			return e == nil && end == json.Delim('}')
		case '[':
			for dec.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, e := dec.Token()
			return e == nil && end == json.Delim(']')
		}
		return false
	}
	if !value(0) {
		return false
	}
	_, e := dec.Token()
	return e == io.EOF
}
