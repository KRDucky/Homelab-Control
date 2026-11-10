<!-- SPDX-FileCopyrightText: 2026 The Homelab Control Authors -->
<!-- SPDX-License-Identifier: GPL-3.0-or-later -->
# Docker metadata broker v1

The independently runnable `hc-docker-metadata` is reusable CORE infrastructure,
not a Traefik helper and not the privileged system-management broker. Its only
backend is the existing bounded rootless Docker list adapter. It accepts no
Docker paths, CLI arguments, shell commands, URLs or session-scope claims from an
API request. The current MediaMouse deployment is unchanged: the new broker and
CORE provider are implemented/tested but neither installed nor enabled live.

## Protocol and authentication

Only GET `/v1/compose/snapshot` and authenticated GET `/v1/health` exist. Requests
with query parameters, bodies, chunked payloads or unsupported methods/paths are
rejected. Linux SO_PEERCRED verifies the configured client host UID, and a separate
private bearer service token is checked in constant time. Both are required.
Browser/user-supplied X-Scopes/X-User headers are irrelevant. CORE authenticates
its current session and enforces docker.compose.read BEFORE calling the broker;
preview additionally needs networking.proxy.read, Origin/CSRF and audit. The
service credential establishes CORE service identity, not arbitrary browser claims.

Wire response: `{schemaVersion:1,source:"docker-rootless",services:[...]}`.
Services contain only ID/name, Compose project/service assertion, state, internal
TCP ports, network names, safe routing labels and unsupported-configuration flags.
Replica counts mean observed containers per project/service, not desired scale.
Both server and CORE validate IDs, arrays, state, Compose identity, ports, network
names and routing-label allowlist; CORE rejects unknown schema fields/version,
trailing JSON, duplicates and oversized responses and recomputes replica counts.
No environment, command, mount paths, credentials, IPs, config-file labels or raw
Traefik middleware/auth settings reach the browser. Unsupported Traefik settings
are a boolean review warning/blocker. Compose labels never establish source trust.

Broker: maximum four concurrent authorized requests, 1-second request-header,
2-second request-body and 5-second write timeout, 8 KiB request headers, 4-second
metadata deadline and 2 MiB response limit. Docker adapter: one fixed GET
`/v1.53/containers/json?all=true&size=false`, 1-second dial, 2-second response-header,
4-second overall, 16 KiB Docker headers/4 MiB Docker body, at most 500 containers,
512 labels/256 port records/64 networks per container. CORE client: 5-second RPC,
2-second headers/16 KiB, at most two connections, no redirects or environment
proxy. Errors never return Docker bodies, raw transport errors or tokens.

The broker's fixed-path metadata adapter has no mutation methods. The socket
transport is NOT a read-only Docker capability: a compromised broker process with
raw socket access can perform powerful operations against the user's entire
rootless daemon. Restricting a bind mount to ro does not prevent Docker mutations.

## Identity and filesystem design

Host broker runs as ian UID/GID 1000. CORE's non-root container UID 1000 maps to
host 100999, which is the broker's expected SO_PEERCRED client UID. In CORE's user
namespace the host broker UID 1000 maps to UID 0, so the CORE client expects server
UID/socket owner 0; this does NOT run the host broker as root. Client validates both
socket type/owner/other permissions and server peer UID before sending its token.
Actual cross-namespace credentials and ACL behavior still need approved live testing.

A new dedicated private transport directory and socket need narrowly scoped ACLs
for host UID 100999. CORE will mount only that directory read-only, never Docker's
socket, and separately mount its broker token read-only. No groups or existing
Docker socket ownership/ACLs change. Listener only creates a new socket in an
already provisioned private directory, refuses existing paths, and creates mode
0600; administrator deployment must grant the reviewed new-socket ACL after bind.
A restart creates a new inode and requires the same reviewed ACL setup again.
No code runs setfacl or arbitrary provisioning commands from the broker API.

Bearer credentials are loaded through the existing bounded/no-follow private-file
loader. Broker token is separate from helper token. It never appears in a response,
UI, sample environment variable or report. The client pins a trusted absolute path;
no browser-controlled endpoint is possible. A same-UID host process can access the
host account's token/daemon: UID 1000 is a trusted administration boundary, not
isolation between all programs running as ian. Ancestor directories and bind mount
sources must remain administrator-controlled; fixed path/peer checks alone do not
protect against a compromised account controlling those directories.

## CORE integration (default disabled)

```
-docker-metadata-socket /run/hc-metadata/broker.sock
-docker-metadata-token-file /run/secrets/docker_metadata_token
-docker-metadata-server-uid 0
-docker-metadata-routing-config /opt/live-test/docker-metadata-routing.json
```

These flags conflict with `-compose-discovery-fixture`; incomplete broker config
fails startup. No flag gives CORE a raw Docker socket. Optional routing config is
bounded, versioned and read-only; it provides separately verified administrator
network/entrypoint/resolver/provider context, never evidence derived from Docker
labels. Without it, no networks/entrypoints are assumed and previews cannot invent
them. `dev/docker-metadata-test/routing.example.json` honestly records the existing
file-provider fixture's Docker provider as false. It does not enable a provider.

Existing catalog route and DTO remain compatible. Capability response adds a
non-navigation compose_discovery descriptor: unavailable/forbidden/configured-
degraded, with actual readiness verified on catalog requests. This status never
forces a privileged broker online. Catalog failures return safe 503 and no stale
partial data; a stopped provider cannot produce a reassuring empty inventory.

## UI and previews

Running supported Compose containers are selectable, while stopped/unsupported
ones show their state and are disabled. Standalone containers are excluded from
the Compose editing form. Selected metadata displays observed replicas, networks
and approved existing labels. One known TCP port auto-selects; multiple ports
require selection. No known port requires an explicitly entered numeric port and
confirmation; the backend still marks it unverified/blocked rather than guessing
or substituting a published host port. Host/none/shared-container networking is
unsupported. Replica/source/label/network conflicts remain explicit blockers.

The primary review shows backend-produced application, hostname, target, internal
port, protocol, certificate resolver and network. Advanced Details contains labels,
network patch, exact key changes and JSON. Source remains unverified, apply is
always unavailable, and unrelated Compose configuration is never replaced/written.
An older live backend without summary fields shows Unknown for absent fields,
rather than inventing data. Preview-only fixture browsing is not real discovery.

## Future source resolver

Phase 6's `TRAEFIK-PHASE6-SECURITY.md` remains the source-resolution design. Require
explicit administrator-approved roots and ordered source/override file sets,
verified owner/device/inode/content identity, directory-FD/openat2 beneath-root
no-symlink traversal, bounds, and rejection of traversal/ambiguous include graphs.
Never open a path merely from Compose labels. Env/include inputs require separate
allowlists and secret redaction; do not invoke shells or export environment values.
Round-trip YAML must preserve comments/anchors/unrelated fields or reject unsupported
constructs, with before/after exact bytes and protected diff plus redacted UI view.
Optimistic content/file-set hashes and per-project coordination detect concurrent
edits. Future approval-bound apply needs protected backups, fsync/atomic staging,
crash journal for multi-file changes, audit and rollback that refuses later edits.
No resolver, Compose edit, Apply/Deploy/Restart or rollback is implemented here.

See the exact proposed host/service/mount/ACL deployment in
`/home/ian/Projects/codex-reports/docker-metadata-broker-deployment.md`. Do not
execute it until approved. Controlled Unix/Docker fixtures are not a claim of live
MediaMouse discovery or peer-identity compatibility.
