#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 probe 9: on a freshly booted host where only the package was installed
# (no harness reload), which entries in the unit directories have a change
# time after systemd's last load? Mirrors the broker's change-gate inputs.
# Disposable containers only; /opt/hc/pkg.{deb,rpm} copied in beforehand.
set -uo pipefail
[ -f /etc/hc-disposable ] || { echo "refusing: not a disposable container"; exit 2; }
[ "${1:-pre}" = pre ] || exit 0
systemctl --version | head -n1
stamp() { systemctl show --timestamp=us+utc | grep -E '^UnitsLoad(Start)?Timestamp=' | sed 's/^/  /'; }
gate() {
  # The broker's rule (loadgate.go): the earliest of the generator directory's
  # change time and (systemd >= 255) UnitsLoadTimestamp, or the boot stamp
  # before any reload; no margin. /run/systemd/system{,.attached} are not
  # scanned by change time: any entry there refuses enable instead.
  local mono t g
  g=$(stat -c %.9Z /run/systemd/generator 2>/dev/null || stat -c %Z /run/systemd/generator)
  t=$g
  mono=$(systemctl show -p UnitsLoadTimestampMonotonic --value)
  if [ -n "$mono" ]; then
    local stamp
    if [ "$mono" != 0 ]; then stamp=$(systemctl show --timestamp=us+utc -p UnitsLoadTimestamp --value)
    else stamp=$(systemctl show --timestamp=us+utc -p UnitsLoadStartTimestamp --value); fi
    stamp=$(date -d "$stamp" +%s.%N)
    t=$(awk -v a="$g" -v b="$stamp" 'BEGIN{printf "%.9f", (a < b ? a : b)}')
  fi
  echo "  threshold (epoch): $t (generator directory $g)"
  for d in /etc/systemd/system /etc/systemd/system.attached /usr/local/lib/systemd/system /usr/lib/systemd/system; do
    [ -e "$d" ] || continue
    find "$d" -printf '%C@ %y %p\n' 2>/dev/null | awk -v t="$t" '$1 >= t {printf "  changed: %s %s (%+.3f s vs. threshold)\n", $2, $3, $1 - t}'
  done
  for d in /run/systemd/system /run/systemd/system.attached; do
    [ -d "$d" ] && [ -n "$(find "$d" -mindepth 1 -maxdepth 1)" ] && echo "  runtime configuration: $d: $(find "$d" -mindepth 1 -maxdepth 1 -printf '%f ' | head -c 300)"
  done
  echo "  (end of list)"
}
echo "## freshly booted, before installing"; stamp; gate
echo "  /run/systemd/system: $(stat -c '%z %y' /run/systemd/system); entries: $(find /run/systemd/system -mindepth 1 -maxdepth 1 | wc -l)"
echo "  /run mount: $(findmnt -no FSTYPE,OPTIONS /run)"
kind=$(find /opt/hc -maxdepth 1 -name 'pkg.*' -printf '%f' | sed 's/.*\.//')
echo "## installing the package ($kind; its scripts run daemon-reload)"
case $kind in
  deb) dpkg -i /opt/hc/pkg.deb >/dev/null 2>&1; echo "  installed: $?" ;;
  rpm) dnf install -y -q /opt/hc/pkg.rpm >/dev/null 2>&1; echo "  installed: $?" ;;
esac
stamp; gate
echo "## after the documented first step (systemctl enable --now the broker socket; enable reloads)"
systemctl enable --now homelab-control-broker.socket >/dev/null 2>&1
stamp; gate
