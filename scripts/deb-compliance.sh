#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Release payload compliance for built .deb files (GitHub Actions ci.yml and the Jenkinsfile).
# Writes <pkg>.inventory.txt and <pkg>.lintian.txt next to each package. Fails on any lintian
# copyright or licence tag; other lintian findings are recorded, not gated (dev package).
# Usage: scripts/deb-compliance.sh build/*.deb
set -euo pipefail
[ $# -gt 0 ] || { echo "usage: $0 PKG.deb..." >&2; exit 2; }
for p in "$@"; do
  [ -f "$p" ] || { echo "deb-compliance: $p is not a file" >&2; exit 1; }
  scripts/package-inventory.sh "$p" "$p.inventory.txt"
done
for p in "$@"; do
  lintian --tag-display-limit 0 "$p" | tee "$p.lintian.txt" || true
  if grep -iE 'copyright|licen[cs]e|dep-?5' "$p.lintian.txt"; then
    echo "deb-compliance: lintian copyright/licence findings in $p" >&2
    exit 1
  fi
done
