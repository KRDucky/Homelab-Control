# Building Homelab Control from a release source archive

Each release publishes `homelab-control-<version>-source.tar.gz`: the **complete corresponding source**
of the binaries and packages in that release. It contains:

- the repository at the release commit (`git archive`), including build scripts, packaging, tests and
  documentation;
- `vendor-sources/npm/`: the published npm tarballs of every package compiled into the web UI bundle
  that `hc-api` embeds (React, React DOM, scheduler, Archivo font), each checked against the sha512
  integrity in `frontend/app/package-lock.json` (`MANIFEST.txt`);
- `RELEASE-BUILDINFO`: version, commit, `SOURCE_DATE_EPOCH` and the exact Go, Node.js and npm versions
  used for the published binaries.

It omits only `compat/webmin/`, the upstream Webmin reference tree, which no binary or package uses. It is
available in the Git repository at the tagged commit and from <https://github.com/webmin/webmin>.

## Requirements
- Go and Node.js/npm at the versions in `RELEASE-BUILDINFO` (other versions build, but produce
  different bytes). Go comes from <https://go.dev/dl/>; verify the download checksum.
- GNU make, bash, coreutils, tar, gzip; `dpkg-deb` for the `.deb`, `rpmbuild` for the `.rpm`.
- Network access to the npm registry for `npm ci`, which installs the build tools pinned in
  `frontend/app/package-lock.json` (Vite, TypeScript, …) and checks each against its recorded integrity.
  These are general-purpose build tools that are not part of the program.

## Build
```bash
tar -xzf homelab-control-<version>-source.tar.gz
cd homelab-control-<version>
grep -E '^(go|node|npm|build_command)=' RELEASE-BUILDINFO
make build VERSION=<version> SOURCE_DATE_EPOCH=<source_date_epoch from RELEASE-BUILDINFO>
packaging/debian/build-deb.sh <version>      # optional: .deb
packaging/fedora/build-rpm.sh <version>      # optional: .rpm
```
`make build` runs `npm ci --ignore-scripts`, builds the UI, embeds it and builds `build/hc-api`,
`build/hlctl` and `build/hc-broker`. Version and timestamps come from the command line, not from Git.

## Verify
With the same toolchain versions the binaries are byte-identical to the published ones:
```bash
cd build && sha256sum hc-api hlctl hc-broker
# compare with homelab-control-<version>-linux-<arch>.tar.gz contents or the release SHA256SUMS
```
CI checks this on every change: `scripts/release-bundle.sh` builds the binaries from the source archive
alone and fails unless they match a build from Git (`release-dry-run` job).

## Licences
`LICENSE.md` (GPL-3.0-or-later), `NOTICE.md` (including the MIT-licensed parts), `REUSE.toml` (per-file
boundaries) and `docs/compliance/THIRD-PARTY-NOTICES.md`.
