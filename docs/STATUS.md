# Current status — M2-2 (service start/stop/restart) merged 2026-10-26, E2 outstanding; Phase 1 release, M2 PR-1 and HCIS merged (updated 2026-10-27)

Reconciled against `main` at `a3447efa` (the merge commit of PR #28). "Merged" means a merge commit in
`main`'s first-parent history (PR #27 is the one exception, described below); dates are from the merge-commit
timestamps. "CI n/n" is the number of GitHub Actions check runs that succeeded out of those run on the PR's
head commit, read from GitHub on 2026-10-25 for PRs #5–#24 (it is not a statement about later commits). For
PRs #25–#28 only each workflow run's overall result is recorded, because the public run pages do not show
individual job conclusions.

## M2-2: controlled systemd service management (authorized 2026-10-23; merged 2026-10-26, PR #28, merge commit `a3447efa`)

The owner authorized M2-2 and approved D-2 (broker-issued plan → confirm → execute), D-3 (password
step-up), D-4 (`root-exec` runner class) and D-5 (built-in protected-unit policy), option A each
(`docs/m2/OWNER-DECISIONS.md`). It was delivered as two stacked PRs: M2-2a, the framework (PR #27, one
commit, `457eddf1`), and M2-2b, operations, API and UI (PR #28). GitHub reports #27 as merged (page fetched
2026-10-27: "KRDucky merged commit 457eddf into main", Oct 26); its commit entered `main` through PR #28, and
there is no separate merge commit for it.

Commits named in this section: `970341e6` is the local and container evidence commit; `987ba011` is the PR #28
head; `a3447efa` is the merge commit, whose tree is identical to `987ba011`'s. `987ba011` differs from
`970341e6` by four frontend/test files (from merging PR #26's changes into the branch) and documentation.

- **What exists on `main`:** start, stop and restart of loaded `.service` units from the Services
  page, scope `services.operate` (never granted by default); broker plan tokens (HMAC-SHA256, 120 s,
  single use, bound to op, action, canonical unit, actor, session and observed state); per-unit locks;
  revalidation; a hash-chained, fsync'ed operation journal with interrupted-operation recovery; step-up
  re-authentication for disruptive operations (5 min, per session); a fixed protected-unit policy
  applied to aliases, unit files, template bases and, transitively, dependencies; outcomes verified by
  reading the unit state back. Design: ADR 0011 and ADR 0008 Amendment 2. Results:
  `docs/m2/M2-2-ACCEPTANCE.md`. User guide: `docs/user/SERVICE-MANAGEMENT.md`.
- **Still not authorized:** M2-3 and later milestones; enable/disable, mask/unmask, unit-file editing,
  daemon-reload, other unit types, host shutdown or reboot, arbitrary commands, storage, Docker,
  packages, users and groups, third-party modules.
- **Acceptance evidence, by source** (details and exact gaps: `docs/m2/M2-2-ACCEPTANCE.md`,
  `docs/evidence/m2/`):
  - **Local and disposable containers (no VM), on `970341e6`:** `make check`; Go tests as non-root;
    Vitest 63; Playwright on Chromium and WebKit; M2-2 real-systemd scenario 86/86 on Ubuntu 26.04.1
    (systemd 259.9) and on Fedora 43 (systemd 258.11), including the token and step-up expiry waits. Not
    re-run on `987ba011` or `a3447efa`.
  - **GitHub CI:** the workflow concluded success overall on ci #83 (PR #27 head `457eddf1`), ci #84
    (`987ba011`) and ci #85 (`main`, `a3447efa`, run 40292176133); each reports 12 notices and no warnings or
    errors. The workflow is configured to run the M2-2 service-operations scenario in short mode on Ubuntu
    24.04 (runner) and 26.04 (container) and the service-operations dialog test on Chromium, Firefox and
    WebKit. **Individual job conclusions and logs were not seen**, so execution of those particular checks in
    CI is not verified. The expiry waits (`HC_M2_LONG`) are not configured in CI, and CI has no non-Debian
    real-systemd job; that evidence is container-only.
  - **Binding checklist (`docs/m2/M2-SECURITY-ACCEPTANCE.md`: every item in §1–§3 must pass before merge):**
    PR #28 was merged while E2 had no evidence and E4's required VM evidence was missing (only a container
    check exists). No evidence is linked in the PR description; other locations (comments, linked discussions)
    were not inspected, and no review was submitted on the PR page (review elsewhere is not verified). No owner
    exception to that condition is recorded in the repository. By the owner's decision of 2026-10-27 (message
    "HomeControl — Documentation Reconciliation Decisions", 00:38 America/Chicago, after the merge; quoted in
    `docs/m2/M2-2-ACCEPTANCE.md`), **E2 is deferred, not waived and not passed**, and is required before
    `services.operate` is enabled in a real deployment. That decision names E2 only; it does not make any
    criterion pass.
- **Latest CI on `main`:** ci #85 concluded success overall.

## UI foundation continuation (2026-10-25, merged in PR #24)

The owner authorized completing the remaining frontend and adding a user Theme Development Kit. PR #24 implements shared responsive tables/states, navigation-only palette, browser-local theme import/export and Theme Studio, and the MIT TDK. [Implementation handoff](ui/11-IMPLEMENTED-UI-AND-TDK.md) records the supported contract. This is frontend authorization; no host-operation permission or server settings store is added.

**Final design review (2026-10-26):** design accepted; seven defects (DT-1 to DT-7) fixed in PR #25, with follow-up fixes in PR #26 (both merged 2026-10-26). See [docs/ui/12](ui/12-FINAL-DESIGN-REVIEW-AND-HANDOFF.md) for what was and was not verified (Chromium locally; no real broker data, screen reader or Windows High Contrast device).

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
- PR #22 (CI results and PRs #20-#21 recorded here; documentation only): merged 2026-10-26 as `531dedb6`; CI
  21/21 on head `36cfe527`.
- PR #23 (CI job count and status; documentation only): merged 2026-10-26 as `5a444a9a`.
- PR #24 (HCIS frontend foundation and Theme Development Kit): merged 2026-10-26 as `f6af207c`; the merge
  commit records CI 21/21 on head `71b93cad`, with DCO sign-off waived by the owner for that PR.
- PR #25 (final HCIS design review fixes; documentation reconciliation): merged 2026-10-26 as `ccab63ce`.
  The CI run on that merge commit (ci #80) failed; its cause was not determined (the run page shows no
  annotation). The next merge to `main` (PR #26) concluded success (ci #82).
- PR #26 (HCIS review follow-up: disclosure targets and signed-out title): merged 2026-10-26 as
  `4194396b`; main ci #82 concluded success.
- PR #27 (M2-2a, write-authorization framework): GitHub reports it merged (page fetched 2026-10-27:
  "KRDucky merged commit 457eddf into main", Oct 26). Its single commit `457eddf1` entered `main` through
  PR #28; there is no separate merge commit for it.
- PR #28 (M2-2b, service start/stop/restart, API and UI): merged 2026-10-26 as `a3447efa` (tree identical
  to the PR head `987ba011`). No evidence is linked in its description (other locations not verified) and no
  review was submitted on its page (review elsewhere not verified). Main ci #85 (run 40292176133) concluded
  success overall; see the M2-2 section above for what that does and does not establish.

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
- **Not in a release:** v0.1.0-dev.1 (tag → `ec34b21e`) predates M2 PR-1, M2-2 and the HCIS steps. No release has
  been published since.
- **Known limits:**
  - login throttling is still in memory;
  - no audit retention or rotation;
  - no admin management of other users' sessions.
- **Authorization at the time of PR-1:** no privileged host writes. Since 2026-10-23 M2-2 (service
  start/stop/restart, plan/confirm/execute, `root-exec`) is authorized; see the M2-2 section above.
  PAM/TOTP, storage configuration, Docker/VM management and Webmin migration remain unauthorized.
  D-8…D-12 are open (`docs/m2/OWNER-DECISIONS.md`).

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
- **Implemented in PR #24:** shared DataTable with responsive cards, status/live states, navigation-only command palette, browser-local `.hltheme` import/export and Theme Studio, and TDK. See the UI handoff above.
- **Still future:** server settings store, resource search, workspaces, dashboard widgets, jobs/notifications and module views requiring backend support.


## CI (`.github/workflows/ci.yml`)
- **Triggers:** push to `main`, every pull request, and a **manual run** (Actions tab → *ci* →
  *Run workflow*), which runs the same jobs with no inputs.
- **Coverage:**
  - lint, Go and Vitest tests;
  - security gate: govulncheck **v1.9.0**, npm audit, reuse, notices/DEP-5/SBOM drift, Webmin inventory, secret scan, shellcheck;
  - reproducible build;
  - DEB/RPM/Arch payload checks with lintian;
  - install matrix on Debian stable/oldstable, Ubuntu 22.04/24.04/26.04, Fedora, openSUSE Tumbleweed/Leap and Arch;
  - real systemd on Ubuntu 24.04 and 26.04, configured to run the M2-2 service-operations scenario in
    short mode (the token and step-up expiry waits need `HC_M2_LONG=1`, which CI does not set);
  - browser e2e plus the theme CSP probe (Chromium, Firefox, WebKit), and the M2-2 service-operations
    dialog test configured for the same engines;
  - upstream Webmin Perl tests;
  - upgrade test from the published v0.1.0-dev.1 package (`upgrade-ubuntu`, PR #13) and 60-second fuzz
    runs of the log-argument (`FuzzLogsArgs`) and write-operation argument (`FuzzWriteArgs`) fuzzers;
  - release dry run through the same reusable workflow as the release (`release-build.yml`, exact
    toolchain pins): two runners plus Arch, reproducibility comparison, fail-closed candidate verification,
    negative tests, release notes.
- **Job count:** 21 check runs on every PR head from #13 to #22, all green. PR #12 had 20; the difference is
  the `upgrade-ubuntu` job added by #13 (compared by check-run name). Not re-counted for #25–#28.
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
- Browser-local theme import/export, Studio and TDK are implemented and merged (PR #24). Themes are colours only, with a fixed built-in base; preferences remain separate. Server-wide persistence remains future work.


## Working, verified (read-only except M2-2 service operations) — M1 evidence in `docs/evidence/m1/README.md`; M2 PR-1 and HCIS records in `docs/project/09-PROJECT-MEMORY.md`
- **hc-api** (unprivileged): sessions/CSRF/RBAC from M0; scopes `services.read`, `hardware.read` (M1) and
  `services.logs`, `audit.read` (M2 PR-1) and `services.operate` (M2-2), none granted by default; `/capabilities`, `/services`,
  `/hardware`, `/legacy/webmin`, `/services/units/{unit}/logs`, `/auth/sessions` (list, revoke own
  sessions), `/audit`, and for M2-2 `/services/units/{unit}/plan`, `/services/units/{unit}/apply`,
  `/auth/step-up` and `/operations`.
- **hc-broker** (root, socket-activated): peer-uid allow-list, strict framing, eight ops: four
  argument-free read-only (`broker.info`, `legacy.webmin.status`, `hardware.dmi.identity`,
  `services.systemd.list`), the argument-taking read-only `services.unit.logs` (PR #12), `services.unit.plan`
  (unprivileged, read-only) and `operations.journal.list` (read-only), and one write, `services.unit.apply`
  (start/stop/restart; `root-exec`); only `internal/broker/runner` executes programs, as
  `homelab-control`, or as root for the fixed `systemctl` templates of `root-exec`; capabilities limited
  to CAP_SETUID|CAP_SETGID.
- **UI**: capability-aware navigation; Services (with a logs panel and, for `services.operate`, start/stop/restart), Hardware, Legacy Webmin, Audit and
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
  and D-7 (new scopes not granted by default) approved. D-2…D-5 and D-8…D-12 were open at that date; D-2…D-5 were approved on 2026-10-23 (below) and
  D-8…D-12 remain open (`docs/m2/OWNER-DECISIONS.md`).
- **2026-10-21/22 (HCIS):** HCIS adopted as the UI design direction; density names, the OLED theme and the
  font licensing policy decided 2026-10-22. Migration step 1 approved 2026-10-22; steps 2 and 3 authorized
  2026-10-23 (`docs/ui/hcis/README.md`, project memory). The owner authorized the remaining frontend and TDK on 2026-10-25 (UI continuation above).

- **2026-10-23 (M2-2):** M2-2 authorized; D-2, D-3, D-4 and D-5 approved (option A each). ADR 0011
  accepted; ADR 0008 Amendment 2 (`root-exec`). Only service start/stop/restart; everything else in M2
  still needs its own authorization.

- **2026-10-27 (post-M2-2 documentation reconciliation; owner message "HomeControl — Documentation Reconciliation
  Decisions", 00:38 America/Chicago; quoted in `docs/m2/M2-2-ACCEPTANCE.md`):** acceptance item E2
  (workstation-VM browser validation) is **deferred, not waived**, and is required before `services.operate`
  is enabled in a real deployment; it is not described as passed anywhere. The decision names E2 only. M2-3 and
  later milestones remain unauthorized. Failed CI run #80 is recorded in the merged-work history only; its cause
  is unknown, and the latest run on `main` (ci #85) concluded success.

## Verified on the owner's workstation VM (2026-10-16, product code `738fa37b`)
Ubuntu 26.04.1 KVM guest, systemd 259.5 (`dev/vm/`, evidence `docs/evidence/m1/vm-26.04/`):
systemd smoke + M1 checks **40/40** (hardening, socket-activated broker, auth, routes, host filesystems),
`systemd-analyze security` 1.6 (hc-api) / 1.5 (broker), rollback + Webmin coexistence **13/13**,
browser e2e **25/25**, axe WCAG 2.1 A/AA **0 violations** on 4 pages, final removal **4/4**.
CI run 38866773032 on the same commit: all 15 jobs green.

## Outstanding external validation (not verified)
- **E2:** M2-2 on the owner's workstation VM through a browser (Ubuntu 26.04 and one non-Debian family).
  Outstanding: deferred by the owner on 2026-10-27, not waived, not passed; required before
  `services.operate` is enabled in a real deployment.
- **E4:** `systemd-analyze security` on a VM (the criterion asks for VM evidence); only the container check
  has passed.
- Individual CI job conclusions and logs for M2-2 (not visible on the public run pages).
- M2-2 under real systemd on Debian, openSUSE and Arch; the service-operation dialog with a screen
  reader and Windows High Contrast; Firefox run locally.
- VM runs for distributions other than Ubuntu 26.04 (held until requested; 26.04 passed first).
- Upstream Webmin 2.680 not released; ~25 unreleased upstream security fixes since 2.670 — see
  `docs/security/WEBMIN-2.670-UPSTREAM-FIXES.md` (mitigations: RPC off, loopback bind, trusted admins).

## Security items that blocked M2 writes (status after M2-2)
Items 1, 3 and 4 are addressed for service start/stop/restart by ADR 0011 (plan tokens, revalidation,
locks, step-up, strict arguments); item 2 by the operation journal. The residual limitation of item 1
(a compromised `hc-api` can request any operation the broker's policy allows) is accepted and
documented in ADR 0011. E2 (VM validation) is outstanding. The original list is kept below for
reference.

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
- No storage-management or configuration-management operations exist. The only privileged host-changing
  operation is starting, stopping and restarting systemd `.service` units (M2-2, scope `services.operate`),
  and a service's start or stop has that service's own effects. Homelab Control also writes its own state
  database, audit logs and operation journal.
