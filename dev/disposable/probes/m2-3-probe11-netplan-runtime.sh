#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 probe 11 (B-2): what netplan writes into /run/systemd/system, when (boot,
# reload) relative to systemd's load timestamps, and what systemd loads from it.
# Disposable netplan container only (dev/disposable/probe-container.sh ubuntu-2404-netplan).
set -uo pipefail
[ -f /etc/hc-disposable ] || { echo "refusing: not a disposable container"; exit 2; }
show() {
  echo "## $1"
  systemctl show --timestamp=us+utc | grep -E '^(UnitsLoadStartTimestamp|UnitsLoadTimestamp|GeneratorsStartTimestamp|GeneratorsFinishTimestamp)=' | sed 's/^/  /'
  echo "  generator dir ctime: $(stat -c %z /run/systemd/generator)"
  find /run/systemd/system -printf '  %C+ %y %p%l\n' 2>/dev/null | sed 's/%l//' | sort -k3
  find /run/systemd/system -type l -printf '  link %p -> %l\n' 2>/dev/null
  find /run/systemd/system -type f -print0 2>/dev/null | while IFS= read -r -d '' f; do echo "  --- $f"; sed 's/^/    /' "$f"; done
}
if [ "${1:-pre}" = pre ]; then
  systemctl --version | head -n1; netplan --help >/dev/null 2>&1 && dpkg -l netplan.io | tail -n1 | awk '{print "  netplan.io " $3}'
  show "boot (before any reload)"
  for u in netplan-ovs-cleanup.service systemd-networkd.service systemd-networkd-wait-online.service; do
    systemctl show -p Id -p LoadState -p FragmentPath -p DropInPaths -p Wants -p WantedBy -p UnitFileState "$u" | sed "s/^/  $u: /"
  done
  sleep 1; systemctl daemon-reload
  show "after daemon-reload"
  exit 0
fi
show "after restart (fresh boot)"
