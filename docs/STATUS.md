# Current status — M1 closeout (updated 2026-10-16)

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

## Outstanding external validation (not verified)
- **Workstation VM run (Ubuntu 26.04, libvirt/KVM):** harness ready in `dev/vm/` (guide: `dev/vm/README.md`),
  **not executed** — no shell or computer-use access to the workstation from the session that wrote it.
  It covers install, hardening, broker socket activation, auth/routes, remove/reinstall/purge rollback,
  Webmin coexistence, browser e2e and axe.
- **axe/WCAG scan:** no current evidence until that VM run (axe is now mandatory there, not skippable).
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
5. **Validation gap:** hardening and socket activation are verified in CI containers and a runner VM but
   not yet on the workstation VM; finish that run before M2 builds on these units.
Not M2-blocking but release-blocking: release builds must use a supported Go toolchain (govulncheck
failed on EOL Go 1.24), and ADR 0005's license audit.

## Not done / limits
- Browser + axe WCAG run against the packaged stack (`tests/e2e/run-m1-e2e.sh`) not re-run on the final
  commit and not in CI; no screenshots this session. Earlier M1 evidence was never committed (see evidence README).
- Ubuntu 22.04 under real systemd: only the earlier, uncommitted run; CI covers 24.04/26.04.
- PAM login deferred; upstream pin 2.670; license split accepted in principle, audit pending (ADR 0005).
- Sessions/throttle in memory; no TLS management; govulncheck runs in CI only (passes on Go 1.26.x).
- No host modifications or storage writes exist (by design).
