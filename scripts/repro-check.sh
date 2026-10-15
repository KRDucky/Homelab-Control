#!/usr/bin/env bash
# Reproducibility check: build the committed tree twice in separate clean
# worktrees and compare SHA-256 of the produced binaries.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
work=$(mktemp -d)
trap 'git worktree remove --force "$work/a" >/dev/null 2>&1 || true; git worktree remove --force "$work/b" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT
rev=$(git rev-parse HEAD)
for d in a b; do
  git worktree add --detach "$work/$d" "$rev" >/dev/null
  (cd "$work/$d" && make -s build >/dev/null)
done
a=$(cat "$work/a/build/SHA256SUMS"); b=$(cat "$work/b/build/SHA256SUMS")
echo "build A:"; echo "$a"; echo "build B:"; echo "$b"
if [ "$a" = "$b" ]; then echo "REPRODUCIBLE: identical artifacts for $rev"; else echo "NOT REPRODUCIBLE"; exit 1; fi
