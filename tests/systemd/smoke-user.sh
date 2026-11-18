#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Idempotent smoke-test account setup. On the first run the account is created;
# on a later run on the same disposable host (whose package uninstall preserves
# /etc/homelab-control/users.json) the account already exists, so its password
# and scopes are reset instead of failing on a duplicate. The database and any
# other account are never deleted. Test-only; run inside a disposable host that
# systemd-smoke.sh has already gated with /etc/hc-disposable.
#
# Env:
#   HC_SMOKE_USERS_FILE   users database path (default /etc/homelab-control/users.json)
#   HC_SMOKE_USER         account name (default admin)
#   HC_SMOKE_SCOPES       comma-separated scopes (default system.read)
#   HC_SMOKE_PW           password to set (required)
#   HC_SMOKE_CHOWN_TO     if set, chown the database to this user:group afterwards
# shellcheck disable=SC2154
set -euo pipefail

file=${HC_SMOKE_USERS_FILE:-/etc/homelab-control/users.json}
user=${HC_SMOKE_USER:-admin}
scopes=${HC_SMOKE_SCOPES:-system.read}
pw=${HC_SMOKE_PW:?HC_SMOKE_PW is required}

# "list" prints "username<TAB>state<TAB>scopes" per line; cut keeps the exact
# first column so a partial name match (e.g. "administrator") cannot match.
if [ -f "$file" ] && hlctl user list --file "$file" 2>/dev/null | cut -f1 | grep -qx "$user"; then
  printf '%s\n' "$pw" | hlctl user passwd --file "$file" --username "$user" >/dev/null
  hlctl user scopes --file "$file" --username "$user" --scopes "$scopes" >/dev/null
else
  printf '%s\n' "$pw" | hlctl user add --file "$file" --username "$user" --scopes "$scopes" >/dev/null
fi
if [ -n "${HC_SMOKE_CHOWN_TO:-}" ]; then
  chown "$HC_SMOKE_CHOWN_TO" "$file"
fi
