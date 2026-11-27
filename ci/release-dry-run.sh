#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# The release dry run of .github/workflows/release-build.yml (mode dry-run) for Jenkins: two
# independent release-bundle.sh runs, each in a fresh toolchain container, then the same
# verification steps as its "verify" job. Publishes nothing. Keep it in step with release-build.yml.
# Usage: ci/release-dry-run.sh   (on the agent; writes dist/release-a, dist/release-b, dist/candidate)
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
commit=$(git rev-parse HEAD)
ver="0.0.0-dryrun.${commit::8}"
ci/run.sh -- python3 -I scripts/release-tool.py version "$ver" --mode dry-run >/dev/null
rm -rf dist
for run in a b; do
  echo "===== release run $run ($ver)"
  ci/run.sh --out "dist/release-$run" -- sh -ec "
    python3 -I scripts/release-tool.py check-toolchain packaging/release/toolchain.env
    scripts/release-bundle.sh '$ver' 'dist/release-$run'"
done
echo "===== verify"
ci/run.sh --out dist/candidate --out dist/notes --env BUILD_URL -- bash -euo pipefail -c "
  echo '== release-run'; cat dist/release-a/SHA256SUMS
  diff -u dist/release-a/SHA256SUMS dist/release-b/SHA256SUMS
  echo \"all \$(wc -l < dist/release-a/SHA256SUMS) release files identical across two independent runs\"
  python3 -I scripts/release-tool.py assemble --run dist/release-a --out dist/candidate --version '$ver'
  python3 -I scripts/release-tool.py verify --dir dist/candidate --version '$ver' --commit '$commit' \
    --toolchain packaging/release/toolchain.env --mode dry-run --require-tools
  tests/release/negative-tests.sh dist/candidate '$ver' '$commit' --require-tools
  tests/release/version-order-test.sh --require deb
  mkdir -p dist/notes
  python3 -I scripts/release-tool.py notes --dir dist/candidate --version '$ver' --commit '$commit' \
    --repo https://github.com/KRDucky/Homelab-Control --run-url \"\${BUILD_URL:-local}\" --ci-run-url '' \
    --template packaging/release/RELEASE-NOTES.template.md --out dist/notes/RELEASE-NOTES.md
  head -n 3 dist/notes/RELEASE-NOTES.md"
