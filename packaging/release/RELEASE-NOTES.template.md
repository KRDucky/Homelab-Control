<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors

SPDX-License-Identifier: GPL-3.0-or-later

Release notes template, rendered by `scripts/release-tool.py notes` (placeholders in double braces).
This comment is removed when rendering. The rendered text must start with the UNSIGNED banner (ADR 0009 R9).
-->
**UNSIGNED DEVELOPMENT BUILD — {{TAG}}**

> [!WARNING]
> This is an **unsigned development prerelease**. **No cryptographic signatures** (OpenPGP, Sigstore
> or other) are supplied for any file. It is not a stable or production release, and it is not
> distributed through any package repository.
>
> `SHA256SUMS` on this page detects corrupted downloads. It does **not** protect against tampering by
> anyone who can change this release, because the checksums come from the same place as the files.
> The independent check is to rebuild from the source archive (see below).

Homelab Control {{VERSION}} — built from commit [`{{SHORT}}`]({{REPO}}/commit/{{COMMIT}}).

## Provenance
- Source commit: `{{COMMIT}}`
- Release workflow run (build, two independent runs, byte-for-byte comparison, verification): {{RUN_URL}}
- CI run on `main` for this commit (full test matrix): {{CI_RUN_URL}}
- Toolchain: Go `{{GO}}`, Node.js `{{NODE}}`, npm `{{NPM}}`; `SOURCE_DATE_EPOCH={{SDE}}` (`RELEASE-BUILDINFO`)
- Prepared by GitHub Actions as a **draft**; the tag `{{TAG}}` is created only when the project owner
  publishes this release by hand after review.

## Files
| File | Content |
|---|---|
| `homelab-control-{{VERSION}}-source.tar.gz` | Complete corresponding source: the repository at the commit (without the `compat/webmin/` Webmin reference tree, which no binary uses), the npm sources of everything compiled in, `RELEASE-BUILDINFO`, `BUILDING.md` |
| `homelab-control-{{VERSION}}-linux-amd64.tar.gz` | `hc-api`, `hlctl`, `hc-broker` and the licence/notice files |
| `{{DEB}}` | Debian/Ubuntu package (package version `{{DEBVER}}`) |
| `homelab-control-*.x86_64.rpm` | Fedora/openSUSE package |
| `homelab-control-*-x86_64.pkg.tar.zst` | Arch Linux package |
| `LICENSE.md`, `NOTICE.md`, `THIRD-PARTY-NOTICES.md` | Licence (GPL-3.0-or-later; MIT parts listed in `NOTICE.md`) and third-party notices |
| `homelab-control-{{VERSION}}.spdx.json` | SPDX 2.3 SBOM stamped with this version and commit |
| `RELEASE-BUILDINFO` | Commit, `SOURCE_DATE_EPOCH`, exact toolchain, build commands |
| `SHA256SUMS` | SHA-256 of every other file |

No Webmin code is included in any file. Upstream Webmin is a separate project.

## Verify
```bash
sha256sum -c SHA256SUMS                      # detects corrupted downloads
tar -xzf homelab-control-{{VERSION}}-source.tar.gz && cd homelab-control-{{VERSION}}
# install Go {{GO}} and Node.js {{NODE}} (npm {{NPM}}), then follow BUILDING.md:
make build VERSION={{VERSION}} SOURCE_DATE_EPOCH={{SDE}}
sha256sum build/hc-api build/hlctl build/hc-broker   # must match the binaries in the linux-amd64 tarball
```
Byte-for-byte reproduction has been demonstrated on GitHub's `ubuntu-24.04` runners (and fresh Arch
containers for the Arch package) with exactly this toolchain. It has not been demonstrated with other
toolchain versions or on other systems.

## Checksums
| File | SHA-256 |
|---|---|
{{CHECKSUMS}}
