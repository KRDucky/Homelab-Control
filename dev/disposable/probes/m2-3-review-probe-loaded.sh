#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 review probe 7b: like m2-3-review-probe.sh A1/A4, but the unit is
# loaded (running) so PID 1 keeps its cached copy. Disposable containers only.
set -uo pipefail
[ -f /etc/hc-disposable ] || { echo "refusing: not a disposable container"; exit 2; }
[ "${1:-pre}" = pre ] || exit 0
E=/etc/systemd/system U=/usr/lib/systemd/system
systemctl --version | head -n1
printf '[Unit]\nDescription=probe target\n' >"$E/hcp-x.target"
printf '[Service]\nExecStart=/bin/sleep infinity\n[Install]\nWantedBy=multi-user.target\n' >"$E/hcp-a.service"
printf '[Service]\nExecStart=/bin/sleep infinity\n[Install]\nWantedBy=multi-user.target\n' >"$U/hcp-c.service"
systemctl daemon-reload; systemctl start hcp-a.service hcp-c.service
show() { systemctl show -p FragmentPath -p DropInPaths -p NeedDaemonReload "$1" | sed 's/^/  /'; }
echo "## A1' new drop-in for a RUNNING unit, no reload"
mkdir -p "$E/hcp-a.service.d"; printf '[Install]\nWantedBy=hcp-x.target\n' >"$E/hcp-a.service.d/zz.conf"
show hcp-a.service
systemctl --no-reload enable hcp-a.service 2>&1 | sed 's/^/  enable: /'
echo "## A4' new /etc fragment shadowing a RUNNING /usr/lib unit, no reload"
printf '[Service]\nExecStart=/bin/sleep infinity\n[Install]\nWantedBy=hcp-x.target\n' >"$E/hcp-c.service"
show hcp-c.service
systemctl --no-reload enable hcp-c.service 2>&1 | sed 's/^/  enable: /'
echo "## B' disable: a link elsewhere whose NAME differs but whose target is the unit file"
find "$E" -name 'hcp-*' -type l -printf '  %p -> %l\n' | sort
