# 10. Phased implementation and migration strategy (planning)

The owner authorized remaining frontend implementation and the TDK on 2026-10-25. HCIS Steps 1–3 were already merged in PR #19; shared tables/states, navigation-only palette, browser-local themes/Studio and TDK were merged in PR #24 (`f6af207c`). [UI handoff](11-IMPLEMENTED-UI-AND-TDK.md) records the actual boundary. The historical U0–U8 decomposition below is a roadmap, not a statement that its entire scope has shipped. Browser-local Studio does not depend on U5; server persistence, plugin widgets, SSE and backend host operations remain future work.

| Phase | Scope | Exit criteria | Depends on |
|---|---|---|---|
| **U0 — Decisions** | LA-03 MIT scope (**decided** 2026-10-16), LA-06 system fonts preferred (**decided**; removal plan in `docs/compliance/LICENSE-AUDIT.md`), `.hltheme` single JSON (**accepted in principle**, ADR 0010), layout preferences separate from themes (**decided**), CSSOM under CSP (**verified**, `tests/e2e/theme_cssom_csp.py`). Open: approval of U1 and the token schema | Decisions recorded in ADRs | This PR |
| **U1 — Token foundation** | Rename/alias M1 tokens to the `02` taxonomy; generate `default`, `oled`, `high-contrast` from JSON at build time; contrast tests in CI; settle schema v1 | Visual parity with M1 (screenshots), axe clean, all built-in themes pass contrast | U0 |
| **U2 — Runtime theme switching** | Client theme engine (validate → resolve → CSSOM apply), appearance settings (mode, motion, contrast) and separate layout preferences (density, navigation; `06` §6.5), `prefers-*` support; per-browser storage only | Theme switch ≤ 16 ms, no CSP change, malicious corpus rejected | U1 |
| **U3 — Data layer & shell** | `useResource` store replacing per-page polling; responsive shell, navigation, command palette (navigation only) | Budgets in `08` met; keyboard e2e; no regressions in M1 checks | U1 (parallel to U2) |
| **U4 — Dashboards** | Widget registry, default dashboard, edit mode with keyboard alternative; layouts stored client-side until M2 settings store exists | Error boundaries, pausing verified; e2e at three viewports | U3 |
| **U5 — Server-side preferences** | Settings store, theme/dashboard persistence, server-side validator, audit events | Security review; part of M2 or later per owner | M2 approval |
| **U6 — Theme Studio** | Editor per `03`, import/export `.hltheme` | Full validator test corpus; accessibility review of the Studio | U2, U5 |
| **U7 — Live telemetry** | SSE metrics endpoint, ring buffers, charts | Server limits tested; idle CPU budget met | M2 security review |
| **U8 — Plugin widgets** | Manifest-declared widgets from reviewed plugin bundles | Plugin SDK and signing in place | M3 |

## Migration rules
- Existing M1 pages (Overview, Services, Hardware, Webmin status) are ported one at a time onto the new
  shell and data layer; each port must keep its e2e and axe checks green.
- Token aliases from M1 names stay for one minor release after U1, then are removed with a migrator.
- No new runtime npm dependency without a licence check against `docs/compliance/DEPENDENCY-LICENSES.md`
  and an ADR 0006 note; regenerate `THIRD-PARTY-NOTICES.md` and the SBOM in the same PR (CI enforces).
- Planning documents are updated as phases land; status lives in `docs/STATUS.md`, not here.
