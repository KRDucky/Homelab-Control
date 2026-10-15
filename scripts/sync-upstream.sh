#!/usr/bin/env bash
# Pull a new upstream Webmin release into compat/webmin (squashed subtree merge).
# Usage: scripts/sync-upstream.sh <tag>      e.g. scripts/sync-upstream.sh 2.680
# Run on a dedicated branch; review the diff and docs/upstream/SYNC.md checklist before merging.
set -euo pipefail
tag=${1:?usage: $0 <upstream-tag>}
[[ "$tag" =~ ^[0-9]+\.[0-9]+$ ]] || { echo "tag must look like 2.680"; exit 2; }
cd "$(git rev-parse --show-toplevel)"
git diff --quiet && git diff --cached --quiet || { echo "working tree not clean"; exit 1; }
url=$(grep '^upstream_url=' compat/webmin.lock | cut -d= -f2)
git fetch --depth 1 "$url" "refs/tags/$tag:refs/tags/webmin-$tag"
commit=$(git rev-parse "webmin-$tag^{commit}")
tree=$(git rev-parse "webmin-$tag^{tree}")
git subtree pull --prefix=compat/webmin --squash "$url" "refs/tags/$tag" \
  -m "compat/webmin: sync upstream Webmin $tag ($commit)"
sed -i -e "s/^upstream_tag=.*/upstream_tag=$tag/" -e "s/^upstream_commit=.*/upstream_commit=$commit/" \
  -e "s/^upstream_tree=.*/upstream_tree=$tree/" compat/webmin.lock
git tag -d "webmin-$tag" >/dev/null
echo "Synced to $tag ($commit). Now: update compat/webmin.lock commit, re-apply/verify compat/PATCHES.md, run make check."
