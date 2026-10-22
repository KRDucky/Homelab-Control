#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Upgrade test (M2 PR-1a): install the published v0.1.0-dev.1 .deb, use it,
# then upgrade to the build under test and check the state database,
# migration, login, scopes and that the old audit log is kept.
# Needs root and systemd as PID 1. Disposable hosts only (/etc/hc-disposable).
#
# Usage: tests/packaging/upgrade-test.sh <old v0.1.0-dev.1 .deb> <new .deb>
set -euo pipefail
old=${1:?old package}; new=${2:?new package}
[ -e /etc/hc-disposable ] || { echo "refusing: not a disposable host (/etc/hc-disposable missing)"; exit 2; }
[ -d /run/systemd/system ] || { echo "systemd is not PID 1"; exit 2; }
# Provenance of the published package (SHA256SUMS of the v0.1.0-dev.1 release).
OLD_SHA256=61630d4b03cc492b8aa81272d675d5697c309361b1a9afb162031b891d0e5978
pass=0; fail=0
check() {
  if [ "$2" = "$3" ]; then echo "PASS  $1 ($3)"; pass=$((pass + 1))
  else echo "FAIL  $1: expected '$2', got '$3'"; fail=$((fail + 1)); fi
}
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
b=http://127.0.0.1:9180
login() { # jar password -> http code
  code -c "$1" -H "Origin: $b" -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$2\"}" $b/api/v1/auth/login
}
wait_api() { for _ in $(seq 50); do curl -fsS $b/api/v1/health >/dev/null 2>&1 && return 0; sleep 0.2; done; return 1; }

check "published package checksum" "$OLD_SHA256" "$(sha256sum "$old" | cut -d' ' -f1)"

echo "== install v0.1.0-dev.1"
apt-get install -y "$old" >/tmp/upg-old.log 2>&1 || dpkg -i "$old" >>/tmp/upg-old.log 2>&1
check "old version installed" 0.1.0-dev.1 "$(hc-api -version)"
pw="upg-$(head -c 9 /dev/urandom | od -An -tx1 | tr -d ' \n')"
printf '%s\n' "$pw" | hlctl user add --file /etc/homelab-control/users.json --username admin >/dev/null
chown homelab-control:homelab-control /etc/homelab-control/users.json
sed -i "s|^HC_API_ARGS=.*|HC_API_ARGS=-listen 127.0.0.1:9180 -dev-insecure-http|" /etc/homelab-control/hc-api.env
systemctl daemon-reload
systemctl enable --now homelab-control-broker.socket homelab-control.service >/dev/null 2>&1
wait_api
j1=$(mktemp)
check "old: login" 200 "$(login "$j1" "$pw")"
check "old: overview" 200 "$(code -b "$j1" $b/api/v1/host/overview)"
check "old: audit.jsonl written" 1 "$([ -s /var/log/homelab-control/audit.jsonl ] && echo 1 || echo 0)"
old_audit_sum=$(sha256sum /var/log/homelab-control/audit.jsonl | cut -d' ' -f1)
users_before=$(sha256sum /etc/homelab-control/users.json | cut -d' ' -f1)
check "old: no state directory yet" 0 "$([ -e /var/lib/homelab-control ] && echo 1 || echo 0)"

echo "== upgrade to $(basename "$new")"
apt-get install -y "$new" >/tmp/upg-new.log 2>&1 || dpkg -i "$new" >>/tmp/upg-new.log 2>&1
systemctl daemon-reload
systemctl restart homelab-control-broker.socket homelab-control.service
wait_api
check "service active after upgrade" active "$(systemctl is-active homelab-control.service)"
check "state directory 0700 homelab-control" "700 homelab-control" "$(stat -c '%a %U' /var/lib/homelab-control)"
check "state database 0600 homelab-control" "600 homelab-control" "$(stat -c '%a %U' /var/lib/homelab-control/state.db)"
check "users file unchanged by the upgrade" "$users_before" "$(sha256sum /etc/homelab-control/users.json | cut -d' ' -f1)"
check "previous API audit log kept unchanged" "$old_audit_sum" "$(sha256sum /var/log/homelab-control/audit.jsonl | cut -d' ' -f1)"
check "previous API audit log still verifies (legacy records)" 0 "$(hlctl audit verify --file /var/log/homelab-control/audit.jsonl >/dev/null 2>&1; echo $?)"
check "old in-memory session ended (expected: sessions were not persistent before)" 401 "$(code -b "$j1" $b/api/v1/auth/session)"
j2=$(mktemp)
check "new: login with the existing account" 200 "$(login "$j2" "$pw")"
caps=$(curl -s -b "$j2" $b/api/v1/capabilities)
check "D-7: service logs not granted by the upgrade" forbidden "$(python3 -c 'import json,sys; print([c["status"] for c in json.load(sys.stdin)["capabilities"] if c["id"]=="service_logs"][0])' <<<"$caps")"
check "D-7: audit not granted by the upgrade" forbidden "$(python3 -c 'import json,sys; print([c["status"] for c in json.load(sys.stdin)["capabilities"] if c["id"]=="audit"][0])' <<<"$caps")"
check "D-7: logs endpoint 403" 403 "$(code -b "$j2" $b/api/v1/services/units/ssh.service/logs)"
check "sessions are now persistent" 1 "$(curl -s -b "$j2" $b/api/v1/auth/sessions | grep -c '"persistent":true' || true)"
systemctl restart homelab-control.service
wait_api
check "new session survives a restart" 200 "$(code -b "$j2" $b/api/v1/auth/session)"
check "audit trail now in the state database (journal checkpoint)" 1 "$(journalctl -u homelab-control.service -o cat --no-pager | grep -c 'audit checkpoint log=/var/lib/homelab-control/state.db' | awk '{print ($1>0)}')"
check "API audit chain in the state database verifies" 0 "$(hlctl audit verify --db /var/lib/homelab-control/state.db >/dev/null 2>&1; echo $?)"
# Explicit grant (D-7) and the new pages' APIs.
hlctl user scopes --file /etc/homelab-control/users.json --username admin \
  --scopes system.read,services.read,hardware.read,services.logs,audit.read >/dev/null
systemctl restart homelab-control.service
wait_api
j3=$(mktemp)
check "login after granting scopes" 200 "$(login "$j3" "$pw")"
check "audit endpoint with audit.read" 200 "$(code -b "$j3" "$b/api/v1/audit?limit=5")"
check "logs endpoint with services.logs" 200 "$(code -b "$j3" "$b/api/v1/services/units/homelab-control.service/logs?lines=5")"

systemctl disable --now homelab-control.service homelab-control-broker.socket homelab-control-broker.service >/dev/null 2>&1 || true
echo "upgrade-test: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
