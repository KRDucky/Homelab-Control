#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# The Jenkins "graphify" stage (ci/README.md): Graphify impact review of the commit under test, in
# the toolchain container. Reports go to reports/graphify/ (ci/graphify_review.py).
#
# The change is compared with HC_GRAPHIFY_BASE when it is set (pre-push mode: the pinned main), else
# with the pull request's target (CHANGE_TARGET, as Jenkins merged it), else with the first parent.
# HC_TESTED_COMMIT, when set, must be HEAD. Exit status is ci/graphify_review.py's.
set -euo pipefail
out=reports/graphify
mkdir -p "$out"
if [ -n "${HC_GRAPHIFY_BASE:-}" ]; then
  base=$HC_GRAPHIFY_BASE
elif [ -n "${CHANGE_TARGET:-}" ] && git rev-parse -q --verify "origin/$CHANGE_TARGET^{commit}" >/dev/null; then
  base=$(git merge-base HEAD "origin/$CHANGE_TARGET")
elif git rev-parse -q --verify 'HEAD^1' >/dev/null; then
  base=$(git rev-parse 'HEAD^1')
else
  base=$(git rev-parse HEAD)
fi
set +e
/opt/graphify/bin/python -I ci/graphify_review.py --base "$base" --out "$out" ${HC_TESTED_COMMIT:+--expect "$HC_TESTED_COMMIT"}
status=$?
set -e
[ "$status" -le 1 ] || echo error > "$out/STATUS"
exit "$status"
