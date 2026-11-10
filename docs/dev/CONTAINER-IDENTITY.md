# Container identity and permissions (Phase 9)

`backend/internal/containeridentity` is CORE's runtime-neutral model for **PUID, PGID, UMASK and
supplemental groups**. It is shared by every plugin and platform. It is not part of Traefik.

It resolves, validates and **previews**. It never applies anything, reads host files or contacts a
runtime.

## Resolution

| Mode | Layers used (lowest → highest) |
|---|---|
| `image` | none; the image's own behaviour applies, and HomeControl sets nothing |
| `inherit` | platform defaults → application defaults → profile |
| `custom` | platform defaults → application defaults → profile → per-container override |

- Each of UID, GID and UMASK comes from the highest layer that sets it. The response reports that
  layer for each value.
- `supplementalGroups` is **replaced** by the highest layer that sets it, never merged. An explicit
  `[]` means "no extra groups".
- Unset values keep the image's defaults. Values are never invented.

## Mechanisms (declared by the plugin manifest, never inferred)

`runtime.container.identity` in the existing v1 plugin manifest (`schemas/plugin/v1.json`):

| `method` | How identity is applied | Compose | Kubernetes |
|---|---|---|---|
| `env` | The image's entrypoint reads `uidVariable`/`gidVariable` | `environment` | container `env` |
| `runtime-user` | The runtime starts the process as uid:gid | `user: "uid:gid"` (both required) | container `securityContext.runAsUser/runAsGroup` (+ `runAsNonRoot`, no privilege escalation) |
| `fixed` | The image's identity cannot change (`configurable: false`) | nothing | nothing |

**UMASK** (`identity.umask.method`):
- `env`: the image documents a variable such as `UMASK` or `UMASK_SET`.
- `entrypoint`: a HomeControl-owned entrypoint applies it.
- `none`: not supported. An unsupported UMASK is **not injected**: a variable the image ignores would
  claim an effect that doesn't exist.

**Supplemental groups:**
- Compose: `group_add`.
- Kubernetes: **pod-level** `securityContext.supplementalGroups`, the only scope where the API
  defines it.

## Safety rules

- UID or GID 0 is never configured.
- Root group 0 is never added as a supplemental group.
- An unsupported mechanism is a **blocker** when the request or application sets it explicitly. It
  is a **warning** when it is only inherited from platform defaults; the image default then applies.
- **Privileged groups** are identified by the platform's configured host group names (`docker`,
  `lxd`, `sudo`, `adm`, `libvirt`, `kvm`, `disk`, `shadow`, …), never by assumed GIDs. They block
  unless the request explicitly acknowledges them, and they still warn afterwards.
- A supplemental GID that is **not** in the platform's configured group table produces a
  `group_unnamed` warning: HomeControl cannot tell whether it is privileged. Configure the host's
  group table to turn privileged GIDs into blockers.
- **Rootless platforms:** the administrator supplies the user-namespace map (`uidMap`/`gidMap`).
  - Unmapped IDs block.
  - An unknown map produces a warning, and no host IDs are reported.
- **Desired vs observed:**
  - Desired values are always separate from values the platform reports (`Workload.Identity`).
  - Kubernetes reports the identity declared in the pod template.
  - Docker and Podman report nothing yet, so the result is `not-reported`.
- **Volume checks:**
  - Host ownership comes only from a trusted host inspector. Without one, results are `unverified`.
  - Proposals are text for an administrator. HomeControl never runs chmod or chown.

## Profiles

`media-application`, `shared-storage`, `backup-service` and `read-only-monitoring` set a UMASK and
group **roles**. Each platform maps a role to a GID in its configuration; unmapped roles warn. Profiles
never contain host IDs.

## API (read-only; documented in OpenAPI)

**`GET /api/v1/containers/identity/profiles`** (`platforms.read`): returns the profiles, the modes
and the names of privileged groups.

**`POST /api/v1/containers/identity/preview`** (`platforms.read` plus the platform's family scope):
- **Body:** `platform`, `mode`, `runtime` (the manifest's `runtime.container`), `application`,
  `profile`, `override`, `acknowledgePrivilegedGroups`, and an optional `workload` reference.
- **Never accepted from clients:** host metadata, ID maps, group tables and observed identity. They
  come from administrator configuration and the adapters.
- **Response:** `applied` and `deployable` are always false.

## Administrator configuration (optional)

`hc-api -container-identity-config <file>`: a private regular file that is not group- or
other-writable, at most 64 KiB, and parsed strictly.

```json
{
  "schemaVersion": 1,
  "platforms": {
    "docker-local": {
      "defaults": { "uid": 2500, "gid": 2500, "umask": "022" },
      "mappingKnown": true,
      "uidMap": [{ "containerId": 0, "hostId": 2001, "size": 1 }, { "containerId": 1, "hostId": 300000, "size": 65536 }],
      "gidMap": [{ "containerId": 0, "hostId": 2001, "size": 1 }, { "containerId": 1, "hostId": 300000, "size": 65536 }],
      "groups": [{ "name": "docker", "gid": 970 }, { "name": "mediashare", "gid": 4100 }],
      "roles": { "media": 4100 }
    }
  }
}
```

- The values above are an example, not defaults.
- Platform IDs must be registered.
- Kind and mode always come from the registry, never from this file.
- `mappingKnown` with empty maps states explicitly that container IDs equal host IDs.

## Management boundary

`backend/internal/platform/management` defines the plan → approval → execute contract for future
deploy, restart, Compose and manifest changes.
- It uses separate `platforms.manage.<family>.<kind>` scopes. Discovery scopes never satisfy them.
- Only the `Disabled` executor exists, and no API route reaches it.
