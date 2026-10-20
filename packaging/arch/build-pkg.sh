#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Builds a binary development package (.pkg.tar.zst) for Arch Linux and
# derivatives from ./build using makepkg. Must run as a non-root user on an
# Arch-based system (CI uses an archlinux container). Unsigned.
set -euo pipefail
ver_in=${1:-0.0.0}
root=$(cd "$(dirname "$0")/../.." && pwd)
for f in hc-api hc-broker hlctl; do [ -x "$root/build/$f" ] || { echo "missing build/$f"; exit 1; }; done
# Version mapping (SemVer prerelease 0.1.0-dev.1 -> 0.1.0dev.1): see the helper.
# shellcheck source=packaging/common/version-map.sh
. "$root/packaging/common/version-map.sh"
pkgver=$(hc_arch_pkgver "$ver_in")
# Fixed build path: makepkg records startdir/builddir in .BUILDINFO, so a random temp dir would make
# every package different. The path is private to this script and removed before and after use.
work=${HC_MAKEPKG_DIR:-/tmp/homelab-control-makepkg}
rm -rf "$work"; mkdir -p "$work"; trap 'rm -rf "$work"' EXIT
cp "$root/build/hc-api" "$root/build/hc-broker" "$root/build/hlctl" "$work/"
cp "$root"/packaging/common/{homelab-control.service,homelab-control-broker.service,homelab-control-broker.socket,sysusers.conf,hc-api.env} "$work/"
cp "$root/LICENSE.md" "$root/NOTICE.md" "$root/README.md" "$root/docs/compliance/THIRD-PARTY-NOTICES.md" "$work/"
cat > "$work/homelab-control.install" <<'INST'
post_install() {
  # Install scriptlets run BEFORE pacman's post-transaction hooks, so the
  # 20-systemd-sysusers hook has not created the user yet. Create it here
  # (idempotent; the hook re-running later is harmless), then set the group.
  systemd-sysusers /usr/lib/sysusers.d/homelab-control.conf
  chgrp homelab-control /etc/homelab-control /etc/homelab-control/hc-api.env
  echo "homelab-control installed but NOT enabled. See /usr/share/doc/homelab-control/README.md"
}
post_upgrade() { post_install; }
pre_remove() {
  systemctl disable --now homelab-control.service homelab-control-broker.socket homelab-control-broker.service >/dev/null 2>&1 || true
}
INST
cat > "$work/PKGBUILD" <<PKG
pkgname=homelab-control
pkgver=$pkgver
pkgrel=1
pkgdesc="Homelab Control API, web UI and privileged broker (development build, prebuilt binaries)"
arch=('x86_64')
url="https://github.com/KRDucky/Homelab-Control---Webmin-Fork"
license=('GPL-3.0-or-later' 'MIT' 'OFL-1.1' 'BSD-3-Clause')
depends=('systemd')
backup=('etc/homelab-control/hc-api.env')
install=homelab-control.install
options=('!debug' '!strip')
source=(hc-api hc-broker hlctl homelab-control.service homelab-control-broker.service homelab-control-broker.socket sysusers.conf hc-api.env LICENSE.md NOTICE.md THIRD-PARTY-NOTICES.md README.md)
sha256sums=(SKIP SKIP SKIP SKIP SKIP SKIP SKIP SKIP SKIP SKIP SKIP SKIP)

package() {
  install -Dm0755 hc-api "\$pkgdir/usr/bin/hc-api"
  install -Dm0755 hlctl "\$pkgdir/usr/bin/hlctl"
  install -Dm0755 hc-broker "\$pkgdir/usr/lib/homelab-control/hc-broker"
  for u in homelab-control.service homelab-control-broker.service homelab-control-broker.socket; do
    install -Dm0644 "\$u" "\$pkgdir/usr/lib/systemd/system/\$u"
  done
  install -Dm0644 sysusers.conf "\$pkgdir/usr/lib/sysusers.d/homelab-control.conf"
  install -dm0750 "\$pkgdir/etc/homelab-control"
  install -Dm0640 hc-api.env "\$pkgdir/etc/homelab-control/hc-api.env"
  install -Dm0644 LICENSE.md NOTICE.md THIRD-PARTY-NOTICES.md -t "\$pkgdir/usr/share/licenses/\$pkgname/"
  install -Dm0644 README.md "\$pkgdir/usr/share/doc/\$pkgname/README.md"
}
PKG
(cd "$work" && SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-0} BUILDDIR="$work/build" PKGDEST="$work" \
  PACKAGER="Homelab Control (unsigned development build)" makepkg --nodeps --noconfirm --clean -f >/dev/null)
out=$(ls "$work"/*.pkg.tar.zst)
cp "$out" "$root/build/"
echo "$root/build/$(basename "$out")"
