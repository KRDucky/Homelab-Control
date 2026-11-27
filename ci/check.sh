#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# The GitHub Actions "check" job's steps, in its order, for the Jenkins "check" stage. It runs in
# the toolchain container (ci/run.sh) and needs nothing else. Keep it in step with ci.yml.
# Reports go to reports/: JUnit for the frontend, a Go coverage profile.
set -euo pipefail
step() { printf '\n===== %s\n' "$*"; "$@"; }
mkdir -p reports
step python3 -I scripts/release-tool.py check-toolchain packaging/release/toolchain.env
step make lint
step make build
step make test-go GO_TEST_FLAGS="-coverprofile=$PWD/reports/go-coverage.out"
step make test-rust
step make test-retention-xcheck
step make fuzz
step make test-frontend VITEST_FLAGS="--reporter=default --reporter=junit --outputFile.junit=$PWD/reports/vitest-junit.xml"
step make test-release
step make security
step make repro-check
step make deb
# shellcheck disable=SC2046 # one argument per package
step scripts/deb-compliance.sh $(ls build/*.deb)
