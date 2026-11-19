#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Regression tests for the disposable-host smoke harness repeatability fixes:
#   1. smoke-user.sh is idempotent (a repeated run resets an existing account
#      instead of failing on a duplicate, and never deletes the database or
#      another account).
#   2. webmin-fixture.sh fails closed when any Webmin state pre-exists, and its
#      cleanup removes only what the current run's flags say it created.
#   3. m1-checks.sh wires the helper in (sources it, registers the EXIT trap,
#      calls cleanup) so cleanup also runs on error exits.
# No root, no systemd, no network; stub `hlctl` and `systemctl` on PATH only.

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

# systemctl: records each invocation's args, always succeeds.
cat >"$tmp/bin/systemctl" <<'EOF'
#!/usr/bin/env bash
if [ -n "${SYSCTL_LOG:-}" ]; then printf '%s\n' "$*" >>"$SYSCTL_LOG"; fi
exit 0
EOF
chmod +x "$tmp/bin/systemctl"

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

# --- 2. webmin-fixture.sh behavior -----------------------------------------
cfg="$tmp/wm/config"; var="$tmp/wm/var"
lib="$tmp/wm/unit-lib.service"; deb="$tmp/wm/unit-deb.service"; etc="$tmp/wm/unit-etc.service"
plink="$tmp/wm/perl-link"; ptarget="$tmp/wm/perl-target"
mkdir -p "$tmp/wm"

export HC_WEBMIN_CONFIG_DIR="$cfg" HC_WEBMIN_VAR_DIR="$var" \
  HC_WEBMIN_UNIT_LIB="$lib" HC_WEBMIN_UNIT_DEB="$deb" HC_WEBMIN_UNIT_ETC="$etc" \
  HC_WEBMIN_PERL_LINK="$plink" HC_WEBMIN_PERL_TARGET="$ptarget"
# shellcheck source=tests/systemd/webmin-fixture.sh
source "$here/webmin-fixture.sh"

# Fail-closed guard: true (0) when any state pre-exists, false (1) when none.
hc_webmin_pre_existing; rc=$?
check "guard: empty -> not pre-existing" 1 "$rc"

mkdir -p "$cfg"; hc_webmin_pre_existing; rc=$?
check "guard: config dir -> pre-existing" 0 "$rc"
rmdir "$cfg"

mkdir -p "$var"; hc_webmin_pre_existing; rc=$?
check "guard: var dir -> pre-existing" 0 "$rc"
rmdir "$var"

: >"$lib"; hc_webmin_pre_existing; rc=$?
check "guard: unit file (usr/lib) -> pre-existing" 0 "$rc"
rm -f "$lib"

: >"$deb"; hc_webmin_pre_existing; rc=$?
check "guard: unit file (lib) -> pre-existing" 0 "$rc"
rm -f "$deb"

: >"$etc"; hc_webmin_pre_existing; rc=$?
check "guard: unit file (etc) -> pre-existing" 0 "$rc"
rm -f "$etc"

# Cleanup only removes what the flags say this run created.
syslog="$tmp/wm/sysctl.log"; : >"$syslog"; export SYSCTL_LOG="$syslog"

mkdir -p "$cfg" "$var"; : >"$lib"
hc_webmin_installed=0; hc_webmin_started=0; hc_perl_symlink_created=0
hc_webmin_cleanup
check "cleanup(installed=0): config kept" 1 "$(exists "$cfg" && echo 1 || echo 0)"
check "cleanup(installed=0): var kept" 1 "$(exists "$var" && echo 1 || echo 0)"
check "cleanup(installed=0): unit kept" 1 "$(exists "$lib" && echo 1 || echo 0)"
check "cleanup(installed=0): no systemctl calls" 0 "$(wc -l <"$syslog")"

: >"$syslog"; : >"$deb"; : >"$etc"
hc_webmin_installed=1
hc_webmin_cleanup
check "cleanup(installed=1): config removed" 0 "$(exists "$cfg" && echo 1 || echo 0)"
check "cleanup(installed=1): var removed" 0 "$(exists "$var" && echo 1 || echo 0)"
check "cleanup(installed=1): unit removed" 0 "$(exists "$lib" && echo 1 || echo 0)"
check "cleanup(installed=1): extra unit removed" 0 "$(exists "$etc" && echo 1 || echo 0)"
check "cleanup(installed=1): disables webmin.service" 1 "$(grep -c 'disable webmin.service' "$syslog" || true)"
check "cleanup(installed=1): daemon-reloads" 1 "$(grep -c 'daemon-reload' "$syslog" || true)"
check "cleanup(installed=1): clears installed flag" 0 "${hc_webmin_installed}"

# Perl symlink: only removed when this run created it.
ln -s "$ptarget" "$plink"
hc_perl_symlink_created=0
hc_webmin_cleanup
check "cleanup: pre-existing perl link kept" 1 "$(exists "$plink" && echo 1 || echo 0)"
hc_perl_symlink_created=1
hc_webmin_cleanup
check "cleanup: owned perl link removed" 0 "$(exists "$plink" && echo 1 || echo 0)"
check "cleanup: clears perl flag" 0 "${hc_perl_symlink_created}"

# started=1 -> cleanup runs the fixture stop script and clears the flag.
mkdir -p "$cfg"
cat >"$cfg/stop" <<'EOF'
#!/bin/sh
printf 'stopped\n' >>"${WM_STOP_LOG:-/dev/null}"
EOF
chmod +x "$cfg/stop"
stoplog="$tmp/wm/stop.log"; export WM_STOP_LOG="$stoplog"
hc_webmin_installed=0; hc_webmin_started=1; hc_perl_symlink_created=0
hc_webmin_cleanup
check "cleanup(started=1): stop script ran" 1 "$(grep -c stopped "$stoplog" 2>/dev/null || true)"
check "cleanup(started=1): clears started flag" 0 "${hc_webmin_started}"

# Idempotency: a second call after a full cleanup is a no-op.
hc_webmin_cleanup
check "cleanup: second call is a no-op (no flags)" 0 "$((hc_webmin_installed + hc_webmin_started + hc_perl_symlink_created))"

# --- 3. m1-checks.sh integration -------------------------------------------
m1="$here/m1-checks.sh"
check "m1-checks.sh sources the fixture helper" 1 "$(grep -c 'source .*webmin-fixture.sh' "$m1")"
check "m1-checks.sh registers cleanup on EXIT" 1 "$(grep -c 'trap hc_webmin_cleanup EXIT' "$m1")"
check "m1-checks.sh calls cleanup explicitly" 1 "$(grep -c '^hc_webmin_cleanup$' "$m1")"
check "m1-checks.sh fails closed on pre-existing state" 1 "$(grep -c 'hc_webmin_pre_existing' "$m1")"

echo "test_smoke_repeatability: $n_pass passed, $n_fail failed"
[ "$n_fail" -eq 0 ]
