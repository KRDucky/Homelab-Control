# M4-1b Software Center checkpoint

**Status:** Proposed. This note records scope only. It does not authorize implementation, accept an ADR, or accept M4.

This is the M4-1b API, UI, and read-ops working note. M4-1a covers the stdlib parser and adapter package in a separate PR.

## Checkpoint

The first Software Center checkpoint is read-only. It covers package search, installed packages, pending-update previews from the local cache, and configured-source views. Label pending updates with the time of the last metadata refresh. This checkpoint does not contact repositories. Package-manager commands use closed argv templates with stdlib parsing, as recorded for D-11 in [the owner decision register](../m2/OWNER-DECISIONS.md).

The checkpoint does not install, remove, or update packages. It does not add or change repositories, import keys, or refresh metadata. Those writes need separate owner approval.

## Authorization boundary

- `packages.read` is the scope for read operations.
- `packages.manage` remains unused until a separate write ADR.
- The Software Center is a `modules.json` module, not a core route.
- Register the module ID in the compiled module registry before `modules.json` may list it. Do not name an ID in this note; the implementing PR will choose it. An unknown ID makes the file invalid and disables every module.
- ADR 0008 Amendment 5 is Proposed. It does not authorize package writes or constitute M4 acceptance. ADR 0014 is reserved for the later package-transaction write ADR and is not written here.

## Non-goals

This checkpoint does not provide a package transaction, dependency resolver, repository mutation, or live package operation. It does not establish M4 acceptance or authorize changes on a production host or retained VM.

## Implemented in this checkpoint (not acceptance)

The read-only Software Center ships as a `modules.json` module (owner decision; not a core route):

- Module ID `packages` — `APIPrefix /api/v1/packages`, `OpPrefix packages.`, `Path /software`, scope `packages.read`.
- **Default-on-when-file-missing:** a missing `modules.json` enables every module, so the Software
  Center is on by default; an invalid file disables every module; an unknown ID invalidates the file
  (fail closed). This is the existing M2-3 module-loader behaviour, not new.
- Routes: `GET /packages/search?q=`, `/packages/installed`, `/packages/updates`, `/packages/sources`.
  Each requires the session CSRF token, checks Origin when present, and writes an audit record with
  counts only (never package lists or the query text) before the result is released.
- Broker read ops `packages.installed` / `packages.updates` / `packages.sources` / `packages.search`
  (ADR 0008 Amendment 5, Proposed): unprivileged `homelab-control`, closed argv templates, replaced
  environment (`LC_ALL=C`), `/dev/null` stdin, cache-only, bounded output, validated query
  (charset/length, never leading `-`). Unsupported families and missing tools fail closed with a reason.
- `packages.read` is in `KnownScopes`, never in `DefaultScopes`, implied by no other scope.
  `packages.manage` stays reserved for the write ADR (0014) and is not granted here.
- No root template, no plan/apply, no write, no metadata refresh, no repository mutation.
