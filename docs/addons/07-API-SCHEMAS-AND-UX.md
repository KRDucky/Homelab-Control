# API, Broker and UI Contract Draft

## UI routes (proposed)
`/shares` (overview), `/shares/new`, `/shares/:id`, `/remote-connections`, `/remote-mounts`, `/iscsi/targets`, `/iscsi/initiators`, `/identity/share-access`, `/proxy/hosts`, `/proxy/certificates`, `/proxy/ddns`, `/software/dependencies`. Do not claim routes exist until implemented.

## Read-only API sketches
`GET /api/v1/file-services/capabilities`, `GET /api/v1/shares`, `GET /api/v1/shares/{id}`, `GET /api/v1/remote-mounts`, `GET /api/v1/iscsi/sessions`, `GET /api/v1/proxy/providers`, `GET /api/v1/proxy/hosts`, `GET /api/v1/certificates`.

## Planning and execution
`POST /api/v1/plans` with `{kind, resource, desired_state, expected_revision}`; return `{plan_id, preview, warnings, dependencies, required_scopes, reversible_steps, irreversible_steps, expires_at}`. `POST /api/v1/jobs` accepts approved plan ID plus explicit confirmation token. Revalidate state/identity before execution; optimistic revision checks, idempotency key, lock domain, durable audit, structured progress. No generic shell/argv or raw config body passed from UI to broker.

## Example conceptual resource
```json
{
  "id": "share_01",
  "name": "media",
  "backend": "samba",
  "source": {"kind": "directory", "path": "/srv/media"},
  "access": {"guest": false, "groups": [{"name": "media", "rights": "read-write"}]},
  "managed": false,
  "status": "discovered"
}
```
**Example only**, not production API schema. Create actual JSON Schema/OpenAPI under `schemas/` before implementation.

## Broker op examples
`shares.samba.inspect`, `shares.samba.plan`, `shares.samba.apply`, `shares.nfs.inspect`, `shares.nfs.apply`, `shares.webdav.apply`, `shares.ftp.apply`, `shares.sftp.apply`, `iscsi.target.inspect`, `iscsi.target.apply`, `remote.mount.inspect`, `remote.mount.apply`, `proxy.provider.inspect`, `proxy.host.apply`, `certificates.renew`. Every op requires narrow input schema and RBAC. M1 broker is read-only; writes must wait for durable jobs, operation locks, approval and tests.

## UI components
Protocol capability cards, install-dependency button, effective permission inspector, path picker with filesystem provenance, client grant editor, security exposure summary, dry-run diff, config ownership warning, mount health, LUN topology, proxy host table, certificate timeline, audit log. Accessibility and keyboard navigation mandatory.
