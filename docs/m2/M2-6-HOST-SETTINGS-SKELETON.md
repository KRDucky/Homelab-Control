# M2-6 host settings — working slice (not accepted)

Status 2026-11-06: owner-authorized apply after the read-only skeleton (PR #40 on main). The backend implements GET, preview, plan and apply. **This is not M2-6 acceptance.** ADR 0008 Amendment 4 and ADR 0011 M2-6 extension remain **Proposed**. Isolated fixtures only; no production or retained-host apply.

## What exists

| Route | Scope | What it does |
|---|---|---|
| `GET /api/v1/host/settings` | `system.read` (already default; the overview already shows the kernel host name) | Kernel, static and pretty host name and time zone, from unprivileged file reads in `hc-api` |
| `POST /api/v1/host/settings/preview` | `host.settings.preview` (**not** in `DefaultScopes`) | Validates requested values and compares them with the observed ones. **Never changes the host**; this scope never authorizes plan or apply |
| `POST /api/v1/host/settings/plan` | `host.settings.apply` (**not** in `DefaultScopes`, never implied by preview) | ADR 0011 review: hostnamectl/timedatectl observed values, reversible risk, TLS/mDNS/`/etc/hosts` warning, single-use plan token. Nothing is changed |
| `POST /api/v1/host/settings/apply` | `host.settings.apply` | Execute the reviewed change with closed `hostnamectl`/`timedatectl` templates; journaled; verified by reading hostnamectl/timedatectl back |

Sources (`backend/internal/hostinfo/hostsettings.go`): `/proc/sys/kernel/hostname`, `/etc/hostname` (first non-comment line), `PRETTY_HOSTNAME` in `/etc/machine-info`, the `/etc/localtime` link target (or `/etc/timezone` when `/etc/localtime` is a copy). Each value is `observed`, `not-set` (the file is absent or empty) or `unavailable` (unknown; no value is guessed). The kernel host name is the one `hc-api` sees: the packaged unit sets `ProtectHostname=yes`, so it is the host name when the service started.

### Why the preview cannot execute

- It is served by `hc-api`, which cannot spawn processes (`guard` tests). Preview calls no broker write. Plan and apply are separate routes with scope `host.settings.apply`; `host.settings.preview` never authorizes them.
- Preview only reads: the current values and, for a time zone, whether `/usr/share/zoneinfo/<name>` is a regular file starting with `TZif` (opened through `os.Root`, so a link cannot leave the database).
- Every preview response carries `preview_only: true`, `executed: false`, `host_changed: false`. `apply_available` is true when this server's broker has `host.settings.apply` and both binaries; the preview still does not apply.
- Unsafe-method protections apply as for every POST: session, Origin check, per-session CSRF token, scope. The preview is audited (`host.settings.preview`) with field names only, never the requested values.

### Apply (backend; not accepted)

- Closed templates (values after `--`): `hostnamectl --no-ask-password set-hostname --static|--pretty -- <name>`, `timedatectl --no-ask-password set-timezone -- <Area/City>`, plus read-only status and `list-timezones`. Empty pretty name is removal.
- Risk class `reversible`; confirmation required; no step-up. Multi-field apply stops on the first failed field; there is no automatic rollback.
- Honest read-back is hostnamectl/timedatectl. Packaged `hc-api` uses `ProtectHostname=yes`; the kernel host name seen by the API may stay at start-time.

### Apply (UI; not accepted)

- Host settings page keeps the non-executing preview. Apply is offered only when the session has `host.settings.apply`, the preview reports `apply_available` and `valid`, and a preview request is stored. Capability `available` is not the apply gate.
- Confirm dialog follows the Services plan/confirm/result shape without password re-entry. Backend plans for this op are reversible with `step_up_required` false; a plan that required step-up would need an ADR, not a copied Services password field.
- Preview `warnings` show on the page as `banner` `role="note"`. TLS/mDNS/`/etc/hosts` text in the confirm dialog comes from the plan payload.

### Validation policy (preview)

- Static host name: lower-case RFC 1123 labels, 1–64 characters, no trailing dot. Stricter than systemd on purpose; `localhost` is accepted with a warning.
- Pretty host name: valid UTF-8, at most 255 bytes, no control characters. An empty value previews removal (API only; the UI does not offer it yet).
- Time zone: `Area/City` syntax and present in this host's time zone database. If the database cannot be read the name is refused as unverifiable.

## Tests

- Go: `internal/hostinfo/hostsettings_test.go` (fixture reads, not-set vs unavailable, link confinement, validators); `internal/api/hostsettings_test.go` (401/403, Origin and CSRF refusals, separate non-default preview and apply scopes, preview leaves a hashed copy of the fixture tree unchanged and reports `executed:false`, preview-only user 403 on plan/apply, audit has no values). Broker: `internal/broker/ops/hostsettings_write_test.go` (isolated fake; replay, conflict, stop on first failed field). `guard` OpenAPI route/scope test covers GET, preview, plan and apply.
- Vitest: `frontend/app/src/hostsettings.test.tsx` (observed display, preview CSRF, Apply gated on scope + `apply_available` + `valid`, no plan/apply from preview, TLS warning from the plan, no password field).
- Opt-in Go fixture `TestHostSettingsBrowserFixture` (`backend/internal/api/hostsettings_browser_test.go`, `HC_HOST_SETTINGS_BROWSER_DIR`) plus `tests/e2e/host_settings_browser.py`: labelled copy of `testdata/hostsettings` only. Preview never posts plan or apply. The fixture has no broker and never runs `hostnamectl` or `timedatectl`. Skip unless the runtime dir already exists mode 0700; fatal if euid 0. Not in CI. Not live host apply, not M2-6 acceptance, not M2 close.
- The fixture `backend/internal/hostinfo/testdata/hostsettings/` is synthetic and labelled; its zone files are not real tz data.

## Deferred (each needs its own owner decision)

- Reading through `hostnamed`/`timedated` (D-Bus) for the host's live view instead of `hc-api`'s namespace.
- Module-registry entry for host settings (today a core route, like storage), CI/browser-e2e coverage of the opt-in Playwright scripts, distribution and KVM evidence, user documentation, owner acceptance.
