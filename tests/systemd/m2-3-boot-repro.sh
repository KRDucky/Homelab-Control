#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 final safety review, phase pre (measurement): sourced by systemd-smoke.sh
# via HC_SMOKE_EXTRA_CHECKS; uses its $pw and $sid. Disposable hosts only; the
# host is rebooted afterwards by dev/disposable/boot-repro.sh.
# shellcheck disable=SC2154
HC_PW=$pw HC_SID=$sid python3 "$(dirname "${BASH_SOURCE[0]}")/m2_3_boot_repro.py" pre
