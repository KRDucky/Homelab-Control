#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Starts a throwaway hc-api (unprivileged, loopback, dev HTTP) with a fresh
# users file and runs the live HTTP security checks plus the browser e2e test.
# Must run as a non-root user. State lives in a temp dir that is removed on exit.
#
# Env: HC_LEGACY_URL (optional, e.g. http://127.0.0.1:10000/), HC_E2E_ARTIFACTS (screenshot dir)
set -euo pipefail
[ "$(id -u)" -ne 0 ] || { echo "run as an unprivileged user (hc-api refuses root by design)"; exit 1; }
root=$(cd "$(dirname "$0")/../.." && pwd)
bin=$root/build
state=$(mktemp -d)
port=${HC_PORT:-9180}
base=http://127.0.0.1:$port
pw="e2e-$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')"
export HC_E2E_PASSWORD=$pw HC_E2E_URL=$base
cleanup() { [ -n "${pid:-}" ] && kill "$pid" 2>/dev/null; rm -rf "$state"; }
trap cleanup EXIT

printf '%s\n' "$pw" | "$bin/hlctl" user add --file "$state/users.json" --username admin >/dev/null
printf 'noscope-password-123\n' | "$bin/hlctl" user add --file "$state/users.json" --username auditor --scopes "" >/dev/null
"$bin/hc-api" -listen "127.0.0.1:$port" -dev-insecure-http -users-file "$state/users.json" \
  -state-db "$state/state.db" ${HC_LEGACY_URL:+-legacy-url "$HC_LEGACY_URL"} >"$state/server.log" 2>&1 &
pid=$!
for _ in $(seq 50); do curl -fsS "$base/api/v1/health" >/dev/null 2>&1 && break; sleep 0.1; done

n_pass=0; n_fail=0
check() { if [ "$2" = "$3" ]; then echo "PASS  $1 ($3)"; n_pass=$((n_pass+1)); else echo "FAIL  $1 (want $2, got $3)"; n_fail=$((n_fail+1)); fi; }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
jar=$state/jar

check "health is public"                 200 "$(code "$base/api/v1/health")"
check "overview requires login"          401 "$(code "$base/api/v1/host/overview")"
check "login without Origin refused"     403 "$(code -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$pw\"}" "$base/api/v1/auth/login")"
check "login from foreign Origin refused" 403 "$(code -H 'Origin: http://evil.example' -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$pw\"}" "$base/api/v1/auth/login")"
check "form-encoded login refused"       415 "$(code -H "Origin: $base" -d "username=admin&password=$pw" "$base/api/v1/auth/login")"
check "wrong password"                   401 "$(code -H "Origin: $base" -H 'Content-Type: application/json' -d '{"username":"admin","password":"not-the-password"}' "$base/api/v1/auth/login")"
csrf=$(curl -s -c "$jar" -H "Origin: $base" -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$pw\"}" "$base/api/v1/auth/login" | sed -n 's/.*"csrf_token":"\([^"]*\)".*/\1/p')
check "login succeeds"                   1 "$([ -n "$csrf" ] && echo 1 || echo 0)"
check "overview with session"            200 "$(code -b "$jar" "$base/api/v1/host/overview")"
check "logout without CSRF token"        403 "$(code -b "$jar" -X POST -H "Origin: $base" "$base/api/v1/auth/logout")"
check "unknown API route"                404 "$(code -b "$jar" "$base/api/v1/system/exec")"
check "logout with CSRF token"           204 "$(code -b "$jar" -X POST -H "Origin: $base" -H "X-CSRF-Token: $csrf" "$base/api/v1/auth/logout")"
check "session dead after logout"        401 "$(code -b "$jar" "$base/api/v1/host/overview")"
rm -f "$jar"
curl -s -o /dev/null -c "$jar" -H "Origin: $base" -H 'Content-Type: application/json' -d '{"username":"auditor","password":"noscope-password-123"}' "$base/api/v1/auth/login"
check "account without system.read gets 403" 403 "$(code -b "$jar" "$base/api/v1/host/overview")"
acsrf=$(curl -s -b "$jar" "$base/api/v1/auth/session" | sed -n 's/.*"csrf_token":"\([^"]*\)".*/\1/p')
# M2-2 write routes: scope, CSRF and Origin are enforced before anything reaches the broker.
check "service plan without services.operate" 403 "$(code -b "$jar" -H "Origin: $base" -H "X-CSRF-Token: $acsrf" -H 'Content-Type: application/json' -d '{"action":"start"}' "$base/api/v1/services/units/demo.service/plan")"
check "service apply without services.operate" 403 "$(code -b "$jar" -H "Origin: $base" -H "X-CSRF-Token: $acsrf" -H 'Content-Type: application/json' -d '{"action":"stop","token":"x"}' "$base/api/v1/services/units/demo.service/apply")"
check "step-up without CSRF token"       403 "$(code -b "$jar" -H "Origin: $base" -H 'Content-Type: application/json' -d '{"password":"noscope-password-123"}' "$base/api/v1/auth/step-up")"
check "step-up from foreign Origin"      403 "$(code -b "$jar" -H 'Origin: http://evil.example' -H "X-CSRF-Token: $acsrf" -H 'Content-Type: application/json' -d '{"password":"noscope-password-123"}' "$base/api/v1/auth/step-up")"
check "step-up with wrong password"      403 "$(code -b "$jar" -H "Origin: $base" -H "X-CSRF-Token: $acsrf" -H 'Content-Type: application/json' -d '{"password":"wrong"}' "$base/api/v1/auth/step-up")"
check "step-up with the right password"  200 "$(code -b "$jar" -H "Origin: $base" -H "X-CSRF-Token: $acsrf" -H 'Content-Type: application/json' -d '{"password":"noscope-password-123"}' "$base/api/v1/auth/step-up")"
"$bin/hlctl" audit export --db "$state/state.db" >"$state/audit.jsonl"
check "audit trail has no secrets"       0 "$(grep -c -e "$pw" -e "$csrf" "$state/audit.jsonl" || true)"
check "state database mode 0600"         600 "$(stat -c %a "$state/state.db")"
check "audit chain verifies"             0 "$("$bin/hlctl" audit verify --db "$state/state.db" >/dev/null 2>&1; echo $?)"
echo "--- audit trail (redacted by design) ---"; cat "$state/audit.jsonl"
echo "hc-api live checks: $n_pass passed, $n_fail failed"

if python3 -c 'import playwright' 2>/dev/null; then
  python3 "$root/tests/e2e/hc_e2e.py" || n_fail=$((n_fail+1))
  # Theme-engine feasibility (ADR 0010): CSSOM custom properties under the real CSP.
  # HC_CSP_BROWSERS selects engines (CI: chromium,firefox,webkit; default chromium).
  python3 "$root/tests/e2e/theme_cssom_csp.py" || n_fail=$((n_fail+1))
  python3 "$root/tests/e2e/hcis_ui.py" || n_fail=$((n_fail+1))
else
  echo "SKIPPED: browser e2e (python3 playwright not installed)"
fi
[ "$n_fail" -eq 0 ]
