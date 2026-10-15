# Notices and provenance

Homelab Control is a fork and modernization of **Webmin**.

## Upstream Webmin (`compat/webmin/`)

- Source: <https://github.com/webmin/webmin>
- Pinned baseline: tag `2.670`, commit `3a2b2b8671c0669c129f5275b3dd1853106ae02b`
  (machine-readable pin: `compat/webmin.lock`; import commit carries
  `git-subtree-split: 3a2b2b8671c0669c129f5275b3dd1853106ae02b`)
- License: BSD 3-Clause, Copyright (c) Jamie Cameron. The full text is retained
  unmodified at `compat/webmin/LICENCE` (and the Japanese translation at
  `compat/webmin/LICENCE.ja`). Redistributions of this directory, in source or
  binary form, must reproduce that notice.
- Webmin's name is used here only to describe provenance. Homelab Control is not
  affiliated with or endorsed by the Webmin project (BSD-3 clause 3).
- Fork-specific changes inside `compat/webmin/` are listed in `compat/PATCHES.md`
  (none as of M0).

Not vendored in M0: the Authentic theme (<https://github.com/webmin/authentic-theme>,
MIT License, Copyright (c) Ilia Rostovtsev), which upstream release packages bundle.
See `docs/adr/0005-release-and-license-strategy.md`.

## New Homelab Control code

Code outside `compat/` is currently covered by the repository `LICENSE.md`
(GNU GPL v3). The final licensing split is a pending decision recorded in
`docs/adr/0005-release-and-license-strategy.md`.

## Third-party components shipped in the web UI build

| Component | License |
|---|---|
| React, React DOM, scheduler | MIT |
| Archivo variable font (`@fontsource-variable/archivo`) | SIL Open Font License 1.1 |

Development-only tooling (Vite, TypeScript, ESLint, Vitest, Prettier, jsdom,
Testing Library) is not shipped. The Go backend uses only the Go standard
library (BSD 3-Clause, The Go Authors).
