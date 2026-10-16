# Project Memory — Verified State and Decisions

> This is a project-local working memory file for Claude Code, **not** a claim that ChatGPT or Claude has stored memory externally. Update only after verifying changes. Keep it short and factual.

## Current state
- Status: **License/independence audit + UI planning on `compliance-audit` (PR #5), awaiting owner review.** M0 (PR #1), M1 (PR #3), M1 closeout (PR #4) merged. M2 on hold until explicitly authorized.
- Compliance (2026-10-16): not release-ready; blockers LA-02 (GPL -only vs -or-later), LA-03 (MIT scope), LA-13 (no release process). Packages contain zero Webmin files (CI payload check, deb/rpm/Arch); third-party notices + SPDX 2.3 SBOM shipped/checked in CI. `docs/compliance/`.
- UI/theme engine: planning docs only in `docs/ui/` (tokens, draft JSON Schema, 3 example themes, Theme Studio, widgets, telemetry, a11y, perf, theme security, phased plan U0–U8). Not implemented.
- M1 (verified on da41162a, 2026-10-16): broker + 4 read-only ops; services/hardware/legacy pages. Local sandbox (Ubuntu 24.04 container, not VM): Go 95/95 non-root, Vitest 15/15, live API 15/15, install 14/14, reproducible. CI run 38852873505 all green: install matrix Debian stable/oldstable, Ubuntu 22.04/24.04/26.04, Fedora, openSUSE TW/Leap, Arch; real systemd Ubuntu 24.04 + 26.04 (systemd 259.5); browser e2e; govulncheck (Go 1.26.x).
- Earlier M1 numbers (d1077e4a) were never committed; withdrawn.
- Workstation KVM VM, Ubuntu 26.04 / systemd 259.5, product `738fa37b` (2026-10-16): smoke 40/40, rollback 13/13, browser 25/25, axe 0 violations (4 pages), removal 4/4; CI run 38866773032 green (15 jobs). Evidence `docs/evidence/m1/vm-26.04/`.
- Fixed from VM run: Overview showed hc-api's sandbox mounts as host (now /proc/1/mounts). Host note: ufw blocks libvirt DHCP on virbr0 unless allowed.
- Product name: Homelab Control. Repo: `KRDucky/Homelab-Control---Webmin-Fork`.
- Upstream baseline: Webmin **2.670** (`3a2b2b8671c0669c129f5275b3dd1853106ae02b`) at `compat/webmin/` via `git subtree --squash`; tree verified identical; no fork patches.
- Implemented: unprivileged Go `hc-api` + `hlctl` (stdlib only), local accounts, sessions/CSRF/RBAC, read-only host overview, React/TS shell, Makefile/CI, Debian dev package, disposable-env scripts.
- M0 verified (2026-10-15, disposable Ubuntu 24.04 container): reproducible build; Go 44/44, Vitest 8/8; live API 15/15; Playwright 9/9; Webmin 2.670 login 9/9; upstream `prove` 4161 tests pass; `.deb` install/run/remove/purge.
- Not verified: VMs other than Ubuntu 26.04; Ubuntu 22.04 under real systemd since M1 fixes; package upgrades between versions.

## Decisions (M0)
- Privilege boundary: unprivileged API + future typed root broker; no process execution in API (ADR 0001, guard-tested).
- Sessions: own accounts, PBKDF2-SHA256 600k, `__Host-` SameSite=Strict cookie, Origin + CSRF token (ADR 0002).
- No generic Webmin bridge/RPC; coexistence + per-op adapters (ADR 0003).
- Secrets owned by broker in root-only files; SQLite deferred to M2 (ADR 0004).
- Go stdlib-only; npm exact pins with `--ignore-scripts` (ADR 0006). Subtree fork maintenance (ADR 0007).
- Rejected: reverse-proxying Webmin CGIs; JSON→foreign_call bridge; running new API as root; JWT sessions.

## Owner decisions (2026-10-16)
- ADR 0001, 0003 accepted (direction). ADR 0008 accepted for the 4 existing read-only M1 ops only. ADR 0005 accepted in principle; release blocked on license compatibility/attribution audit. PAM deferred. Not authorization for privileged writes; M2 on hold until explicitly authorized.
- Webmin 2.680 not released (checked 2026-10-16); keep 2.670, document unreleased upstream fixes, no backports.
- Owner directive (2026-10-16): product independence from Webmin, original UI + theme engine, license compliance. Not authorized: M2, privileged writes, frontend rewrite, Theme Studio implementation, deleting compat/webmin, dependency/license changes, auto-merging the compliance PR.
- VM validation: libvirt harness on the CachyOS workstation (qemu:///system, hc-test pool, hc-test- names), Ubuntu 26.04 first.

## Decisions requiring confirmation
- Cockpit track shown in UI-CONCEPT.png: confirm out of scope.
- Bundle a pruned `compat/webmin` in packages, or rely on upstream Webmin packages.
- Long-term multi-host architecture and marketplace trust policy (unchanged).

## Milestone log
| Date | Milestone | Verified results | Open blockers |
|---|---|---|---|
| 2026-10-15 | Planning | Specifications drafted; no build claimed | M0 not started |
| 2026-10-15 | M0 | See "Current state"; evidence in `docs/evidence/` | Owner approvals above; VM run on owner hardware |
| 2026-10-15 | M1 | docs/M1-REPORT.md (uncommitted evidence, withdrawn) | CI first run; ADR approvals; PAM; Webmin 2.680 |
| 2026-10-16 | M1 verification | Merged main (addon docs). CI green on da41162a after fixes: Go 1.26.x for govulncheck, Arch post_install user creation, apt-based .deb install test, cgroup v2 + /opt staging for systemd containers. Ubuntu 26.04 added to support matrix. Evidence: docs/evidence/m1/README.md | ADR 0001/0003/0005/0008 approval; axe re-run; VM run; PAM; Webmin 2.680 |
| 2026-10-16 | M1 closeout | ADR decisions recorded; Webmin 2.680 absent (advisory doc); PAM deferral doc; dev/vm harness; workstation VM Ubuntu 26.04 on 738fa37b: 40/40, 13/13, 25/25 + axe 0, 4/4; host-filesystem defect fixed | PR #4 merged; M2 authorization; M2 security blockers in docs/STATUS.md |
| 2026-10-16 | Compliance audit (PR #5) | LICENSE-AUDIT LA-01..15; notices shipped; SBOM valid (spdx-tools 0.8.3); payload checks pass in CI; Webmin independence assessment; UI plan docs 01–10 | Owner decisions LA-02/03/06/12/13/14, EX acceptance, Webmin migration phases |

## How to update
Append dated verified decisions and test results. Record alternatives rejected and why. Never replace unknowns with guesses or present planned work as shipped.
