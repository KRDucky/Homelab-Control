<!-- SPDX-FileCopyrightText: 2026 The Homelab Control Authors -->
<!-- SPDX-License-Identifier: GPL-3.0-or-later -->
# Traefik proxy-host previews

Networking → Reverse Proxy → Traefik is a compiled CORE contribution. CORE owns login, CSRF, authorization, navigation, audit and Docker discovery. The separate non-root helper owns Traefik inventory, diagnostics and the Traefik label model. No Docker socket, shell execution or apply endpoint is added to the helper.

## Dashboard

Overview shows connection health, enabled routers, registered services, middleware and known configuration warnings. Proxy Hosts shows only verified inventory fields, with Unknown for absent hostname, target ownership, internal port or TLS information. Router identity does not prove a Compose application association. Services, Middleware, Settings and Diagnostics have read-only functionality. Certificates is explicitly planned and disabled. Forbidden CORE navigation entries are hidden; direct routes and APIs retain authoritative server authorization. The broker warning is omitted on the independent Traefik page, without enabling a broker.

## Discovery contract and opt-in fixture

`backend/internal/composediscovery.Provider` has one operation: `Snapshot(context.Context)`. It has no mutation, socket-path or arbitrary-command parameter. Metadata distinguishes Compose services from standalone containers and carries project, service, container, state, internal TCP/UDP ports, Docker networks and existing labels. Labels remain private on the server; browser DTOs contain only bounded routing-model label keys and validated values. Authentication middleware settings and unrelated application labels are never returned.

No live Docker adapter is implemented or enabled. Default discovery returns `503 discovery_unavailable`; missing `docker.compose.read` returns 403. For a separate future development run only, `-compose-discovery-fixture` enables hard-coded demonstration applications and requires `-dev-insecure-http` (loopback). Grant `docker.compose.read` and `networking.proxy.read` explicitly to a development account; neither scope is granted by default. Do not alter the currently running Phase 4 instance or its accounts in this phase.

The fixture is intentionally honest: the current test Traefik uses only a file provider. Therefore its Docker-label proposals are blocked with `provider_disabled`. A missing shared network is shown as a separate blocker; the proposal never attaches a container. A future approved adapter must verify Traefik network/provider/entrypoint/resolver context independently and must not infer Compose source-file paths solely from container labels. Discovery cannot substitute published host ports for internal application ports.

## Preview contract

GET `/api/v1/networking/reverse-proxy/traefik/applications` requires `docker.compose.read`, session, CSRF and audit. POST `/api/v1/networking/reverse-proxy/traefik/compose-preview` requires both scopes, valid Origin/CSRF, JSON with no unknown properties, configured discovery/helper, and successful inventory conflict checks.

Request:

```json
{"serviceId":"fixture-notes-web","network":"hc-traefik-test","model":{"name":"notes","host":"notes.example.com","port":8080,"entrypoint":"web","protocol":"http","upstreamScheme":"http"}}
```

HTTPS requires `"protocol":"https","tls":{}` or an explicitly selected existing `certificateResolver`. Incoming TLS is independent of `upstreamScheme`. Selecting upstream HTTPS does not disable certificate checks. No resolver or certificate is created; an existing/default certificate is unverified.

The backend validates the Traefik model and adds the selected Docker network label using trusted discovery context. It returns existing/proposed routing labels and network references plus individual path changes, warnings, blockers, `applyAvailable:false` and `sourceFileVerified:false`. It detects duplicate router/host labels, live inventory conflicts when supported rules exist, unknown/complex rule limitations, conflicting existing labels, missing ports/networks, standalone containers, replicated-service ambiguity and stale router TLS settings. The helper's existing `/labels` contract remains compatible.

This is a structured **partial service patch proposal**, not a whole-file YAML diff. Compose source is not loaded. Unrelated environment, volumes, labels, networks and application settings are preserved because there is no complete-service replacement and no write operation. Existing unrelated values are not exposed in a preview. Never write this partial DTO over a Compose file. Exact round-trip file preservation, source verification, approvals, deployment, rollback and audit history belong to a future Compose management component.

The simple form selects a Compose project/service/internal port, single hostname and HTTP/HTTPS. Advanced controls cover the validated shared router/service name, entrypoint, upstream scheme and network. Multiple hostnames, paths, middleware assignment, separate router/service names and TLS options remain future work, rather than unsupported controls.

## Development validation

Use the pinned Go/Node builder, without installing host packages:

```sh
docker --context rootless build --progress=plain --file dev/traefik-core.Dockerfile --target artifacts --output type=local,dest=/tmp/hc-traefik-phase5-artifacts .
```

It runs all Go tests/vet, React tests, TypeScript/Vite, ESLint/Prettier, SDK tests and the CORE build. It creates build cache and exported artifacts only, not running services. Do not tag over Phase 4 runtime images or recreate existing containers to view this work without separate approval.

Extension: `node test/traefik.test.mjs` in `plugins/traefik`. Harness: `python3 dev/traefik-live-test/test_plan.py` and `node --test dev/traefik-live-test/host-header.test.mjs` in Extensions. The Host probe now uses native HTTP with an explicit Host header; the browser uses `select[name="protocol"]`. The new dashboard associates the protocol select with a real `htmlFor` label, covered by React tests.

## Next approval boundary

Review the source changes and preview DTO first. Enabling real Docker discovery requires approval for a separate bounded metadata adapter with independent RBAC and safe fixed socket configuration. Deploying rebuilt CORE/helper assets, enabling fixture discovery, changing accounts, changing Traefik provider/network configuration or adding Compose apply operations requires separate approval. This phase performs none of those actions.

The read-only `dev/traefik-live-test/phase5-browser.py` runner in Extensions can
use the exported frontend assets in browser memory against the existing private
fixture. It passes real CORE authentication and API calls through unchanged,
checks missing discovery is disabled, inventory tabs, permission-aware navigation,
credential boundaries and mobile width, and exports a labeled screenshot. It does
not deploy assets, enable discovery or mock the API. It refuses to overwrite an
existing screenshot. The report distinguishes this proof from a deployed build.
