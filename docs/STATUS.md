# Current status — end of M1 (2026-10-15)

## Working (read-only), verified — evidence in docs/evidence/m1/
- **hc-api** (unprivileged): sessions/CSRF/RBAC from M0; new scopes `services.read`, `hardware.read`;
  `/capabilities`, `/services`, `/hardware`, `/legacy/webmin`.
- **hc-broker** (root, socket-activated): peer-uid allow-list, strict framing, four read-only argument-free
  ops (`broker.info`, `legacy.webmin.status`, `hardware.dmi.identity`, `services.systemd.list`); only
  `internal/broker/runner` executes programs, as `homelab-control`; capabilities limited to CAP_SETUID|CAP_SETGID.
- **UI**: capability-aware navigation; Services, Hardware, Legacy Webmin pages; degrades when broker is down.
- **Packaging**: `.deb` validated under real systemd (Ubuntu 22.04/24.04) and without systemd; binary `.rpm` builds.
- **Guards**: exec only in runner; hc-api/hlctl never link broker internals; OpenAPI contract test.

## Not done / limits
- CI matrix (Debian, Fedora, openSUSE, Arch, runner systemd, browser) written but **never executed** — push the branch to run it.
- Last commit (virtual-disk media type fix) verified by `make check` only.
- No full VM run (no hypervisor); Vagrant file still untested.
- PAM login (deferred), upstream pin still 2.670 (2.680 not released), license decision pending (ADR 0005).
- Sessions/throttle in memory; no TLS management; govulncheck not run locally.
- No host modifications or storage writes exist (by design).
