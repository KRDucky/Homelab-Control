# M2 implementation roadmap (proposed)

**Date:** 2026-10-21 · **Status:** proposal for owner approval (D-8). **No PR in this roadmap is authorized yet.**
The first PR (PR-1) is specified in `M2-PR1-PROPOSAL.md`.

## 1. Reconciliation with existing specs and the owner's candidate sequence
| Owner candidate | Existing spec | Recommendation |
|---|---|---|
| M2.1 Security and persistence foundation | M2 = "Core API, RBAC, resource graph, jobs/audit"; ADR 0001/0002/0004/0008 M2 items | **Keep, but ship it with user-visible function** (PR-1: durable state, audit, service logs, session management) instead of an infrastructure-only phase |
| M2.2 First privileged service operation | Not assigned to a milestone (feature catalogue "Services") | **Adopt** as PR M2-2. It is the smallest real write and proves the plan/apply framework the spec's "jobs" item needs |
| M2.3 Expanded service management | — | **Adopt** as PR M2-3: enable/disable, timers and sockets, unit detail |
| M2.4 Host administration (accounts, groups, host config) | Identity is a core feature without a milestone | **Split** (D-9): hostname and timezone optional in M2 (PR M2-6); users and groups in their own later milestone |
| M2.5 Storage integration | Disk inventory and bay map = M5; storage adapters M7–M8 | **Read-only part only, pulled forward:** storage inventory graph (PR M2-4, "resource graph" of spec M2) and drive locator Stage A (PR M2-5). Storage writes stay out of M2 |

**What M2 delivers to the user:**
- service logs, start/stop/restart and enable/disable with confirmations and audit;
- surviving sessions with revocation;
- an audit viewer;
- a storage inventory showing what owns each disk;
- a visual drive locator;
- optionally, hostname and timezone settings.

## 2. M2 PR sequence
Dependencies: PR-1 → M2-2 → M2-3; PR-1 → M2-4 → M2-5; M2-2 → M2-6. M2-4/M2-5 can proceed in parallel with M2-2/M2-3.

**Mandatory:** PR-1, M2-2, M2-3, M2-4. **Optional:** M2-5 (recommended; high value, no privilege), M2-6 (D-9).

### PR-1 — Durable state, audit trail and service logs (read-only)
See `M2-PR1-PROPOSAL.md`. **No privileged writes.** Decisions: D-1, D-6, D-7.

### PR M2-2 — Write framework and first privileged operation: service start/stop/restart
| | |
|---|---|
| **Objective** | First privileged write, end to end, safely |
| **Scope** | Plan/apply framework (broker-minted tokens, revalidation, locks, journal); `root-exec` runner class; ops `services.unit.plan` / `services.unit.apply` with verbs start/stop/restart for `.service` units; protected-unit policy; step-up; UI action menu on the Services page with `ConfirmDialog`, `StepUpPrompt` and result view; audit viewer shows operations and interrupted intents |
| **Prerequisites** | PR-1 merged; owner approval of D-2, D-3, D-4, D-5; ADR 0011 accepted |
| **Architecture** | ADR 0011 (write ops). ADR 0008 amendment (root-exec, argv templates). New routes `POST /api/v1/services/units/{unit}/plan`, `POST /api/v1/services/units/{unit}/apply` (scope `services.operate`) |
| **Security** | Full checklist §2–§3 in `M2-SECURITY-ACCEPTANCE.md` |
| **Tests** | Unit, broker negative (replay, expiry, wrong actor/target, lock, revalidation, protected units, injection, fuzz), API authz/CSRF, Vitest for dialogs and permission gates, Playwright flow + axe, CI real-systemd jobs with `hc-test-dummy.service`, owner VM on Ubuntu 26.04 and one non-Debian family |
| **Acceptance** | All checklist items pass with evidence; restart of a test unit shows the observed state; protected units refused; interrupted op reported after broker kill |
| **User-visible** | Start/stop/restart buttons for permitted units with a clear impact preview and result |
| **Docs** | ADR 0011, ADR 0008 amendment, threat model update, user guide section, `STATUS.md` |
| **Exclusions** | enable/disable, other unit types, unit file editing, configurable policy, TOTP |
| **Owner decisions** | D-2, D-3, D-4, D-5 |

### PR M2-3 — Expanded service management
| | |
|---|---|
| **Objective** | Make the Services module complete for day-to-day use |
| **Scope** | enable/disable (`services.configure`, persistent → `reversible`, still confirmed); timers and sockets views (read); unit detail (dependencies, unit-file path, last exit, restart counters, read-only `systemctl cat` output with size cap); module registry with the Services module as its first member (nav driven by the registry) |
| **Prerequisites** | M2-2 |
| **Security** | Same framework; protected-unit policy extended to disable/mask (mask is **excluded**) |
| **Tests** | As M2-2 for new verbs; registry tests (module disabled → routes 404, ops refused) |
| **User-visible** | Enable/disable at boot, timer schedules, unit details |
| **Exclusions** | Editing unit files, creating units, mask/unmask |
| **Owner decisions** | none new (D-10 for the registry approach) |

### PR M2-4 — Storage inventory and ownership graph (read-only)
| | |
|---|---|
| **Objective** | One trustworthy view of disks → partitions → md/LVM/ZFS/Btrfs membership → filesystems → mounts, with ownership claims |
| **Scope** | Resource graph core (spec 02) for block storage. Unprivileged sources: sysfs, `/proc/self/mountinfo`, `/dev/disk/by-*`, `/proc/mdstat`. Broker read op `storage.blkid.probe` (fixed argv `blkid -o export` over enumerated devices; no caller paths). Ownership resolver (`MODULE-ARCHITECTURE.md` §3) shows claims and ambiguous devices. Storage page |
| **Prerequisites** | PR-1 (graph snapshots in `state.db`); D-7 scope `storage.read` |
| **Security** | Read-only; network filesystems listed, not probed (existing rule); no device paths from callers |
| **Tests** | Fixture trees for md, LVM, ZFS, Btrfs, LUKS, mergerfs signatures; loop-device tests in the disposable systemd container; identity never keyed by kernel name |
| **User-visible** | Storage page: what is on each disk, who owns it, what is mounted where |
| **Exclusions** | Any write; technology-specific detail views (M3) |

### PR M2-5 (optional, recommended) — Physical drive locator, Stage A (reusing the HBA Viewer concept, D-12)
| | |
|---|---|
| **Objective** | Usable visual drive locator without enterprise hardware |
| **Scope** | The existing HBA Viewer concept (`MODULE-ARCHITECTURE.md` §5.0; source pending, D-12) as the shared Hardware → Drives & Bays view: chassis/cell model in `state.db`; grid editor (rows/cols, labels, cell kinds, empty/disabled/reserved); assign drives from the M2-4 inventory by stable identity (drag-and-drop **and** keyboard); states present / missing / unassigned / mismatch; bay click → disk detail; export/import with identifiers redacted by default |
| **Prerequisites** | PR-1, M2-4; scope `layout.manage` (D-7) |
| **Security** | No broker writes; import validated (size caps, schema); manual assignments labelled *unverified* |
| **Tests** | Model/API tests (renumbering a kernel name does not move an assignment; replacement detected), Vitest for grid keyboard use, Playwright + axe |
| **User-visible** | "Where is this disk?", with location shown on the Storage page |
| **Exclusions** | Stage B integration with technology modules, enclosure discovery (C), LEDs (D) |

### PR M2-6 (optional, D-9) — Host settings: hostname and timezone
| | |
|---|---|
| **Scope** | Read current values; set hostname (static and pretty) and timezone through `hostnamectl`/`timedatectl` argv templates; validation (RFC 1123 hostname, timezone in `timedatectl list-timezones`) |
| **Prerequisites** | M2-2 framework |
| **Security** | `reversible` class; confirmation; warns that hostname affects TLS names and mDNS |
| **Exclusions** | Network configuration, NTP servers, users and groups |

## 3. Testing requirements
| Test | Mandatory before the first privileged write (M2-2) | Introduced later |
|---|---|---|
| Go unit + broker security tests (injection, replay, locks, revalidation, protected units, fuzz) | Yes | — |
| API authz/CSRF negative tests for every new route | Yes | — |
| OpenAPI contract tests for new routes | Yes | — |
| Vitest for dialogs, permission gates, plan preview | Yes | — |
| Playwright flow + axe 0 violations | Yes | Mobile layout checks for every new page (M2-3+) |
| Real systemd in CI (Ubuntu 24.04 runner, 26.04 container) with a disposable unit | Yes | Fedora/Arch systemd containers (M2-3) |
| Owner VM (`dev/vm/`) on Ubuntu 26.04 + one non-Debian family | Yes | All five families before M2 closes |
| Package install/upgrade test incl. `state.db` migration | Yes (from PR-1) | Downgrade refusal test |
| Storage fixtures and loop devices | — | M2-4 onward (no real host disks; CLAUDE.md) |
| Existing CI | Unchanged; nothing is weakened | — |

## 4. After M2: proposed module milestones (D-8)
Each milestone delivers complete modules (backend, API, UI, tests, docs), read-only first and then writes behind the M2 framework. The old roadmap numbers are given for traceability; the theme/plugin SDK (old M3) is proposed to follow M5.

| Proposed | Content | Old roadmap |
|---|---|---|
| **M3 Storage, read-only** | ZFS, mdadm, LVM2, Btrfs, ext4/XFS/F2FS details, mergerfs, SnapRAID status, SMART/NVMe; drive locator Stage B | parts of M5, M7, M8 (read) |
| **M4 Software Center** | Package search/install/remove with transaction preview per family; updates and repositories only after separate owner approval | M4 |
| **M5 Containers** | Docker Engine, containers, Compose projects, images, networks, volumes, Buildx | M11 (part) |
| **M6 Storage configuration** | Mounts, ext4/XFS/F2FS create and grow, LVM, mdadm, ZFS datasets/snapshots, Btrfs subvolumes, mergerfs pools, SnapRAID config/sync/scrub; coordinated storage workflows; destructive ops gated by VM evidence | M6, M7, M8 |
| **M7 File sharing** | SMB, SFTP, NFS, WebDAV, FTP/FTPS, then iSCSI target; share workflows | addon A1–A5 |
| **M8 Virtualization** | libvirt VMs, networks, storage pools; VM-on-pool workflow | M11 (part) |
| **M9 Hardware and replacement** | Drive locator Stages C and D, guided disk replacement and SnapRAID recovery feasibility, Thunderbolt | M9 + new |
| **M10+** | Unchanged from 04-ROADMAP: reverse proxy/ACME/DDNS, identity/Tailscale/cooling/UPS, backups/monitoring, theme/plugin SDK (moved), distro release hardening | M3, M10–M13 |

Users and groups administration gets its own milestone once identity work (PAM, still deferred) is scheduled.

## 5. Mandatory vs optional summary
- **Mandatory for M2 close:** PR-1, M2-2, M2-3, M2-4, plus the security checklist and VM evidence.
- **Optional enhancements:** M2-5 drive locator Stage A (recommended), M2-6 host settings, TOTP step-up (D-3 option B).
