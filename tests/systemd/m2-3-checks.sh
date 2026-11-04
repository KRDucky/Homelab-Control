#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3: boot-time enable/disable, read-only unit views and the module
# registry under real systemd. Sourced by systemd-smoke.sh via
# HC_SMOKE_EXTRA_CHECKS (after m2-service-ops.sh); uses its $pw and $sid.
# Only disposable hc-test-en-* units are enabled or disabled.
# shellcheck disable=SC2154
echo "== M2-3: boot-time enablement, unit views, module registry (real systemd)"
if HC_PW=$pw HC_SID=$sid python3 "$(dirname "${BASH_SOURCE[0]}")/m2_3_enablement.py"; then
  check "M2-3 enablement scenario" 0 0
else
  check "M2-3 enablement scenario" 0 1
fi
# E4 comparison: exposure and capabilities with the M2-3 templates (no new capability).
echo "== E4 (M2-3): systemd-analyze security"
for u in homelab-control.service homelab-control-broker.service; do
  echo "$u: $(systemd-analyze security --no-pager "$u" 2>/dev/null | tail -1)"
done
check "broker capability bounding set unchanged (CAP_SETUID CAP_SETGID)" "cap_setgid cap_setuid" \
  "$(systemctl show -p CapabilityBoundingSet --value homelab-control-broker.service | tr ' ' '\n' | sort | xargs)"
check "API has no capabilities" "" "$(systemctl show -p CapabilityBoundingSet --value homelab-control.service)"
