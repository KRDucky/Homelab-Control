# 8. Performance budgets and testing strategy (planning)

## 8.1 Baseline (measured 2026-10-16, production build of `main`)
| Asset | Size | gzip |
|---|---|---|
| JS bundle (`index-*.js`) | 247.1 kB | 76.0 kB |
| CSS | 14.2 kB | 3.5 kB |
| Archivo font subsets (3 × woff2) | 210.8 kB | (already compressed) |

React 19 + React DOM account for most of the JS. Switching to system fonts (LICENSE-AUDIT LA-06) would
remove ~211 kB of font downloads.

**Update 2026-10-23 (HCIS step 2):** Archivo was replaced by Inter (7 subsets, 218.5 kB) and JetBrains Mono
(6 subsets, 86.2 kB): 304.7 kB embedded in `hc-api`. Each subset has a `unicode-range`, so a browser
downloads only the subsets the page's text needs; Latin-only pages fetch Inter Latin (48.3 kB) and
JetBrains Mono Latin (40.4 kB), 88.7 kB against Archivo Latin's 90.1 kB. JS 259.8 kB (78.9 kB gzip), CSS
30.3 kB (5.5 kB gzip).

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
