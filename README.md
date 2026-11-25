# Homelab Control

Homelab Control is an independent, open-source Linux server administration platform for homelabs and
NAS machines. It is built from its own components:

- **`hc-api`**: an unprivileged Go web service with local accounts, persistent sessions, role-scoped
  access, CSRF and Origin checks, and a versioned OpenAPI contract. It embeds the web UI.
- **`hc-broker`**: a small, separate privileged Go broker. It exposes a closed list of typed
  operations: read-only, except for starting, stopping and restarting systemd `.service` units. It has no shell, no generic command execution and no pass-through to any other
  system ([ADR 0001](docs/adr/0001-privilege-boundary.md), [ADR 0008](docs/adr/0008-broker-design.md)).
- **A React/TypeScript web UI** that talks only to the `hc-api` API.
- **`hlctl`**: a command-line tool for local user management and audit-log verification.

The direction is a modular, safety-first homelab manager: storage, software, networking, containers and
more, each as an independent module behind the same typed, audited privilege boundary. The roadmap is in
[`docs/project/04-ROADMAP.md`](docs/project/04-ROADMAP.md) and [`docs/m2/M2-ROADMAP.md`](docs/m2/M2-ROADMAP.md).
**Almost all of that is still planned.** See "Current status" below for what actually exists.

Homelab Control is an independent project. It is not a fork of Webmin, contains no Webmin code, and is
not affiliated with or endorsed by the Webmin project.

![Homelab Control dashboard: host, processor, memory, storage, network and module status from a test VM](docs/screenshots/overview-desktop.png)

<sub>The dashboard of the current build, signed in with a read-only test account on a disposable Ubuntu 26.04
VM. More screenshots: [Web UI screenshots](#web-ui-screenshots).</sub>

## Current web interface

The React/TypeScript interface is **Homelab Control's own UI**. Its navigation and routes do not include a Webmin or legacy-Webmin page. Modules appear according to API-reported capabilities, configured module enablement and the signed-in account's permissions.

The current UI source contains these module groups:

| Navigation group | Pages in the current frontend |
|---|---|
| Dashboard | Overview (dashboard and detailed host report) |
| System | Services, Hardware, Host settings, Software |
| Storage & files | Storage, MergerFS, ZFS, LVM, Btrfs, Ext4, mdadm, F2FS, XFS, Files |
| Network & applications | Ports, Bonds, Firewall, Traefik |
| Administration | Audit; session management is available from the account menu |
| Appearance | Built-in themes and Theme Studio |

Not all modules or operations are available on every installation. A module's availability and permitted actions depend on the host, broker, installed dependencies, and account scopes. Showing a module in the frontend source does **not** imply that all its backend operations are production-ready; consult [current status](docs/STATUS.md) and the milestone acceptance records.

## Web UI screenshots

Real screenshots of the current build (`f8a259b`), taken with Playwright (Chromium, 1440 × 900; phones
390 × 844) on a **disposable Ubuntu 26.04.1 VM** with the privileged broker and the read-only storage
helper enabled, signed in with a **read-only test account**. The storage shown is **test data on blank
virtual disks** created for the capture: an LVM volume group on a virtual SATA disk, a ZFS mirror, a
Btrfs filesystem and a MergerFS pool with a SnapRAID configuration; the NVMe and SATA disks are QEMU's
emulated drives. Nothing here is a production system. Procedure and checks:
[`docs/evidence/ui/SCREENSHOTS.md`](docs/evidence/ui/SCREENSHOTS.md).

<table>
<tr>
<td width="32%"><img src="docs/screenshots/navigation-desktop.png" alt="Sidebar with the Dashboard, System, Storage &amp; files, Network &amp; applications and Administration groups"></td>
<td><img src="docs/screenshots/storage-desktop.png" alt="Storage page after a probe: test disks with identifiers, size, use and SMART health"></td>
</tr>
<tr>
<td><sub><b>Navigation.</b> Modules grouped by area. "Limited" marks a module that works with reduced data on this host.</sub></td>
<td><sub><b>Storage</b> after <i>Probe storage</i>: lsblk, smartctl and pvs/lvs run by the read-only helper. SATA and NVMe SMART health is read; virtio disks have no SMART. Filtered to the VM's test disks.</sub></td>
</tr>
</table>

| | |
|---|---|
| ![Services](docs/screenshots/services-desktop.png) **Services**: systemd units with state; start, stop, restart, enable and the closed systemctl verbs are plan → confirm → apply (not used by this read-only account). | ![Hardware](docs/screenshots/hardware-desktop.png) **Hardware**: DMI identity and drives with stable identifiers and confidence labels. |
| ![Software](docs/screenshots/software-desktop.png) **Software**: installed packages, update preview, sources, APT preview, APT settings and mirrors (Debian/Ubuntu). | ![Files](docs/screenshots/files-desktop.png) **Files**: browsing of administrator-allowed roots (here a test share). |
| ![ZFS](docs/screenshots/zfs-desktop.png) **ZFS**: kernel module, tooling and mounted datasets (read-only; pool topology and health are not shown yet). | ![MergerFS](docs/screenshots/mergerfs-desktop.png) **MergerFS / SnapRAID**: pools with per-branch capacity, configured mounts and SnapRAID configuration checks. |
| ![LVM](docs/screenshots/lvm-desktop.png) **LVM**: device-mapper volumes from sysfs (read-only; the full LVM inventory is on the Storage page after a probe). | ![Btrfs](docs/screenshots/btrfs-desktop.png) **Btrfs**: kernel support, tooling and mounted filesystems (read-only). |
| ![Ports](docs/screenshots/network-ports-desktop.png) **Port use**: listening sockets with their process, systemd socket or account, and bind scope (firewall rules are not applied here). | ![Firewall](docs/screenshots/network-firewall-desktop.png) **Firewall**: the active backend and its saved configuration (read-only; the live kernel ruleset is not read). |
| ![Bonds](docs/screenshots/network-bonds-desktop.png) **Bonds / LACP**: kernel bonding inventory. Shown as *Limited*: the test VM has no bond. | ![Theme Studio](docs/screenshots/theme-studio-desktop.png) **Themes & Theme Studio**: built-in themes and browser-local theme editing. |

**Light theme (Daybreak) and phones:**

<table>
<tr>
<td width="40%"><img src="docs/screenshots/theme-daybreak.png" alt="Dashboard in the Daybreak light theme"></td>
<td width="30%"><img src="docs/screenshots/overview-mobile.png" alt="Dashboard on a phone"></td>
<td width="30%"><img src="docs/screenshots/navigation-mobile.png" alt="Navigation drawer on a phone"></td>
</tr>
<tr>
<td><sub>Dashboard in <b>Daybreak</b> (the light theme; the screenshots above use <b>Midnight</b>).</sub></td>
<td><sub>Dashboard on a phone.</sub></td>
<td><sub>Navigation drawer on a phone.</sub></td>
</tr>
</table>

**Not shown as working:** the Traefik reverse-proxy page needs a configured Traefik extension, which the test
VM does not have; its honest unavailable state is in [`traefik-desktop.png`](docs/screenshots/traefik-desktop.png).
ext4, XFS, f2fs and Software RAID pages exist but are not pictured; f2fs is *Limited* on this VM (no
`dump.f2fs`). Screenshots show the interface, not production acceptance of a module; see
[current status](docs/STATUS.md).

## Current status

Homelab Control is early-stage software. **Its privileged broker writes are typed systemctl
operations, not a shell.** New accounts are granted `services.operate` (start, stop and restart of
`.service` units), `services.configure` (boot-time enable and disable), `services.logs`,
`services.files`, and `services.systemctl` (the closed systemctl verbs: mask, unmask, drop-in edit,
daemon-reload, host power, and the other templates registered on that operation). An existing
`users.json` is not rewritten. Every other broker operation stays read-only. `audit.read` stays off
the default grant.
Homelab Control also keeps its own state database, audit logs and operation journal. Owner 2026-11-21:
these service operations are production. Browser validation item E2 was not re-run and is not a
deployment gate ([results](docs/m2/M2-2-ACCEPTANCE.md)).

### Published release
The only published release is the **unsigned development prerelease
[`v0.1.0-dev.1`](https://github.com/KRDucky/Homelab-Control/releases/tag/v0.1.0-dev.1)**
(Debian/Ubuntu `.deb`, and, in that prerelease only, a Fedora/openSUSE `.rpm` and an Arch `.pkg.tar.zst`,
plus binaries, source archive, SBOM and `SHA256SUMS`). The first Home Control release is Debian and Ubuntu
only. It is not signed, there are no stable releases and no package repositories
([`docs/release/RELEASING.md`](docs/release/RELEASING.md), [ADR 0009](docs/adr/0009-release-pipeline-and-signing.md)).
It was built before the "merged to `main`" items below.

### Implemented and merged
| Area | What exists | Notes |
|---|---|---|
| Authentication | Local accounts (`hlctl user ...`), PBKDF2-SHA256 password hashing, `__Host-` session cookie, CSRF token and Origin checks on every request | No PAM, no MFA/TOTP ([`docs/security/PAM-AUTH-DEFERRED.md`](docs/security/PAM-AUTH-DEFERRED.md)) |
| Authorization | Server-side scopes on every route: `system.read`, `services.read`, `hardware.read`, `services.operate`, `services.configure`, `services.logs` and `services.files` by default for a new account; `audit.read` only when granted explicitly. An existing account is not changed | |
| Host overview | CPU, memory, uptime, host filesystems and network from unprivileged reads | |
| Services | systemd unit list through the broker | Read-only list |
| Service operations | Start, stop and restart of `.service` units, boot enable/disable, and closed systemctl verbs (mask, drop-in, daemon-reload, host power, other unit types) through plan, token, protected-unit policy and password re-entry ([user guide](docs/user/SERVICE-MANAGEMENT.md)) | `services.operate`, `services.configure` and `services.systemctl`. E2 is not a deployment gate. No free-form shell |
| Service logs | Recent journal entries for one unit | Merged to `main` after the prerelease; needs `services.logs` |
| Hardware | DMI identity and drive inventory with stable identifiers and confidence labels | Read-only inventory; no bay map, SMART or storage management |
| Persistent state | SQLite state database for sessions and the audit trail | Merged to `main` after the prerelease |
| Sessions and audit | Session list and revoke for your own sessions; hash-chained, tamper-evident audit logs and `hlctl audit verify` | Merged to `main` after the prerelease; no retention or rotation yet ([`docs/security/AUDIT-LOG.md`](docs/security/AUDIT-LOG.md)) |
| Web UI | Overview, Services and Hardware pages with capability-aware navigation; Sessions and Audit pages and the HCIS colour tokens and bundled Inter and JetBrains Mono fonts were merged to `main` after the prerelease | The rest of the HCIS design system is a design direction, not shipped UI ([`docs/ui/hcis/README.md`](docs/ui/hcis/README.md)) |
| Packaging and releases | Debian/Ubuntu `.deb`, hardened systemd units, reproducible builds, SBOM, and a manual draft-only release workflow. Fedora and Arch scripts stay in the tree for later | Unsigned development builds |

### Not implemented
None of the following exists yet. Documents in `docs/` describe plans and designs, not shipped features.
- Any privileged write other than starting, stopping and restarting systemd `.service` units or
  boot-time enable/disable: no masking or unit-file editing, and no management of packages, users,
  networking, files or storage. Every other broker operation is read-only.
- Disk, partition, filesystem, ZFS, mdraid, Btrfs, mergerfs, SnapRAID or LVM management; SMART data;
  drive-bay mapping; drive replacement.
- Software Center and package management; reverse proxy, ACME/TLS and DDNS management; Tailscale;
  cooling and UPS control; file sharing; containers and virtual machines; backups; alerts.
- A plugin system, first-party plugins, server-wide theme installation and persistence, and theme packs.
  `plugins/` and `themes/` are placeholders. (A browser-local Theme Studio and the Theme Development Kit in
  `sdk/theme/` are implemented: see "Create your own theme" below.)
- PAM or MFA login, remote or multi-host management, and an admin view of other users' sessions.

### Verification
Continuous integration runs on every change. It covers lint, Go and frontend tests, a security gate,
reproducible builds, package install tests in Debian and Ubuntu containers,
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
| `sdk/`, `plugins/`, `themes/` | `sdk/theme/` is the Theme Development Kit (MIT); `plugins/` and `themes/` are placeholders for planned plugins and theme packs |
| `packaging/` | systemd units, the Debian/Ubuntu package, later Fedora and Arch scripts, release pins |
| `tests/`, `dev/` | Integration, end-to-end, security and packaging tests; disposable container and VM setup |
| `docs/` | Specifications, ADRs, threat model, compliance records, plans and test evidence |

## Security model
`hc-api` runs as an unprivileged user and cannot spawn processes (enforced by tests). It checks the
session, scope, Origin and CSRF token on every request. Privileged data comes only from `hc-broker`,
which accepts a closed list of typed operations, authenticates its caller by peer UID, and runs
the programs it needs through a single reviewed runner, normally under a dedicated service account.
Privileged writes are the typed service operations: start, stop and restart (`services.operate`),
boot-time enable/disable (`services.configure`), and the closed systemctl templates
(`services.systemctl`: mask, unmask, drop-in edit, daemon-reload, host power, and the other verbs
in that operation). New accounts get those scopes. Each change is previewed, confirmed with a
single-use broker-issued plan token, re-checked against a built-in protected-unit policy, requires
a password re-entry, and is journaled
([ADR 0011](docs/adr/0011-write-operation-authorization.md),
[user guide](docs/user/SERVICE-MANAGEMENT.md)). Owner 2026-11-21: this is production. E2 is not a
deployment gate ([results](docs/m2/M2-2-ACCEPTANCE.md)). There is no endpoint that runs arbitrary
commands or arbitrary root code. `systemctl link`, `set-property`, a chosen kill signal, and
`switch-root` are not templates.
Threat model: [`docs/security/THREAT-MODEL.md`](docs/security/THREAT-MODEL.md).

## License
Homelab Control's own code is licensed under **GPL-3.0-or-later** ([`LICENSE.md`](LICENSE.md)), with
these **MIT** exceptions: the SDK (`sdk/`), schemas including the OpenAPI contract (`schemas/`), theme
definitions (`themes/`, the draft theme schema and example themes under `docs/ui/`) and the reusable design
tokens (`frontend/design-system/`, `docs/ui/hcis/tokens/`). Exact per-file boundaries are in
[`REUSE.toml`](REUSE.toml) and in SPDX headers. Copyright 2026 The Homelab Control Authors
([`AUTHORS.md`](AUTHORS.md)). Contributions follow [`CONTRIBUTING.md`](CONTRIBUTING.md).

Third-party components in release artifacts and their notices:
[`docs/compliance/THIRD-PARTY-NOTICES.md`](docs/compliance/THIRD-PARTY-NOTICES.md) (installed with every
package). Provenance and attribution: [`NOTICE.md`](NOTICE.md). Compliance record:
[`docs/compliance/LICENSE-AUDIT.md`](docs/compliance/LICENSE-AUDIT.md). The dependency inventory and
open-source license review requirements are in
[`docs/compliance/DEPENDENCY-LICENSES.md`](docs/compliance/DEPENDENCY-LICENSES.md).

### Create your own theme

The [Theme Development Kit (TDK)](sdk/theme/README.md) provides a dependency-free validator, CLI, schema and four starters. Create and preview themes in **Appearance → Themes & Theme Studio**, then export a single `.hltheme` file. Theme preferences stay in your browser; themes change colours without changing layout or permissions.
