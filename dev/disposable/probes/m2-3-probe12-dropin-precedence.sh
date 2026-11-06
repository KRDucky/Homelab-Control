#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 probe 12 (final review): which of two drop-ins with the SAME file name
# applies, across unit directories (/etc, /run, /run/systemd/system.control,
# /usr/lib), drop-in masks (a link to /dev/null) and the drop-in hierarchy
# (own, prefix "foo-.service.d", type-level "service.d"). Each case uses its
# own unit name prefix; the applied drop-in sets Description=. Also prints the
# manager's UnitPath and systemd-analyze unit-paths. Disposable containers only.
set -uo pipefail
[ -f /etc/hc-disposable ] || { echo "refusing: not a disposable container"; exit 2; }
[ "${1:-pre}" = pre ] || { echo "post: nothing to do"; exit 0; }
systemctl --version | head -n1
echo "UnitPath=$(systemctl show -p UnitPath --value)"
echo "unit-paths: $(systemd-analyze unit-paths | tr '\n' ' ')"
L=/usr/lib/systemd/system E=/etc/systemd/system R=/run/systemd/system C=/run/systemd/system.control
unitf() { printf '[Unit]\nDescription=base\n[Service]\nType=oneshot\nExecStart=/bin/true\n' >"$L/$1.service"; }
put() { mkdir -p "$(dirname "$1")"; printf '[Unit]\nDescription=%s\n' "$2" >"$1"; }
mask() { mkdir -p "$(dirname "$1")"; ln -sf /dev/null "$1"; }
cases=(
 "a|own /usr/lib vs own /run|put $L/hcpa-x.service.d/10-p.conf usrlib-own; put $R/hcpa-x.service.d/10-p.conf run-own"
 "b|own /etc vs own /run|put $E/hcpb-x.service.d/10-p.conf etc-own; put $R/hcpb-x.service.d/10-p.conf run-own"
 "c|own /etc vs own system.control (runtime)|put $E/hcpc-x.service.d/10-p.conf etc-own; put $C/hcpc-x.service.d/10-p.conf control-own"
 "d|own /usr/lib vs runtime MASK|put $L/hcpd-x.service.d/10-p.conf usrlib-own; mask $R/hcpd-x.service.d/10-p.conf"
 "e|own /usr/lib vs /etc MASK|put $L/hcpe-x.service.d/10-p.conf usrlib-own; mask $E/hcpe-x.service.d/10-p.conf"
 "f|prefix /etc vs own /run|put $E/hcpf-.service.d/10-p.conf etc-prefix; put $R/hcpf-x.service.d/10-p.conf run-own"
 "g|prefix /run vs own /usr/lib|put $R/hcpg-.service.d/10-p.conf run-prefix; put $L/hcpg-x.service.d/10-p.conf usrlib-own"
 "h|type-level /etc vs own /run|put $E/service.d/10-hcph.conf etc-type; put $R/hcph-x.service.d/10-hcph.conf run-own"
 "i|type-level /run vs own /usr/lib|put $R/service.d/10-hcpi.conf run-type; put $L/hcpi-x.service.d/10-hcpi.conf usrlib-own"
 "j|prefix /usr/lib vs runtime MASK in own|put $L/hcpj-.service.d/10-p.conf usrlib-prefix; mask $R/hcpj-x.service.d/10-p.conf"
)
for c in "${cases[@]}"; do IFS='|' read -r id _ cmd <<<"$c"; unitf "hcp$id-x"; eval "$cmd"; done
systemctl daemon-reload
for c in "${cases[@]}"; do
  IFS='|' read -r id what _ <<<"$c"
  echo "CASE $id ($what): Description=$(systemctl show -p Description --value hcp$id-x.service) DropInPaths=$(systemctl show -p DropInPaths --value hcp$id-x.service)"
done
