#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Basic security gate run by `make security` and CI. Fails on any finding.
# CI mode (GitHub Actions, or HC_CI=1 as the Jenkinsfile sets it): every scanner must be installed
# and the RustSec database is fetched, so no check is skipped. Locally a missing tool is skipped.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
fail=0
cur=""
step() { cur="$*"; printf '\n== %s\n' "$*"; }
ci_mode() { [ -n "${GITHUB_ACTIONS:-}${HC_CI:-}" ]; }
# skipped TOOL: a skip locally, a failure in CI mode.
skipped() {
  if ci_mode; then echo "FAIL: $1 must be installed in CI"; bad "$1 not installed"
  else echo "SKIPPED: $1 not installed (CI installs it)"; fi
}
# In GitHub Actions, also emit an annotation naming the failed step: annotations
# are readable through the checks API even where raw job logs are not.
bad() {
  fail=1
  if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::error title=Security gate::failed step: ${cur}${1:+ - $1}"; fi
}

step "Extensions import (pinned subtree, no edits, no package or module manifests)"
scripts/verify-extensions.sh || bad
node scripts/build-extensions.mjs --check || bad

step "Go static guards (no process execution, all API routes wrapped)"
(cd backend && go test -count=1 ./internal/guard/) || bad

step "go vet"
(cd backend && go vet ./...) || bad

step "Go dependency policy (ADR 0006 amendment 1: reviewed allow-list only)"
if python3 -I scripts/check-go-deps.py; then :; else bad; fi

step "govulncheck (Go stdlib/toolchain advisories)"
if command -v govulncheck >/dev/null 2>&1; then
  vout=$(mktemp)
  if ! (cd backend && govulncheck ./...) >"$vout" 2>&1; then
    bad "$(grep -oE 'GO-[0-9]{4}-[0-9]+' "$vout" | sort -u | tr '\n' ' ')"
  fi
  cat "$vout"
  # Annotation = evidence that the scan ran, with the scanner version (job logs are not always readable).
  if [ -n "${GITHUB_ACTIONS:-}" ]; then
    echo "::notice title=govulncheck::$(govulncheck -version 2>&1 | tr '\n' ' ') | $(grep -m1 -E 'No vulnerabilities found|affected by' "$vout" || echo 'result: see log')"
  fi
  rm -f "$vout"
else skipped govulncheck; fi

step "npm audit (production dependencies)"
(cd frontend/app && npm audit --omit=dev --audit-level=high) || bad

step "Frontend XSS lint rules"
(cd frontend/app && npx eslint .) || bad

step "Third-party notices match shipped dependencies"
python3 -I scripts/gen-third-party-notices.py --check || bad

step "SBOM matches inputs and is valid SPDX 2.3"
python3 -I scripts/gen-sbom.py --check || bad
if command -v pyspdxtools >/dev/null 2>&1; then pyspdxtools -i docs/compliance/sbom/homelab-control.spdx.json || bad "pyspdxtools validation"
else skipped "pyspdxtools (spdx-tools)"; fi

step "Rust dependency policy (ADR 0006 amendment 4: reviewed crate allow-list only)"
if python3 -I scripts/check-rust-deps.py; then :; else bad; fi

step "RustSec advisories (backend/audit-retention/Cargo.lock)"
if [ -n "${RUSTSEC_DB:-}" ]; then
  python3 -I scripts/check-rust-advisories.py --db "$RUSTSEC_DB" || bad "rust advisories"
elif ci_mode; then
  adb=$(mktemp -d)
  if git clone -q --depth 1 https://github.com/rustsec/advisory-db.git "$adb"; then
    python3 -I scripts/check-rust-advisories.py --db "$adb" || bad "rust advisories"
  else bad "rust advisory database not reachable"; fi
  rm -rf "$adb"
else echo "SKIPPED: set RUSTSEC_DB to a checkout of github.com/rustsec/advisory-db (CI fetches it)"; fi

step "Licensing metadata (REUSE 3.3: SPDX licence + copyright for every file)"
if command -v reuse >/dev/null 2>&1; then
  if reuse lint --quiet; then echo "OK: REUSE compliant"; else reuse lint | tail -30; bad "reuse lint"; fi
else skipped reuse; fi

step "Committed secrets / private keys"
if git grep -nE -- '-----BEGIN ([A-Z]+ )?PRIVATE KEY-----' -- ':!**/testdata/**'; then
  echo "private key material committed"; bad
else echo "OK: no private keys outside test fixtures"; fi

step "Shell scripts"
if command -v shellcheck >/dev/null 2>&1; then shellcheck -x scripts/*.sh packaging/*/*.sh tests/integration/*.sh tests/systemd/*.sh tests/packaging/*.sh tests/e2e/*.sh tests/retention/*.sh dev/disposable/*.sh ci/*.sh || bad
else skipped shellcheck; fi

exit $fail
