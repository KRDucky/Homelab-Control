#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Pre-push mode of the Jenkinsfile (ci/README.md "Pre-push testing"). Runs on the agent in a full
# clone of GitHub's main and turns it into the commit to test: the unpushed head commit, fetched
# from a git bundle that n8n holds, merged with main as GitHub would merge a pull request.
#
#   HC_PREPUSH_SHA=<40 hex> HC_PREPUSH_BUNDLE_URL=<url> [HC_PREPUSH_BASE=<main sha>] ci/prepush-checkout.sh
#
# HC_PREPUSH_BASE pins main for every stage of one build; without it, origin/main is used and its
# commit is printed on the last line ("base <sha>") for the Jenkinsfile to pin. The merge commit has a
# fixed author, committer and date, so every stage of a build tests the same commit.
set -euo pipefail

sha=${HC_PREPUSH_SHA:-}
url=${HC_PREPUSH_BUNDLE_URL:-}
[[ $sha =~ ^[0-9a-f]{40}$ ]] || { echo "prepush-checkout: HC_PREPUSH_SHA is not a full commit id" >&2; exit 2; }
[[ $url =~ ^https?://[A-Za-z0-9.:-]+/[A-Za-z0-9._~/?=\&%-]+$ ]] || { echo "prepush-checkout: bad HC_PREPUSH_BUNDLE_URL" >&2; exit 2; }

base=${HC_PREPUSH_BASE:-$(git rev-parse --verify "origin/main^{commit}")}
[[ $base =~ ^[0-9a-f]{40}$ ]] || { echo "prepush-checkout: bad base $base" >&2; exit 2; }
git cat-file -e "$base^{commit}" || { echo "prepush-checkout: main commit $base is not in the clone" >&2; exit 2; }

bundle=$(git rev-parse --git-dir)/hc-prepush.bundle
rm -f "$bundle"
# A bundle holds only the commits that are not on main yet; 200 MB is far above any real change.
curl -fsS --retry 2 --max-time 120 --max-filesize 209715200 -o "$bundle" "$url"
git bundle verify -q "$bundle" >/dev/null # its prerequisites (commits on main) must be in this clone
git fetch -q --no-tags "$bundle" "+refs/hc-prepush/head:refs/hc-prepush/head"
rm -f "$bundle"
got=$(git rev-parse refs/hc-prepush/head)
[ "$got" = "$sha" ] || { echo "prepush-checkout: the bundle holds $got, not $sha" >&2; exit 2; }

git checkout -q --force --detach "$sha"
git clean -qffdx
if git merge-base --is-ancestor "$base" "$sha"; then
  echo "prepush-checkout: $sha already contains main $base; testing it as it is" >&2
else
  date=$( { git show -s --format=%ct "$sha"; git show -s --format=%ct "$base"; } | sort -n | tail -n1)
  GIT_AUTHOR_NAME=hc-prepush GIT_AUTHOR_EMAIL=hc-prepush@invalid GIT_AUTHOR_DATE="@$date +0000" \
  GIT_COMMITTER_NAME=hc-prepush GIT_COMMITTER_EMAIL=hc-prepush@invalid GIT_COMMITTER_DATE="@$date +0000" \
    git merge -q --no-ff --no-edit -m "Merge main $base into pre-push head $sha" "$base" \
    || { git merge --abort || true; echo "prepush-checkout: $sha does not merge cleanly with main $base" >&2; exit 1; }
fi
echo "prepush-checkout: testing $(git rev-parse HEAD) (head $sha, main $base)" >&2
echo "base $base"
