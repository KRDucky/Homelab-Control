# HCIS UI continuation — local evidence, 2026-10-25

Base: main `5a444a9a` (later main changes during this session were documentation only). Owner authorized remaining frontend and TDK in this session. This is local verification, not a claim that GitHub CI completed or that the change was merged.

- `GOTOOLCHAIN=go1.26.10 make check`: passes. Non-root Go tests, 49 frontend tests, three standalone TDK tests, 35 release-tooling tests, package-version ordering, provenance/guard/dependency policy, production npm audit (zero vulnerabilities), third-party notices and SBOM drift checks pass.
- Local optional tools not installed: govulncheck, reuse, pyspdxtools and shellcheck. Their existing CI gates remain enabled. Full npm audit reports a pre-existing **development-only** `undici` advisory; production audit is clean. No npm dependencies or lockfile changes were added by this work.
- `GOTOOLCHAIN=go1.26.10 make build`: passes with the embedded UI. Browser fixtures are served by the built hc-api on loopback under its actual CSP; all `/api/` responses used by the matrix are labelled fixture data, never live host-management responses.
- Browser matrices: Chromium and Firefox; four themes × six widths (390, 560, 768, 1100, 1920, 3440px), Studio and Services, 125% text and System fonts. Per engine: 48 page-overflow checks plus 16 axe WCAG A/AA scans with **zero violations**, and interaction/security checks. Full check names and engine versions are in the adjacent JSON reports.
- Interactions include bounded table pagination, permission-filtered palette/navigation, browser-local save, explicit Keep, Escape and real 15-second timeout reversion, duplicate-key rejection, no browser exceptions and no CSP violations while applying a custom palette. CSP retains no `unsafe-inline` allowance.
- WebKit was downloaded but could not start on the CachyOS workstation: its Ubuntu fallback build requires libicu74 and libflite1. No workstation system packages were changed. CI installs browser dependencies and runs this matrix through the existing Python Playwright driver in Chromium, Firefox and WebKit.
- After the matrix, the shared validator gained additional text-on-hover/status and non-text border contrast pairs; all built-in bases and all 49 frontend tests still pass. This validation-only change does not change rendered palette values.

Reproduce with the existing Python Playwright dependency using `tests/integration/run-hc-e2e.sh`; the harness now invokes `tests/e2e/hcis_ui.py` against its temporary built hc-api. The JS matrix can also run standalone with `HC_E2E_URL`, `HC_PLAYWRIGHT_MODULE`, `HC_BROWSER` and optional `HC_CHROMIUM`.

## WebKit follow-up, 2026-10-26

The first PR #24 CI run passed 20/21 jobs. Chromium and Firefox passed the full matrix; WebKit passed every layout/axe check but its final exception assertion caught mocked capability requests canceled by immediate document reloads. The harness now waits for the actual capability navigation result before replacing a document and logs browser exception details. No exception filter, CSP relaxation or production behavior change was added.

WebKit was reproduced locally using Ubuntu libicu74/libflite1 unpacked into temporary directories (no Arch system package changes). With the navigation synchronization fix it passes all 76 checks with zero exceptions; see `webkit.json`. Updated CI must still pass before merge. The owner waived DCO sign-off specifically for PR #24 and authorized merge after all CI jobs pass.

## Final design review, 2026-10-26

Reviewed on `f6af207c` with a Chromium-only local pass (four themes, phone to ultrawide, preference combinations, Theme Studio, TDK). Seven defects were fixed on `review/hcis-final` and covered by new checks in `tests/e2e/hcis-ui.mjs` and Vitest/TDK tests; a negative control confirmed the new e2e checks fail without the CSS fixes. Not covered locally: Firefox and WebKit (CI), real broker data, a real screen reader, Windows High Contrast hardware. See `docs/ui/12-FINAL-DESIGN-REVIEW-AND-HANDOFF.md`.
