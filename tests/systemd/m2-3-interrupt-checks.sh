#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 Phase B on its own: only the deliberate broker interruption scenario
# (temporary systemctl test wrapper, SIGKILL of the broker). Sourced by
# systemd-smoke.sh via HC_SMOKE_EXTRA_CHECKS; uses its $pw and $sid.
# Disposable hosts only (/etc/hc-disposable) and HC_M2_INTERRUPT=1.
# shellcheck disable=SC2154
echo "== M2-3 Phase B: broker interruption during a boot-time change"
if HC_PW=$pw HC_SID=$sid python3 "$(dirname "${BASH_SOURCE[0]}")/m2_3_interrupt.py"; then
  check "M2-3 broker interruption scenario" 0 0
else
  check "M2-3 broker interruption scenario" 0 1
fi
