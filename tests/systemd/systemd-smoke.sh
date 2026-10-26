#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Package + systemd smoke test. Run as root INSIDE a disposable systemd host
# (systemd as PID 1): a throwaway VM, a CI runner, or a systemd container.
# Installs the given package, starts the hardened units, checks behaviour,
# records `systemd-analyze security`, then removes and purges the package.
# Never run on a production host.
#
# Usage: systemd-smoke.sh <package-file>     (.deb now; .rpm / .pkg.tar.zst via HC_PKG_KIND)
set -euo pipefail
pkg=${1:?usage: $0 <package>}
[ "$(id -u)" -eq 0 ] || { echo "must run as root in a disposable systemd host"; exit 1; }
[ "$(cat /proc/1/comm)" = systemd ] || { echo "PID 1 is not systemd"; exit 1; }
[ -e /etc/hc-disposable ] || { echo "refusing: create /etc/hc-disposable to confirm this host is disposable"; exit 1; }

n_pass=0; n_fail=0
check() { if [ "$2" = "$3" ]; then echo "PASS  $1 ($3)"; n_pass=$((n_pass+1)); else echo "FAIL  $1 (want $2, got $3)"; n_fail=$((n_fail+1)); fi; }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

echo "== environment"; echo "$(grep -m1 ^PRETTY_NAME= /etc/os-release | cut -d\" -f2); $(systemctl --version | head -1); kernel $(uname -r)"
kind=${HC_PKG_KIND:-${pkg##*.}}
echo "== install ($kind)"
case "$kind" in
  deb) dpkg -i "$pkg" ;;
  rpm) rpm -Uvh "$pkg" ;;
  zst) pacman -U --noconfirm "$pkg" ;;
  *) echo "unknown package kind $kind"; exit 2 ;;
esac
check "service not enabled on install" "disabled" "$(systemctl is-enabled homelab-control.service 2>/dev/null || true)"

pw="smoke-$(head -c 9 /dev/urandom | od -An -tx1 | tr -d ' \n')"
printf '%s\n' "$pw" | hlctl user add --file /etc/homelab-control/users.json --username admin \
  --scopes "${HC_SMOKE_SCOPES:-system.read}" >/dev/null
chown homelab-control:homelab-control /etc/homelab-control/users.json
# Loopback + behind-tls-proxy is the packaged default; the smoke test talks to it directly.
systemctl daemon-reload
for u in ${HC_SMOKE_EXTRA_UNITS:-}; do systemctl start "$u"; done
systemctl start homelab-control.service
for _ in $(seq 50); do curl -fsS http://127.0.0.1:9180/api/v1/health >/dev/null 2>&1 && break; sleep 0.2; done
check "service active" active "$(systemctl is-active homelab-control.service)"
check "runs as homelab-control" homelab-control "$(ps -o user= -p "$(systemctl show -p MainPID --value homelab-control.service)")"
check "no capabilities in effective set" 0000000000000000 "$(awk '/^CapEff/{print $2}' /proc/"$(systemctl show -p MainPID --value homelab-control.service)"/status)"
check "health" 200 "$(code http://127.0.0.1:9180/api/v1/health)"
o=https://127.0.0.1:9180
jar=$(mktemp)
curl -s -o /dev/null -c "$jar" -H "Origin: $o" -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$pw\"}" http://127.0.0.1:9180/api/v1/auth/login
# Behind a TLS proxy the cookie is Secure (__Host-); curl over plain HTTP still stores it with -c but
# will not send it, so pass it explicitly.
sid=$(awk '$6=="__Host-hc_session"{print $7}' "$jar")
check "login issues __Host- cookie" 1 "$([ -n "$sid" ] && echo 1 || echo 0)"
ov=$(curl -s -H "Cookie: __Host-hc_session=$sid" http://127.0.0.1:9180/api/v1/host/overview)
check "overview under hardening returns hostname" 1 "$(grep -q '"hostname":"' <<<"$ov" && echo 1 || echo 0)"
check "overview: filesystems readable (ProtectHome/PrivateDevices)" 1 "$(grep -q '"mount_point":"/"' <<<"$ov" && echo 1 || echo 0)"
check "overview: network section ok" 1 "$(grep -q '"network":{"status":"ok"' <<<"$ov" && echo 1 || echo 0)"
# Space-separated list of check scripts, sourced in order (they see $sid and $pw).
for extra in ${HC_SMOKE_EXTRA_CHECKS:-}; do
  # shellcheck disable=SC1090
  HC_SID=$sid source "$extra"
done
echo "== journal (last lines)"; journalctl -u homelab-control.service --no-pager -n 5 -o cat || true
echo "== systemd-analyze security"; systemd-analyze security --no-pager homelab-control.service 2>/dev/null | tail -1 || true
for u in ${HC_SMOKE_EXTRA_UNITS:-}; do systemd-analyze security --no-pager "$u" 2>/dev/null | tail -1 || true; done
rm -f "$jar"

echo "== remove + purge"
# shellcheck disable=SC2086  # unit list is intentionally word-split
systemctl stop homelab-control.service ${HC_SMOKE_EXTRA_UNITS:-}
case "$kind" in
  deb) dpkg -P "$(dpkg-deb -f "$pkg" Package)" ;;
  rpm) rpm -e homelab-control ;;
  zst) pacman -Rns --noconfirm homelab-control ;;
esac
check "binaries removed" 0 "$([ -e /usr/bin/hc-api ] && echo 1 || echo 0)"
check "unit removed" 0 "$([ -e /usr/lib/systemd/system/homelab-control.service ] && echo 1 || echo 0)"
echo "systemd-smoke: $n_pass passed, $n_fail failed"
[ "$n_fail" -eq 0 ]
