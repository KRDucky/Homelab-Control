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

It omits only `compat/webmin/`, the upstream Webmin reference tree, which no binary or package uses
(owner decision, ADR 0009). It is available in the Git repository at the tagged commit and from
<https://github.com/webmin/webmin>.

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
packaging/arch/build-pkg.sh <version>        # optional, on Arch Linux as a normal user: .pkg.tar.zst
```
Run the packaging commands with the same `SOURCE_DATE_EPOCH` exported; package timestamps come from it.
`make build` runs `npm ci --ignore-scripts`, builds the UI, embeds it and builds `build/hc-api`,
`build/hlctl` and `build/hc-broker`. Version and timestamps come from the command line, not from Git.

## Verify
```bash
cd build && sha256sum hc-api hlctl hc-broker *.deb *.rpm
# compare with the binaries in homelab-control-<version>-linux-<arch>.tar.gz and the release SHA256SUMS
```
For a prerelease such as `0.1.0-dev.1`, the `.deb` you build is named `homelab-control_0.1.0~dev.1_amd64.deb`
(Debian version `0.1.0~dev.1`, which sorts before `0.1.0`); the release publishes the same bytes as
`homelab-control_0.1.0-dev.1_amd64.deb`, because GitHub may rename asset names containing `~`.
The exact toolchain of every release is pinned in `packaging/release/toolchain.env` and recorded in
`RELEASE-BUILDINFO`.
**What has been demonstrated** (CI on every change, and locally): binaries, `.deb` and `.rpm` built from
the source archive alone are byte-identical to those built from Git at the same commit, and all release
files are identical across two complete runs on independent GitHub `ubuntu-24.04` runners (the Arch
package across two fresh Arch containers). **What has not**: identical output with a different Go or
Node.js patch version, on other distributions, or by an independent third party. Use exactly the
toolchain listed in `RELEASE-BUILDINFO`; the Arch package also depends on the container's package set
recorded in its `.BUILDINFO`.

## Licences
`LICENSE.md` (GPL-3.0-or-later), `NOTICE.md` (including the MIT-licensed parts), `REUSE.toml` (per-file
boundaries) and `docs/compliance/THIRD-PARTY-NOTICES.md`.
