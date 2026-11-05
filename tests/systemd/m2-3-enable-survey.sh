#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 enable survey (plan only; measurement). Sourced by systemd-smoke.sh via
# HC_SMOKE_EXTRA_CHECKS; uses its $pw and $sid. Disposable hosts only.
# shellcheck disable=SC2154
echo "== M2-3 enable survey (plan only)"
if HC_PW=$pw HC_SID=$sid python3 "$(dirname "${BASH_SOURCE[0]}")/m2_3_enable_survey.py"; then
  check "M2-3 enable survey ran" 0 0
else
  check "M2-3 enable survey ran" 0 1
fi
