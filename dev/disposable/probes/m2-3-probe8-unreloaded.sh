#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 probe 8: unreloaded [Unit] changes and the manager's load timestamp.
# Disposable containers only (dev/disposable/probe-container.sh). Homelab
# Control is not installed; hcp-* fixtures; hcp-prot.service stands in for a
# protected service (a long-running unit that must stay running at boot).
set -uo pipefail
[ -f /etc/hc-disposable ] || { echo "refusing: not a disposable container"; exit 2; }
phase=${1:?pre|post}
# PROBE_CASE=g1|g2|g3|g4 runs only that change (one change per boot); unset runs all.
case_on() { [ -z "${PROBE_CASE:-}" ] || [ "$PROBE_CASE" = "$1" ]; }
E=/etc/systemd/system
say() { printf '\n## %s\n' "$*"; }
svc() { printf '[Unit]\nDescription=probe %s\n%s\n[Service]\nExecStart=/bin/sleep infinity\n[Install]\nWantedBy=multi-user.target\n' "$1" "${2:-}" >"$E/$1"; }
ts() { systemctl show --timestamp=us+utc -p UnitsLoadStartTimestamp -p UnitsLoadFinishTimestamp 2>&1 | sed 's/^/  /'; }
ctime() { stat -c '%z  %n' "$@" | sed 's/^/  ctime /'; }

if [ "$phase" = pre ]; then
  systemctl --version | head -n1
  say "T1 manager load timestamps (us+utc) and the property names this version has"
  systemctl show | grep -iE '^UnitsLoad' | sed 's/^/  /'
  ts
  say "T2 does daemon-reload update them? (setup reload)"
  svc hcp-prot.service; svc hcp-a-x.service; svc hcp-b.service; svc hcp-c.service
  printf '[Unit]\nDescription=probe conflictor\nConflicts=hcp-prot.service\n[Service]\nExecStart=/bin/sleep infinity\n' >"$E/hcp-conflictor.service"
  svc hcp-d.service "Wants=hcp-dep.service"
  printf '[Unit]\nDescription=probe dependency\n[Service]\nExecStart=/bin/sleep infinity\n' >"$E/hcp-dep.service"
  systemctl daemon-reload
  ts
  say "T3 generator, transient and control files vs. the load start"
  ctime /run/systemd/generator* 2>/dev/null | head -n 6
  systemd-run --unit hcp-transient /bin/sleep 300 >/dev/null 2>&1
  ctime /run/systemd/transient/hcp-transient.service 2>/dev/null
  systemctl set-property hcp-prot.service CPUWeight=50 >/dev/null 2>&1
  ctime /etc/systemd/system.control/hcp-prot.service.d/* 2>/dev/null
  systemctl enable --now hcp-prot.service >/dev/null 2>&1
  systemctl disable hcp-a-x.service hcp-b.service hcp-c.service hcp-d.service >/dev/null 2>&1
  systemctl daemon-reload
  ts
  echo "  hcp-prot: $(systemctl is-active hcp-prot.service)"

  if case_on g1; then
  say "G1 NEW prefix drop-in (hcp-a-.service.d) adding Conflicts=hcp-prot.service, no reload"
  systemctl start hcp-a-x.service   # loaded, so systemd keeps its cached copy
  mkdir -p "$E/hcp-a-.service.d"
  printf '[Unit]\nConflicts=hcp-prot.service\n' >"$E/hcp-a-.service.d/zz-g1.conf"
  systemctl show -p Conflicts -p DropInPaths -p NeedDaemonReload hcp-a-x.service | sed 's/^/  loaded: /'
  ctime "$E/hcp-a-.service.d/zz-g1.conf"
  systemctl stop hcp-a-x.service
  systemctl --no-reload enable hcp-a-x.service 2>&1 | sed 's/^/  enable: /'

  fi
  if case_on g2; then
  say "G2 existing fragment edited: Wants=hcp-conflictor.service, no reload"
  systemctl start hcp-b.service
  sed -i 's/^\[Service\]/Wants=hcp-conflictor.service\n[Service]/' "$E/hcp-b.service"
  systemctl show -p Wants -p NeedDaemonReload hcp-b.service | sed 's/^/  loaded: /'
  ctime "$E/hcp-b.service"
  systemctl stop hcp-b.service
  systemctl --no-reload enable hcp-b.service 2>&1 | sed 's/^/  enable: /'

  fi
  if case_on g3; then
  say "G3 OTHER unit's new drop-in: hcp-prot.service.d Conflicts=hcp-c.service, no reload"
  mkdir -p "$E/hcp-prot.service.d"
  printf '[Unit]\nConflicts=hcp-c.service\n' >"$E/hcp-prot.service.d/zz-g3.conf"
  systemctl show -p ConflictedBy -p NeedDaemonReload hcp-c.service | sed 's/^/  loaded hcp-c: /'
  systemctl start hcp-c.service; systemctl show -p ConflictedBy hcp-c.service | sed 's/^/  loaded hcp-c (started): /'; systemctl stop hcp-c.service
  systemctl --no-reload enable hcp-c.service 2>&1 | sed 's/^/  enable: /'
  fi
  if case_on g4; then
  say "G4 a DEPENDENCY's file edited (hcp-d Wants=hcp-dep; hcp-dep gains Conflicts=hcp-prot.service), no reload"
  systemctl start hcp-d.service   # loads hcp-d and hcp-dep
  sed -i 's/^\[Service\]/Conflicts=hcp-prot.service\n[Service]/' "$E/hcp-dep.service"
  systemctl show -p Conflicts -p NeedDaemonReload hcp-dep.service | sed 's/^/  loaded hcp-dep: /'
  systemctl stop hcp-d.service hcp-dep.service
  systemctl --no-reload enable hcp-d.service 2>&1 | sed 's/^/  enable: /'
  fi
  say "T4 entries under /etc/systemd/system newer than the load start (find -newer is mtime; ctime list)"
  ts
  find "$E" -newer /proc/1 -printf '  %C+ %y %p\n' 2>/dev/null | sort | head -n 20
  exit 0
fi

say "after restart (fresh PID 1)"
systemctl --version | head -n1
ts
for u in hcp-prot hcp-a-x hcp-b hcp-c hcp-d hcp-dep hcp-conflictor; do
  echo "  $u.service: active=$(systemctl is-active $u.service) enabled=$(systemctl is-enabled $u.service 2>&1)"
done
say "jobs and conflicts this boot"
journalctl -b --no-pager -o cat | grep -iE 'hcp-prot|conflict' | head -n 20 | sed 's/^/  /'
echo "  (end)"
