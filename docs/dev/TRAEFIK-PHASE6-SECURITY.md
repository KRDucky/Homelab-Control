<!-- SPDX-FileCopyrightText: 2026 The Homelab Control Authors -->
<!-- SPDX-License-Identifier: GPL-3.0-or-later -->
# Phase 6 read-only discovery and source verification design

## Disabled Docker adapter

`composediscovery.NewDocker` implements the existing Provider contract. It is NOT
wired to CLI startup, default configuration or a runtime socket mount. CORE's
fixture switch remains the only enabled development option. Request authorization
remains `docker.compose.read`; preview additionally requires `networking.proxy.read`,
session, CSRF/Origin and audit. No browser chooses a Docker endpoint or socket path.

The adapter accepts one explicit administrator-selected Unix socket, expected
nonzero owner UID and separately verified Traefik routing context. It rejects TCP,
relative/unclean paths, the rootful default path, root ownership, symlinks,
non-sockets and other-readable/writable sockets, and rechecks before each dial.
It ignores DOCKER_HOST, does not execute the Docker CLI, follows no redirects,
has no environment proxy, and makes only GET
`/v1.53/containers/json?all=true&size=false`. No inspect, exec, logs, archive,
create/update/start/stop/delete endpoints or filesystem source access exist.

Bounds: 4 seconds overall (caller cancellation honored), 1-second dial,
2-second response headers, 16 KiB response headers, 4 MiB body, 500 containers,
512 labels/container, 256 port entries/container and 64 networks/container.
Malformed/oversized/truncated/duplicate metadata fails closed with a generic
error, never daemon body/transport details. PrivatePort is authoritative;
PublicPort, command, mounts, network addresses and Compose source labels are
never returned. Only valid routing label keys/values are retained. Other Traefik
settings become a boolean manual-review blocker; auth hashes never reach the DTO.
Metadata errors prevent a preview rather than silently claiming completeness.
No port scanning: absent Docker-reported internal ports stay absent.

Compose identity requires both project and service labels. Standalone containers
remain distinguished. Labels are metadata assertions, not cryptographic evidence
of Compose ownership. One-off jobs are unsupported deployment targets. Existing
preview detects replicated-service ambiguity, network/label/hostname conflicts,
missing ports and unknown or disabled Docker provider context. Provider, resolver,
network and entrypoint context must come from separately approved configuration;
container labels never prove these capabilities. The present fixture is file-only,
so Docker label proposals must remain blocked with provider_disabled.

## Required live-access review (no permission changes applied)

Raw Docker socket access grants broad daemon authority even if bind-mounted `ro`.
The adapter's allowlist limits honest code, not a compromised process. Socket
ownership also does not prove rootless mode. Verify MediaMouse's rootless context,
endpoint and daemon security options independently before any deployment.

Recommended future boundary: a separate minimal management broker on the host as
the rootless daemon owner (ian UID 1000), using this adapter and exposing only a
fixed sanitized metadata snapshot over a private authenticated Unix interface.
Do not put the raw socket into the browser-facing CORE or Traefik helper. CORE
retains RBAC/CSRF/audit and authorizes each broker read. Peer credentials, dedicated
identity, socket directory permissions, request limits, broker authentication and
scope/audit propagation need a separate concrete deployment review. Do not enable
the existing privileged system-management broker just for this purpose.

Existing runtime UID 1000 maps to host UID 100999, while the rootless daemon socket
belongs to host UID 1000. These are different identities: do not grant a socket ACL,
add groups, switch to root or weaken permissions as an implicit workaround.
This phase makes no such changes. There is no operational live-discovery flag yet.

## Future Compose source resolver (design only)

1. Administrator explicitly registers a project ID, canonical approved root,
   relative compose/override files and expected owner. No paths are accepted from
   the browser or trusted solely because they appear in Docker labels. Label hints
   may only be displayed for administrator reconciliation, never opened.
2. A separate resolver opens relative paths using directory FDs and beneath-root,
   no-symlink traversal (Linux openat2 RESOLVE_BENEATH/NO_SYMLINKS or an audited
   equivalent), rejecting devices, FIFOs, sockets, hardlink ambiguity, traversal,
   unsafe ownership and unapproved writable directories. Validate every component;
   string-prefix and realpath-only checks do not stop races. Bound files/counts.
3. Record device/inode, owner, mode, size, content SHA-256, project registration and
   ordered source/override set. Verify source service identity against registered
   Compose project/service and current metadata. Labels alone are insufficient.
   Resolve interpolation/includes/extends only under explicit approved policy;
   never silently read arbitrary env files or run Compose/shell to discover paths.
4. Parse using a round-trip YAML representation preserving comments, anchors,
   scalar types, quoting, unrelated services/environment/volumes/networks/labels.
   Reject duplicate keys, ambiguous merge/override semantics, unsupported include
   graphs and unresolved interpolation instead of normalizing or guessing.
5. Bind preview to original bytes/hash and ordered file-set hash. Produce exact
   unified file diff plus structured changes and all validation warnings. Redact
   unrelated secret values from the UI diff; exact bytes stay in protected resolver
   storage. Users must know when a redacted diff is incomplete. No browser YAML is
   authoritative.
6. Future apply (outside this phase) reopens/verifies identity, hashes and policy,
   uses exclusive per-project coordination and rejects concurrent edits or changed
   Docker/project metadata. Require fresh approval scoped to the exact diff digest.
   Preserve protected versioned backups, ownership/mode/ACLs; fsync staged file,
   atomic same-filesystem replace, directory fsync, and record before/after audit.
   Multi-file atomicity and crash recovery need explicit journal design.
7. Deployment/rollback is a distinct approval-controlled component. Validate the
   proposed model before deployment, record deployment state/health and verify
   rollback preconditions; never overwrite subsequent user edits automatically.
   Backups are secrets: dedicated access controls, retention, encryption policy and
   tested restoration. No Apply/Deploy/Restart operations are implemented here.

## Git proposal (not executed)

Preserve the detached worktree's current HEAD and dirty/untracked files. Proposed
branch: `dev/traefik-phase6-preview`. After explicit review and collision checks,
`git switch -c dev/traefik-phase6-preview` in the existing worktree can name the
current HEAD without moving files. Do not switch to another existing baseline.
Review/stage explicit paths only; no git add -A. Proposed commits after approval:
(1) existing CORE/helper integration and Phase 5 UI, (2) disabled Docker discovery
contract/fixtures/tests, (3) isolated deployment harness/docs. The prior integration
is uncommitted, so review those paths together and use partial staging where needed.
No commit, branch creation, merge, fetch, rebase, reset or push in this phase.

## Primary references

- [Docker Engine API v1.53](https://docs.docker.com/reference/api/engine/version/v1.53/)
- [Rootless Docker operational tips](https://docs.docker.com/engine/security/rootless/tips/)

The adapter deliberately uses a versioned subset and fails on incompatibility;
there is no fallback that broadens requests or enables TCP.
