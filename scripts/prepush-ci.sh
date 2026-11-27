#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Pre-push testing on MediaMouse: Jenkins and Graphify on a commit before it reaches GitHub
# (ci/README.md "Pre-push testing").
#
#   scripts/prepush-ci.sh submit [REV]   send REV's commits that are not on main to n8n; Jenkins tests them
#   scripts/prepush-ci.sh status [REV]   the run for REV: queued, running, SUCCESS, FAILURE, UNSTABLE ...
#   scripts/prepush-ci.sh wait [REV]     wait for the run; exit 0 only when it passed
#   scripts/prepush-ci.sh install-hook   make this checkout's pre-push hook call "hook"
#   scripts/prepush-ci.sh hook REMOTE URL   (git pre-push hook) refuse a push to GitHub of commits
#                                        without a passing run; submit those that have none
#
# REV defaults to HEAD. A run passes when Jenkins' pre-push build of that exact commit (merged with
# main) is SUCCESS and Graphify indexed it with every changed source file covered. Configuration, in
# ~/.config/hc-prepush/env (mode 0600):
#   HC_PREPUSH_URL=http://192.168.1.44:5678   n8n
#   HC_PREPUSH_TOKEN=...                      the value of n8n's "HC pre-push client" header credential
# Skip the hook once with HC_PREPUSH_SKIP=1 git push ... (or git push --no-verify).
set -euo pipefail

conf=${HC_PREPUSH_CONFIG:-$HOME/.config/hc-prepush/env}
zero=0000000000000000000000000000000000000000

die() { echo "prepush-ci: $*" >&2; exit 2; }

load_config() {
  [ -r "$conf" ] || return 1
  # shellcheck disable=SC1090 # the user's own configuration file
  . "$conf"
  [ -n "${HC_PREPUSH_URL:-}" ] && [ -n "${HC_PREPUSH_TOKEN:-}" ]
}

# api METHOD PATH [CURL ARGS...]: n8n webhook call; the token goes to curl on stdin, never in argv.
api() {
  local method=$1 path=$2; shift 2
  printf 'header = "X-HC-Prepush-Token: %s"\n' "$HC_PREPUSH_TOKEN" |
    curl -sS --fail-with-body --max-time 120 -K - -X "$method" "$@" "${HC_PREPUSH_URL%/}/webhook/hc-prepush/$path"
}

field() { # field NAME < json
  python3 -Ic 'import json,sys; v=json.load(sys.stdin).get(sys.argv[1]); print("" if v is None else (json.dumps(v) if isinstance(v,(dict,list)) else v))' "$1"
}

resolve() { git rev-parse --verify "${1:-HEAD}^{commit}"; }

submit() { # submit SHA BRANCH
  local sha=$1 branch=$2 gitdir bundle nonce
  git fetch -q origin main
  if git merge-base --is-ancestor "$sha" origin/main; then
    echo "prepush-ci: $sha is already on main; nothing to test" >&2
    return 0
  fi
  gitdir=$(git rev-parse --git-dir)
  bundle="$gitdir/hc-prepush-$sha.bundle"
  git update-ref refs/hc-prepush/head "$sha"
  git bundle create -q "$bundle" refs/hc-prepush/head --not origin/main
  git update-ref -d refs/hc-prepush/head
  nonce=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
  local out
  out=$(api POST "submit?sha=$sha&branch=$(printf %s "$branch" | python3 -Ic 'import sys,urllib.parse; print(urllib.parse.quote(sys.stdin.read(), safe=""))')&nonce=$nonce" \
    -H 'Content-Type: application/octet-stream' --data-binary "@$bundle") || { rm -f "$bundle"; echo "$out" >&2; die "n8n refused the submission"; }
  rm -f "$bundle"
  echo "prepush-ci: submitted $sha ($branch): $(printf %s "$out" | field queue)" >&2
}

status_json() { api GET "status?sha=$1"; }

# report < status JSON: prints one line; exit 0 pass, 1 fail, 3 pending, 4 unknown.
read -r -d '' REPORT_PY <<'PY' || true
import json, sys
s = json.load(sys.stdin)
state, build, result = s.get("state"), s.get("build") or {}, s.get("result") or {}
graphify = result.get("graphify") or {}
line = "%s %s" % (s.get("sha", "")[:12], state)
if build:
    line += " (build %s: %s)" % (build.get("number"), build.get("url"))
if result:
    line += "; failed stages: %s; Graphify: %s" % (", ".join(result.get("failedStages") or []) or "none", graphify.get("status"))
print(line)
if s.get("pass"):
    sys.exit(0)
if state in ("queued", "running"):
    sys.exit(3)
if state == "unknown":
    sys.exit(4)
if s.get("impactUrl") and graphify.get("status") not in ("complete", "not-applicable"):
    print("  Graphify report: " + s["impactUrl"])
sys.exit(1)
PY
report() { python3 -Ic "$REPORT_PY"; }

hook() { # hook REMOTE URL, refs on stdin as git gives them
  local url=${2:-} lref lsha rref rsha blocked=0 code
  case $url in *github.com[:/]KRDucky/Homelab-Control*) ;; *) exit 0 ;; esac
  [ "${HC_PREPUSH_SKIP:-}" = 1 ] && { echo "prepush-ci: HC_PREPUSH_SKIP=1; not checking" >&2; exit 0; }
  load_config || die "no $conf; see ci/README.md \"Pre-push testing\" (or HC_PREPUSH_SKIP=1 to push without it)"
  git fetch -q origin main
  while read -r lref lsha rref rsha; do
    [ "$lsha" = "$zero" ] && continue # deleting a branch
    git merge-base --is-ancestor "$lsha" origin/main && continue # nothing new to test
    code=0
    status_json "$lsha" | report || code=$?
    case $code in
      0) ;;
      3) echo "prepush-ci: $rref: the run for $lsha has not finished; scripts/prepush-ci.sh wait $lsha" >&2; blocked=1 ;;
      4) submit "$lsha" "${lref#refs/heads/}"
         echo "prepush-ci: $rref: started a run for $lsha; push again when scripts/prepush-ci.sh wait $lsha passes" >&2
         blocked=1 ;;
      *) echo "prepush-ci: $rref: $lsha did not pass; fix it and commit, or resubmit with scripts/prepush-ci.sh submit $lsha" >&2
         blocked=1 ;;
    esac
    : "$rsha"
  done
  exit "$blocked"
}

cmd=${1:-}; shift || true
case $cmd in
  submit)
    load_config || die "no $conf; see ci/README.md \"Pre-push testing\""
    sha=$(resolve "${1:-}")
    submit "$sha" "$(git symbolic-ref -q --short HEAD || echo detached)" ;;
  status)
    load_config || die "no $conf"
    status_json "$(resolve "${1:-}")" | report ;;
  wait)
    load_config || die "no $conf"
    sha=$(resolve "${1:-}")
    while :; do
      code=0; line=$(status_json "$sha" | report) || code=$?
      [ "$code" = 3 ] || { echo "$line"; exit "$code"; }
      sleep 30
    done ;;
  install-hook)
    hookfile="$(git rev-parse --git-path hooks)/pre-push"
    [ -e "$hookfile" ] && ! grep -q 'Installed by scripts/prepush-ci.sh install-hook' "$hookfile" &&
      die "$hookfile exists; add: exec scripts/prepush-ci.sh hook \"\$@\""
    # The hook is shared by every worktree. A branch made before pre-push testing has no script; its
    # pushes go through unchecked, with a note (GitHub Actions still judges the pull request).
    # shellcheck disable=SC2016 # expanded when the hook runs
    printf '%s\n' '#!/bin/sh' \
      '# Installed by scripts/prepush-ci.sh install-hook (ci/README.md).' \
      's="$(git rev-parse --show-toplevel)/scripts/prepush-ci.sh"' \
      'if [ ! -x "$s" ]; then echo "prepush-ci: this branch has no scripts/prepush-ci.sh; not checking" >&2; exit 0; fi' \
      'exec "$s" hook "$@"' > "$hookfile"
    chmod +x "$hookfile"
    echo "prepush-ci: installed $hookfile" >&2 ;;
  hook) hook "$@" ;;
  *) sed -n '8,23p' "$0" >&2; exit 2 ;;
esac
