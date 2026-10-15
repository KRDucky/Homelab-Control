# Current status — end of M0 (2026-10-15)

## What works (verified, see docs/evidence/)
- **Fork with provenance:** upstream Webmin 2.670 imported as a squashed subtree; tree byte-identical
  to upstream; BSD-3 `LICENCE` retained; per-file GPL/LGPL exceptions documented in `NOTICE.md`.
- **hc-api** (Go, stdlib only): runs unprivileged, refuses root, refuses plain HTTP off loopback; local
  accounts (PBKDF2), server-side sessions, Origin + CSRF token, per-route scopes, throttling, audit log,
  strict CSP/security headers.
- **Read-only host overview** (`GET /api/v1/host/overview`, scope `system.read`): hostname, OS, kernel,
  uptime, load, CPU model/usage, memory/swap, local filesystems (df-style usage; network mounts listed
  but not probed), network interfaces, legacy-Webmin detection. Every section reports its sources and
  "unavailable" states instead of inventing values.
- **UI shell** (React/TS/Vite): login, overview, sign-out, link to legacy Webmin; dark/light tokens;
  keyboard focus; mobile layout without page-level horizontal scroll; no CSP violations.
- **Tooling:** `make build|test|lint|security|check|repro-check|deb`; CI workflow; reproducible builds.
- **Upstream baseline:** Webmin 2.670 starts and its login flow works in a disposable environment;
  upstream test suite passes.
- **Packaging:** Debian dev package installs, runs as `homelab-control`, removes and purges cleanly.

## Not available yet (by design or pending)
| Missing | Why / when |
|---|---|
| Any write/modify action on the host | Read-only until the broker, jobs and audit exist (M1–M2) |
| Privileged data (SMART, Webmin port/TLS, journal) | Needs the root broker (M1) |
| PAM / Unix-account login | Broker op (M1); M0 uses local accounts via `hlctl` |
| Persistent sessions, SQLite state | M2 |
| Themes/plugins, Software Center, storage, proxy, etc. | M3+ per roadmap |
| TLS certificate management | Operator supplies cert/key or a local TLS reverse proxy |
| Signed releases, SBOM | M13 (scan from M1) |

## Missing dependencies / environment gaps observed
- `govulncheck`, Go module proxy: blocked by the build environment's network policy → runs in CI.
- No hypervisor: Vagrant VM written but not executed; systemd unit not started under systemd.
- Upstream Webmin optional Perl modules absent in the test container: `Net::SSLeay` (TLS), `Authen::PAM`
  (PAM login) — baseline used SSL off on 127.0.0.1 and Webmin's own user file.
- Upstream shebang `/usr/local/bin/perl` must exist or `perlpath.pl` must run; the disposable script
  symlinks it.
- jsdom (dev-only) prefers Node ≥ 22.22.2; tests pass on 22.22.0 with an engine warning.

## Known limitations
- Sessions and login throttling are in memory (restart logs everyone out; counters reset).
- Mobile tables wrap long values inside a horizontally scrollable container; card layout planned.
- Legacy-Webmin detection checks `/etc/webmin` only (non-standard config dirs show "not installed").
- Webmin legacy UI at 2.670 lacks post-2.670 upstream security fixes (`docs/upstream/SYNC.md`).

## Build / run
See `docs/dev/DEVELOPMENT.md`. Packaging plan: `docs/packaging/PLAN.md`. Rollback: `docs/dev/ROLLBACK.md`.
