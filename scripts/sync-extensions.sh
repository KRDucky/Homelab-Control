#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Import a reviewed Extensions commit into extensions/ (squashed subtree merge) and update the pin.
# Usage: scripts/sync-extensions.sh <40-hex commit>
# Run on a dedicated branch. Every new contribution ID, file kind or broker operation still needs its
# own core review; this script only moves already-reviewed Extensions content.
set -euo pipefail
commit=${1:?usage: $0 <40-hex Extensions commit>}
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || { echo "commit must be a full 40-hex SHA"; exit 2; }
cd "$(git rev-parse --show-toplevel)"
if ! git diff --quiet || ! git diff --cached --quiet; then echo "working tree not clean"; exit 1; fi
remote=$(sed -n 's/^remote=//p' extensions.lock)
git fetch --quiet "$remote" "$commit"
[ "$(git rev-parse FETCH_HEAD)" = "$commit" ] || { echo "fetched commit does not match $commit"; exit 1; }
tree=$(git rev-parse "$commit^{tree}")
git subtree pull --prefix=extensions --squash "$remote" "$commit" \
  -m "extensions: sync Homelab-Control-Extensions ${commit:0:8} (squashed subtree)"
sed -i -e "s/^commit=.*/commit=$commit/" -e "s/^tree=.*/tree=$tree/" extensions.lock
echo "Synced extensions/ to $commit (tree $tree)."
echo "Now: node scripts/build-extensions.mjs, review the generated registry, commit extensions.lock, run make check."
