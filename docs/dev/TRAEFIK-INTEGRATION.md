# Traefik CORE integration (development, not deployed)

CORE owns sessions, `networking.proxy.read`, module enablement, capability navigation
and the compiled React page at `/networking/reverse-proxy/traefik`. The separately
packaged Extensions helper owns GET-only Traefik API access and pure label previews.
There is no dynamic UI/code loading and no new executable `.hlplugin` format. PDK v1
remains metadata mapped to known compiled contributions; the authenticated server
integration is a compiled first-party `networking` module, not browser-local enablement.

## Configuration and contracts

Opt in using both `hc-api -traefik-helper-origin http://traefik-plugin:9080` and
`-traefik-helper-token-file /run/secrets/traefik-helper-token`. Missing paired config
or unsafe token fails startup. With neither flag the capability is unavailable.
The new permission is known but never granted by default; grant it through the
existing reviewed account administration workflow, never through a plugin manifest.
Administrator module configuration may disable `networking`, removing routes and nav.
The Networking module has no broker-operation prefix or privileged operations.

CORE routes beneath `/api/v1/networking/reverse-proxy/traefik`:

| Route | Method | Response |
| --- | --- | --- |
| `/inventory` | GET | schemaVersion=1, routers/services/middlewares identity/status arrays |
| `/diagnostics` | GET | schemaVersion=1, safe per-resource status/code/hint checks |
| `/labels` | POST | validated string-label preview mapping (optional PathPrefix) |
| `/compose-preview` | POST | labels plus file-provider YAML (`fileYAML`); `applyAvailable` when `-traefik-dynamic-dir` is set |
| `/file-apply` | POST | writes CORE-generated `<name>.yml` into that directory; requires `networking.proxy.apply` |
| `/compose-apply` | POST | patches Safe Traefik labels into an allowlisted Compose file; requires `networking.proxy.apply`; does not recreate containers |
| `/setup` | POST | writes official Traefik compose + static config when no Traefik container is present; 409 when one is already detected; `started` is false until a detected Arcane/Dockhand API or HomeControl's compose fallback can start the project; does not require the Traefik helper |

Every route requires the session and dedicated scope. GET/HEAD additionally requires
`X-CSRF-Token`; a present Origin must match. POST uses shared Origin/CSRF protection.
Audit availability is required; events omit hostnames, labels, helper credentials and
raw upstream errors. Unsupported query parameters are rejected. Preview JSON is limited
to 4096 bytes, rejects unknown fields and trailing documents, and validates the pure
model before helper access. Label responses must exactly match the independently
calculated CORE preview: a helper cannot inject extra Compose labels.

The typed Go client has fixed paths/methods, a five-second total timeout, limited
connections, four-MiB response limit, verified TLS and redirect refusal. Environment
HTTP proxies are disabled so private helper bearer tokens are not sent through a proxy.
Configured origin is trusted administrator input; no URL is accepted from the browser.
Inventory projects typed identity/status fields. Diagnostics hints are reconstructed
from known codes rather than forwarding helper-provided text. Error responses are
safe/generic; helper credentials never enter session/capability responses or React props.

Incoming HTTP/TLS and upstream HTTP/HTTPS are explicit independent fields. HTTPS
requires a TLS object, with optional existing certificate resolver. No automatic
redirect, entrypoint creation, certificate provisioning or verification bypass. An
explicit resolver reference can trigger issuance if the owner later applies labels.
Router TLS may replace entrypoint defaults: review options separately. File apply
writes CORE-generated YAML into `-traefik-dynamic-dir` only. Compose-label apply
(`-compose-apply-dir`) patches Safe Traefik keys on `services.<name>.labels` in an
allowlisted compose.yaml. Easy-setup (`-traefik-setup-dir`) writes official Traefik
compose (host HTTP 8880, 80/443 left free) only when discovery finds no official
Traefik container or Compose service named `traefik`. If Traefik is already present
the plugin configures that instance (labels and file YAML). This slice writes
compose files; it does not start or recreate containers.

HomeControl is a baremetal application. Docker Engine access is
`/var/run/docker.sock` or the rootless socket (`/run/user/<uid>/docker.sock`)
through the broker, not by adding `hc-api` to the `docker` group.

If Arcane or Dockhand is detected on the host, reverse-proxy plugins talk to
that tool's published API for compose edits and start/recreate (example: Arcane
`X-Api-Key` and `/api/environments/:id/projects`). If both are present, prefer
the manager that already owns the project's files. Do not vendor or copy those
products; Dockhand REST paths come from public docs only. HomeControl's own
Compose slice is a fallback: YAML editor, ENV editor, and
Start/Stop/Restart/Remove/Pull/Update. It is not feature parity.
`started: false` on `/setup` is the current stopgap until that handoff exists.

## Build and tests

Pinned Go 1.26.11, Node 22.23.3, npm 10.9.9 are asserted in
`dev/traefik-core.Dockerfile`. It installs only locked application dependencies inside
build stages, runs Go/backend, React/frontend and SDK tests, and builds embedded CORE.
Dockerfile-specific ignore excludes Git, local environment and development state.
No host packages or application services need starting.

```sh
# Requires approved outside-sandbox Docker execution on MediaMouse:
docker --context rootless build --progress=plain --file dev/traefik-core.Dockerfile \
  --target artifacts --output type=local,dest=/tmp/hc-traefik-core-artifacts .
```

Exported files include formatted source and binaries; no service runs automatically.
Inspect test/build report for actual results and unavailable checks. Go process tests
use isolated local test listeners/fixtures, not production services. The helper's
separate 20-test suite remains in Extensions and should be run for regression coverage.

## Concrete test deployment proposal — requires approval, not executed

Proposed names (must be confirmed against the actual owner-managed deployment):
network `hc-traefik-test`, helper service/alias `traefik-plugin`, user-managed Traefik
container `hc-traefik-owner-test`, Traefik network alias `traefik-test`, private API
origin `http://traefik-test:9081`. No helper or Traefik administrative port is published.
CORE test runtime must join this network; a host CORE process cannot be assumed to
route into a rootless internal bridge. The runtime CORE image/container is not created
by the artifact-only validation build.

1. Owner confirms container, alias, API port, version and API security before any start.
   Read-only commands for approval: `docker --context rootless network inspect hc-traefik-test`
   and a narrowly formatted inspect of the selected container's image/networks/port
   bindings. Require dedicated `Internal=true`, no unrelated members and no host API
   publication. Do not inspect full environment fields that might contain secrets.
2. If absent, proposed creation (requires separate approval):
   `docker --context rootless network create --internal hc-traefik-test`.
   Owner separately manages Traefik. Proposed attachment only after approval:
   `docker --context rootless network connect --alias traefik-test hc-traefik-test hc-traefik-owner-test`.
   Do not apply this to an unconfirmed production container.
3. Owner enables Traefik API using a dedicated private entrypoint, for example `hc-admin`
   on `:9081`, `api=true`, and a file-provider router on that entrypoint with a
   `PathPrefix(`/api`)` rule and `service: api@internal`. No Docker provider/socket is
   required for this test. No insecure public dashboard or API port publication.
   The helper currently has no upstream auth/custom-CA configuration; authenticated
   Traefik API requires that additional work before connecting. An isolated, private
   unauthenticated test API needs the owner's explicit agreement, never an implicit
   production security downgrade. No Traefik configuration was applied.
4. Verify the active rootless daemon UID/GID map via its host PID (`/proc/<pid>/uid_map`
   and `gid_map`) with approved read-only access. `/etc/subuid` ranges alone are not
   proof of the active map. Under the common 100000-start mapping, container UID 1000
   maps to host UID 100999, not host `ian` UID 1000.
5. Create a random token OUTSIDE both repositories/build contexts only after approving
   secret provisioning. Proposed dedicated directory
   `/home/ian/Projects/.homecontrol-dev-secrets/` mode 0700 and token mode 0600 owned by
   ian. Grant only the verified mapped runtime UID a read ACL on the dedicated file;
   do not make it world-readable or run the helper as root. File-backed Compose secret
   uid/gid/mode directives do not remap bind-file ownership. Both helper and CORE
   runtime need read access; a host CORE uses the owner's existing access. No mkdir,
   secret generation, chmod, chown or ACL command was executed for this plan.
6. Provide the Extensions Compose variables: network, private Traefik origin and actual
   token-file path. CORE gets its own paired flags and server-held file, not a browser
   setting. Review resolved Compose before approving any container start; do not run
   `compose up` during integration. A CORE development UI may later need an explicitly
   approved loopback-only binding; no external/public binding is proposed here.
7. Health checks should run inside the helper as UID 1000: read its token file and GET
   `http://127.0.0.1:9080/v1/diagnostics`, require schemaVersion=1 and status=available.
   Use a small Node check that reads the file in process; do not put token values in
   shell arguments, Compose YAML, health logs or environment. Timeout 5s, interval 30s,
   three retries. Do not add a new unauthenticated API just for a health probe.
8. Once a start is approved, verify runtime UID, immutable image ID, read-only mounts,
   secret readability without printing contents, network membership and absence of host
   bindings. Confirm all three inventories, bad-token 401, CORE unauthenticated 401,
   missing-scope/CSRF 403, disabled-module 404, safe helper-down 502, and no credential
   leak in responses. Exercise HTTP/TLS/resolver preview without applying labels.
   Disconnect/stop a test API only with owner approval; otherwise simulate errors in
   fixture tests. Browser/axe checks and real API compatibility are still necessary.

No deploy command is automatically executed by this document or the integration.

Proposed helper healthcheck fragment for review (not applied to Compose):

```yaml
healthcheck:
  test:
    - CMD
    - node
    - --input-type=module
    - -e
    - |
      import { readToken } from './src/secrets.mjs';
      try {
        const response = await fetch('http://127.0.0.1:9080/v1/diagnostics', {
          headers: { Authorization: 'Bearer ' + readToken(process.env.HC_HELPER_TOKEN_FILE) },
          signal: AbortSignal.timeout(4000)
        });
        const result = await response.json();
        process.exit(response.ok && result.schemaVersion === 1 && result.status === 'available' ? 0 : 1);
      } catch { process.exit(1); }
  interval: 30s
  timeout: 5s
  retries: 3
```

The command contains only code and a file-path variable, never a token value. It emits
no credentials. API unavailability is unhealthy, rather than a misleading success
because the helper process alone is alive. Use the previously verified helper image
ID `sha256:57fb5e12dd3c035ef7b06ee82eb318bdd292f25d90a609d0771de302df9f7f1c`
for the proposed test, then verify its actual presence/metadata before any approved start.

## Arcane compose-apply (slice 1)

When Arcane is configured (`ARCANE_API_KEY` plus `-arcane-url`, or a detected
container publishing 3552), `POST /networking/reverse-proxy/traefik/compose-apply`
writes the patched Compose content through Arcane's REST API (`via:"arcane"`)
instead of the local `-compose-apply-dir` file patch. Docker Settings checkboxes
(address, port, optional API key) are the product UX; this slice takes flags/env.
A configured URL is the source of truth and does not require the Arcane container
to appear in CORE's discovery snapshot.

- **Config:** `ARCANE_API_KEY` (env) enables the path; `-arcane-url` is the
  operator/Settings override (empty derives `http://127.0.0.1:<published 3552>`
  from a detected container). `-arcane-env` selects the environment id (default `0`).
- **Detection** (`backend/internal/arcanediscovery`): image last path segment `arcane`,
  `getarcaneapp/arcane`/`getarcaneapp/manager`, or container name `arcane`; published
  port 3552 is supporting evidence for the URL.
- **Matching:** list projects, GET each project's Compose, and pick the one whose service
  key or `container_name` equals the selected service and whose compose `name:` or project
  name equals the project (when set).
- **Patch:** the same stdlib-only `SafeLabels` rules as the file apply (`PatchLabelsText`).
  Only `composeContent` is PUT; `envContent` is never sent.
- **Fallback:** no matching project, a missing key, or an Arcane API error falls back to
  the `-compose-apply-dir` file patch. A conflicting existing label is still a 409.
- No Arcane code is vendored; the client is stdlib HTTP with redirects disabled and
  8 s GET / 15 s PUT timeouts. Response bodies are never logged (they may contain env).
