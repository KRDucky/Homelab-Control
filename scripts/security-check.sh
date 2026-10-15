#!/usr/bin/env bash
# Basic security gate run by `make security` and CI. Fails on any finding.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
fail=0
step() { printf '\n== %s\n' "$*"; }

step "Upstream provenance"
scripts/verify-upstream.sh || fail=1

step "Go static guards (no process execution, all API routes wrapped)"
(cd backend && go test -count=1 ./internal/guard/) || fail=1

step "go vet"
(cd backend && go vet ./...) || fail=1

step "Go dependency policy (M0: standard library only)"
if grep -qE '^require' backend/go.mod; then
  echo "backend/go.mod declares third-party modules; review ADR 0006 before adding dependencies"; fail=1
else echo "OK: no third-party Go modules"; fi

step "govulncheck (Go stdlib/toolchain advisories)"
if command -v govulncheck >/dev/null 2>&1; then (cd backend && govulncheck ./...) || fail=1
else echo "SKIPPED: govulncheck not installed (CI installs it)"; fi

step "npm audit (production dependencies)"
(cd frontend/app && npm audit --omit=dev --audit-level=high) || fail=1

step "Frontend XSS lint rules"
(cd frontend/app && npx eslint .) || fail=1

step "Committed secrets / private keys"
if git grep -nE -- '-----BEGIN ([A-Z]+ )?PRIVATE KEY-----' -- ':!compat/webmin' ':!**/testdata/**'; then
  echo "private key material committed"; fail=1
else echo "OK: no private keys outside compat/webmin"; fi

step "Shell scripts"
if command -v shellcheck >/dev/null 2>&1; then shellcheck scripts/*.sh packaging/debian/*.sh tests/integration/*.sh || fail=1
else echo "SKIPPED: shellcheck not installed (CI installs it)"; fi

exit $fail
