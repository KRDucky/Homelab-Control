# 8. Performance budgets and testing strategy (planning)

## 8.1 Baseline (measured 2026-10-16, production build of `main`)
| Asset | Size | gzip |
|---|---|---|
| JS bundle (`index-*.js`) | 247.1 kB | 76.0 kB |
| CSS | 14.2 kB | 3.5 kB |
| Archivo font subsets (3 × woff2) | 210.8 kB | (already compressed) |

React 19 + React DOM account for most of the JS. Switching to system fonts (LICENSE-AUDIT LA-06) would
remove ~211 kB of font downloads.

## 8.2 Budgets (CI-enforced once the build step measures them)
| Metric | Budget | Notes |
|---|---|---|
| Initial JS (gzip) | ≤ 110 kB | Routes beyond Overview are lazy-loaded |
| Initial CSS (gzip) | ≤ 15 kB | |
| Fonts on first load | 0 kB (system stack) or ≤ 90 kB | Depends on LA-06 decision |
| Per-route chunk (gzip) | ≤ 40 kB | Charts load only where used |
| LCP on Overview, LAN, mid-range laptop | ≤ 1.5 s | Measured with Playwright tracing |
| Interaction to Next Paint | ≤ 200 ms (p75) | Includes table sort/filter on 1 000 rows |
| Theme switch | ≤ 16 ms script, no layout shift | One CSSOM batch on `:root` |
| Idle dashboard (10 widgets, 5 s refresh) | ≤ 2 % CPU on host browser, ≤ 1 request/s | Shared resources, paused when hidden |
| Server: hc-api RSS | ≤ 40 MB with 5 sessions | Modest homelab hardware |

Charts: a small purpose-built SVG/canvas sparkline and line chart first; a charting library only after an
ADR 0006 review if requirements outgrow it.

## 8.3 Test strategy
| Level | What | Tooling |
|---|---|---|
| Unit | Token resolution, theme validator (every rule, positive and negative), contrast maths, migrators, data store policies | Vitest |
| Component | States from `01` §1.5, keyboard behaviour, ARIA | Vitest + Testing Library + axe-core |
| Theme | Every built-in theme × mode: schema-valid, contrast-clean, visual snapshot of a component gallery | Vitest + Playwright screenshots |
| Security | Malicious theme corpus (`09` §9.4) must be rejected or neutralised | Vitest |
| E2E | Primary journeys on desktop/tablet/phone viewports, keyboard-only variants, reduced motion, forced colours | Playwright (as M1) |
| Performance | Bundle budget check on every build; LCP/INP traces on the VM harness | Vite build stats, Playwright |
| Accessibility | axe-core on every page/state; manual screen-reader pass per release | axe-core, manual |
