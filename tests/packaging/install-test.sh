#!/usr/bin/env bash
# Distro-agnostic package install test for containers WITHOUT systemd as
# PID 1 (CI distro matrix). Installs the package with the native tool, runs the
# broker and API by hand, checks behaviour (including "systemd not running"
# handling), then removes the package. Disposable containers only.
#
# Usage: install-test.sh <package.(deb|rpm|pkg.tar.zst)>
set -euo pipefail
pkg=${1:?package}
[ "$(id -u)" -eq 0 ] || { echo "run as root in a disposable container"; exit 1; }
[ -e /etc/hc-disposable ] || { echo "refusing: touch /etc/hc-disposable to confirm a disposable host"; exit 1; }
n_pass=0; n_fail=0
check() { if [ "$2" = "$3" ]; then echo "PASS  $1 ($3)"; n_pass=$((n_pass+1)); else echo "FAIL  $1 (want $2, got $3)"; n_fail=$((n_fail+1)); fi; }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
echo "== $(grep -m1 ^PRETTY_NAME= /etc/os-release | cut -d'"' -f2)"
case "$pkg" in
  *.deb) dpkg -i "$pkg"; remove() { dpkg -P homelab-control; } ;;
  *.rpm) rpm -Uvh "$pkg"; remove() { rpm -e homelab-control; } ;;
  *.pkg.tar.zst) pacman -U --noconfirm "$pkg"; remove() { pacman -Rns --noconfirm homelab-control; } ;;
  *) echo "unknown package type"; exit 2 ;;
esac
check "service user created" homelab-control "$(getent passwd homelab-control | cut -d: -f1)"
check "config dir mode/group" "750 homelab-control" "$(stat -c '%a %G' /etc/homelab-control)"
check "broker binary installed" 1 "$([ -x /usr/lib/homelab-control/hc-broker ] && echo 1 || echo 0)"
check "units installed" 3 "$(find /usr/lib/systemd/system -name 'homelab-control*' | wc -l)"

pw="ci-$(head -c 9 /dev/urandom | od -An -tx1 | tr -d ' \n')"
printf '%s\n' "$pw" | hlctl user add --file /etc/homelab-control/users.json --username admin >/dev/null
chown homelab-control:homelab-control /etc/homelab-control/users.json
install -d -m 0755 /run/homelab-control
install -d -m 0700 -o homelab-control -g homelab-control /var/log/homelab-control
/usr/lib/homelab-control/hc-broker -audit-log /tmp/broker-audit.jsonl >/tmp/broker.log 2>&1 &
bpid=$!
for _ in $(seq 50); do [ -S /run/homelab-control/broker.sock ] && break; sleep 0.1; done
check "dev socket owner/mode" "660 root:homelab-control" "$(stat -c '%a %U:%G' /run/homelab-control/broker.sock)"
runuser -u homelab-control -- hc-api -listen 127.0.0.1:9180 -dev-insecure-http \
  -users-file /etc/homelab-control/users.json -audit-log /var/log/homelab-control/audit.jsonl >/tmp/api.log 2>&1 &
apid=$!
trap 'kill $apid $bpid 2>/dev/null || true' EXIT
for _ in $(seq 50); do curl -fsS http://127.0.0.1:9180/api/v1/health >/dev/null 2>&1 && break; sleep 0.1; done
b=http://127.0.0.1:9180; jar=$(mktemp)
check "health" 200 "$(code $b/api/v1/health)"
check "login" 200 "$(code -c "$jar" -H "Origin: $b" -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$pw\"}" $b/api/v1/auth/login)"
check "overview" 200 "$(code -b "$jar" $b/api/v1/host/overview)"
caps=$(curl -s -b "$jar" $b/api/v1/capabilities)
check "broker reachable by service user" 1 "$(grep -c '"broker":{"status":"available"' <<<"$caps")"
check "services reported unsupported without systemd" 1 "$(grep -c '"id":"services"[^}]*"status":"unsupported"' <<<"$caps")"
check "hardware endpoint" 200 "$(code -b "$jar" $b/api/v1/hardware)"
check "legacy endpoint via broker" 1 "$(curl -s -b "$jar" $b/api/v1/legacy/webmin | grep -c '"source":"broker"')"
check "broker audit written" 1 "$([ -s /tmp/broker-audit.jsonl ] && echo 1 || echo 0)"
kill $apid $bpid 2>/dev/null || true; wait 2>/dev/null || true
remove
check "package removed" 0 "$([ -e /usr/bin/hc-api ] && echo 1 || echo 0)"
echo "install-test: $n_pass passed, $n_fail failed"
[ "$n_fail" -eq 0 ]
