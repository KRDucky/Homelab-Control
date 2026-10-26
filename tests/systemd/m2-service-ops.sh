#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-2: start/stop/restart through plan → confirm → execute under real systemd
# (M2-SECURITY-ACCEPTANCE E1). Sourced by systemd-smoke.sh via
# HC_SMOKE_EXTRA_CHECKS; uses its $pw and $sid. The account needs the scopes
# services.read,services.logs,services.operate,audit.read. Only disposable
# hc-test-* units are changed; protected units are planned, never executed.
# shellcheck disable=SC2154
echo "== M2-2: service operations (real systemd)"
if HC_PW=$pw HC_SID=$sid python3 "$(dirname "${BASH_SOURCE[0]}")/m2_service_ops.py"; then
  check "M2-2 service operations scenario" 0 0
else
  check "M2-2 service operations scenario" 0 1
fi
# E4: exposure of the broker with root-exec (no capabilities are added by M2-2).
echo "== systemd-analyze security homelab-control-broker.service"
systemd-analyze security --no-pager homelab-control-broker.service 2>/dev/null | tail -1 || true
check "broker capability bounding set unchanged (CAP_SETUID CAP_SETGID)" "cap_setgid cap_setuid" \
  "$(systemctl show -p CapabilityBoundingSet --value homelab-control-broker.service | tr ' ' '\n' | sort | xargs)"
