#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Verifies that the extensions/ subtree is byte-identical to the commit and tree pinned in
# extensions.lock, with no local edits (committed or not) and no package or module manifests.
# Unlike compat/webmin, core never patches this prefix: there is no PATCHES exception.
# Usage: scripts/verify-extensions.sh [--fetch]   (--fetch also checks the pin against the remote)
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
lock=extensions.lock
field() { sed -n "s/^$1=//p" "$lock"; }
remote=$(field remote); commit=$(field commit); tree=$(field tree); prefix=$(field prefix)
[[ "$commit" =~ ^[0-9a-f]{40}$ && "$tree" =~ ^[0-9a-f]{40}$ ]] || { echo "FAIL: $lock needs a 40-hex commit and tree"; exit 1; }
[ "$prefix" = extensions ] || { echo "FAIL: $lock prefix must be 'extensions' (got '$prefix')"; exit 1; }
[ "$remote" = https://github.com/KRDucky/Homelab-Control-Extensions.git ] || { echo "FAIL: unexpected remote '$remote'"; exit 1; }

cur_tree=$(git rev-parse "HEAD:$prefix" 2>/dev/null) || { echo "FAIL: $prefix/ is not in HEAD"; exit 1; }
if [ "$cur_tree" != "$tree" ]; then
  echo "FAIL: HEAD:$prefix is $cur_tree, but $lock pins $tree"
  exit 1
fi
if ! git diff --quiet HEAD -- "$prefix" || [ -n "$(git ls-files --others --exclude-standard -- "$prefix")" ]; then
  echo "FAIL: $prefix/ has uncommitted or untracked changes; edit the Extensions repository instead"
  git status --short -- "$prefix"
  exit 1
fi
split=$(git log --grep="^git-subtree-dir: $prefix\$" --format=%B -n1 | sed -n 's/^git-subtree-split: //p' | head -n1)
if [ "$split" != "$commit" ]; then
  echo "FAIL: latest subtree squash records split '${split:-<none>}', but $lock pins $commit"
  exit 1
fi
if git ls-files -- "$prefix" | grep -E '(^|/)(package(-lock)?\.json|go\.(mod|sum)|node_modules/)' ; then
  echo "FAIL: package or module manifests are not allowed under $prefix/"
  exit 1
fi
if [ "${1:-}" = --fetch ]; then
  git fetch --quiet "$remote" "$commit"
  [ "$(git rev-parse "FETCH_HEAD^{tree}")" = "$tree" ] || { echo "FAIL: remote commit $commit does not have tree $tree"; exit 1; }
  echo "OK: remote commit $commit has the pinned tree"
fi
echo "OK: $prefix/ matches Extensions $commit (tree $tree)"
