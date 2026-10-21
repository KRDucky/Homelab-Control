#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Checks with each distribution's own version comparison that the package versions produced from
# SemVer prerelease versions (vX.Y.Z-dev.N / -rc.N) sort before the final X.Y.Z release and in the
# right order among themselves, so upgrades from a prerelease to the release work.
#   tests/release/version-order-test.sh [--require deb,rpm,arch]
# deb: dpkg --compare-versions on build-deb.sh's mapping; rpm: rpm.vercmp on build-rpm.sh's
# Version-Release; arch: pacman's vercmp on build-pkg.sh's pkgver. A tool that is missing is
# skipped unless listed in --require (CI runs deb+rpm on Ubuntu and arch in the Arch container).
set -euo pipefail
require=""
[ "${1:-}" = "--require" ] && require=",${2:-},"
fail=0
root=$(cd "$(dirname "$0")/../.." && pwd)
# The mapping functions the packaging scripts use, so the test cannot drift from them.
# shellcheck source=packaging/common/version-map.sh
. "$root/packaging/common/version-map.sh"

# Pairs that must compare as "older newer".
pairs=(
  "0.1.0-dev.1 0.1.0-dev.2"
  "0.1.0-dev.9 0.1.0-dev.10"
  "0.1.0-dev.3 0.1.0-rc.1"
  "0.1.0-rc.1 0.1.0-rc.2"
  "0.1.0-rc.2 0.1.0"
  "0.1.0-dev.1 0.1.0"
  "0.1.0 0.2.0-dev.1"
)
res() { if [ "$1" = 0 ]; then echo "$2 < $3"; else echo "$2 >= $3"; return 1; fi; }
deb_lt() { local a b r=0; a=$(hc_deb_version "$1"); b=$(hc_deb_version "$2"); dpkg --compare-versions "$a" lt "$b" || r=1; res "$r" "$a" "$b"; }
# A final release is assumed to use RPM Release 1 (prereleases use Release 0.*, see version-map.sh).
rpm_vr() { if [[ "$1" == *-* ]]; then echo "$(hc_rpm_version "$1")-$(hc_rpm_release "$1")"; else echo "$1-1"; fi; }
rpm_lt() {
  local a b r=0; a=$(rpm_vr "$1"); b=$(rpm_vr "$2")
  [ "$(rpm --eval "%{lua: print(rpm.vercmp('$a', '$b'))}")" = "-1" ] || r=1; res "$r" "$a" "$b"
}
arch_lt() { local a b r=0; a=$(hc_arch_pkgver "$1"); b=$(hc_arch_pkgver "$2"); [ "$(vercmp "$a" "$b")" = "-1" ] || r=1; res "$r" "$a" "$b"; }

check() { # kind tool compare-function
  local kind=$1 tool=$2 fn=$3 p older newer out
  if ! command -v "$tool" >/dev/null 2>&1; then
    if [[ "$require" == *",$kind,"* ]]; then echo "FAIL  $kind: $tool not installed (required)"; fail=1
    else echo "SKIPPED $kind ($tool not installed)"; fi
    return 0
  fi
  # Self-test: a reversed pair must be reported as wrong, or this check proves nothing.
  if "$fn" 0.1.0 0.1.0-dev.1 >/dev/null; then echo "FAIL  $kind: comparison accepts 0.1.0 < 0.1.0-dev.1"; fail=1; fi
  for p in "${pairs[@]}"; do
    read -r older newer <<<"$p"
    if out=$("$fn" "$older" "$newer"); then echo "PASS  $kind $older < $newer  ($out)"
    else echo "FAIL  $kind $older is not older than $newer ($out)"; fail=1; fi
  done
}

check deb dpkg deb_lt
check rpm rpm rpm_lt
check arch vercmp arch_lt
if [ "$fail" = 0 ]; then echo "version order: OK"; else echo "version order: FAILED"; exit 1; fi
