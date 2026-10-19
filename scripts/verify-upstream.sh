#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Verifies that compat/webmin matches the pinned upstream tree, or that any
# divergence is accounted for in compat/PATCHES.md.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
# shellcheck disable=SC1091
pin_tree=$(grep '^upstream_tree=' compat/webmin.lock | cut -d= -f2)
pin_tag=$(grep '^upstream_tag=' compat/webmin.lock | cut -d= -f2)
cur_tree=$(git rev-parse HEAD:compat/webmin)
split=$(git log --grep='^git-subtree-dir: compat/webmin' --format=%B -n1 | sed -n 's/^git-subtree-split: //p' | head -n1)
echo "pinned tag:          $pin_tag"
echo "subtree split (git): ${split:-<none>}"
test -f compat/webmin/LICENCE || { echo "FAIL: compat/webmin/LICENCE missing (BSD-3 notice must be retained)"; exit 1; }
if [ "$cur_tree" = "$pin_tree" ]; then
  echo "OK: compat/webmin is byte-identical to upstream $pin_tag (tree $cur_tree)"
  exit 0
fi
patches=$(grep -cE '^\| P[0-9]+' compat/PATCHES.md || true)
if [ "$patches" -gt 0 ]; then
  echo "NOTE: compat/webmin differs from pinned upstream tree; $patches fork patch(es) recorded in compat/PATCHES.md"
  exit 0
fi
echo "FAIL: compat/webmin differs from pinned upstream tree $pin_tree but compat/PATCHES.md lists no patches"
exit 1
