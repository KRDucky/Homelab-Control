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
# 3. Builds the same commit from a git worktree and requires byte-identical binaries, which
#    shows the archive is complete and the build reproducible.
# 4. Writes SHA256SUMS over all release files. No signing, tagging or upload happens here.
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
cleanup() { git worktree remove --force "$work/wt" >/dev/null 2>&1 || true; chmod -R u+w "$work" 2>/dev/null || true; rm -rf "$work"; }
trap cleanup EXIT
mkdir -p "$out"; out=$(cd "$out" && pwd)
[ -z "$(ls -A "$out")" ] || { echo "refusing: $out is not empty"; exit 1; }
export SOURCE_DATE_EPOCH=$sde

echo "== 1. corresponding-source archive ($commit)"
python3 -I scripts/release-npm-sources.py "$work/vendor/vendor-sources/npm"
buildinfo=$(cat <<INFO
# Homelab Control release build information (see BUILDING.md)
version=$ver
commit=$commit
source_date_epoch=$sde
go=$(go env GOVERSION)
node=$(node --version)
npm=$(npm --version)
build_command=make build VERSION=$ver SOURCE_DATE_EPOCH=$sde
packages_command=packaging/debian/build-deb.sh $ver; packaging/fedora/build-rpm.sh $ver
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
(cd "$src" && make -s build VERSION="$ver" SOURCE_DATE_EPOCH="$sde" >/dev/null)
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

echo "== 4. release files"
bin=$work/bin/$name-linux-$(go env GOARCH)
install -Dm0755 -t "$bin" "$src/build/hc-api" "$src/build/hlctl" "$src/build/hc-broker"
install -Dm0644 -t "$bin" "$src/LICENSE.md" "$src/NOTICE.md" "$src/docs/compliance/THIRD-PARTY-NOTICES.md"
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$sde" -C "$work/bin" -cf - "$(basename "$bin")" | gzip -n -9 > "$out/$(basename "$bin").tar.gz"
cp "$src"/build/*.deb "$out/"
cp "$src"/build/*.rpm "$out/" 2>/dev/null || true
cp "$src/LICENSE.md" "$src/NOTICE.md" "$src/docs/compliance/THIRD-PARTY-NOTICES.md" "$out/"
cp "$src/docs/compliance/sbom/homelab-control.spdx.json" "$out/$name.spdx.json"
printf '%s\n' "$buildinfo" > "$out/RELEASE-BUILDINFO"
for p in "$out"/*.deb "$out"/*.rpm; do [ -e "$p" ] && "$root/scripts/package-inventory.sh" "$p" >/dev/null; done
(cd "$out" && sha256sum -- * > "$work/SHA256SUMS") && mv "$work/SHA256SUMS" "$out/SHA256SUMS"
echo "DRY RUN complete, nothing published. Files in $out:"
(cd "$out" && ls -l)
