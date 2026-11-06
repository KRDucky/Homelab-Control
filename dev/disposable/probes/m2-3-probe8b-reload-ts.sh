#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 probe 8b: which manager timestamps and generator-directory ctimes change
# on daemon-reload. Disposable containers only.
set -uo pipefail
[ -f /etc/hc-disposable ] || { echo "refusing: not a disposable container"; exit 2; }
[ "${1:-pre}" = pre ] || exit 0
systemctl --version | head -n1
show() {
  systemctl show --timestamp=us+utc | grep -E '^(UnitsLoad|GeneratorsStart)' | grep -v Monotonic | sed "s/^/  $1 /"
  for d in /run/systemd/generator /run/systemd/generator.early /run/systemd/generator.late; do
    [ -e "$d" ] && echo "  $1 ctime $(stat -c '%z' "$d") $d"
  done
}
show boot
sleep 1; systemctl daemon-reload; show reload1
sleep 1; systemctl daemon-reload; show reload2
