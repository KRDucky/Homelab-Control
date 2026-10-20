# shellcheck shell=bash
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Maps a Homelab Control version (SemVer release/prerelease, git describe output or a commit id) to
# each package format's version syntax. Sourced by packaging/debian/build-deb.sh,
# packaging/fedora/build-rpm.sh, packaging/arch/build-pkg.sh and tests/release/version-order-test.sh;
# deb_version() is mirrored in scripts/release-tool.py.
#
# A SemVer prerelease (0.1.0-dev.1, 0.1.0-rc.2) must sort BEFORE the final 0.1.0 in every format,
# so that upgrading from a prerelease to the release works (tests/release/version-order-test.sh).

# Debian: versions must start with a digit. Anything that is not a dotted release number (e.g. a git
# hash that happens to start with a digit) becomes 0.0.0+git.<id> so development builds sort below
# real releases. A SemVer prerelease uses '~' (Debian Policy 5.6.12): 0.1.0~dev.1 < 0.1.0.
hc_deb_version() {
  local v=${1#v}
  if [[ "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+-[0-9A-Za-z.]+$ ]]; then v=${v/-/\~}; fi
  v=$(sed -E 's/[^A-Za-z0-9.+~]/+/g' <<<"$v")
  [[ "$v" =~ ^[0-9]+\.[0-9]+ ]] || v="0.0.0+git.$v"
  echo "$v"
}

# RPM: Version may not contain '-'; the rest goes into Release, which starts with "0." so a later
# final release with Release 1 sorts after every prerelease (Fedora prerelease convention).
hc_rpm_version() {
  local base; base=$(sed -E 's/^v//; s/[-+].*$//' <<<"$1")
  [[ "$base" =~ ^[0-9]+(\.[0-9]+)*$ ]] || base=0.1.0
  echo "$base"
}
hc_rpm_release() { echo "0.m1.$(sed -E 's/[^A-Za-z0-9]+/_/g' <<<"$1")"; }

# Arch: pkgver allows [A-Za-z0-9.]. A SemVer prerelease becomes 0.1.0dev.1: pacman's vercmp sorts a
# letter that directly follows the release number before the release (0.1.0dev.1 < 0.1.0), whereas
# "0.1.0.dev.1" would sort after it.
hc_arch_pkgver() {
  local v=${1#v}
  if [[ "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+-[A-Za-z][0-9A-Za-z.]*$ ]]; then v=${v/-/}; fi
  sed -E 's/[^A-Za-z0-9.]+/./g; s/^([^0-9])/0.\1/' <<<"$v"
}
