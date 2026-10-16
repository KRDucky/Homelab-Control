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

Webmin's Authentic theme (<https://github.com/webmin/authentic-theme>, MIT, Copyright (c) Ilia
Rostovtsev), which upstream release packages bundle, is **not** part of this repository, and no Webmin
or Authentic theme assets are used by the new interface.

**Release packages contain no Webmin code or assets**; `compat/webmin/` is a development reference only
(verified per build by `scripts/package-inventory.sh`; see `docs/compliance/WEBMIN-INDEPENDENCE.md`).

## New Homelab Control code

Code outside `compat/` is covered by the repository `LICENSE.md` (GNU GPL version 3). Whether later GPL
versions are also granted, and the proposed MIT licence for SDK, schemas and themes (ADR 0005), await owner
decisions — see `docs/compliance/LICENSE-AUDIT.md` (LA-02, LA-03).

## Third-party components in release artifacts

The binaries and packages contain the Go standard library (BSD-3-Clause), React, React DOM and scheduler
(MIT) and the Archivo variable font (SIL OFL 1.1). Their full copyright notices and licence texts are in
`THIRD-PARTY-NOTICES.md`, which every package installs next to this file. Development-only tooling is not
redistributed (`docs/compliance/DEPENDENCY-LICENSES.md`).
