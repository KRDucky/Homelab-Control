#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Builds a development .deb (homelab-control) from ./build.
# Unsigned, not for distribution. See docs/packaging/PLAN.md.
set -euo pipefail
ver_in=${1:-0.0.0}
root=$(cd "$(dirname "$0")/../.." && pwd)
# Version mapping (SemVer prerelease -> 0.1.0~dev.1, commit ids -> 0.0.0+git.<id>): see the helper.
# shellcheck source=packaging/common/version-map.sh
. "$root/packaging/common/version-map.sh"
ver=$(hc_deb_version "$ver_in")
arch=$(dpkg --print-architecture)
stage=$(mktemp -d); chmod 0755 "$stage"; trap 'rm -rf "$stage"' EXIT
install -Dm0755 "$root/build/hc-api" "$stage/usr/bin/hc-api"
install -Dm0755 "$root/build/hlctl"  "$stage/usr/bin/hlctl"
install -Dm0755 "$root/build/hc-broker" "$stage/usr/lib/homelab-control/hc-broker"
install -Dm0644 "$root/packaging/common/homelab-control-broker.service" "$stage/usr/lib/systemd/system/homelab-control-broker.service"
install -Dm0644 "$root/packaging/common/homelab-control-broker.socket" "$stage/usr/lib/systemd/system/homelab-control-broker.socket"
install -Dm0644 "$root/packaging/common/homelab-control.service" "$stage/usr/lib/systemd/system/homelab-control.service"
install -Dm0644 "$root/packaging/common/sysusers.conf" "$stage/usr/lib/sysusers.d/homelab-control.conf"
install -Dm0640 "$root/packaging/common/hc-api.env" "$stage/etc/homelab-control/hc-api.env"
install -Dm0644 "$root/LICENSE.md" "$stage/usr/share/doc/homelab-control/LICENSE.md"
install -Dm0644 "$root/NOTICE.md"  "$stage/usr/share/doc/homelab-control/NOTICE.md"
install -Dm0644 "$root/docs/compliance/THIRD-PARTY-NOTICES.md" "$stage/usr/share/doc/homelab-control/THIRD-PARTY-NOTICES.md"
install -Dm0644 "$root/README.md"  "$stage/usr/share/doc/homelab-control/README.md"
# Debian Policy 12.5: machine-readable copyright file (generated, LA-12).
install -Dm0644 "$root/packaging/debian/copyright" "$stage/usr/share/doc/homelab-control/copyright"
mkdir -p "$stage/DEBIAN"
echo "/etc/homelab-control/hc-api.env" > "$stage/DEBIAN/conffiles"
cat > "$stage/DEBIAN/control" <<CTRL
Package: homelab-control
Provides: homelab-control-api
Conflicts: homelab-control-api
Replaces: homelab-control-api
Version: $ver
Architecture: $arch
Maintainer: Homelab Control developers <noreply@example.invalid>
Section: admin
Priority: optional
Depends: adduser | systemd
Recommends: systemd
Homepage: https://github.com/KRDucky/Homelab-Control---Webmin-Fork
Description: Homelab Control API, web UI and privileged broker (development build, M1)
 Unprivileged API with authenticated sessions and a root broker that exposes
 only typed, read-only operations. Coexists with upstream Webmin; does not
 modify /etc/webmin.
CTRL
cat > "$stage/DEBIAN/postinst" <<'POST'
#!/bin/sh
set -e
if [ "$1" = configure ]; then
  if command -v systemd-sysusers >/dev/null 2>&1; then
    systemd-sysusers /usr/lib/sysusers.d/homelab-control.conf
  elif ! getent passwd homelab-control >/dev/null; then
    adduser --system --group --no-create-home --home /nonexistent homelab-control
  fi
  chgrp homelab-control /etc/homelab-control /etc/homelab-control/hc-api.env
  chmod 0750 /etc/homelab-control
  install -d -m 0700 -o homelab-control -g homelab-control /var/log/homelab-control
  if [ -d /run/systemd/system ]; then systemctl daemon-reload || true; fi
  echo "homelab-control installed but NOT enabled. Create an account with:"
  echo "  sudo hlctl user add --file /etc/homelab-control/users.json --username admin"
  echo "  sudo chown homelab-control:homelab-control /etc/homelab-control/users.json"
  echo "then configure /etc/homelab-control/hc-api.env and run:"
  echo "  systemctl enable --now homelab-control-broker.socket homelab-control.service"
fi
POST
cat > "$stage/DEBIAN/prerm" <<'PRE'
#!/bin/sh
set -e
if [ -d /run/systemd/system ] && [ "$1" = remove ]; then
  systemctl stop homelab-control.service homelab-control-broker.service homelab-control-broker.socket || true
  systemctl disable homelab-control.service homelab-control-broker.socket >/dev/null 2>&1 || true
fi
PRE
cat > "$stage/DEBIAN/postrm" <<'PRM'
#!/bin/sh
set -e
if [ "$1" = purge ]; then
  rm -f /etc/homelab-control/users.json
  rmdir /etc/homelab-control 2>/dev/null || true
  echo "Audit logs kept in /var/log/homelab-control and /var/log/homelab-control-broker (remove manually if no longer needed)."
fi
if [ -d /run/systemd/system ]; then systemctl daemon-reload || true; fi
PRM
chmod 0755 "$stage/DEBIAN/postinst" "$stage/DEBIAN/prerm" "$stage/DEBIAN/postrm"
mkdir -p "$root/build"
out="$root/build/homelab-control_${ver}_${arch}.deb"
find "$stage" -exec touch -h -d "@${SOURCE_DATE_EPOCH:-0}" {} +
dpkg-deb --root-owner-group -Zxz --build "$stage" "$out" >/dev/null
echo "$out"
