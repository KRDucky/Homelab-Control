# M2-6 host settings — working skeleton (not accepted)

Status 2026-11-06: owner-authorized skeleton-first development (project conversation, 2026-11-05/06). This is a read-only slice. It is not M2-6 acceptance, and it does not authorize any host write.

## What exists

| Route | Scope | What it does |
|---|---|---|
| `GET /api/v1/host/settings` | `system.read` (already default; the overview already shows the kernel host name) | Kernel, static and pretty host name and time zone, from unprivileged file reads in `hc-api` |
| `POST /api/v1/host/settings/preview` | `host.settings.preview` (new; **not** in `DefaultScopes`) | Validates requested values and compares them with the observed ones. **Never changes the host** |

Sources (`backend/internal/hostinfo/hostsettings.go`): `/proc/sys/kernel/hostname`, `/etc/hostname` (first non-comment line), `PRETTY_HOSTNAME` in `/etc/machine-info`, the `/etc/localtime` link target (or `/etc/timezone` when `/etc/localtime` is a copy). Each value is `observed`, `not-set` (the file is absent or empty) or `unavailable` (unknown; no value is guessed). The kernel host name is the one `hc-api` sees: the packaged unit sets `ProtectHostname=yes`, so it is the host name when the service started.

### Why the preview cannot execute

- It is served by `hc-api`, which cannot spawn processes (`guard` tests). It calls no broker operation; there is no new broker op, no `hostnamectl`/`timedatectl` template and no apply or plan route.
- It only reads: the current values and, for a time zone, whether `/usr/share/zoneinfo/<name>` is a regular file starting with `TZif` (opened through `os.Root`, so a link cannot leave the database).
- Every response carries `preview_only: true`, `executed: false`, `host_changed: false`, `apply_available: false` and the notice "Preview only. Nothing was changed on this host…". The UI shows a "Preview only" banner, a "Preview (does not apply)" button and a "Preview result — not applied" table caption.
- Unsafe-method protections apply as for every POST: session, Origin check, per-session CSRF token, scope. The preview is audited (`host.settings.preview`) with field names only, never the requested values.

### Validation policy (preview)

- Static host name: lower-case RFC 1123 labels, 1–64 characters, no trailing dot. Stricter than systemd on purpose; `localhost` is accepted with a warning.
- Pretty host name: valid UTF-8, at most 255 bytes, no control characters. An empty value previews removal (API only; the UI does not offer it yet).
- Time zone: `Area/City` syntax and present in this host's time zone database. If the database cannot be read the name is refused as unverifiable.

## Tests

- Go: `internal/hostinfo/hostsettings_test.go` (fixture reads, not-set vs unavailable, link confinement, validators); `internal/api/hostsettings_test.go` (401/403, Origin and CSRF refusals, separate non-default scope, unavailable collector → 503, preview leaves a hashed copy of the fixture tree unchanged and reports `executed:false`, no apply/plan route, audit has no values, invalid input, unknown current value). `guard` OpenAPI route/scope test covers both routes.
- Vitest: `frontend/app/src/hostsettings.test.tsx` (observed/not-set/unavailable display, form hidden without the scope, CSRF header on the preview, "not applied" labelling, no apply call, server refusal shown).
- The fixture `backend/internal/hostinfo/testdata/hostsettings/` is synthetic and labelled; its zone files are not real tz data.

## Deferred (each needs its own owner decision and, for writes, an ADR 0011 review)

- Applying changes: broker ops with fixed `hostnamectl set-hostname --static|--pretty` and `timedatectl set-timezone` argv templates, plan → confirm → execute, step-up, operation journal, revalidation, read-back; a separate apply scope (`host.settings.preview` must never authorize it). Warnings about TLS names, mDNS and `/etc/hosts`.
- Reading through `hostnamed`/`timedated` (D-Bus) or the broker for the host's live view instead of `hc-api`'s namespace; `timedatectl list-timezones` parity.
- Module-registry entry for host settings (today a core route, like storage), Playwright/axe and mobile checks, distribution and KVM evidence, user documentation.
