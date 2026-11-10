// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package kubernetes

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"homelab-control/backend/internal/platform"
	"homelab-control/backend/internal/traefikclient"
)

const managedBy = "homelab-control-preview"

// Preview builds one Kubernetes-native routing resource for a Service that
// selects the chosen workload. The resource kind follows the detected provider;
// nothing is created, and the Service and workload are never modified.
func (a *Adapter) Preview(ctx context.Context, inv platform.Inventory, req platform.PreviewRequest, routers traefikclient.Inventory) (platform.Preview, error) {
	if _, err := req.Model.Labels(); err != nil || req.Network != "" || !slices.Contains(inv.Capabilities.Detected, req.Target) {
		return platform.Preview{}, platform.ErrInvalid
	}
	switch req.Target {
	case platform.CapPreviewIngress, platform.CapPreviewIngressRoute, platform.CapPreviewHTTPRoute:
	default:
		return platform.Preview{}, platform.ErrUnsupported
	}
	// Re-read so preview and conflict checks use current metadata.
	st, err := a.collect(ctx, inv.Platform)
	if err != nil {
		return platform.Preview{}, err
	}
	var workload *platform.Workload
	for i := range st.workloads {
		if st.workloads[i].Ref.Key() == req.Workload {
			workload = &st.workloads[i]
		}
	}
	var service *platform.Service
	for i := range st.services {
		if st.services[i].Ref.Key() == req.Service {
			service = &st.services[i]
		}
	}
	if workload == nil || service == nil || service.Ref.Namespace != workload.Ref.Namespace {
		return platform.Preview{}, platform.ErrInvalid
	}
	m := req.Model
	if !slices.Contains(a.opts.Entrypoints, m.Entrypoint) {
		return platform.Preview{}, platform.ErrInvalid
	}
	resolver := ""
	if m.TLS != nil {
		resolver = m.TLS.CertificateResolver
	}
	if resolver != "" && !slices.Contains(a.opts.Resolvers, resolver) {
		return platform.Preview{}, platform.ErrInvalid
	}
	ns := workload.Ref.Namespace
	svcRef := service.Ref
	out := platform.Preview{SchemaVersion: 1, Target: req.Target, Format: "kubernetes-manifests", Workload: workload.Ref, Service: &svcRef,
		Manifests: []any{}, Warnings: []platform.Issue{}, Blockers: []platform.Issue{}, Validation: platform.BaseValidation()}
	warn := func(code, msg string) { out.Warnings = append(out.Warnings, platform.Issue{Code: code, Message: msg}) }
	block := func(code, msg string) { out.Blockers = append(out.Blockers, platform.Issue{Code: code, Message: msg}) }
	if inv.Fixture {
		warn("fixture", "Demonstration cluster data; these are not your Kubernetes workloads.")
	}
	warn("preview_only", "Preview only. Nothing is created in the cluster; applying requires a separately approved operation.")
	if !slices.ContainsFunc(service.Selects, func(r platform.Ref) bool { return r.Key() == workload.Ref.Key() }) {
		block("service_not_selecting", "The selected Service does not select this workload's pods.")
	}
	if service.Type == "ExternalName" {
		block("service_external_name", "ExternalName Services are not supported routing targets.")
	}
	var port *platform.Port
	for i := range service.Ports {
		if service.Ports[i].Port == m.Port && service.Ports[i].Protocol == "tcp" {
			port = &service.Ports[i]
		}
	}
	if port == nil {
		block("port_missing", "Select a TCP port exposed by the Service; container and node ports are not used directly.")
	} else {
		target := port.Target
		resolved := false
		for _, wp := range workload.Ports {
			if wp.Protocol == "tcp" && ((target == "" && wp.Port == port.Port) || target == strconv.Itoa(wp.Port) || (target != "" && target == wp.Name)) {
				resolved = true
			}
		}
		if !resolved {
			warn("target_port_unverified", "The Service target port is not declared by the workload's containers; verify the application listens on it.")
		}
	}
	if workload.State != "ready" {
		warn("workload_not_ready", "The workload does not report all replicas ready.")
	}
	if m.UpstreamScheme == "https" && req.Target != platform.CapPreviewIngressRoute {
		block("upstream_https_unsupported", "HTTPS to the backend needs Service annotations or a BackendTLSPolicy, which this preview does not change. Use an IngressRoute or review manually.")
	}
	if m.Protocol == "https" {
		warn("tls_review", "Incoming TLS is independent of upstream encryption; certificate issuance is not established by this preview.")
		if resolver == "" {
			warn("certificate_default", "No resolver selected: the default certificate only.")
		}
	}
	if st.legacyTraefik {
		warn("legacy_traefik_crds", "Legacy traefik.containo.us resources exist; they are not inspected for conflicts.")
	}
	// Conflicts: cluster routes in allowed namespaces and live Traefik routers.
	if owner, ok := st.hosts[m.Host]; ok {
		block("hostname_duplicate", "Hostname is already routed by "+owner+".")
	}
	for k := range st.hosts {
		if strings.HasPrefix(k, "\x00complex:") {
			warn("complex_rule", "Some IngressRoutes use rules that cannot be compared by this single-host model.")
			break
		}
	}
	for _, r := range routers.Routers {
		if mm := hostMatch.FindStringSubmatch(r.Rule); len(mm) == 2 && mm[1] == m.Host {
			block("hostname_duplicate", "Hostname already exists in Traefik inventory.")
		}
	}
	labels := map[string]any{"app.kubernetes.io/managed-by": managedBy}
	backend := map[string]any{"name": service.Ref.Name, "port": m.Port}
	var manifest map[string]any
	switch req.Target {
	case platform.CapPreviewIngress:
		class, ok := pick(st.ingressClasses, req.RouteClass)
		if !ok {
			return platform.Preview{}, platform.ErrInvalid
		}
		if st.names[ns+"/Ingress/"+m.Name] {
			block("name_duplicate", "An Ingress with this name already exists in the namespace.")
		}
		ann := map[string]any{"traefik.ingress.kubernetes.io/router.entrypoints": m.Entrypoint}
		if m.Protocol == "https" {
			ann["traefik.ingress.kubernetes.io/router.tls"] = "true"
			if resolver != "" {
				ann["traefik.ingress.kubernetes.io/router.tls.certresolver"] = resolver
			}
		}
		manifest = map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "Ingress",
			"metadata": map[string]any{"name": m.Name, "namespace": ns, "labels": labels, "annotations": ann},
			"spec": map[string]any{"ingressClassName": class, "rules": []any{map[string]any{"host": m.Host, "http": map[string]any{"paths": []any{
				map[string]any{"path": "/", "pathType": "Prefix", "backend": map[string]any{"service": map[string]any{"name": service.Ref.Name, "port": map[string]any{"number": m.Port}}}}}}}}}}
	case platform.CapPreviewIngressRoute:
		if st.names[ns+"/IngressRoute/"+m.Name] {
			block("name_duplicate", "An IngressRoute with this name already exists in the namespace.")
		}
		if m.UpstreamScheme == "https" {
			backend["scheme"] = "https"
		}
		spec := map[string]any{"entryPoints": []any{m.Entrypoint}, "routes": []any{map[string]any{"match": "Host(`" + m.Host + "`)", "kind": "Rule", "services": []any{backend}}}}
		if m.Protocol == "https" {
			tls := map[string]any{}
			if resolver != "" {
				tls["certResolver"] = resolver
			}
			spec["tls"] = tls
		}
		manifest = map[string]any{"apiVersion": "traefik.io/v1alpha1", "kind": "IngressRoute",
			"metadata": map[string]any{"name": m.Name, "namespace": ns, "labels": labels}, "spec": spec}
	case platform.CapPreviewHTTPRoute:
		gw, ok := pick(st.gateways, req.RouteClass)
		if !ok {
			return platform.Preview{}, platform.ErrInvalid
		}
		gwNS, gwName, _ := strings.Cut(gw, "/")
		if st.names[ns+"/HTTPRoute/"+m.Name] {
			block("name_duplicate", "An HTTPRoute with this name already exists in the namespace.")
		}
		warn("gateway_listener", "Entrypoint and TLS come from the Gateway's listeners; the selected entrypoint and resolver are not encoded in an HTTPRoute.")
		if gwNS != ns {
			warn("gateway_cross_namespace", "The Gateway is in another namespace; its listeners must allow routes from this namespace.")
		}
		manifest = map[string]any{"apiVersion": "gateway.networking.k8s.io/v1", "kind": "HTTPRoute",
			"metadata": map[string]any{"name": m.Name, "namespace": ns, "labels": labels},
			"spec": map[string]any{"parentRefs": []any{map[string]any{"name": gwName, "namespace": gwNS}}, "hostnames": []any{m.Host},
				"rules": []any{map[string]any{"backendRefs": []any{backend}}}}}
	}
	out.Manifests = append(out.Manifests, manifest)
	return out, nil
}

// pick returns the requested candidate, or the only one when none is named.
func pick(candidates []string, want string) (string, bool) {
	if want == "" {
		if len(candidates) == 1 {
			return candidates[0], true
		}
		return "", false
	}
	return want, slices.Contains(candidates, want)
}
