# Homelab Control

Homelab Control is an independent, open-source Linux server administration platform for homelabs and
NAS machines. It is built from its own components:

- **`hc-api`**: an unprivileged Go web service with local accounts, persistent sessions, role-scoped
  access, CSRF and Origin checks, and a versioned OpenAPI contract. It embeds the web UI.
- **`hc-broker`**: a small, separate privileged Go broker. It exposes a closed list of typed,
  read-only operations. It has no shell, no generic command execution and no pass-through to any other
  system ([ADR 0001](docs/adr/0001-privilege-boundary.md), [ADR 0008](docs/adr/0008-broker-design.md)).
- **A React/TypeScript web UI** that talks only to the `hc-api` API.
- **`hlctl`**: a command-line tool for local user management and audit-log verification.

The direction is a modular, safety-first homelab manager: storage, software, networking, containers and
more, each as an independent module behind the same typed, audited privilege boundary. The roadmap is in
[`docs/project/04-ROADMAP.md`](docs/project/04-ROADMAP.md) and [`docs/m2/M2-ROADMAP.md`](docs/m2/M2-ROADMAP.md).
**Almost all of that is still planned.** See "Current status" below for what actually exists.

Homelab Control does not depend on Webmin, does not run Webmin code, and does not ship Webmin code. It
began from a Webmin source tree and keeps that tree in this repository for reference only; see
[Webmin: reference only](#webmin-reference-only).

## Current status

Homelab Control is early-stage software. **Everything that exists today is read-only: nothing in
this software changes your system.**

### Published release
The only published release is the **unsigned development prerelease
[`v0.1.0-dev.1`](https://github.com/KRDucky/Homelab-Control---Webmin-Fork/releases/tag/v0.1.0-dev.1)**
(Debian/Ubuntu `.deb`, Fedora/openSUSE `.rpm`, Arch `.pkg.tar.zst`, binaries, source archive, SBOM and
`SHA256SUMS`). It is not signed, there are no stable releases and no package repositories
([`docs/release/RELEASING.md`](docs/release/RELEASING.md), [ADR 0009](docs/adr/0009-release-pipeline-and-signing.md)).
It was built before the "merged to `main`" items below.

### Implemented and merged
| Area | What exists | Notes |
|---|---|---|
| Authentication | Local accounts (`hlctl user ...`), PBKDF2-SHA256 password hashing, `__Host-` session cookie, CSRF token and Origin checks on every request | No PAM, no MFA/TOTP ([`docs/security/PAM-AUTH-DEFERRED.md`](docs/security/PAM-AUTH-DEFERRED.md)) |
| Authorization | Server-side scopes on every route: `system.read`, `services.read`, `hardware.read` by default; `services.logs` and `audit.read` only when granted explicitly | |
| Host overview | CPU, memory, uptime, host filesystems and network from unprivileged reads | |
| Services | systemd unit list through the broker | Read-only: no start, stop or restart |
| Service logs | Recent journal entries for one unit | Merged to `main` after the prerelease; needs `services.logs` |
| Hardware | DMI identity and drive inventory with stable identifiers and confidence labels | Read-only inventory; no bay map, SMART or storage management |
| Persistent state | SQLite state database for sessions and the audit trail | Merged to `main` after the prerelease |
| Sessions and audit | Session list and revoke for your own sessions; hash-chained, tamper-evident audit logs and `hlctl audit verify` | Merged to `main` after the prerelease; no retention or rotation yet ([`docs/security/AUDIT-LOG.md`](docs/security/AUDIT-LOG.md)) |
| Legacy Webmin page | Detects and reports on a Webmin installation that the operator installed separately, with a security review of its configuration | Detection only; it reads files and never executes or calls Webmin ([ADR 0003](docs/adr/0003-legacy-module-bridge.md)) |
| Web UI | Overview, Services, Hardware and Legacy Webmin pages with capability-aware navigation; Sessions and Audit pages and the HCIS colour tokens and bundled Inter and JetBrains Mono fonts were merged to `main` after the prerelease | The rest of the HCIS design system is a design direction, not shipped UI ([`docs/ui/hcis/README.md`](docs/ui/hcis/README.md)) |
| Packaging and releases | `.deb`, `.rpm` and Arch packages, hardened systemd units, reproducible builds, SBOM, and a manual draft-only release workflow | Unsigned development builds |

### Not implemented
None of the following exists yet. Documents in `docs/` describe plans and designs, not shipped features.
- Any privileged write: no changes to services, packages, users, networking, files or storage, and no
  plan/confirm/execute workflow. Every broker operation is read-only.
- Disk, partition, filesystem, ZFS, mdraid, Btrfs, mergerfs, SnapRAID or LVM management; SMART data;
  drive-bay mapping; drive replacement.
- Software Center and package management; reverse proxy, ACME/TLS and DDNS management; Tailscale;
  cooling and UPS control; file sharing; containers and virtual machines; backups; alerts.
- The theme engine, Theme Studio, the plugin system and a usable SDK. `sdk/`, `plugins/` and
  `themes/` are placeholders.
- PAM or MFA login, remote or multi-host management, and an admin view of other users' sessions.

### Verification
Continuous integration runs on every change. It covers lint, Go and frontend tests, a security gate,
reproducible builds, package install tests in containers (Debian, Ubuntu, Fedora, openSUSE, Arch),
real-systemd tests on Ubuntu 24.04 and 26.04, and browser tests. Evidence for the published M1
functionality is in [`docs/evidence/m1/README.md`](docs/evidence/m1/README.md). Current status and
known limits: [`docs/STATUS.md`](docs/STATUS.md). A distribution counts as supported only when it
has actually been tested.

## Quick start (development)
Requires Go ≥ 1.26 (CI and releases use the exact version in
[`packaging/release/toolchain.env`](packaging/release/toolchain.env)), Node ≥ 22, make and git.
```bash
make build
mkdir -p .dev && chmod 700 .dev
read -rs PW && printf '%s\n' "$PW" | build/hlctl user add --file .dev/users.json --username admin; unset PW
build/hc-api -dev-insecure-http -listen 127.0.0.1:9180 -users-file .dev/users.json -state-db .dev/state.db
# open http://127.0.0.1:9180/
```
Without a running broker, the Services page shows as unavailable and Hardware as limited, by design.
Run the broker only on a disposable machine. See [`docs/dev/DEVELOPMENT.md`](docs/dev/DEVELOPMENT.md).
Tests: `make check`. Building from a release source archive: [`BUILDING.md`](BUILDING.md).

## Repository map
| Path | What it is |
|---|---|
| `backend/` | Go module: `hc-api` (API server), `hc-broker` (privileged broker), `hlctl` (CLI) |
| `frontend/` | React/TypeScript UI (`app/`) and design tokens (`design-system/`) |
| `schemas/` | OpenAPI contract and JSON schemas |
| `sdk/`, `plugins/`, `themes/` | Placeholders for the planned extension SDK, plugins and themes |
| `packaging/` | systemd units, Debian, Fedora/openSUSE and Arch packaging, release pins |
| `tests/`, `dev/` | Integration, end-to-end, security and packaging tests; disposable container and VM setup |
| `docs/` | Specifications, ADRs, threat model, compliance records, plans and test evidence |
| `compat/webmin/` | **Reference only, never shipped.** Unmodified upstream Webmin 2.670. See below |

## Security model
`hc-api` runs as an unprivileged user and cannot spawn processes (enforced by tests). It checks the
session, scope, Origin and CSRF token on every request. Privileged data comes only from `hc-broker`,
which accepts a closed list of five typed read-only operations, authenticates its caller by peer
UID, and runs the programs it needs through a single reviewed runner under a dedicated service
account. There is no endpoint that runs arbitrary commands or arbitrary root code. Future write
operations will each need their own reviewed broker operation, preview, confirmation and audit entry.
Threat model: [`docs/security/THREAT-MODEL.md`](docs/security/THREAT-MODEL.md).

## Webmin: reference only
Homelab Control began as a fork of [Webmin](https://github.com/webmin/webmin). It has since been
re-established as an independent product with its own backend, frontend and privilege model.

- `compat/webmin/` is retained **exclusively as an upstream development and reference subtree**:
  unmodified Webmin 2.670, pinned in [`compat/webmin.lock`](compat/webmin.lock), with provenance in
  [`NOTICE.md`](NOTICE.md) and any local patch recorded in `compat/PATCHES.md` (none today).
- It is used only for maintenance checks, for Webmin's own upstream test suite in CI, and to test the
  optional "Legacy Webmin" detection against a real Webmin installation.
- **Webmin code is excluded from distributed binaries, packages and release source archives.** CI
  verifies this for every package build and for the source archive
  ([`docs/compliance/WEBMIN-INDEPENDENCE.md`](docs/compliance/WEBMIN-INDEPENDENCE.md)).
- No package depends on Webmin. Homelab Control does not call, proxy or embed Webmin, and the new
  interface uses no Webmin themes or assets.
- Homelab Control is not affiliated with or endorsed by the Webmin project.

## License
Homelab Control's own code is licensed under **GPL-3.0-or-later** ([`LICENSE.md`](LICENSE.md)), with
these **MIT** exceptions: the SDK (`sdk/`), schemas including the OpenAPI contract (`schemas/`), theme
definitions (`themes/`, the draft theme schema and example themes under `docs/ui/`) and the reusable design
tokens (`frontend/design-system/`, `docs/ui/hcis/tokens/`). Exact per-file boundaries are in
[`REUSE.toml`](REUSE.toml) and in SPDX headers. Copyright 2026 The Homelab Control Authors
([`AUTHORS.md`](AUTHORS.md)). Contributions follow [`CONTRIBUTING.md`](CONTRIBUTING.md).

**`compat/webmin/` is not covered by those licences and is not relicensed.** It keeps Webmin's own
licensing: BSD-3-Clause, Copyright (c) Jamie Cameron (full text at `compat/webmin/LICENCE`), plus the
different licences some bundled modules and third-party libraries carry (for example GPL, MIT,
Apache-2.0, Artistic) and some files that state none. The per-file inventory is
[`docs/compliance/WEBMIN-LICENSE-INVENTORY.md`](docs/compliance/WEBMIN-LICENSE-INVENTORY.md). Its notices
and licences stay intact in the repository.

Third-party components in release artifacts and their notices:
[`docs/compliance/THIRD-PARTY-NOTICES.md`](docs/compliance/THIRD-PARTY-NOTICES.md) (installed with every
package). Provenance and attribution: [`NOTICE.md`](NOTICE.md). Compliance record:
[`docs/compliance/LICENSE-AUDIT.md`](docs/compliance/LICENSE-AUDIT.md).

### Create your own theme

The [Theme Development Kit (TDK)](sdk/theme/README.md) provides a dependency-free validator, CLI, schema and four starters. Create and preview themes in **Appearance → Themes & Theme Studio**, then export a single `.hltheme` file. Theme preferences stay in your browser; themes change colours without changing layout or permissions.
