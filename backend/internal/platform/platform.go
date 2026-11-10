// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Package platform is CORE's container-platform abstraction: a registry of
// administrator-approved connections, a read-only adapter contract, normalized
// resource references that always carry their owning platform, and a capability
// schema that states what is supported, detected or deliberately unavailable.
//
// Nothing here executes programs, opens arbitrary endpoints or performs writes.
// Adapters receive their transport from startup configuration only; browsers and
// extensions can never supply socket paths, API URLs or credentials.
package platform

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"

	"homelab-control/backend/internal/traefikclient"
)

// Kind is the platform family. Distribution is the detected implementation.
type Kind string

const (
	KindDocker     Kind = "docker"
	KindPodman     Kind = "podman"
	KindKubernetes Kind = "kubernetes"
)

type Distribution string

const (
	DistDockerEngine Distribution = "docker-engine"
	DistPodman       Distribution = "podman"
	DistKubernetes   Distribution = "kubernetes"
	DistK3s          Distribution = "k3s"
	DistUnknown      Distribution = "unknown"
)

// Mode describes daemon/cluster privilege. Rootful daemons are root-equivalent
// and need an explicit administrator acknowledgement at registration.
type Mode string

const (
	ModeRootless Mode = "rootless"
	ModeRootful  Mode = "rootful"
	ModeCluster  Mode = "cluster"
)

// AuthMechanism names how CORE authenticates to the adapter's transport. The
// credential itself never leaves the server process.
type AuthMechanism string

const (
	AuthFixture             AuthMechanism = "fixture"
	AuthBrokerPeerToken     AuthMechanism = "broker-peer-uid-and-token"
	AuthKubernetesBearer    AuthMechanism = "kubernetes-scoped-bearer-token"
	AuthKubernetesClientTLS AuthMechanism = "kubernetes-client-certificate"
)

// Capability is a stable capability identifier. Every capability is read-only
// or preview-only; CapApply exists so the schema can state it is unavailable.
type Capability string

const (
	CapWorkloads           Capability = "workloads.read"
	CapServices            Capability = "services.read"
	CapPorts               Capability = "ports.read"
	CapNetworks            Capability = "networks.read"
	CapNamespaces          Capability = "namespaces.read"
	CapHealth              Capability = "health.read"
	CapPreviewComposeLabel Capability = "routing.preview.compose-labels"
	CapPreviewIngress      Capability = "routing.preview.kubernetes-ingress"
	CapPreviewIngressRoute Capability = "routing.preview.traefik-ingressroute"
	CapPreviewHTTPRoute    Capability = "routing.preview.gateway-httproute"
	CapApply               Capability = "routing.apply"
)

// PreviewCapabilities are the capabilities that make a platform eligible for
// the Traefik "Add Proxy Host" flow.
var PreviewCapabilities = []Capability{CapPreviewComposeLabel, CapPreviewIngress, CapPreviewIngressRoute, CapPreviewHTTPRoute}

type Unsupported struct {
	Capability Capability `json:"capability"`
	Reason     string     `json:"reason"`
}

// Capabilities separates what the adapter implements (declared), what the live
// or fixture metadata proved available (detected) and what is unavailable.
type Capabilities struct {
	Declared    []Capability  `json:"declared"`
	Detected    []Capability  `json:"detected"`
	Unsupported []Unsupported `json:"unsupported"`
}

func (c Capabilities) Has(cap Capability) bool { return slices.Contains(c.Detected, cap) }

// Endpoint is the browser-safe identity of a connection: its transport class,
// an administrator label and a fingerprint of the server-side reference. It
// never contains a socket path, URL, token, kubeconfig or certificate.
type Endpoint struct {
	Transport   string `json:"transport"`
	Label       string `json:"label"`
	Fingerprint string `json:"fingerprint"`
}

type Descriptor struct {
	Kind         Kind          `json:"kind"`
	Distribution Distribution  `json:"distribution"`
	Mode         Mode          `json:"mode"`
	Auth         AuthMechanism `json:"authentication"`
	Endpoint     Endpoint      `json:"endpoint"`
	Fixture      bool          `json:"fixture"`
}

// Ref is a normalized reference. Kind is the platform-native resource type
// (container, pod, Deployment, StatefulSet, DaemonSet, Service, network,
// namespace); UID is the platform-native identity.
type Ref struct {
	Platform  string `json:"platform"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}

// Key is the stable, opaque selection key used by previews.
func (r Ref) Key() string {
	ns := r.Namespace
	if ns == "" {
		ns = "-"
	}
	return r.Platform + "/" + r.Kind + "/" + ns + "/" + r.Name + "/" + r.UID
}

type Port struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Name     string `json:"name,omitempty"`
	// Target is the workload port a Kubernetes Service forwards to ("" when the
	// same as Port or unresolvable by name).
	Target string `json:"target,omitempty"`
}

type Workload struct {
	Ref Ref `json:"ref"`
	// Group is the Compose project, Podman pod or Kubernetes namespace.
	Group string `json:"group,omitempty"`
	// Component is the Compose service name, when applicable.
	Component     string   `json:"component,omitempty"`
	State         string   `json:"state"`
	Replicas      int      `json:"replicas"`
	ReadyReplicas int      `json:"readyReplicas"`
	Ports         []Port   `json:"ports"`
	Networks      []string `json:"networks"`
	// Services lists Kubernetes Services that select this workload.
	Services []Ref `json:"services"`
	// RoutingLabels are safe Traefik routing labels only; everything else stays
	// server-side. HiddenRouting reports unsupported or sensitive settings.
	RoutingLabels map[string]string `json:"routingLabels"`
	HiddenRouting bool              `json:"hiddenRouting"`
	Issues        []string          `json:"issues"`
	// Selector is server-side only (Kubernetes pod template labels).
	Selector map[string]string `json:"-"`
}

type Service struct {
	Ref      Ref               `json:"ref"`
	Type     string            `json:"type"`
	Ports    []Port            `json:"ports"`
	Selects  []Ref             `json:"selects"`
	Selector map[string]string `json:"-"`
}

type Network struct {
	Ref Ref `json:"ref"`
	// TraefikAttached means the administrator routing context lists it.
	TraefikAttached bool `json:"traefikAttached"`
}

// RoutingProvider is a detected routing integration. Available is nil when
// unknown; detection never implies a preview is deployable.
type RoutingProvider struct {
	Kind      string   `json:"kind"`
	Available *bool    `json:"available"`
	Classes   []string `json:"classes"`
	Detail    string   `json:"detail"`
}

type Inventory struct {
	Platform     string            `json:"platform"`
	Fixture      bool              `json:"fixture"`
	Distribution Distribution      `json:"distribution"`
	Version      string            `json:"version,omitempty"`
	Workloads    []Workload        `json:"workloads"`
	Services     []Service         `json:"services"`
	Networks     []Network         `json:"networks"`
	Namespaces   []string          `json:"namespaces"`
	Routing      []RoutingProvider `json:"routing"`
	Entrypoints  []string          `json:"entrypoints"`
	Resolvers    []string          `json:"resolvers"`
	Capabilities Capabilities      `json:"capabilities"`
}

// Adapter is the runtime-specific read contract. Implementations must be
// bounded, metadata-only and fail closed on malformed input. The platform ID
// is assigned by the registry and passed in, so resources always carry it.
type Adapter interface {
	Descriptor() Descriptor
	Declared() []Capability
	Inventory(ctx context.Context, platformID string) (Inventory, error)
}

// Previewer produces platform-native routing previews from a validated
// inventory. A preview is never deployable by itself.
type Previewer interface {
	Preview(ctx context.Context, inv Inventory, req PreviewRequest, routers traefikclient.Inventory) (Preview, error)
}

type PreviewRequest struct {
	Platform string     `json:"platform"`
	Target   Capability `json:"target"`
	Workload string     `json:"workload"`
	Service  string     `json:"service,omitempty"`
	Network  string     `json:"network,omitempty"`
	// RouteClass selects a detected IngressClass or Gateway ("namespace/name");
	// empty selects the only candidate.
	RouteClass            string              `json:"routeClass,omitempty"`
	ConfirmUnverifiedPort bool                `json:"confirmUnverifiedPort,omitempty"`
	Model                 traefikclient.Model `json:"model"`
}

type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Preview struct {
	SchemaVersion  int          `json:"schemaVersion"`
	Platform       PublicView   `json:"platform"`
	Target         Capability   `json:"target"`
	Format         string       `json:"format"`
	Workload       Ref          `json:"workload"`
	Service        *Ref         `json:"service,omitempty"`
	Compose        any          `json:"compose,omitempty"`
	Manifests      []any        `json:"manifests,omitempty"`
	Warnings       []Issue      `json:"warnings"`
	Blockers       []Issue      `json:"blockers"`
	ApplyAvailable bool         `json:"applyAvailable"`
	Deployable     bool         `json:"deployable"`
	Validation     []Validation `json:"validation"`
}

// Validation lists what a preview does NOT establish; each item must be
// verified separately before any future, separately approved apply.
type Validation struct {
	Check  string `json:"check"`
	Status string `json:"status"`
}

var (
	ErrNotFound    = errors.New("platform not found")
	ErrUnavailable = errors.New("platform metadata unavailable or invalid")
	ErrInvalid     = errors.New("invalid platform request")
	ErrUnsupported = errors.New("operation unsupported by platform")
)

var (
	idRE       = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)
	labelRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ()._/-]{0,63}$`)
	nameRE     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,252}$`)
	uidRE      = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
	nativeKind = map[string]bool{"container": true, "pod": true, "Deployment": true, "StatefulSet": true, "DaemonSet": true, "Service": true, "network": true, "namespace": true}
)

// ValidID reports whether an administrator-chosen platform ID is acceptable.
func ValidID(id string) bool { return idRE.MatchString(id) }

func validRef(r Ref, platformID string) bool {
	return r.Platform == platformID && nativeKind[r.Kind] && nameRE.MatchString(r.Name) && uidRE.MatchString(r.UID) &&
		(r.Namespace == "" || nameRE.MatchString(r.Namespace)) && !strings.Contains(r.Name+r.Namespace+r.UID, "/")
}

// ScopeFor returns the platform-specific read permission. Each family has its
// own scope so that, for example, Docker access never implies cluster access.
func ScopeFor(k Kind) string {
	switch k {
	case KindDocker:
		return "docker.compose.read"
	case KindPodman:
		return "podman.read"
	case KindKubernetes:
		return "kubernetes.read"
	}
	return ""
}
