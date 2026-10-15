# M1 implementation plan (proposed, not started)

**Goal (roadmap):** log in, navigate, view real read-only host info, fall back to legacy Webmin.
**Builds on M0:** unprivileged `hc-api`, sessions/CSRF/RBAC, read-only overview, pinned upstream 2.670.
**Out of scope for M1:** any write operation on the host, plugins/themes, storage management.

## Prioritized work items

| # | Item | Depends on | Size | Acceptance evidence |
|---|---|---|---|---|
| 1 | **Run the Vagrant VM** on the owner's workstation; repeat M0 evidence there (Webmin baseline login, hc-api e2e, `.deb` install **under real systemd** with the hardened unit) | M0 | S | Logs + screenshots in `docs/evidence/m1/` |
| 2 | **Move upstream pin to 2.680** once tagged (or backport the listed post-2.670 security commits as recorded patches) | upstream release | S | `verify-upstream.sh`, `prove` suite, login test |
| 3 | **Broker skeleton** `hc-broker`: root service, Unix socket 0660 `root:homelab-control`, `SO_PEERCRED` check, JSON request envelope with op name + schema-validated args + forwarded user/scope, timeout, output cap, audit, op registry with **zero generic ops**; guard tests extended so `os/exec` is allowed only in `backend/broker/ops/` | ADR 0001 approval | L | Unit tests, fuzz test of the decoder, socket permission tests, review sign-off |
| 4 | **First read-only broker ops**: `legacy.webmin.status` (installed, version, port, TLS, bind — reads root-only `miniserv.conf`), `services.systemd.list` (D-Bus, read-only), `hardware.inventory` (PCI/USB/block devices via sysfs/udev; identity by WWN/serial, kernel names labeled volatile) | 3 | M | Fixture tests + VM run |
| 5 | **PAM login via broker** (`auth.pam.verify`), allowed only for members of a configured group mapped to scopes; local accounts remain as fallback; throttling preserved | 3 | M | Tests with a PAM test service in the VM; no shadow access from hc-api |
| 6 | **UI navigation shell**: router, sections Overview / Services / Hardware / Legacy; capability-aware nav (hide/explain unavailable sections); legacy link uses detected port | 4 | M | Playwright keyboard + a11y checks, mobile layout |
| 7 | **OpenAPI contract tests**: generate/validate responses against `schemas/openapi/homelab-control.v1.yaml` in Go tests | M0 | S | CI job |
| 8 | **CI hardening**: pin Actions by SHA, add shellcheck, govulncheck, Playwright e2e job, `.deb` build artifact, license report | M0 | S | Green workflow on GitHub |
| 9 | **License decision applied** (ADR 0005): SPDX headers, `LICENSES/`, MIT for SDK/schemas/tokens | owner decision | S | REUSE lint |
| 10 | **Arch/CachyOS PKGBUILD** tested on the owner's host; Fedora spec built in a Fedora VM | 1 | S | Install/remove logs |

Suggested order: 1 → 8 → 2 → 3 → 4 → 7 → 5 → 6 → 9/10 in parallel.

## Risk register

| Risk | Category | Likelihood | Impact | Level | Mitigation | Owner | Status |
|---|---|---|---|---|---|---|---|
| Broker op design leaks into a generic executor ("just one more flag") | Security | Medium | High | **High** | Closed op registry; each op needs schema, scope, tests and reviewer sign-off; guard test restricts `os/exec` to `broker/ops/`; no op takes a path/command string without allow-list validation | Lead engineer | Open |
| Socket auth bypass (hc-api compromise → broker abuse) | Security | Medium | High | **High** | Broker re-checks scope per op; rate limits; ops are read-only in M1; `SO_PEERCRED` uid must equal `homelab-control`; audit every call | Lead engineer | Open |
| PAM integration lets any local user log in, or leaks timing/enumeration | Security | Medium | High | **High** | Group allow-list required; same throttle/timing equalisation as local accounts; PAM service file `homelab-control` with `pam_unix` only by default; VM tests | Lead engineer | Open |
| Unpatched legacy Webmin exposure (post-2.670 fixes) while users run the fallback UI | Security | High | Medium | **High** | Move to 2.680 promptly or backport; UI warns when detected Webmin version < pinned version; docs recommend binding Webmin to localhost/tailnet | Project owner | Open |
| Hardened systemd unit breaks reading some host data (ProtectHome, PrivateDevices) | Operational | Medium | Medium | Medium | Exercise unit in VM (item 1); per-section "unavailable" states already in UI | Lead engineer | Open |
| No hypervisor in the cloud build environment → VM evidence depends on owner's machine | Operational | High | Low | Medium | Vagrant definition ready; owner runs item 1; keep container tests for CI | Project owner | Open |
| Go dependency needs (D-Bus, SQLite) conflict with stdlib-only policy | Strategic | High | Low | Medium | Use systemd D-Bus via a small vetted module or `busctl --json` through a broker op; decide per ADR 0006 with review | Lead engineer | Open |
| License decision delayed blocks SDK publication | Compliance | Medium | Low | Low | ADR 0005 recommendation ready; SDK publishes in M3, not M1 | Project owner | Open |
| Scope creep from the UI concept (Cockpit track, dashboards with mock data) | Strategic | Medium | Medium | Medium | Specs win; concept marked as mockup; M1 limited to items above | Project owner | Open |
| Upstream Webmin changes `setup.sh`/miniserv behaviour, breaking the baseline scripts | Operational | Low | Low | Low | Baseline scripts run on every sync; `prove` suite kept as evidence | Lead engineer | Open |

## Decisions needed before M1 starts
1. Approve ADR 0001 (broker model) and ADR 0003 (no generic bridge).
2. Approve or amend ADR 0005 (GPL-3.0-or-later core, MIT SDK/schemas/tokens, BSD-3 for upstream fixes).
3. Confirm the Cockpit track in `UI-CONCEPT.png` is out of scope.
4. Whether the M1 package should bundle a pruned `compat/webmin` or keep relying on upstream Webmin packages.
5. PAM group name and default scope mapping (proposal: `homelab-admins` → `system.read` in M1).
