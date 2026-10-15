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
