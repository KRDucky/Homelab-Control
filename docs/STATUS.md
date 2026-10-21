# Current status — Phase 1 release complete; M2 PR-1 in progress (updated 2026-10-21)

## Merged work
- PR #5 (licence compliance, Webmin independence, UI plan): merged 2026-10-19 as `bc7205e3`; 20/20 checks
  on `main`.
- PR #6 (release signing deferred, phased releases): merged 2026-10-19 as `8b0f017e`; 20/20 checks on
  `main` (run 39210847731).
- PR #7 (CI modernization: node24 actions pinned by SHA, manual runs, `govulncheck` v1.9.0): merged
  2026-10-20 as `259cf5bb`; 20/20 checks on `main` (run 39375492889).
- PR #8 (Phase 1 release workflow): merged 2026-10-21 as `296ebd0b`.
- PR #9 (Go 1.26.10 for GO-2026-6809): merged 2026-10-21 as `ec34b21e`; main CI run 39560364523 20/20.
- PR #10 (release closeout; v0.1.0-dev.1 published and verified; LA-13 closed for Phase 1): merged
  2026-10-21 as `31f39778`; main CI run 39570975685 20/20.

## M2 PR-1 (authorized 2026-10-21; D-1, D-6, D-7 approved)
- PR #11 (planning) merged 2026-10-21 as `6e0f6218`. Its last commit (the D-1/D-6/D-7 record and the D-12
  correction) missed the merge by a minute and is carried in PR-1b.
- **PR-1b (branch `feature/m2-pr1-state-audit-sessions-logs`, open, awaiting owner review):**
  - read-only `services.unit.logs` broker op (ADR 0008 Amendment 1), API
    `GET /api/v1/services/units/{unit}/logs`, scope `services.logs` (not granted by default);
  - logs panel on Services;
  - hash-chained broker and API audit logs with journald checkpoints and `hlctl audit verify`
    (tamper-evident only; `docs/security/AUDIT-LOG.md`);
  - fail-closed audit for broker results and log reads; `hlctl user scopes`.
- **PR-1a (next):** SQLite state (modernc.org/sqlite), persistent sessions with revocation, API audit in the
  database, Audit and Sessions pages, upgrade test from v0.1.0-dev.1.
- **Still not authorized:** privileged host writes, start/stop/restart, plan/confirm/execute, root command
  execution, PAM/TOTP, storage configuration, Docker/VM management, theme engine, Webmin migration.
  D-2…D-5 and D-12 pending.

## CI (`.github/workflows/ci.yml`, 20 jobs)
- **Triggers:** push to `main`, every pull request, and a **manual run** (Actions tab → *ci* →
  *Run workflow*), which runs the same jobs with no inputs.
- **Coverage:**
  - lint, Go and Vitest tests;
  - security gate: govulncheck **v1.9.0**, npm audit, reuse, notices/DEP-5/SBOM drift, Webmin inventory, secret scan, shellcheck;
  - reproducible build;
  - DEB/RPM/Arch payload checks with lintian;
  - install matrix on Debian stable/oldstable, Ubuntu 22.04/24.04/26.04, Fedora, openSUSE Tumbleweed/Leap and Arch;
  - real systemd on Ubuntu 24.04 and 26.04;
  - browser e2e plus the theme CSP probe (Chromium, Firefox, WebKit);
  - upstream Webmin Perl tests;
  - release dry run through the same reusable workflow as the release (`release-build.yml`, exact
    toolchain pins): two runners plus Arch, reproducibility comparison, fail-closed candidate verification,
    negative tests, release notes.
- **Actions:** official `actions/*` releases on the node24 runtime, pinned by commit SHA, read-only token,
  no persisted git credentials, no dependency caches.

## License compliance and product independence
- **Release readiness: Phase 1 complete (2026-10-21).** LA-13 closed for Phase 1. First unsigned development
  prerelease **v0.1.0-dev.1** (tag → `ec34b21e`, release run 39562079197) published by the owner and verified
  after publication, including an independent rebuild outside GitHub (binaries and `.deb` byte-identical).
  Signing (Phase 3) and attestations (Phase 2) remain deferred; no stable releases or package repositories.
  Procedure: `docs/release/RELEASING.md`.
- Release **dry run** in CI: complete corresponding-source archive; binaries, DEB, RPM and Arch packages
  built only from it; archive build = Git build; two independent runs compared file by file. RPM and Arch
  nondeterminism found and fixed. Not tested with other toolchain versions or distributions.
- **Webmin independence:** distributed Homelab Control artifacts (packages, binaries, release source
  archives) contain no Webmin code, enforced in CI. `compat/webmin/` stays an unchanged
  development/reference subtree until separately authorized migration work (phase 1 is approved for its
  own PR). There is no open choice about bundling Webmin.
- Licences: core **GPL-3.0-or-later**; **MIT** for SDK, schemas (incl. `schemas/openapi/`), theme
  definitions and design tokens; SPDX headers, `REUSE.toml`, `reuse lint` in CI; DEP-5 `copyright` in the
  `.deb`. `compat/webmin/` has a generated **per-file licence inventory** (not relicensed; 835 third-party
  files with no stated licence marked unverified) and is excluded from packages and source archives.
- Approved policies: `CONTRIBUTING.md` (DCO, AI disclosure; history grandfathered), "The Homelab Control
  Authors". Follow-ups: CI sign-off check, `SECURITY.md`, Archivo removal (LA-06), Webmin phase 1.
- UI/theme engine: planning only (`docs/ui/`); `.hltheme` single JSON (ADR 0010); layout preferences
  separate from themes; CSSOM/CSP probe passes in Chromium, Firefox and WebKit. **Nothing implemented.**

## Working (read-only), verified — evidence in `docs/evidence/m1/README.md`
- **hc-api** (unprivileged): sessions/CSRF/RBAC from M0; new scopes `services.read`, `hardware.read`;
  `/capabilities`, `/services`, `/hardware`, `/legacy/webmin`.
- **hc-broker** (root, socket-activated; M2 PR-1b adds a fifth, argument-taking read-only op, `services.unit.logs`): peer-uid allow-list, strict framing, four read-only argument-free
  ops (`broker.info`, `legacy.webmin.status`, `hardware.dmi.identity`, `services.systemd.list`); only
  `internal/broker/runner` executes programs, as `homelab-control`; capabilities limited to CAP_SETUID|CAP_SETGID.
- **UI**: capability-aware navigation; Services, Hardware, Legacy Webmin pages; degrades when broker is down.
- **Packaging**: CI-verified install → run → remove on Debian stable/oldstable, Ubuntu 22.04/24.04/26.04,
  Fedora, openSUSE Tumbleweed/Leap and Arch (containers, no systemd); package + hardened units under real
  systemd on Ubuntu 24.04 (runner VM) and 26.04 (systemd 259.5 container).
- **Guards**: exec only in runner; hc-api/hlctl never link broker internals; OpenAPI contract test.

## Owner decisions recorded
- **2026-10-16 (M1 closeout):**
  - ADR 0001 and 0003 accepted (direction).
  - ADR 0008 accepted for the four existing read-only ops only.
  - ADR 0005 accepted in principle at that point, with release blocked on a licence audit.
  - PAM deferred (`docs/security/PAM-AUTH-DEFERRED.md`).
- **2026-10-16 (compliance PR #5, later the same day):**
  - ADR 0005 accepted after the audit (GPL-3.0-or-later core; MIT SDK/schemas/themes/tokens).
  - ADR 0009 accepted with **mandatory** Sigstore and OpenPGP/hardware-token signing.
  - DCO policy approved.
  - Webmin excluded from release source archives.
- **2026-10-19:** ADR 0009 **revised**; this **supersedes** the 2026-10-16 signing requirement.
  - Phase 1: unsigned development/prereleases, published manually by the owner.
  - Phase 2: optional keyless Sigstore attestations.
  - Phase 3: OpenPGP/hardware-token signing, deferred.
  - No signing keys are current prerequisites.

**None of this authorizes privileged write operations.** M2 remains on hold.

## Verified on the owner's workstation VM (2026-10-16, product code `738fa37b`)
Ubuntu 26.04.1 KVM guest, systemd 259.5 (`dev/vm/`, evidence `docs/evidence/m1/vm-26.04/`):
systemd smoke + M1 checks **40/40** (hardening, socket-activated broker, auth, routes, host filesystems),
`systemd-analyze security` 1.6 (hc-api) / 1.5 (broker), rollback + Webmin coexistence **13/13**,
browser e2e **25/25**, axe WCAG 2.1 A/AA **0 violations** on 4 pages, final removal **4/4**.
CI run 38866773032 on the same commit: all 15 jobs green.

## Outstanding external validation (not verified)
- VM runs for distributions other than Ubuntu 26.04 (held until requested; 26.04 passed first).
- Upstream Webmin 2.680 not released; ~25 unreleased upstream security fixes since 2.670 — see
  `docs/security/WEBMIN-2.670-UPSTREAM-FIXES.md` (mitigations: RPC off, loopback bind, trusted admins).

## Security items that block M2 (must be designed and approved before any write operation)
1. **Broker trust:** a compromised hc-api can call every registered broker op. Safe today only because all
   four are read-only and argument-free. Write ops need per-operation plan/confirmation tokens bound to the
   user and target, target revalidation in the broker, exclusive locks and timeouts (ADR 0001/0008 follow-up).
2. **Audit durability:** audit is an append-only JSONL file and sessions/throttle are in process memory.
   Writes need durable, ordered audit and operation state that survive restarts (SQLite per ADR 0004)
   before the first privileged change.
3. **Step-up authentication:** no re-authentication or second factor before a privileged action; local
   accounts have no MFA.
4. **Argument-taking broker ops:** none exist yet (PAM would be the first). Input validation, size limits
   and secret handling for broker arguments need their own reviewed design.
5. ~~Validation gap~~ — closed 2026-10-16: hardening and socket activation verified on the workstation
   KVM VM (Ubuntu 26.04).
No licence/release blocker remains for Phase 1 prereleases (LA-13 closed 2026-10-21). CI and releases build with the same exact Go version (1.26.10, `packaging/release/toolchain.env`); govulncheck is pinned (v1.9.0).

## Not done / limits
- Earlier M1 evidence (d1077e4a) was never committed and is withdrawn (see evidence README).
- Ubuntu 22.04 under real systemd: not verified (CI covers 24.04 runner and 26.04 container; VM covers 26.04).
- PAM login deferred; upstream pin 2.670; licence decided (ADR 0005: GPL-3.0-or-later core, MIT SDK/schemas/themes/tokens); release workflow implemented (draft only), no release published yet (ADR 0009).
- Sessions/throttle in memory; no TLS management; govulncheck runs in CI only (v1.9.0 on Go 1.26.10).
- No host modifications or storage writes exist (by design).
