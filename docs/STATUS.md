# Current status — M1 closeout + compliance audit (updated 2026-10-16)

## License compliance and product independence (branch `compliance-audit`, PR #5, awaiting owner review)
- **Release readiness: not ready.** Blockers: LA-02 (GPL-3.0 `-only` vs `-or-later`, owner decision),
  LA-03 (MIT scope for SDK/schemas/themes/design tokens, owner decision), LA-13 (no release process for
  corresponding source, checksums, signatures). See `docs/compliance/LICENSE-AUDIT.md` and
  `docs/compliance/RELEASE-COMPLIANCE.md`.
- Fixed in PR #5: third-party notices (MIT, OFL-1.1, BSD-3 texts) now ship in deb/rpm/Arch; package
  metadata adds BSD-3-Clause; SPDX 2.3 SBOM (`docs/compliance/sbom/`); CI payload checks reject any
  Webmin/Perl/CGI file and require notices.
- Webmin: packages contain zero Webmin files; runtime use is optional read-only detection only
  (`docs/compliance/WEBMIN-INDEPENDENCE.md`). Migration phases 1–3 await approval; `compat/webmin/` unchanged.
- UI/theme engine: planning documents only (`docs/ui/`); **nothing implemented**.

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
failed on EOL Go 1.24), and the license audit blockers above (LA-02, LA-03, LA-13).

## Not done / limits
- Earlier M1 evidence (d1077e4a) was never committed and is withdrawn (see evidence README).
- Ubuntu 22.04 under real systemd: not verified (CI covers 24.04 runner and 26.04 container; VM covers 26.04).
- PAM login deferred; upstream pin 2.670; license split accepted in principle; audit done, decisions pending (ADR 0005, PR #5).
- Sessions/throttle in memory; no TLS management; govulncheck runs in CI only (passes on Go 1.26.x).
- No host modifications or storage writes exist (by design).
