# M2 browser evidence — 2026-11-18

The original four runs exercise local closeout candidate `dreggbot/m2-final-closeout`
(base `main` `59170f772e907d21a41842401c292c610e7ff4b3`). A fifth File Manager run was
performed by its implementer on separate branch `claude/files-ui-navigation`, also using
the local synthetic fixture. These runs are not production or hardware tests, and the
fifth run has not been rerun in the integration checkout.

| Surface | Browser checks | axe violations | Mobile overflow | Notes |
|---|---:|---:|---|---|
| Files | 33 passed | 0 desktop / 0 mobile | None | Read-only jailed fixture |
| Drive layout | 18 passed | 0 desktop / 0 mobile | None | Synthetic identities; API writes only to fixture SQLite |
| Storage | 22 passed | 0 desktop / 0 mobile | None | Synthetic kernel/LVM data; does not invoke the on-demand probe |
| Host settings | 34 passed | 0 desktop / 0 mobile | None | Apply-capable synthetic user opens confirmation; one dry-run plan; zero apply requests |
| File Manager UI | 47 reported passed | 0 desktop / 0 mobile | None | Implementer-reported synthetic jail, headless Chromium; evidence in `files-ui/` |

Reported total across the five separate runs: 154 browser checks. The first four runs
were recorded in the original closeout candidate; the File Manager run's evidence is
separately copied under `files-ui/`. All runs include screenshot, result, axe, and
overflow artifacts as applicable. The layout harness was corrected to include the
current optimistic-concurrency `version` in assignment and delete requests; the
first run exposed this stale test payload and failed, and the rerun passed.

The host-settings plan endpoint reaches only a fail-closed test broker. Its Go fixture
reported `plan_calls=1 apply_calls=0`; neither `hostnamectl` nor `timedatectl` ran.
The storage UI flow does not click “Probe storage”; that separate action is covered by
frontend tests for scope gating, one CSRF POST, and no probe from the polling GET.

Commands were run in temporary, non-root Docker test containers with Playwright,
Chromium, Go, the branch's frontend and axe-core. No host paths or block devices were
mounted. Selected PNG/JSON output and the Go fixture logs are preserved here. The
temporary browser images/containers are removed after evidence checks.

The File Manager implementer reports Vitest, typecheck, lint, format, build and the
47-check fixture passing in its isolated environment. The integration environment had
no Node/npm/Go toolchain, so these reports and artifacts were not independently rerun
here. File Manager security-review findings and verification limits are recorded in
`../FILE-MANAGER-SKELETON.md`.

Dependency audit note: the recorded npm audit found 0 production advisories and 16
development-tool advisories (8 moderate, 8 high, 0 critical). These affect the test/build
toolchain; dependency upgrades need a separate compatibility review rather than an
automatic major-version change.
