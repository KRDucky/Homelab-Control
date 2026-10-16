#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Basic security gate run by `make security` and CI. Fails on any finding.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
fail=0
cur=""
step() { cur="$*"; printf '\n== %s\n' "$*"; }
# In GitHub Actions, also emit an annotation naming the failed step: annotations
# are readable through the checks API even where raw job logs are not.
bad() {
  fail=1
  if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::error title=Security gate::failed step: ${cur}${1:+ - $1}"; fi
}

step "Upstream provenance"
scripts/verify-upstream.sh || bad

step "Go static guards (no process execution, all API routes wrapped)"
(cd backend && go test -count=1 ./internal/guard/) || bad

step "go vet"
(cd backend && go vet ./...) || bad

step "Go dependency policy (M0: standard library only)"
if grep -qE '^require' backend/go.mod; then
  echo "backend/go.mod declares third-party modules; review ADR 0006 before adding dependencies"; bad
else echo "OK: no third-party Go modules"; fi

step "govulncheck (Go stdlib/toolchain advisories)"
if command -v govulncheck >/dev/null 2>&1; then
  vout=$(mktemp)
  if ! (cd backend && govulncheck ./...) >"$vout" 2>&1; then
    bad "$(grep -oE 'GO-[0-9]{4}-[0-9]+' "$vout" | sort -u | tr '\n' ' ')"
  fi
  cat "$vout"; rm -f "$vout"
else echo "SKIPPED: govulncheck not installed (CI installs it)"; fi

step "npm audit (production dependencies)"
(cd frontend/app && npm audit --omit=dev --audit-level=high) || bad

step "Frontend XSS lint rules"
(cd frontend/app && npx eslint .) || bad

step "Third-party notices match shipped dependencies"
python3 -I scripts/gen-third-party-notices.py --check || bad

step "SBOM matches inputs and is valid SPDX 2.3"
python3 -I scripts/gen-sbom.py --check || bad
if command -v pyspdxtools >/dev/null 2>&1; then pyspdxtools -i docs/compliance/sbom/homelab-control.spdx.json || bad "pyspdxtools validation"
else echo "SKIPPED: pyspdxtools not installed (CI installs spdx-tools)"; fi

step "Webmin reference tree licence inventory (per-file upstream statements reviewed)"
python3 -I scripts/gen-webmin-license-inventory.py --check || bad

step "Licensing metadata (REUSE 3.3: SPDX licence + copyright for every file)"
if command -v reuse >/dev/null 2>&1; then
  if reuse lint --quiet; then echo "OK: REUSE compliant"; else reuse lint | tail -30; bad "reuse lint"; fi
else echo "SKIPPED: reuse not installed (CI installs it)"; fi

step "Committed secrets / private keys"
if git grep -nE -- '-----BEGIN ([A-Z]+ )?PRIVATE KEY-----' -- ':!compat/webmin' ':!**/testdata/**'; then
  echo "private key material committed"; bad
else echo "OK: no private keys outside compat/webmin"; fi

step "Shell scripts"
if command -v shellcheck >/dev/null 2>&1; then shellcheck -x scripts/*.sh packaging/*/*.sh tests/integration/*.sh tests/systemd/*.sh tests/packaging/*.sh tests/e2e/*.sh dev/disposable/*.sh || bad
else echo "SKIPPED: shellcheck not installed (CI installs it)"; fi

exit $fail
