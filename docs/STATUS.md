# Current status — end of M1 (updated 2026-10-16, commit `da41162a`)

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

## Not done / limits
- Browser + axe WCAG run against the packaged stack (`tests/e2e/run-m1-e2e.sh`) not re-run on the final
  commit and not in CI; no screenshots this session. Earlier M1 evidence was never committed (see evidence README).
- Ubuntu 22.04 under real systemd: only the earlier, uncommitted run; CI covers 24.04/26.04.
- No full VM run (no hypervisor); Vagrant file still untested.
- PAM login (deferred), upstream pin still 2.670 (2.680 not released), license decision pending (ADR 0005).
- Sessions/throttle in memory; no TLS management; govulncheck runs in CI only (passes on Go 1.26.x).
- No host modifications or storage writes exist (by design).
