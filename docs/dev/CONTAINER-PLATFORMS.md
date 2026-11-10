<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors
SPDX-License-Identifier: GPL-3.0-or-later
-->
# Container platform registry and adapters (development)

Status: development foundation, fixture-backed. Only the Docker Metadata Broker connection is a real transport. Podman and Kubernetes/K3s have parsers and fixtures but **no live transport**. Apply/deploy does not exist.

## Packages

| Package | Role |
|---|---|
| `backend/internal/platform` | Contract: kinds, distributions, modes, auth mechanisms, capability schema, normalized `Ref`/`Workload`/`Service`/`Network`, `Adapter`/`Previewer`, immutable `Registry`, CORE-side inventory validation, `DecodeBounded` (duplicate-key/depth/size-checked JSON). |
| `platform/composeadapter` | Wraps any `composediscovery.Provider` (Docker broker client, direct rootless reader, fixtures, Podman parser). Previews delegate to the verified `composediscovery.Build`. |
| `platform/podman` | libpod `containers/json` parser: libpod states, port ranges, pods/infra containers, `io.podman.compose.*` vs `com.docker.compose.*` labels. Labeled fixture. |
| `platform/kubernetes` | Shared Kubernetes/K3s adapter over a `Source` (fixed GET path allowlist, namespace-scoped). Detects K3s from `gitVersion`, Traefik IngressClasses, `traefik.io/v1alpha1`, Gateway API GatewayClasses/Gateways. Previews Ingress, IngressRoute or HTTPRoute. Labeled K3s fixture. |

## Rules enforced in code

- Registrations come only from `hc-api` startup flags (`-docker-metadata-*`, `-compose-discovery-fixture`, `-platform-fixtures`). There is no API to add, change or remove one.
- Each family has its own scope: `docker.compose.read`, `podman.read`, `kubernetes.read`. Listing also needs `platforms.read`; previews also need `networking.proxy.read`. Unknown and unauthorized platforms both return 404.
- Browser views carry an endpoint *label* and a fingerprint, never socket paths, URLs, tokens or kubeconfigs.
- A rootful daemon registration requires `AcknowledgeRootEquivalent`. No rootful transport is wired.
- CORE re-validates adapter output: every resource carries the platform ID, names and counts are bounded, duplicates and undeclared capabilities fail closed, and `routing.apply` is always listed as unsupported.
- Compose labels are never generated for Kubernetes. Kubernetes manifests are never generated for container platforms. Every preview has `applyAvailable: false` and `deployable: false`, and lists the checks it does not perform.

## API

- `GET /api/v1/platforms`
- `GET /api/v1/platforms/{id}/inventory`
- `POST /api/v1/networking/reverse-proxy/traefik/platform-preview`

These are documented in `schemas/openapi/homelab-control.v1.yaml`. The Phase 6/7 Compose endpoints are unchanged, and the UI keeps using them for Docker platforms.
