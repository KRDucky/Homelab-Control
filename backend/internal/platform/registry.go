// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sync"
	"time"
)

// Registration is created only from administrator startup configuration.
// ConnectionRef names the server-side connection configuration; it is hashed
// into the public fingerprint and never returned.
type Registration struct {
	ID            string
	DisplayName   string
	ConnectionRef string
	Adapter       Adapter
	// AcknowledgeRootEquivalent must be set to register a rootful daemon: its
	// socket grants root-equivalent host authority even for read requests.
	AcknowledgeRootEquivalent bool
}

type Health struct {
	Status    string `json:"status"` // unknown | available | unavailable
	CheckedAt string `json:"checkedAt,omitempty"`
	Reason    string `json:"reason"`
}

// Policy is the per-platform authorization policy enforced by CORE.
type Policy struct {
	ReadScope    string `json:"readScope"`
	PreviewScope string `json:"previewScope"`
	Apply        string `json:"apply"`
}

// PublicView is everything a browser may learn about a registered platform.
type PublicView struct {
	ID           string       `json:"id"`
	DisplayName  string       `json:"displayName"`
	Descriptor                // kind, distribution, mode, auth, endpoint, fixture
	Capabilities Capabilities `json:"capabilities"`
	Health       Health       `json:"health"`
	Policy       Policy       `json:"policy"`
}

type entry struct {
	reg    Registration
	desc   Descriptor
	mu     sync.Mutex
	health Health
	caps   Capabilities
}

// Registry is immutable after construction: there is no runtime registration
// API, so no browser or extension can add a socket or endpoint.
type Registry struct {
	entries []*entry
	byID    map[string]*entry
	now     func() time.Time
	timeout time.Duration
}

// Scopes is the caller's effective permission check (session scopes).
type Scopes func(scope string) bool

func NewRegistry(regs ...Registration) (*Registry, error) {
	r := &Registry{byID: map[string]*entry{}, now: time.Now, timeout: 6 * time.Second}
	if len(regs) > 32 {
		return nil, ErrInvalid
	}
	fingerprints := map[string]bool{}
	for _, reg := range regs {
		if !ValidID(reg.ID) || r.byID[reg.ID] != nil || !labelRE.MatchString(reg.DisplayName) || reg.Adapter == nil || reg.ConnectionRef == "" || len(reg.ConnectionRef) > 512 {
			return nil, ErrInvalid
		}
		d := reg.Adapter.Descriptor()
		if ScopeFor(d.Kind) == "" || !labelRE.MatchString(d.Endpoint.Label) || d.Endpoint.Transport == "" {
			return nil, ErrInvalid
		}
		switch d.Mode {
		case ModeRootless, ModeCluster:
		case ModeRootful:
			if !reg.AcknowledgeRootEquivalent {
				return nil, ErrInvalid
			}
		default:
			return nil, ErrInvalid
		}
		if (d.Kind == KindKubernetes) != (d.Mode == ModeCluster) || (d.Fixture != (d.Auth == AuthFixture)) {
			return nil, ErrInvalid
		}
		sum := sha256.Sum256([]byte(string(d.Kind) + "\x00" + reg.ConnectionRef))
		d.Endpoint.Fingerprint = "sha256:" + hex.EncodeToString(sum[:8])
		// One connection must not be registered twice under different IDs:
		// that would split its audit trail and health state.
		if fingerprints[d.Endpoint.Fingerprint] {
			return nil, ErrInvalid
		}
		fingerprints[d.Endpoint.Fingerprint] = true
		declared := cleanCaps(reg.Adapter.Declared())
		if slices.Contains(declared, CapApply) {
			return nil, ErrInvalid
		}
		e := &entry{reg: reg, desc: d, health: Health{Status: "unknown", Reason: "Not checked yet; inventory reads verify connectivity."},
			caps: Capabilities{Declared: declared, Detected: []Capability{}, Unsupported: []Unsupported{{CapApply, "Apply/deploy is not implemented and requires separate approval."}}}}
		r.entries = append(r.entries, e)
		r.byID[reg.ID] = e
	}
	return r, nil
}

func cleanCaps(in []Capability) []Capability {
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

func (e *entry) view() PublicView {
	e.mu.Lock()
	defer e.mu.Unlock()
	caps := Capabilities{Declared: slices.Clone(e.caps.Declared), Detected: slices.Clone(e.caps.Detected), Unsupported: slices.Clone(e.caps.Unsupported)}
	scope := ScopeFor(e.desc.Kind)
	return PublicView{ID: e.reg.ID, DisplayName: e.reg.DisplayName, Descriptor: e.desc, Capabilities: caps, Health: e.health,
		Policy: Policy{ReadScope: scope, PreviewScope: "networking.proxy.read+" + scope, Apply: "unavailable"}}
}

func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.entries)
}

// Visible lists only platforms whose platform-specific scope the caller holds.
func (r *Registry) Visible(has Scopes) []PublicView {
	out := []PublicView{}
	if r == nil {
		return out
	}
	for _, e := range r.entries {
		if has(ScopeFor(e.desc.Kind)) {
			out = append(out, e.view())
		}
	}
	return out
}

// AutoSelect returns the single visible platform that declares any of the
// wanted capabilities; with zero or several candidates the user must choose.
func AutoSelect(views []PublicView, wanted ...Capability) (string, bool) {
	id, n := "", 0
	for _, v := range views {
		for _, c := range wanted {
			if slices.Contains(v.Capabilities.Declared, c) {
				id, n = v.ID, n+1
				break
			}
		}
	}
	return id, n == 1
}

// lookup fails identically for unknown and unauthorized platforms so the
// response never reveals which connections exist.
func (r *Registry) lookup(id string, has Scopes) (*entry, error) {
	if r == nil {
		return nil, ErrNotFound
	}
	e := r.byID[id]
	if e == nil || !has(ScopeFor(e.desc.Kind)) {
		return nil, ErrNotFound
	}
	return e, nil
}

func (r *Registry) View(id string, has Scopes) (PublicView, error) {
	e, err := r.lookup(id, has)
	if err != nil {
		return PublicView{}, err
	}
	return e.view(), nil
}

// Inventory reads, bounds and validates one platform's inventory, and records
// health and detected capabilities. Malformed adapter output fails closed.
func (r *Registry) Inventory(ctx context.Context, id string, has Scopes) (Inventory, PublicView, error) {
	e, err := r.lookup(id, has)
	if err != nil {
		return Inventory{}, PublicView{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	inv, err := e.reg.Adapter.Inventory(ctx, id)
	if err == nil {
		err = validateInventory(&inv, id, e.desc, e.caps.Declared)
	}
	e.mu.Lock()
	e.health.CheckedAt = r.now().UTC().Format(time.RFC3339)
	if err != nil {
		e.health.Status, e.health.Reason = "unavailable", "Metadata read failed or returned invalid data."
		e.caps.Detected = []Capability{}
	} else {
		e.health.Status, e.health.Reason = "available", "Last inventory read succeeded."
		e.caps.Detected = slices.Clone(inv.Capabilities.Detected)
		e.caps.Unsupported = slices.Clone(inv.Capabilities.Unsupported)
	}
	e.mu.Unlock()
	if err != nil {
		return Inventory{}, e.view(), ErrUnavailable
	}
	return inv, e.view(), nil
}

// Previewer returns the adapter's preview implementation if it has one.
func (r *Registry) Previewer(id string, has Scopes) (Previewer, error) {
	e, err := r.lookup(id, has)
	if err != nil {
		return nil, err
	}
	p, ok := e.reg.Adapter.(Previewer)
	if !ok {
		return nil, ErrUnsupported
	}
	return p, nil
}

const (
	maxWorkloads = 1000
	maxServices  = 1000
	maxNetworks  = 256
)

// validateInventory is CORE's independent check of adapter output: every
// resource carries this platform's ID, names and counts are bounded, and the
// adapter cannot claim capabilities it did not declare (or apply at all).
func validateInventory(inv *Inventory, id string, d Descriptor, declared []Capability) error {
	if inv.Platform != id || inv.Fixture != d.Fixture || len(inv.Workloads) > maxWorkloads || len(inv.Services) > maxServices || len(inv.Networks) > maxNetworks || len(inv.Namespaces) > maxNetworks {
		return ErrUnavailable
	}
	if inv.Workloads == nil {
		inv.Workloads = []Workload{}
	}
	if inv.Services == nil {
		inv.Services = []Service{}
	}
	if inv.Networks == nil {
		inv.Networks = []Network{}
	}
	if inv.Namespaces == nil {
		inv.Namespaces = []string{}
	}
	if inv.Routing == nil {
		inv.Routing = []RoutingProvider{}
	}
	if inv.Entrypoints == nil {
		inv.Entrypoints = []string{}
	}
	if inv.Resolvers == nil {
		inv.Resolvers = []string{}
	}
	seen := map[string]bool{}
	checkRef := func(ref Ref) bool {
		if !validRef(ref, id) || seen[ref.Key()] {
			return false
		}
		seen[ref.Key()] = true
		return true
	}
	ports := func(ps []Port) bool {
		if len(ps) > 256 {
			return false
		}
		for _, p := range ps {
			if p.Port < 1 || p.Port > 65535 || (p.Protocol != "tcp" && p.Protocol != "udp" && p.Protocol != "sctp") || len(p.Name) > 63 || len(p.Target) > 63 {
				return false
			}
		}
		return true
	}
	for i := range inv.Workloads {
		w := &inv.Workloads[i]
		if !checkRef(w.Ref) || w.Ref.Kind == "Service" || w.Ref.Kind == "network" || w.Ref.Kind == "namespace" || !ports(w.Ports) || len(w.Networks) > 64 || len(w.Services) > 64 || len(w.RoutingLabels) > 512 || w.Replicas < 0 || w.ReadyReplicas < 0 || w.Replicas > 10000 {
			return ErrUnavailable
		}
		for _, s := range w.Services {
			if !validRef(s, id) || s.Kind != "Service" {
				return ErrUnavailable
			}
		}
		if w.Ports == nil {
			w.Ports = []Port{}
		}
		if w.Networks == nil {
			w.Networks = []string{}
		}
		if w.Services == nil {
			w.Services = []Ref{}
		}
		if w.RoutingLabels == nil {
			w.RoutingLabels = map[string]string{}
		}
		if w.Issues == nil {
			w.Issues = []string{}
		}
	}
	for i := range inv.Services {
		s := &inv.Services[i]
		if !checkRef(s.Ref) || s.Ref.Kind != "Service" || !ports(s.Ports) || len(s.Selects) > 64 {
			return ErrUnavailable
		}
		if s.Selects == nil {
			s.Selects = []Ref{}
		}
	}
	for _, n := range inv.Networks {
		if !checkRef(n.Ref) || (n.Ref.Kind != "network" && n.Ref.Kind != "namespace") {
			return ErrUnavailable
		}
	}
	for _, c := range inv.Capabilities.Detected {
		if c == CapApply || !slices.Contains(declared, c) {
			return ErrUnavailable
		}
	}
	inv.Capabilities.Declared = slices.Clone(declared)
	if inv.Capabilities.Detected == nil {
		inv.Capabilities.Detected = []Capability{}
	}
	hasApply := false
	for _, u := range inv.Capabilities.Unsupported {
		hasApply = hasApply || u.Capability == CapApply
	}
	if !hasApply {
		inv.Capabilities.Unsupported = append(inv.Capabilities.Unsupported, Unsupported{CapApply, "Apply/deploy is not implemented and requires separate approval."})
	}
	return nil
}

// BaseValidation is shared by every preview: it lists what a preview does not
// establish, so the UI never implies deployability.
func BaseValidation() []Validation {
	return []Validation{
		{"provider-availability", "not-verified-by-preview"},
		{"namespace-or-network-placement", "not-verified-by-preview"},
		{"routing-conflicts-at-apply-time", "not-verified-by-preview"},
		{"tls-certificate-issuance", "not-verified-by-preview"},
		{"network-reachability", "not-verified-by-preview"},
	}
}
