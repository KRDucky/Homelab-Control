// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Package kubernetes is the shared Kubernetes/K3s adapter. It reads a fixed,
// namespace-scoped set of GET paths through a Source supplied by CORE startup
// configuration. It never lists Secrets, ConfigMaps or Pods' environment, never
// keeps annotations, and needs no cluster-admin rights: the required RBAC is
// get/list on the listed resources in the allowed namespaces plus get/list on
// cluster-scoped IngressClasses and GatewayClasses. K3s is detected from the
// server version; there is no separate K3s code path.
package kubernetes

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"homelab-control/backend/internal/platform"
)

// Source performs one bounded, authenticated GET for an adapter-built path.
// Implementations must reject any path not produced by Paths().
type Source interface {
	Get(ctx context.Context, path string) ([]byte, error)
}

type Options struct {
	// Namespaces is the administrator allowlist; discovery never lists all.
	Namespaces    []string
	Entrypoints   []string
	Resolvers     []string
	Auth          platform.AuthMechanism
	Transport     string
	EndpointLabel string
}

type Adapter struct {
	src  Source
	opts Options
}

const maxBytes = 4 << 20

var (
	dns1123   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	subdomain = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	uidRE     = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
	versionRE = regexp.MustCompile(`^v[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}[0-9A-Za-z.+-]{0,64}$`)
	labelKey  = regexp.MustCompile(`^([a-z0-9.-]{1,253}/)?[A-Za-z0-9]([A-Za-z0-9_.-]{0,61}[A-Za-z0-9])?$`)
	labelVal  = regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9_.-]{0,61}[A-Za-z0-9])?)?$`)
	hostMatch = regexp.MustCompile("^Host\\(`([a-z0-9.-]+)`\\)$")
)

const (
	traefikIngressController = "traefik.io/ingress-controller"
	traefikGatewayController = "traefik.io/gateway-controller"
)

func New(src Source, o Options) (*Adapter, error) {
	if src == nil || o.Transport == "" || len(o.Namespaces) == 0 || len(o.Namespaces) > 32 {
		return nil, platform.ErrInvalid
	}
	for _, list := range [][]string{o.Namespaces, o.Entrypoints, o.Resolvers} {
		seen := map[string]bool{}
		for _, v := range list {
			if !dns1123.MatchString(v) || seen[v] {
				return nil, platform.ErrInvalid
			}
			seen[v] = true
		}
	}
	for _, ns := range o.Namespaces {
		// System namespaces are never application discovery targets.
		if ns == "kube-system" || ns == "kube-public" || ns == "kube-node-lease" {
			return nil, platform.ErrInvalid
		}
	}
	o.Namespaces = slices.Clone(o.Namespaces)
	slices.Sort(o.Namespaces)
	return &Adapter{src: src, opts: o}, nil
}

func (a *Adapter) Descriptor() platform.Descriptor {
	return platform.Descriptor{Kind: platform.KindKubernetes, Distribution: platform.DistUnknown, Mode: platform.ModeCluster, Auth: a.opts.Auth,
		Endpoint: platform.Endpoint{Transport: a.opts.Transport, Label: a.opts.EndpointLabel}, Fixture: a.opts.Auth == platform.AuthFixture}
}

func (a *Adapter) Declared() []platform.Capability {
	return []platform.Capability{platform.CapWorkloads, platform.CapServices, platform.CapPorts, platform.CapNamespaces, platform.CapHealth,
		platform.CapPreviewIngress, platform.CapPreviewIngressRoute, platform.CapPreviewHTTPRoute}
}

// Paths lists every GET path the adapter may request, for transport
// allowlisting and for the RBAC plan. Optional API groups are included.
func (a *Adapter) Paths() []string {
	out := []string{"/version", "/apis", "/apis/networking.k8s.io/v1/ingressclasses", "/apis/gateway.networking.k8s.io/v1/gatewayclasses"}
	for _, ns := range a.opts.Namespaces {
		out = append(out, nsPaths(ns, true, true)...)
	}
	return out
}

func nsPaths(ns string, gateway, traefik bool) []string {
	out := []string{
		"/apis/apps/v1/namespaces/" + ns + "/deployments",
		"/apis/apps/v1/namespaces/" + ns + "/statefulsets",
		"/apis/apps/v1/namespaces/" + ns + "/daemonsets",
		"/api/v1/namespaces/" + ns + "/services",
		"/apis/networking.k8s.io/v1/namespaces/" + ns + "/ingresses",
	}
	if gateway {
		out = append(out, "/apis/gateway.networking.k8s.io/v1/namespaces/"+ns+"/gateways", "/apis/gateway.networking.k8s.io/v1/namespaces/"+ns+"/httproutes")
	}
	if traefik {
		out = append(out, "/apis/traefik.io/v1alpha1/namespaces/"+ns+"/ingressroutes")
	}
	return out
}

// ---- wire types: only the fields the adapter validates and uses ----

type meta struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	UID       string            `json:"uid"`
	Labels    map[string]string `json:"labels"`
	// Annotations are decoded only to read the default-class marker; they are
	// never retained (they often contain applied configuration and secrets).
	Annotations map[string]string `json:"annotations"`
}

type containerPort struct {
	ContainerPort int    `json:"containerPort"`
	Name          string `json:"name"`
	Protocol      string `json:"protocol"`
}

type workloadItem struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Replicas *int `json:"replicas"`
		Selector struct {
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"selector"`
		Template struct {
			Metadata struct {
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Name  string          `json:"name"`
					Ports []containerPort `json:"ports"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas          int `json:"readyReplicas"`
		DesiredNumberScheduled int `json:"desiredNumberScheduled"`
		NumberReady            int `json:"numberReady"`
	} `json:"status"`
}

type intOrString struct {
	num int
	str string
}

func (v *intOrString) UnmarshalJSON(b []byte) error {
	if n, err := strconv.Atoi(string(b)); err == nil {
		v.num = n
		return nil
	}
	s, err := strconv.Unquote(string(b))
	if err != nil {
		return platform.ErrUnavailable
	}
	v.str = s
	return nil
}

type serviceItem struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Type     string            `json:"type"`
		Selector map[string]string `json:"selector"`
		Ports    []struct {
			Name       string      `json:"name"`
			Port       int         `json:"port"`
			TargetPort intOrString `json:"targetPort"`
			Protocol   string      `json:"protocol"`
		} `json:"ports"`
	} `json:"spec"`
}

type list[T any] struct {
	Items []T `json:"items"`
}

type classItem struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Controller     string `json:"controller"`
		ControllerName string `json:"controllerName"`
	} `json:"spec"`
}

type gatewayItem struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		GatewayClassName string `json:"gatewayClassName"`
	} `json:"spec"`
}

type ingressItem struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Rules []struct {
			Host string `json:"host"`
		} `json:"rules"`
	} `json:"spec"`
}

type httpRouteItem struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Hostnames []string `json:"hostnames"`
	} `json:"spec"`
}

type ingressRouteItem struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Routes []struct {
			Match string `json:"match"`
		} `json:"routes"`
	} `json:"spec"`
}

type apiGroups struct {
	Groups []struct {
		Name     string `json:"name"`
		Versions []struct {
			GroupVersion string `json:"groupVersion"`
		} `json:"versions"`
	} `json:"groups"`
}

// state is the adapter-internal validated view used by inventory and preview.
type state struct {
	version        string
	dist           platform.Distribution
	groups         map[string]bool
	workloads      []platform.Workload
	services       []platform.Service
	ingressClasses []string
	gateways       []string // namespace/name of Gateways using a Traefik class
	hosts          map[string]string
	names          map[string]bool // namespace/kind/name of existing routes
	legacyTraefik  bool
}

func (a *Adapter) get(ctx context.Context, path string, v any) error {
	b, err := a.src.Get(ctx, path)
	if err != nil {
		return platform.ErrUnavailable
	}
	return platform.DecodeBounded(b, maxBytes, 32, v)
}

func validMeta(m meta, ns string) bool {
	if !dns1123.MatchString(m.Name) || !uidRE.MatchString(m.UID) || m.Namespace != ns || len(m.Labels) > 64 || len(m.Annotations) > 128 {
		return false
	}
	for k, v := range m.Labels {
		if !labelKey.MatchString(k) || !labelVal.MatchString(v) {
			return false
		}
	}
	return true
}

func (a *Adapter) collect(ctx context.Context, id string) (*state, error) {
	st := &state{groups: map[string]bool{}, hosts: map[string]string{}, names: map[string]bool{}}
	var ver struct {
		GitVersion string `json:"gitVersion"`
	}
	if a.get(ctx, "/version", &ver) != nil || !versionRE.MatchString(ver.GitVersion) {
		return nil, platform.ErrUnavailable
	}
	st.version = ver.GitVersion
	st.dist = platform.DistKubernetes
	if strings.Contains(ver.GitVersion, "+k3s") {
		st.dist = platform.DistK3s
	}
	var groups apiGroups
	if a.get(ctx, "/apis", &groups) != nil || len(groups.Groups) > 512 {
		return nil, platform.ErrUnavailable
	}
	for _, g := range groups.Groups {
		for _, v := range g.Versions {
			st.groups[v.GroupVersion] = true
		}
		if g.Name == "traefik.containo.us" {
			st.legacyTraefik = true
		}
	}
	gateway := st.groups["gateway.networking.k8s.io/v1"]
	traefik := st.groups["traefik.io/v1alpha1"]
	if st.groups["networking.k8s.io/v1"] {
		var classes list[classItem]
		if a.get(ctx, "/apis/networking.k8s.io/v1/ingressclasses", &classes) != nil || len(classes.Items) > 64 {
			return nil, platform.ErrUnavailable
		}
		for _, c := range classes.Items {
			if !validMeta(c.Metadata, "") {
				return nil, platform.ErrUnavailable
			}
			if c.Spec.Controller == traefikIngressController {
				st.ingressClasses = append(st.ingressClasses, c.Metadata.Name)
			}
		}
	}
	traefikGatewayClasses := map[string]bool{}
	if gateway {
		var classes list[classItem]
		if a.get(ctx, "/apis/gateway.networking.k8s.io/v1/gatewayclasses", &classes) != nil || len(classes.Items) > 64 {
			return nil, platform.ErrUnavailable
		}
		for _, c := range classes.Items {
			if !validMeta(c.Metadata, "") {
				return nil, platform.ErrUnavailable
			}
			if c.Spec.ControllerName == traefikGatewayController {
				traefikGatewayClasses[c.Metadata.Name] = true
			}
		}
	}
	for _, ns := range a.opts.Namespaces {
		if err := a.collectNamespace(ctx, id, ns, gateway, traefik, traefikGatewayClasses, st); err != nil {
			return nil, err
		}
	}
	slices.Sort(st.ingressClasses)
	slices.Sort(st.gateways)
	// Link Services to the workloads they select (same namespace, non-empty
	// selector that is a subset of the pod template labels).
	for si := range st.services {
		s := &st.services[si]
		if len(s.Selector) == 0 {
			continue
		}
		for wi := range st.workloads {
			w := &st.workloads[wi]
			if w.Ref.Namespace != s.Ref.Namespace {
				continue
			}
			match := true
			for k, v := range s.Selector {
				if w.Selector[k] != v {
					match = false
					break
				}
			}
			if match {
				s.Selects = append(s.Selects, w.Ref)
				w.Services = append(w.Services, s.Ref)
			}
		}
	}
	return st, nil
}

func protocol(p string) (string, bool) {
	switch p {
	case "", "TCP":
		return "tcp", true
	case "UDP":
		return "udp", true
	case "SCTP":
		return "sctp", true
	}
	return "", false
}

func (a *Adapter) collectNamespace(ctx context.Context, id, ns string, gateway, traefik bool, gwClasses map[string]bool, st *state) error {
	for _, kind := range []string{"Deployment", "StatefulSet", "DaemonSet"} {
		var items list[workloadItem]
		if a.get(ctx, "/apis/apps/v1/namespaces/"+ns+"/"+strings.ToLower(kind)+"s", &items) != nil || len(items.Items) > 500 {
			return platform.ErrUnavailable
		}
		for _, it := range items.Items {
			if !validMeta(it.Metadata, ns) || len(it.Spec.Template.Spec.Containers) > 32 || len(it.Spec.Template.Metadata.Labels) > 64 {
				return platform.ErrUnavailable
			}
			w := platform.Workload{Ref: platform.Ref{Platform: id, Kind: kind, Namespace: ns, Name: it.Metadata.Name, UID: it.Metadata.UID}, Group: ns,
				Ports: []platform.Port{}, Networks: []string{ns}, Services: []platform.Ref{}, RoutingLabels: map[string]string{}, Issues: []string{}, Selector: map[string]string{}}
			for k, v := range it.Spec.Template.Metadata.Labels {
				if !labelKey.MatchString(k) || !labelVal.MatchString(v) {
					return platform.ErrUnavailable
				}
				w.Selector[k] = v
			}
			switch kind {
			case "DaemonSet":
				w.Replicas, w.ReadyReplicas = it.Status.DesiredNumberScheduled, it.Status.NumberReady
			default:
				w.Replicas = 1
				if it.Spec.Replicas != nil {
					w.Replicas = *it.Spec.Replicas
				}
				w.ReadyReplicas = it.Status.ReadyReplicas
			}
			if w.Replicas < 0 || w.Replicas > 10000 || w.ReadyReplicas < 0 || w.ReadyReplicas > 10000 {
				return platform.ErrUnavailable
			}
			switch {
			case w.Replicas == 0:
				w.State = "scaled-to-zero"
			case w.ReadyReplicas >= w.Replicas:
				w.State = "ready"
			case w.ReadyReplicas > 0:
				w.State = "degraded"
			default:
				w.State = "unavailable"
			}
			seen := map[string]bool{}
			for _, c := range it.Spec.Template.Spec.Containers {
				if len(c.Ports) > 64 {
					return platform.ErrUnavailable
				}
				for _, p := range c.Ports {
					proto, ok := protocol(p.Protocol)
					if !ok || p.ContainerPort < 1 || p.ContainerPort > 65535 || (p.Name != "" && !dns1123.MatchString(p.Name)) {
						return platform.ErrUnavailable
					}
					key := strconv.Itoa(p.ContainerPort) + "/" + proto
					if !seen[key] {
						seen[key] = true
						w.Ports = append(w.Ports, platform.Port{Port: p.ContainerPort, Protocol: proto, Name: p.Name})
					}
				}
			}
			slices.SortFunc(w.Ports, func(x, y platform.Port) int { return x.Port - y.Port })
			st.workloads = append(st.workloads, w)
		}
	}
	var services list[serviceItem]
	if a.get(ctx, "/api/v1/namespaces/"+ns+"/services", &services) != nil || len(services.Items) > 500 {
		return platform.ErrUnavailable
	}
	for _, it := range services.Items {
		if !validMeta(it.Metadata, ns) || len(it.Spec.Ports) > 64 || len(it.Spec.Selector) > 64 {
			return platform.ErrUnavailable
		}
		switch it.Spec.Type {
		case "", "ClusterIP", "NodePort", "LoadBalancer", "ExternalName":
		default:
			return platform.ErrUnavailable
		}
		s := platform.Service{Ref: platform.Ref{Platform: id, Kind: "Service", Namespace: ns, Name: it.Metadata.Name, UID: it.Metadata.UID}, Type: it.Spec.Type, Ports: []platform.Port{}, Selects: []platform.Ref{}, Selector: map[string]string{}}
		if s.Type == "" {
			s.Type = "ClusterIP"
		}
		if s.Type != "ExternalName" {
			for k, v := range it.Spec.Selector {
				if !labelKey.MatchString(k) || !labelVal.MatchString(v) {
					return platform.ErrUnavailable
				}
				s.Selector[k] = v
			}
		}
		for _, p := range it.Spec.Ports {
			proto, ok := protocol(p.Protocol)
			if !ok || p.Port < 1 || p.Port > 65535 || (p.Name != "" && !dns1123.MatchString(p.Name)) || p.TargetPort.num < 0 || p.TargetPort.num > 65535 || (p.TargetPort.str != "" && !dns1123.MatchString(p.TargetPort.str)) {
				return platform.ErrUnavailable
			}
			target := p.TargetPort.str
			if p.TargetPort.num > 0 && p.TargetPort.num != p.Port {
				target = strconv.Itoa(p.TargetPort.num)
			}
			s.Ports = append(s.Ports, platform.Port{Port: p.Port, Protocol: proto, Name: p.Name, Target: target})
		}
		st.services = append(st.services, s)
	}
	addHost := func(host, owner string) error {
		if host == "" {
			return nil
		}
		if !subdomain.MatchString(strings.TrimPrefix(host, "*.")) {
			return platform.ErrUnavailable
		}
		st.hosts[host] = owner
		return nil
	}
	var ingresses list[ingressItem]
	if st.groups["networking.k8s.io/v1"] && (a.get(ctx, "/apis/networking.k8s.io/v1/namespaces/"+ns+"/ingresses", &ingresses) != nil || len(ingresses.Items) > 500) {
		return platform.ErrUnavailable
	}
	for _, it := range ingresses.Items {
		if !validMeta(it.Metadata, ns) || len(it.Spec.Rules) > 64 {
			return platform.ErrUnavailable
		}
		st.names[ns+"/Ingress/"+it.Metadata.Name] = true
		for _, r := range it.Spec.Rules {
			if addHost(r.Host, ns+"/Ingress/"+it.Metadata.Name) != nil {
				return platform.ErrUnavailable
			}
		}
	}
	if gateway {
		var gws list[gatewayItem]
		if a.get(ctx, "/apis/gateway.networking.k8s.io/v1/namespaces/"+ns+"/gateways", &gws) != nil || len(gws.Items) > 64 {
			return platform.ErrUnavailable
		}
		for _, g := range gws.Items {
			if !validMeta(g.Metadata, ns) {
				return platform.ErrUnavailable
			}
			if gwClasses[g.Spec.GatewayClassName] {
				st.gateways = append(st.gateways, ns+"/"+g.Metadata.Name)
			}
		}
		var routes list[httpRouteItem]
		if a.get(ctx, "/apis/gateway.networking.k8s.io/v1/namespaces/"+ns+"/httproutes", &routes) != nil || len(routes.Items) > 500 {
			return platform.ErrUnavailable
		}
		for _, r := range routes.Items {
			if !validMeta(r.Metadata, ns) || len(r.Spec.Hostnames) > 64 {
				return platform.ErrUnavailable
			}
			st.names[ns+"/HTTPRoute/"+r.Metadata.Name] = true
			for _, h := range r.Spec.Hostnames {
				if addHost(h, ns+"/HTTPRoute/"+r.Metadata.Name) != nil {
					return platform.ErrUnavailable
				}
			}
		}
	}
	if traefik {
		var routes list[ingressRouteItem]
		if a.get(ctx, "/apis/traefik.io/v1alpha1/namespaces/"+ns+"/ingressroutes", &routes) != nil || len(routes.Items) > 500 {
			return platform.ErrUnavailable
		}
		for _, r := range routes.Items {
			if !validMeta(r.Metadata, ns) || len(r.Spec.Routes) > 64 {
				return platform.ErrUnavailable
			}
			st.names[ns+"/IngressRoute/"+r.Metadata.Name] = true
			for _, route := range r.Spec.Routes {
				if m := hostMatch.FindStringSubmatch(route.Match); len(m) == 2 {
					if addHost(m[1], ns+"/IngressRoute/"+r.Metadata.Name) != nil {
						return platform.ErrUnavailable
					}
				} else if route.Match != "" {
					st.hosts["\x00complex:"+ns+"/"+r.Metadata.Name] = ns + "/IngressRoute/" + r.Metadata.Name
				}
			}
		}
	}
	return nil
}

func boolp(v bool) *bool { return &v }

func (a *Adapter) Inventory(ctx context.Context, id string) (platform.Inventory, error) {
	st, err := a.collect(ctx, id)
	if err != nil {
		return platform.Inventory{}, err
	}
	inv := platform.Inventory{Platform: id, Fixture: a.opts.Auth == platform.AuthFixture, Distribution: st.dist, Version: st.version,
		Workloads: st.workloads, Services: st.services, Networks: []platform.Network{}, Namespaces: slices.Clone(a.opts.Namespaces),
		Entrypoints: slices.Clone(a.opts.Entrypoints), Resolvers: slices.Clone(a.opts.Resolvers)}
	for _, ns := range a.opts.Namespaces {
		inv.Networks = append(inv.Networks, platform.Network{Ref: platform.Ref{Platform: id, Kind: "namespace", Name: ns, UID: ns}})
	}
	caps := &inv.Capabilities
	caps.Detected = []platform.Capability{platform.CapWorkloads, platform.CapServices, platform.CapPorts, platform.CapNamespaces, platform.CapHealth}
	ingress := platform.RoutingProvider{Kind: "kubernetes-ingress", Available: boolp(len(st.ingressClasses) > 0), Classes: slices.Clone(st.ingressClasses), Detail: "IngressClasses with controller " + traefikIngressController + "."}
	route := platform.RoutingProvider{Kind: "traefik-ingressroute", Available: boolp(st.groups["traefik.io/v1alpha1"]), Classes: []string{}, Detail: "traefik.io/v1alpha1 API group served by the cluster."}
	gw := platform.RoutingProvider{Kind: "gateway-api-httproute", Available: boolp(len(st.gateways) > 0), Classes: slices.Clone(st.gateways), Detail: "Gateways in allowed namespaces whose GatewayClass uses " + traefikGatewayController + "."}
	if st.legacyTraefik {
		route.Detail += " Legacy traefik.containo.us resources are present and not supported by previews."
	}
	inv.Routing = []platform.RoutingProvider{ingress, route, gw}
	for _, p := range []struct {
		ok  bool
		cap platform.Capability
		why string
	}{{*ingress.Available, platform.CapPreviewIngress, "No IngressClass uses the Traefik ingress controller."},
		{*route.Available, platform.CapPreviewIngressRoute, "The traefik.io/v1alpha1 CRDs are not served."},
		{*gw.Available, platform.CapPreviewHTTPRoute, "No Gateway in an allowed namespace uses a Traefik GatewayClass."}} {
		if p.ok {
			caps.Detected = append(caps.Detected, p.cap)
		} else {
			caps.Unsupported = append(caps.Unsupported, platform.Unsupported{Capability: p.cap, Reason: p.why})
		}
	}
	caps.Unsupported = append(caps.Unsupported,
		platform.Unsupported{Capability: platform.CapPreviewComposeLabel, Reason: "Docker Compose labels are never generated for Kubernetes workloads."},
		platform.Unsupported{Capability: platform.CapNetworks, Reason: "Kubernetes uses namespaces and Services rather than attachable container networks."})
	return inv, nil
}

var _ platform.Adapter = (*Adapter)(nil)
var _ platform.Previewer = (*Adapter)(nil)
