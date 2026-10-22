#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Release DRY RUN: assembles everything a release would publish, but publishes nothing.
# See docs/compliance/RELEASE-PIPELINE.md (LA-13) and BUILDING.md.
#
#   scripts/release-bundle.sh <version> [out-dir]      (default out-dir: dist/release)
#
# 1. Writes the corresponding-source archive from the committed tree (git archive, minus the
#    compat/webmin/ reference tree, which no binary uses) plus the published npm sources of
#    every package compiled into the binaries, and RELEASE-BUILDINFO (exact toolchain).
# 2. Extracts that archive in an empty directory and builds the binaries and packages ONLY
#    from it. A missing file fails here.
# 3. Builds the same commit from a git worktree and requires byte-identical binaries and
#    .deb/.rpm packages. This shows the archive is complete and the build reproducible across
#    two build directories on the same machine; CI repeats the whole run on two independent
#    runners and compares the results (release-repro job).
# 4. Writes SHA256SUMS over all release files and the version-stamped SBOM. No signing, tagging or
#    upload happens here; .github/workflows/release.yml is the only place a (draft) release is created.
set -euo pipefail
ver=${1:?usage: release-bundle.sh <version> [out-dir]}
root=$(git rev-parse --show-toplevel)
cd "$root"
out=${2:-$root/dist/release}
[ -z "$(git status --porcelain --untracked-files=no)" ] || { echo "refusing: uncommitted changes (a release is built from a commit)"; exit 1; }
commit=$(git rev-parse HEAD)
sde=$(git log -1 --format=%ct HEAD)
name=homelab-control-$ver
work=$(mktemp -d)
cleanup() { chmod -R u+w "$work" 2>/dev/null || true; git worktree remove --force "$work/wt" >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null || true; rm -rf "$work"; }
trap cleanup EXIT
# go mod download leaves read-only module trees; make them removable.
mkdir -p "$out"; out=$(cd "$out" && pwd)
[ -z "$(ls -A "$out")" ] || { echo "refusing: $out is not empty"; exit 1; }
export SOURCE_DATE_EPOCH=$sde

echo "== 1. corresponding-source archive ($commit)"
python3 -I scripts/release-npm-sources.py "$work/vendor/vendor-sources/npm"
# Go module sources (ADR 0006 amendment 1): the module proxy files (.zip/.mod/.info) of every
# module the build needs, verified against go.sum, so the archive builds offline with
# GOPROXY=file://... and yields the same binaries as a module-mode build (go.sum hashes in buildinfo).
gm=$work/gomodcache
(cd backend && GOMODCACHE=$gm GOFLAGS=-mod=readonly go mod download)
mkdir -p "$work/vendor/vendor-sources/go"
cp -r "$gm/cache/download" "$work/vendor/vendor-sources/go/download"
# Checksum-database tiles vary over time (go.sum is what verifies); a Go toolchain module is not source of ours.
rm -rf "$work/vendor/vendor-sources/go/download/sumdb" "$work/vendor/vendor-sources/go/download/golang.org/toolchain"
find "$work/vendor/vendor-sources/go/download" \( -name '*.lock' -o -name '*.partial' -o -name '*.tmp' \) -delete
chmod -R u+rw,go+r-w "$gm" "$work/vendor/vendor-sources/go"
buildinfo=$(cat <<INFO
# Homelab Control release build information (see BUILDING.md)
version=$ver
commit=$commit
source_date_epoch=$sde
go=$(go env GOVERSION)
node=$(node --version)
npm=$(npm --version)
build_command=make build VERSION=$ver SOURCE_DATE_EPOCH=$sde
packages_command=packaging/debian/build-deb.sh $ver; packaging/fedora/build-rpm.sh $ver; packaging/arch/build-pkg.sh $ver (on Arch Linux, packaging the binaries built above)
excluded=compat/webmin/ (upstream Webmin reference tree; not used to build any binary; available at the tagged commit)
INFO
)
git archive --format=tar --prefix="$name/" --add-virtual-file="$name/RELEASE-BUILDINFO:$buildinfo
" -o "$work/src.tar" HEAD -- . ':(exclude)compat/webmin'
tar --append -f "$work/src.tar" --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$sde" \
  --mode='u+rw,go+r-w' --transform="s#^vendor-sources#$name/vendor-sources#" -C "$work/vendor" vendor-sources
gzip -n -9 -c "$work/src.tar" > "$out/$name-source.tar.gz"

echo "== 2. build from the archive only"
mkdir -p "$work/x"
tar -xzf "$out/$name-source.tar.gz" -C "$work/x"
src=$work/x/$name
[ ! -e "$src/.git" ] || { echo "archive unexpectedly contains .git"; exit 1; }
# Offline: Go modules only from the archive (go.sum still verifies every one).
(cd "$src" && GOMODCACHE="$work/gomod-archive" GOPROXY="file://$src/vendor-sources/go/download" GOSUMDB=off \
  GOFLAGS=-mod=readonly GOTOOLCHAIN=local make -s build VERSION="$ver" SOURCE_DATE_EPOCH="$sde" >/dev/null)
(cd "$src" && packaging/debian/build-deb.sh "$ver" >/dev/null)
if command -v rpmbuild >/dev/null 2>&1; then (cd "$src" && packaging/fedora/build-rpm.sh "$ver" >/dev/null); else echo "SKIPPED: rpm (rpmbuild not installed)"; fi

echo "== 3. same commit from git must give identical binaries"
git worktree add --detach "$work/wt" "$commit" >/dev/null
(cd "$work/wt" && make -s build VERSION="$ver" SOURCE_DATE_EPOCH="$sde" >/dev/null)
echo "archive build:"; cat "$src/build/SHA256SUMS"
echo "git build:";     cat "$work/wt/build/SHA256SUMS"
cmp -s "$src/build/SHA256SUMS" "$work/wt/build/SHA256SUMS" || { echo "FAIL: binaries differ between archive and git builds"; exit 1; }
echo "OK: archive is complete and the build is reproducible"
if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::notice title=release dry run::binaries built only from the source archive are byte-identical to the git build ($(tr '\n' ' ' < "$src/build/SHA256SUMS"))"; fi

echo "== 3b. packages from both trees must be byte-identical"
(cd "$work/wt" && packaging/debian/build-deb.sh "$ver" >/dev/null)
if command -v rpmbuild >/dev/null 2>&1; then (cd "$work/wt" && packaging/fedora/build-rpm.sh "$ver" >/dev/null); fi
pkgsum=""
for p in "$src"/build/*.deb "$src"/build/*.rpm; do
  [ -e "$p" ] || continue
  q="$work/wt/build/$(basename "$p")"
  cmp -s "$p" "$q" || { echo "FAIL: $(basename "$p") differs between archive and git builds"; sha256sum "$p" "$q"; exit 1; }
  pkgsum="$pkgsum $(sha256sum "$p" | cut -c1-16)…  $(basename "$p");"
done
echo "OK: packages identical:$pkgsum"
if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::notice title=release dry run::packages built from the archive and from git are byte-identical:$pkgsum"; fi

echo "== 4. release files"
bin=$work/bin/$name-linux-$(go env GOARCH)
install -Dm0755 -t "$bin" "$src/build/hc-api" "$src/build/hlctl" "$src/build/hc-broker"
install -Dm0644 -t "$bin" "$src/LICENSE.md" "$src/NOTICE.md" "$src/docs/compliance/THIRD-PARTY-NOTICES.md"
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$sde" -C "$work/bin" -cf - "$(basename "$bin")" | gzip -n -9 > "$out/$(basename "$bin").tar.gz"
cp "$src"/build/*.deb "$out/"
cp "$src"/build/*.rpm "$out/" 2>/dev/null || true
cp "$src/LICENSE.md" "$src/NOTICE.md" "$src/docs/compliance/THIRD-PARTY-NOTICES.md" "$out/"
# SBOM stamped with this release's version, commit and exact Go toolchain (ADR 0009 R4).
python3 -I "$root/scripts/release-tool.py" stamp-sbom "$src/docs/compliance/sbom/homelab-control.spdx.json" \
  "$out/$name.spdx.json" --version "$ver" --commit "$commit" --go "$(go env GOVERSION)"
printf '%s\n' "$buildinfo" > "$out/RELEASE-BUILDINFO"
for p in "$out"/*.deb "$out"/*.rpm; do [ -e "$p" ] && "$root/scripts/package-inventory.sh" "$p" >/dev/null; done
(cd "$out" && sha256sum -- * > "$work/SHA256SUMS") && mv "$work/SHA256SUMS" "$out/SHA256SUMS"
echo "DRY RUN complete, nothing published. Files in $out:"
(cd "$out" && ls -l)
