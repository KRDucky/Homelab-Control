# Test plan (M0 baseline)

Pyramid: many fast unit tests next to the code, HTTP-level integration tests for every security
boundary, a few browser end-to-end flows, plus environment tests in disposable systems.

| Area | Type | Where | What is covered | M0 result |
|---|---|---|---|---|
| Password hashing, users file | Unit | `backend/internal/auth` | PBKDF2 encode/verify, weak/malformed hashes, 0600 enforcement, symlink refusal, scope/username validation, disabled users, timing equalisation path | pass |
| Sessions, CSRF, throttle | Unit | `backend/internal/auth` | idle/absolute expiry, raw-ID never stored, table cap, CSRF compare, per-user and per-IP lockout | pass |
| procfs/sysfs parsers | Unit (fixtures) | `backend/internal/hostinfo` | os-release quoting, distro family, cpuinfo, meminfo (incl. missing MemAvailable flagged), mount octal escapes, pseudo-fs filtering, network fs not probed, statfs failure reported, missing roots → unavailable (no invented values) | pass |
| HTTP security boundary | Integration (httptest) | `backend/internal/api` | 401 without session, forged cookie, scope 403, Origin matrix, strict JSON/415/size limit, CSRF on logout, fixation, disabled account, headers/CSP, SPA fallback, traversal, `__Host-` cookie in TLS mode, audit has no secrets | pass |
| Architecture guards | Static | `backend/internal/guard` | no process execution APIs; every route wrapped by authz | pass (and verified to fail on a planted `os/exec` import) |
| CLI | Unit | `backend/cmd/hlctl` | add/duplicate/short password/unknown scope/disable/list, 0600 file | pass |
| UI | Component (Vitest + jsdom) | `frontend/app/src/app.test.tsx` | formatters, unknown ≠ zero, no inline styles, login/overview/unavailable labels, error copy, CSRF header on sign-out | pass |
| Live API | Integration (curl) | `tests/integration/run-hc-e2e.sh` | 15 checks against a running unprivileged hc-api | pass |
| Browser | E2E (Playwright Chromium) | `tests/e2e/hc_e2e.py` | keyboard focus, error copy, real hostname, cookie flags, no inline styles, no CSP violations, mobile overflow, light theme, sign-out, legacy link | pass |
| Upstream baseline | Environment | `tests/integration/webmin-baseline-login.sh` | Webmin 2.670 startup, login form, bad/good password, cookie flags, module index, Referer check, logout | pass (9/9) |
| Upstream regression | Upstream suite | `compat/webmin/t` (`prove -lr`) | 26 files, 4161 tests | pass (1 VM-only test skipped by upstream design) |
| Build | Reproducibility | `scripts/repro-check.sh` | identical SHA-256 from two clean worktrees | pass |
| Packaging | Install test | `packaging/debian/build-deb.sh` | dpkg install, file modes, user creation, remove, purge | see `docs/evidence/` |

## Gaps (known, planned)
- Coverage of `cmd/hc-api` flag validation (TLS/loopback rules) is manual only → add table test (M1).
- No contract test against the OpenAPI file yet (M1 item 7).
- systemd unit not exercised under systemd; Vagrant VM not run (M1 item 1).
- No accessibility scanner (axe) in e2e yet; manual checks only (M1).
- Fuzzing of JSON decoders and parsers (M1/M2).

## M1 additions

| Area | Type | Where | What is covered |
|---|---|---|---|
| Broker protocol | Unit | `backend/internal/broker/proto` | op-name grammar, frame round-trip and size limits, strict args decoding |
| Exec runner | Unit (real processes) | `backend/internal/broker/runner` | allow-list only, shell metacharacters/oversized args rejected, environment replaced, timeout kills process group, output cap, unsafe binary refused |
| Broker ops | Unit (fixtures + real systemctl JSON from systemd 249/255) | `backend/internal/broker/ops` | closed registry, all ops reject arguments, Webmin posture incl. implicit-RPC rule, no hashes/keys in results, symlink refusal, pidfile confinement, DMI placeholders, hostile unit names, systemd-not-booted |
| Broker server | Integration (real Unix socket) | `backend/internal/broker/server` | socket mode, peer uid rejection, scope re-check, malformed/oversized/trailing/versioned frames, slow client, panic containment, busy limit, audit content |
| Capabilities + new endpoints | Integration (httptest + fake broker) | `backend/internal/api/capabilities_test.go` | statuses for broker up/down/no-systemd/missing scopes, error mapping without leaking internals, hardware degrades, legacy fallback |
| Hardware inventory | Unit (fixture sysfs) | `backend/internal/hwinfo` | DMI, PCI names via pci.ids, USB interface skipping, disk identity ranking (WWN > serial > by-id), partition links ignored, hwmon units, empty host reports unavailable |
| Architecture guards | Static | `backend/internal/guard` | exec only in runner; hc-api/hlctl import graph excludes broker internals; every route documented in OpenAPI with matching scope |
| UI | Component | `frontend/app/src/m1.test.tsx` | capability-aware nav (links vs explained entries), deep link to unavailable section, Services filters/not-installed toggle, Hardware identity/placeholder labels, Legacy review, client-side navigation |
| Real systemd | System (systemd PID 1) | `tests/systemd/systemd-smoke.sh` + `m1-checks.sh` | package install, hardened units, CapEff, NoNewPrivs, socket ownership, peer refusal, helper uid, real upstream Webmin detection, broker-down degradation, purge |
| Packages without systemd | System | `tests/packaging/install-test.sh` | native install/remove, dev socket ownership, "systemd not running" handling |
| Browser | E2E + axe-core | `tests/e2e/hc_m1_e2e.py` | nav, routing, history/deep links, real data pages, mobile, broker-down nav, CSP, WCAG 2.1 AA (axe) |
| Cross-distro | CI | `.github/workflows/ci.yml` | Debian stable/oldstable, Ubuntu 22.04/24.04, Fedora, openSUSE Tumbleweed/Leap, Arch install tests; GitHub runner systemd smoke — **not yet executed** |
