#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Regression tests for the disposable-host smoke harness repeatability fixes:
#   1. smoke-user.sh is idempotent (a repeated run resets an existing account
#      instead of failing on a duplicate, and never deletes the database or
#      another account).
# No root, no systemd, no network; stub `hlctl` on PATH only.

set -u
here=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
n_pass=0
n_fail=0
check() {
  if [ "$2" = "$3" ]; then
    echo "PASS  $1"
    n_pass=$((n_pass + 1))
  else
    echo "FAIL  $1 (want [$2], got [$3])"
    n_fail=$((n_fail + 1))
  fi
}
exists() { [ -e "$1" ] || [ -L "$1" ]; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# --- Stubs ------------------------------------------------------------------
mkdir -p "$tmp/bin"

# hlctl: one username per line in the --file; logs subcommands when asked.
cat >"$tmp/bin/hlctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = user ] && shift   # hlctl is invoked as `hlctl user <sub> ...`
sub=$1; shift
file=""; user=""
while [ $# -gt 0 ]; do
  case "$1" in
    --file) file=$2; shift 2 ;;
    --username) user=$2; shift 2 ;;
    --scopes) shift 2 ;;
    *) shift ;;
  esac
done
if [ -n "${HLCTL_STUB_LOG:-}" ]; then printf '%s %s\n' "$sub" "$user" >>"$HLCTL_STUB_LOG"; fi
case "$sub" in
  list) [ -f "$file" ] && cat "$file" || true ;;
  add)
    if [ -f "$file" ] && grep -qx "$user" "$file"; then
      echo "hlctl: user \"$user\" already exists" >&2; exit 1
    fi
    read -r _pw
    echo "$user" >>"$file" ;;
  passwd)
    if ! [ -f "$file" ] || ! grep -qx "$user" "$file"; then
      echo "hlctl: no such user \"$user\"" >&2; exit 1
    fi
    read -r _pw ;;
  scopes)
    if ! [ -f "$file" ] || ! grep -qx "$user" "$file"; then
      echo "hlctl: no such user \"$user\"" >&2; exit 1
    fi ;;
esac
EOF
chmod +x "$tmp/bin/hlctl"

PATH="$tmp/bin:$PATH"

# --- 1. smoke-user.sh idempotency ------------------------------------------
users="$tmp/users.json"
log="$tmp/calls.log"
run_smoke_user() {
  PATH="$tmp/bin:$PATH" HLCTL_STUB_LOG="$log" \
    HC_SMOKE_USERS_FILE="$users" HC_SMOKE_USER=admin \
    HC_SMOKE_SCOPES=system.read HC_SMOKE_PW="pw-12345678" \
    "$here/smoke-user.sh"
}

printf 'keepme\nadministrator\n' >"$users"

rc=0; run_smoke_user || rc=$?
check "first run exits 0" 0 "$rc"
check "first run calls add" 1 "$(grep -c '^add admin$' "$log" || true)"
check "admin created exactly once" 1 "$(grep -cx admin "$users" || true)"
check "pre-existing account keepme preserved" 1 "$(grep -cx keepme "$users" || true)"
check "pre-existing account administrator preserved" 1 "$(grep -cx administrator "$users" || true)"

: >"$log"
rc=0; run_smoke_user || rc=$?
check "repeated run exits 0" 0 "$rc"
check "repeated run does not call add again" 0 "$(grep -c '^add admin$' "$log" || true)"
check "repeated run resets password (passwd)" 1 "$(grep -c '^passwd admin$' "$log" || true)"
check "repeated run resets scopes (scopes)" 1 "$(grep -c '^scopes admin$' "$log" || true)"
check "admin still unique after repeated run" 1 "$(grep -cx admin "$users" || true)"
check "keepme untouched after repeated run" 1 "$(grep -cx keepme "$users" || true)"

echo "test_smoke_repeatability: $n_pass passed, $n_fail failed"
[ "$n_fail" -eq 0 ]
