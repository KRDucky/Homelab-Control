# Current status — M1 closeout + compliance audit (updated 2026-10-16)

## License compliance and product independence (branch `compliance-audit`, PR #5, awaiting owner review)
- **Release readiness: not ready.** One blocker: LA-13 — publishing/signing designed (ADR 0009,
  `docs/compliance/RELEASE-PIPELINE.md`), not approved or enabled. A release **dry run** runs in CI: it
  builds a complete corresponding-source archive, rebuilds the binaries only from it and requires them to
  be byte-identical to a Git build.
- Owner decisions 2026-10-16, implemented: core **GPL-3.0-or-later** (explicit grant), **MIT** for SDK,
  schemas (incl. OpenAPI), theme definitions and design tokens (exact boundaries in `REUSE.toml`); SPDX
  headers + `reuse lint` in CI; DEP-5 `copyright` in the `.deb` (lintian-checked); synthetic PCI fixture;
  exceptions EX-02…EX-06 accepted under their conditions.
- Proposed, awaiting approval: `CONTRIBUTING.md` (DCO), ADR 0009. Planned separately: Archivo font removal
  (LA-06), Webmin migration phase 1 (approved for its own PR).
- Webmin: packages contain zero Webmin files; runtime use is optional read-only detection only
  (`docs/compliance/WEBMIN-INDEPENDENCE.md`). `compat/webmin/` unchanged.
- UI/theme engine: planning only (`docs/ui/`). `.hltheme` single JSON accepted in principle (ADR 0010);
  CSSOM theme application under the real CSP verified by a browser probe. **Nothing implemented.**

## Working (read-only), verified — evidence in `docs/evidence/m1/README.md`
- **hc-api** (unprivileged): sessions/CSRF/RBAC from M0; new scopes `services.read`, `hardware.read`;
  `/capabilities`, `/services`, `/hardware`, `/legacy/webmin`.
- **hc-broker** (root, socket-activated): peer-uid allow-list, strict framing, four read-only argument-free
  ops (`broker.info`, `legacy.webmin.status`, `hardware.dmi.identity`, `services.systemd.list`); only
  `internal/broker/runner` executes programs, as `homelab-control`; capabilities limited to CAP_SETUID|CAP_SETGID.
- **UI**: capability-aware navigation; Services, Hardware, Legacy Webmin pages; degrades when broker is down.
- **Packaging**: CI-verified install → run → remove on Debian stable/oldstable, Ubuntu 22.04/24.04/26.04,
  Fedora, openSUSE Tumbleweed/Leap and Arch (containers, no systemd); package + hardened units under real
  systemd on Ubuntu 24.04 (runner VM) and 26.04 (systemd 259.5 container).
- **Guards**: exec only in runner; hc-api/hlctl never link broker internals; OpenAPI contract test.

## Owner decisions recorded (2026-10-16)
ADR 0001 and 0003 accepted (direction); ADR 0008 accepted for the four existing read-only ops only;
ADR 0005 accepted in principle, release blocked on a license compatibility/attribution audit;
PAM deferred (`docs/security/PAM-AUTH-DEFERRED.md`). **None of this authorizes privileged write operations.**

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
Not M2-blocking but release-blocking: release builds must use a supported Go toolchain (govulncheck
failed on EOL Go 1.24), and the remaining license blocker above (LA-13).

## Not done / limits
- Earlier M1 evidence (d1077e4a) was never committed and is withdrawn (see evidence README).
- Ubuntu 22.04 under real systemd: not verified (CI covers 24.04 runner and 26.04 container; VM covers 26.04).
- PAM login deferred; upstream pin 2.670; licence decided (ADR 0005: GPL-3.0-or-later core, MIT SDK/schemas/themes/tokens); release publishing pending (ADR 0009).
- Sessions/throttle in memory; no TLS management; govulncheck runs in CI only (passes on Go 1.26.x).
- No host modifications or storage writes exist (by design).
