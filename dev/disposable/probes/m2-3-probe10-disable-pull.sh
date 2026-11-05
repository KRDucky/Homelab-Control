#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 probe 10 (B-1): does disabling a unit that pulls in a "protected" unit
# stop that unit from starting at the next boot? hcp-vpn.service stands in for
# a remote-access unit (no [Install]: it starts only when pulled in).
# PROBE_CASE=direct|transitive|alias|anchored|socket; CONTROL=1 keeps the puller
# enabled. Phase pre enables the puller (and disables it again unless CONTROL=1,
# with --no-reload); the container restarts; phase post records the boot.
# Disposable containers only.
set -uo pipefail
[ -f /etc/hc-disposable ] || { echo "refusing: not a disposable container"; exit 2; }
phase=${1:?pre|post}; c=${PROBE_CASE:?PROBE_CASE}
E=/etc/systemd/system
state() { for u in hcp-b1 hcp-mid hcp-vpn hcp-anchor; do
  [ -e "$E/$u.service" ] && echo "  $1 $u.service: active=$(systemctl is-active $u.service) enabled=$(systemctl is-enabled $u.service 2>&1)"; done; }
svc() { printf '[Unit]\nDescription=probe %s\n%s\n[Service]\nExecStart=/bin/sleep infinity\n%s\n' "$1" "${2:-}" "${3:-}" >"$E/$1.service"; }
if [ "$phase" = pre ]; then
  systemctl --version | head -n1
  svc hcp-vpn
  case $c in
    direct)     svc hcp-b1 "Wants=hcp-vpn.service" "[Install]
WantedBy=multi-user.target" ;;
    transitive) svc hcp-mid "Requires=hcp-vpn.service"; svc hcp-b1 "Wants=hcp-mid.service" "[Install]
WantedBy=multi-user.target" ;;
    alias)      ln -s "$E/hcp-vpn.service" "$E/hcp-vpnalias.service"
                svc hcp-b1 "Wants=hcp-vpnalias.service" "[Install]
WantedBy=multi-user.target" ;;
    anchored)   svc hcp-anchor "Wants=hcp-vpn.service" "[Install]
WantedBy=multi-user.target"
                svc hcp-b1 "Wants=hcp-vpn.service" "[Install]
WantedBy=multi-user.target" ;;
    socket)     printf '[Socket]\nListenStream=127.0.0.1:47999\nService=hcp-vpn.service\n[Install]\nWantedBy=sockets.target\n' >"$E/hcp-vpn.socket"
                svc hcp-b1 "Wants=hcp-vpn.service" "[Install]
WantedBy=multi-user.target" ;;
  esac
  systemctl daemon-reload
  systemctl enable hcp-b1.service >/dev/null 2>&1
  [ "$c" = anchored ] && systemctl enable hcp-anchor.service >/dev/null 2>&1
  [ "$c" = socket ] && systemctl enable hcp-vpn.socket >/dev/null 2>&1
  state before
  if [ "${CONTROL:-0}" = 1 ]; then
    echo "## $c (control): puller stays enabled; reboot"
  else
    echo "## $c: disable the puller now (no reload), then reboot"
    systemctl --no-reload disable hcp-b1.service 2>&1 | sed 's/^/  disable: /'
  fi
  exit 0
fi
echo "## $c$([ "${CONTROL:-0}" = 1 ] && echo ' (control)'): after reboot (fresh PID 1)"
systemctl --version | head -n1
state after-reboot
[ "$c" = socket ] && echo "  hcp-vpn.socket: $(systemctl is-active hcp-vpn.socket)"
