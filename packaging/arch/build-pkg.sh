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
for f in hc-api hc-broker hlctl hc-audit-retention hc-storage-probe hc-mergerfs-apply; do [ -x "$root/build/$f" ] || { echo "missing build/$f"; exit 1; }; done
# Version mapping (SemVer prerelease 0.1.0-dev.1 -> 0.1.0dev.1): see the helper.
# shellcheck source=packaging/common/version-map.sh
. "$root/packaging/common/version-map.sh"
pkgver=$(hc_arch_pkgver "$ver_in")
# Fixed build path: makepkg records startdir/builddir in .BUILDINFO, so a random temp dir would make
# every package different. The path is private to this script and removed before and after use.
work=${HC_MAKEPKG_DIR:-/tmp/homelab-control-makepkg}
rm -rf "$work"; mkdir -p "$work"; trap 'rm -rf "$work"' EXIT
cp "$root/build/hc-api" "$root/build/hc-broker" "$root/build/hlctl" "$root/build/hc-audit-retention" "$root/build/hc-storage-probe" "$root/build/hc-mergerfs-apply" "$work/"
cp "$root"/packaging/common/{homelab-control.service,homelab-control-broker.service,homelab-control-broker.socket,homelab-control-storage-helper.service,homelab-control-storage-probe@.service,homelab-control-storage-probe.socket,homelab-control-mergerfs-apply@.service,homelab-control-mergerfs-apply.socket,homelab-control-audit-retention.service,homelab-control-audit-retention.timer,sysusers.conf,hc-api.env,audit-retention.conf,mergerfs-apply.enabled} "$work/"
cp "$root/LICENSE.md" "$root/NOTICE.md" "$root/README.md" "$root/docs/compliance/THIRD-PARTY-NOTICES.md" "$work/"
sources=(hc-api hc-broker hlctl hc-audit-retention hc-storage-probe hc-mergerfs-apply homelab-control.service homelab-control-broker.service homelab-control-broker.socket homelab-control-storage-helper.service homelab-control-storage-probe@.service homelab-control-storage-probe.socket homelab-control-mergerfs-apply@.service homelab-control-mergerfs-apply.socket homelab-control-audit-retention.service homelab-control-audit-retention.timer sysusers.conf hc-api.env audit-retention.conf mergerfs-apply.enabled LICENSE.md NOTICE.md THIRD-PARTY-NOTICES.md README.md)
checksums=()
for source in "${sources[@]}"; do
  checksums+=("$(sha256sum -- "$work/$source" | awk '{print $1}')")
done
printf -v source_line 'source=(%s)' "${sources[*]}"
printf -v checksum_line 'sha256sums=(%s)' "${checksums[*]}"
cat > "$work/homelab-control.install" <<'INST'
post_install() {
  # Install scriptlets run BEFORE pacman's post-transaction hooks, so the
  # 20-systemd-sysusers hook has not created the user yet. Create it here
  # (idempotent; the hook re-running later is harmless), then set the group.
  hc_owner
  # Audit retention runs daily from the first install on (upgrades keep the administrator's choice).
  systemctl enable homelab-control-audit-retention.timer >/dev/null 2>&1 || true
  if [ -d /run/systemd/system ]; then systemctl start homelab-control-audit-retention.timer >/dev/null 2>&1 || true; fi
  echo "homelab-control installed but NOT enabled. See /usr/share/doc/homelab-control/README.md"
  echo "The mergerfs apply marker is installed. Its socket is not enabled and does not start at boot."
}
hc_owner() {
  systemd-sysusers /usr/lib/sysusers.d/homelab-control.conf
  chgrp homelab-control /etc/homelab-control /etc/homelab-control/hc-api.env /etc/homelab-control/audit-retention.conf
  # The marker is the off switch. An administrator who removed it must still be able to upgrade.
  if [ -e /etc/homelab-control/mergerfs-apply.enabled ]; then
    chgrp homelab-control /etc/homelab-control/mergerfs-apply.enabled
  fi
}
post_upgrade() { hc_owner; }
pre_remove() {
  systemctl disable --now homelab-control.service homelab-control-broker.socket homelab-control-broker.service homelab-control-audit-retention.timer >/dev/null 2>&1 || true
  # The socket has no [Install] section, so disable does not stop a running helper.
  systemctl stop homelab-control-mergerfs-apply.socket 'homelab-control-mergerfs-apply@*' >/dev/null 2>&1 || true
}
INST
cat > "$work/PKGBUILD" <<PKG
pkgname=homelab-control
pkgver=$pkgver
pkgrel=1
pkgdesc="Homelab Control API, web UI and privileged broker (development build, prebuilt binaries)"
arch=('x86_64')
url="https://github.com/KRDucky/Homelab-Control---Webmin-Fork"
license=('GPL-3.0-or-later' 'MIT' 'OFL-1.1' 'BSD-3-Clause' 'Apache-2.0' 'Unicode-3.0' 'Zlib')
depends=('systemd')
backup=('etc/homelab-control/hc-api.env' 'etc/homelab-control/audit-retention.conf' 'etc/homelab-control/mergerfs-apply.enabled')
install=homelab-control.install
options=('!debug' '!strip')
$source_line
$checksum_line

package() {
  install -Dm0755 hc-api "\$pkgdir/usr/bin/hc-api"
  install -Dm0755 hlctl "\$pkgdir/usr/bin/hlctl"
  install -Dm0755 hc-broker "\$pkgdir/usr/lib/homelab-control/hc-broker"
  install -Dm0755 hc-storage-probe "\$pkgdir/usr/lib/homelab-control/hc-storage-probe"
  install -Dm0755 hc-mergerfs-apply "\$pkgdir/usr/lib/homelab-control/hc-mergerfs-apply"
  install -Dm0755 hc-audit-retention "\$pkgdir/usr/bin/hc-audit-retention"
  for u in homelab-control.service homelab-control-broker.service homelab-control-broker.socket homelab-control-storage-helper.service homelab-control-storage-probe@.service homelab-control-storage-probe.socket homelab-control-mergerfs-apply@.service homelab-control-mergerfs-apply.socket homelab-control-audit-retention.service homelab-control-audit-retention.timer; do
    install -Dm0644 "\$u" "\$pkgdir/usr/lib/systemd/system/\$u"
  done
  install -Dm0644 sysusers.conf "\$pkgdir/usr/lib/sysusers.d/homelab-control.conf"
  install -dm0750 "\$pkgdir/etc/homelab-control"
  install -Dm0640 hc-api.env "\$pkgdir/etc/homelab-control/hc-api.env"
  install -Dm0640 audit-retention.conf "\$pkgdir/etc/homelab-control/audit-retention.conf"
  install -Dm0640 mergerfs-apply.enabled "\$pkgdir/etc/homelab-control/mergerfs-apply.enabled"
  install -Dm0644 LICENSE.md NOTICE.md THIRD-PARTY-NOTICES.md -t "\$pkgdir/usr/share/licenses/\$pkgname/"
  install -Dm0644 README.md "\$pkgdir/usr/share/doc/\$pkgname/README.md"
}
PKG
(cd "$work" && SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-0} BUILDDIR="$work/build" PKGDEST="$work" \
  PACKAGER="Homelab Control (unsigned development build)" makepkg --nodeps --noconfirm --clean -f >/dev/null)
out=$(ls "$work"/*.pkg.tar.zst)
cp "$out" "$root/build/"
echo "$root/build/$(basename "$out")"
