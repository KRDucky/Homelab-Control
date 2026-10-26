# Current status — Phase 1 release complete; M2 PR-1 and HCIS steps 1–3 merged (updated 2026-10-25)

Reconciled against `main` at `1553908b` (merge of PR #21). "Merged" means a merge commit in `main`'s
first-parent history; dates are UTC, from the merge-commit timestamps. "CI n/n" is the number of
GitHub Actions check runs that succeeded out of those run on the PR's head commit, read from GitHub on
2026-10-25 (it is not a statement about later commits).

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
- PR #12 (M2 PR-1b: read-only service logs, hash-chained audit): merged 2026-10-21 as `04358bbf`; CI 20/20
  on head `2a5e699a`.
- PR #13 (M2 PR-1a: SQLite state, persistent sessions, audit trail, Audit and Sessions pages): merged
  2026-10-22 as `c012f299`; CI 21/21 on head `2dd78997`.
- PR #14 (HCIS design system; documents, a `REUSE.toml` licence annotation and a `CLAUDE.md` pointer, no
  application code): merged 2026-10-22 as `880309a4`; CI 21/21 on head `911cf736`.
- PR #15 (HCIS step 1, colour tokens): merged 2026-10-22 as `505f3709`; CI 21/21 on head `030f02bc`.
- PR #16 (HCIS font licensing policy; documentation only): merged 2026-10-23 as `870a69c5`; CI 21/21 on
  head `bec10e68`.
- PR #17 (HCIS step 2, bundled Inter and JetBrains Mono): merged 2026-10-23 as `e5ec4165`; CI 21/21 on
  head `738217a5`.
- PR #18 (README presents Homelab Control as an independent product; documentation only): merged
  2026-10-23 as `4bbef83a`; CI 21/21 on head `23e23073`.
- PR #19 (HCIS step 3, responsive shell and appearance preferences): merged 2026-10-23 as `16650cb6`; CI
  21/21 on head `8b71139a`.
- PR #20 (`NOTICE.md` lists `modernc.org/sqlite`; documentation only): merged 2026-10-26 as `f1b919bc`; CI
  21/21 on head `d76ad33d`.
- PR #21 (this file reconciled with M2 PR-1 and HCIS steps 1-3; documentation only): merged 2026-10-26 as
  `1553908b`; CI 21/21 on head `fc0e39f8`.

## M2 PR-1 (authorized 2026-10-21; D-1, D-6, D-7 approved): merged, read-only
- PR #11 (planning) merged 2026-10-21 as `6e0f6218`. Its last commit (the D-1/D-6/D-7 record and the D-12
  correction) missed the merge by a minute and was carried in PR-1b.
- **PR-1b (PR #12, merged 2026-10-21 as `04358bbf`):**
  - read-only `services.unit.logs` broker op (ADR 0008 Amendment 1), API
    `GET /api/v1/services/units/{unit}/logs`, scope `services.logs` (not granted by default);
  - logs panel on Services;
  - hash-chained broker and API audit logs with journald checkpoints and `hlctl audit verify`
    (tamper-evident only; `docs/security/AUDIT-LOG.md`);
  - fail-closed audit for broker results and log reads; `hlctl user scopes`;
  - a 60-second fuzz run of the `services.unit.logs` argument decoder on every CI run.
- **PR-1b CI:** run on `2a5e699a`, 20/20 green, including real systemd on 24.04 and 26.04.
- **PR-1a (PR #13, merged 2026-10-22 as `c012f299`; developed stacked on PR-1b):**
  - **State database:** `/var/lib/homelab-control/state.db`, using `modernc.org/sqlite` v1.60.1
    (ADR 0006 amendment 1; reviewed allow-list `backend/go-deps-allowlist.json`; `hc-broker` stays
    stdlib-only).
  - **Migrations:** transactional, with a backup before migrating and downgrade refusal.
  - **Persistent sessions (D-6):** hashes only, with own-session list and revoke.
  - **API audit:** hash-chained in the database; `hlctl audit verify --db` and `hlctl audit export --db`.
  - **New scope `audit.read`:** not granted by default (D-7).
  - **UI:** new Audit and Sessions pages.
  - **Release:** the source archive carries the Go module zips (`vendor-sources/go/download`) and builds
    offline from them.
  - **CI:** new `upgrade-ubuntu` job (v0.1.0-dev.1 → build).
- **Not in a release:** v0.1.0-dev.1 (tag → `ec34b21e`) predates M2 PR-1 and the HCIS steps. No release has
  been published since.
- **Known limits:**
  - login throttling is still in memory;
  - no audit retention or rotation;
  - no admin management of other users' sessions.
- **Still not authorized:** privileged host writes, start/stop/restart, plan/confirm/execute, root command
  execution, PAM/TOTP, storage configuration, Docker/VM management, theme engine, Webmin migration.
  D-2…D-5 and D-8…D-12 are open (`docs/m2/OWNER-DECISIONS.md`).

## HCIS UI migration (merged steps 1–3; each step is a separate, approved PR)
- **Design documents:** the HCIS design system (PR #14) and the font licensing policy (PR #16) changed no
  application code. Planned and Proposed components in `docs/ui/hcis/` are designs, not shipped features.
- **Step 1, colour tokens (PR #15):** generated HCIS colour tokens for Midnight, Daybreak, Graphite and
  OLED; the 12 M1 colour names alias them.
- **Step 2, typography (PR #17):** Inter and JetBrains Mono (OFL-1.1) are bundled and served from
  `hc-api`'s own origin, replacing Archivo. No font CDN.
- **Step 3, shell and appearance (PR #19):** responsive shell (sidebar, icon rail, drawer); appearance menu
  for theme, density, interface font, text size, surfaces and motion; scrolling tables as labelled,
  focusable regions. Preferences are stored per browser (`localStorage`) until the server settings store
  (`docs/ui/10-PHASED-PLAN.md`, phase U5).
- **Not implemented:** shared components (step 4 onward), the HCIS type scale, radii and page components
  (still the M1 implementation), the theme engine, `.hltheme` import, Theme Studio, a server-side settings
  store, command palette, workspaces and dashboard widgets.

## CI (`.github/workflows/ci.yml`)
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
  - upgrade test from the published v0.1.0-dev.1 package (`upgrade-ubuntu`, PR #13) and a 60-second fuzz
    run of the `services.unit.logs` argument decoder;
  - release dry run through the same reusable workflow as the release (`release-build.yml`, exact
    toolchain pins): two runners plus Arch, reproducibility comparison, fail-closed candidate verification,
    negative tests, release notes.
- **Job count:** 21 check runs on every PR head from #13 to #21, all green. PR #12 had 20; the difference is
  the `upgrade-ubuntu` job added by #13 (compared by check-run name).
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
  Authors". Follow-ups: CI sign-off check, `SECURITY.md`, Webmin phase 1. (Archivo removal, LA-06, was superseded by HCIS step 2: bundled Inter and JetBrains Mono.)
- Theme engine, `.hltheme` import and Theme Studio: planning only (`docs/ui/`); `.hltheme` single JSON
  (ADR 0010); layout preferences separate from themes; CSSOM/CSP probe passes in Chromium, Firefox and
  WebKit. **Not implemented.** `main` has only the fixed built-in HCIS themes and per-browser appearance
  preferences (HCIS section above).

## Working (read-only), verified — M1 evidence in `docs/evidence/m1/README.md`; M2 PR-1 and HCIS records in `docs/project/09-PROJECT-MEMORY.md`
- **hc-api** (unprivileged): sessions/CSRF/RBAC from M0; scopes `services.read`, `hardware.read` (M1) and
  `services.logs`, `audit.read` (M2 PR-1, never granted by default); `/capabilities`, `/services`,
  `/hardware`, `/legacy/webmin`, `/services/units/{unit}/logs`, `/auth/sessions` (list, revoke own
  sessions) and `/audit`.
- **hc-broker** (root, socket-activated): peer-uid allow-list, strict framing, five read-only ops: four
  argument-free (`broker.info`, `legacy.webmin.status`, `hardware.dmi.identity`, `services.systemd.list`)
  and, since PR #12, the argument-taking `services.unit.logs`; only `internal/broker/runner` executes
  programs, as `homelab-control`; capabilities limited to CAP_SETUID|CAP_SETGID.
- **UI**: capability-aware navigation; Services (with a logs panel), Hardware, Legacy Webmin, Audit and
  Sessions pages; HCIS colours, fonts, responsive shell and appearance menu; degrades when broker is down.
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

- **2026-10-21 (M2 readiness):** M2 PR-1 authorized; D-1 (`modernc.org/sqlite`), D-6 (persistent sessions)
  and D-7 (new scopes not granted by default) approved. D-2…D-5 and D-8…D-12 remain open
  (`docs/m2/OWNER-DECISIONS.md`).
- **2026-10-21/22 (HCIS):** HCIS adopted as the UI design direction; density names, the OLED theme and the
  font licensing policy decided 2026-10-22. Migration step 1 approved 2026-10-22; steps 2 and 3 authorized
  2026-10-23 (`docs/ui/hcis/README.md`, project memory). Step 4 (shared components) onward needs separate
  approval.

**None of this authorizes privileged write operations.** M2 beyond PR-1 remains on hold.

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
   five are read-only (four take no arguments; `services.unit.logs` takes validated ones). Write ops need per-operation plan/confirmation tokens bound to the
   user and target, target revalidation in the broker, exclusive locks and timeouts (ADR 0001/0008 follow-up).
2. **Audit durability (partly addressed):** PRs #12 and #13 merged a SQLite state store, persistent
   sessions (hashes only) and hash-chained, tamper-evident audit logs that fail closed for broker results
   and log reads. Still open: the login throttle is in memory, there is no audit retention or rotation,
   and no operation state exists. Writes need durable, ordered audit and operation state before the first
   privileged change; whether the merged work meets that is for the M2 security review
   (`docs/m2/M2-SECURITY-ACCEPTANCE.md`), not decided here.
3. **Step-up authentication:** no re-authentication or second factor before a privileged action; local
   accounts have no MFA.
4. **Argument-taking broker ops (partly addressed):** one exists, the read-only `services.unit.logs`
   (PR #12; ADR 0008 Amendment 1: strict decoding, validation in both tiers, fuzz test). Argument
   handling for write operations, PAM and secrets still needs its own reviewed design.
5. ~~Validation gap~~ — closed 2026-10-16: hardening and socket activation verified on the workstation
   KVM VM (Ubuntu 26.04).

The full blocker list, B1–B5, is in `docs/m2/M2-READINESS.md`; B5 (root child processes) is not repeated here.

No licence/release blocker remains for Phase 1 prereleases (LA-13 closed 2026-10-21). CI and releases build with the same exact Go version (1.26.10, `packaging/release/toolchain.env`); govulncheck is pinned (v1.9.0).

## Not done / limits
- Earlier M1 evidence (d1077e4a) was never committed and is withdrawn (see evidence README).
- Ubuntu 22.04 under real systemd: not verified (CI covers 24.04 runner and 26.04 container; VM covers 26.04).
- PAM login deferred; upstream pin 2.670; licence decided (ADR 0005: GPL-3.0-or-later core, MIT SDK/schemas/themes/tokens); one unsigned development prerelease, v0.1.0-dev.1, published 2026-10-21 (ADR 0009), and none since.
- The login throttle is in memory (sessions are persisted since PR #13); no TLS management; govulncheck runs in CI only (v1.9.0 on Go 1.26.10).
- No host modifications or storage writes exist (by design), apart from Homelab Control's own state database
  and audit logs.
