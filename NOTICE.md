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
- Not every file in the tree is BSD-3-Clause: several modules and bundled third-party libraries carry
  other licences (GPL, MIT, Apache-2.0, Artistic, Perl terms), and some bundled third-party files state
  none. Per-file inventory: `docs/compliance/WEBMIN-LICENSE-INVENTORY.md` (machine-readable in
  `REUSE.toml`). Homelab Control does not relicense any of these files.

Webmin's Authentic theme (<https://github.com/webmin/authentic-theme>, MIT, Copyright (c) Ilia
Rostovtsev), which upstream release packages bundle, is **not** part of this repository, and no Webmin
or Authentic theme assets are used by the new interface.

**Release packages contain no Webmin code or assets**; `compat/webmin/` is a development reference only
(verified per build by `scripts/package-inventory.sh`; see `docs/compliance/WEBMIN-INDEPENDENCE.md`).

## Homelab Control code

Copyright 2026 The Homelab Control Authors (see `AUTHORS.md`).

Homelab Control is free software: you can redistribute it and/or modify it under the terms of the GNU
General Public License as published by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version. It is distributed in the hope that it will be useful, but WITHOUT ANY
WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See
`LICENSE.md` for the full licence text.

SPDX: `GPL-3.0-or-later`, except the parts below. File-level boundaries are recorded in `REUSE.toml` and in
SPDX headers; `compat/webmin/` keeps its upstream licences and is not relicensed.

### MIT-licensed parts

The public SDK (`sdk/`), schemas (`schemas/`, including the OpenAPI contract), theme definitions
(`themes/`, theme schema and example themes under `docs/ui/`) and reusable design tokens
(`frontend/design-system/`) are licensed under the MIT licence, so that third-party themes, plugins and API
clients can use them without adopting the GPL. The design tokens are compiled into the web UI that `hc-api`
embeds; this notice covers that use.

```text
Copyright (c) 2026 The Homelab Control Authors

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and
associated documentation files (the "Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the
following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial
portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT
LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO
EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE
USE OR OTHER DEALINGS IN THE SOFTWARE.
```

## Third-party components in release artifacts

The binaries and packages contain the Go standard library (BSD-3-Clause), React, React DOM and scheduler
(MIT) and the Archivo variable font (SIL OFL 1.1). Their full copyright notices and licence texts are in
`THIRD-PARTY-NOTICES.md`, which every package installs next to this file. Development-only tooling is not
redistributed (`docs/compliance/DEPENDENCY-LICENSES.md`).
