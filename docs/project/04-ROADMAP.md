# Development Roadmap

Times are rough effort bands for a small team with AI assistance, not delivery guarantees. Do not skip acceptance gates.

| Milestone | Focus | Acceptance gate |
|---|---|---|
| M0 | Fork/audit Webmin; source provenance, license audit, CI, dev VM | Reproducible baseline build, documented upstream sync and threat model |
| M1 | Authenticated modern UI shell + compatibility bridge | Log in, navigate, view real read-only host info, fallback to legacy |
| M2 | Core API, RBAC, resource graph, jobs/audit | Versioned schemas, security tests, operation state persistence |
| M3 | Theme SDK + plugin SDK | Install/preview/switch/remove theme; load/unload read-only sample plugin |
| M4 | Distro Software Center and dependency resolver | Search, update preview, trusted source selection, safe package transaction in VMs |
| M5 | Physical disk inventory, SMART, controllers, bay map | Stable identity, confidence labeling, manual bay mapping, test fixtures |
| M6 | Partition/format/mount planner | Visual layout, validated dry run, virtual-disk tests; destructive execution gated |
| M7 | ZFS, mdraid, Btrfs adapters | Real read-only topology, health, scrub/rebuild status, safe maintenance jobs |
| M8 | mergerfs + SnapRAID adapters | Branch/parity config discovery, validation, scheduled status/scrub, combined planner |
| M9 | Guided drive replacement | Technology-specific preflight, identity confirmation, VM-verified procedures |
| M10 | Reverse proxy, ACME, DDNS | Caddy/Nginx/Traefik adapters, TLS, Cloudflare DNS, safe config reload |
| M11 | Port-use tracker, LACP/bonding detect, firewall WebUI (active backend) | Read-only port map; bonding detect; firewall UI for the installed **active** backend; installed-tooling gate |
| M12 | Identity, Tailscale, cooling, UPS, containers, virtualization | Supported capability adapters and permissions; hardware safety checks |
| M13 | Backups, monitoring, alerts, automation, multi-host exploration | Verified restore workflows, notification tests, resilience |
| M14 | Native distro releases and hardening | Debian/Ubuntu, Fedora, openSUSE, Arch package test matrix and signed releases |

> **Proposed revision (2026-10-21, not yet approved):** `docs/m2/M2-ROADMAP.md` §4 proposes a module-oriented order after M2: storage read-only, software center, containers, storage configuration, file sharing, virtualization, hardware and replacement; the theme/plugin SDK moves after the first modules. This table stays authoritative until the owner decides (D-8 in `docs/m2/OWNER-DECISIONS.md`).
>
> **Owner 2026-11-07 catalog extensions** (product scope, not implementation, not moved into M3): exFAT, ZoneFS, Ceph logical options (after M8, unslotted backlog — no table row), disk identity split, compose/env editor, Kubernetes optional. Details: [`05-FEATURE-CATALOG.md`](05-FEATURE-CATALOG.md), [`../m2/CAPABILITY-MATRIX.md`](../m2/CAPABILITY-MATRIX.md).
>
> **2026-11-07 owner slot (later-row insert only):** new **M11** is port-use tracker (read-only first), LACP/bonding detect, and firewall WebUI for the **active** backend, with the installed-tooling gate. Former M11–M13 become M12–M14. D-8 still keeps **M3** as Theme SDK + plugin SDK.

## Priority and dependencies
M0–M4 establish the foundation. M5 precedes M6–M9 because all disk operations require trustworthy identity. M10 can run in parallel with later storage work once M2–M4 are stable. Release packaging must be exercised from M0, not deferred until M14.

## Reporting format per milestone
Goal; code changes; architecture decisions; tests run with evidence; unsupported cases; security impact; regressions; packaging results; blockers; next proposed milestone. Stop for approval.
