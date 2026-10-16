#!/usr/bin/env bash
# M1 validation inside a disposable hc-test- VM (guest side). Run as root via
# dev/vm/run-m1-validation.sh, which stages these inputs under /opt/hc:
#   pkg.deb  tests/  webmin/  axe.min.js
# Writes logs, screenshots and metadata to /opt/hc/out. Refuses to run unless
# /etc/hc-disposable exists (created by the harness's cloud-init seed).
set -uo pipefail
H=/opt/hc; OUT=$H/out
[ "$(id -u)" -eq 0 ] || { echo "run as root"; exit 1; }
[ -e /etc/hc-disposable ] || { echo "refusing: /etc/hc-disposable missing (not a harness VM)"; exit 1; }
rm -rf "$OUT"; mkdir -p "$OUT/screenshots"; chmod 755 "$OUT"
export DEBIAN_FRONTEND=noninteractive
# Fresh cloud images ship without package lists (the seed sets package_update: false so boot stays
# fast); without this, installs of anything not already in the image fail.
apt-get update > "$OUT/00-apt-update.log" 2>&1 || { echo "apt-get update failed; see $OUT/00-apt-update.log"; exit 1; }
summary=$OUT/SUMMARY.txt; : > "$summary"
step() { echo; echo "######## $*"; }
record() { printf '%-34s %s\n' "$1" "$2" | tee -a "$summary"; }
n_pass=0; n_fail=0
check() { if [ "$2" = "$3" ]; then echo "PASS  $1 ($3)"; n_pass=$((n_pass+1)); else echo "FAIL  $1 (want $2, got $3)"; n_fail=$((n_fail+1)); fi; }
# Upstream Webmin from the pinned compat/webmin tree, loopback only (legacy page + coexistence checks).
ensure_webmin() {
  if [ ! -e /etc/webmin/start ]; then
    [ -x /usr/local/bin/perl ] || ln -s /usr/bin/perl /usr/local/bin/perl
    ( cd $H/webmin && config_dir=/etc/webmin var_dir=/var/webmin perl=/usr/bin/perl port=10000 \
        login=admin password="webmin-vm-$RANDOM-pw" ssl=0 atboot=0 nostart=1 nouninstall=1 \
        nopostinstall=1 noperlpath=1 autoos=1 autothird=1 ./setup.sh >"$OUT/webmin-setup.log" 2>&1 )
  fi
  grep -q '^bind=127.0.0.1$' /etc/webmin/miniserv.conf || echo bind=127.0.0.1 >> /etc/webmin/miniserv.conf
  /etc/webmin/stop >/dev/null 2>&1 || true; /etc/webmin/start >/dev/null 2>&1 || true
  for _ in $(seq 25); do curl -s -o /dev/null http://127.0.0.1:10000/ && return 0; sleep 0.2; done
}

step "environment metadata"
{
  echo "date_utc=$(date -u +%FT%TZ)"
  grep -E '^(PRETTY_NAME|VERSION_ID|VERSION_CODENAME)=' /etc/os-release
  echo "kernel=$(uname -r)"
  echo "virt=$(systemd-detect-virt 2>/dev/null || true)"
  echo "systemd=$(systemctl --version | head -1)"
  echo "cpus=$(nproc) mem_kib=$(awk '/MemTotal/{print $2}' /proc/meminfo)"
  echo "cgroup_fs=$(stat -fc %T /sys/fs/cgroup)"
  echo "package=$(dpkg-deb -f $H/pkg.deb Package Version | paste -sd' ')"
  echo "package_sha256=$(sha256sum $H/pkg.deb | awk '{print $1}')"
  echo "block_devices:"; lsblk -dno NAME,SIZE,TYPE,MODEL | sed 's/^/  /'
} | tee "$OUT/environment.txt"

step "1. systemd smoke + M1 checks (install, hardening, socket-activated broker, auth, routes, remove+purge)"
HC_SMOKE_EXTRA_UNITS=homelab-control-broker.socket HC_SMOKE_EXTRA_CHECKS=$H/tests/systemd/m1-checks.sh \
  HC_WEBMIN_SRC=$H/webmin HC_SMOKE_SCOPES=system.read,services.read,hardware.read \
  bash $H/tests/systemd/systemd-smoke.sh $H/pkg.deb > "$OUT/01-systemd-smoke.log" 2>&1
rc=$?; record "systemd smoke + M1 checks" "exit=$rc $(grep -E '[0-9]+ passed, [0-9]+ failed' "$OUT/01-systemd-smoke.log" | tail -1)"

step "2. rollback: remove keeps config, reinstall reuses it, purge removes it; Webmin keeps working"
{
  ensure_webmin
  apt-get install -y "$H/pkg.deb" >/dev/null 2>&1
  check "installed" ii "$(dpkg-query -W -f='${db:Status-Abbrev}' homelab-control 2>/dev/null | cut -c1-2)"
  printf '%s\n' "rb-$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')" | hlctl user add --file /etc/homelab-control/users.json --username rollback >/dev/null
  chown homelab-control:homelab-control /etc/homelab-control/users.json
  systemctl enable --now homelab-control-broker.socket homelab-control.service >/dev/null 2>&1
  check "service active before rollback" active "$(systemctl is-active homelab-control.service)"
  check "webmin (upstream) running alongside" 1 "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:10000/ | grep -cE '^(200|302|401)$')"
  apt-get remove -y homelab-control >/dev/null 2>&1
  check "after remove: service unit gone" 0 "$([ -e /usr/lib/systemd/system/homelab-control.service ] && echo 1 || echo 0)"
  check "after remove: nothing listening on 9180" 0 "$(ss -Hltn 'sport = :9180' | wc -l)"
  check "after remove: broker socket gone" 0 "$([ -S /run/homelab-control/broker.sock ] && echo 1 || echo 0)"
  check "after remove: config kept" 1 "$([ -s /etc/homelab-control/users.json ] && echo 1 || echo 0)"
  check "after remove: upstream Webmin still answers" 1 "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:10000/ | grep -cE '^(200|302|401)$')"
  apt-get install -y "$H/pkg.deb" >/dev/null 2>&1
  check "reinstall keeps existing accounts" 1 "$(grep -c '"rollback"' /etc/homelab-control/users.json)"
  check "reinstall does not auto-enable service" disabled "$(systemctl is-enabled homelab-control.service 2>/dev/null || true)"
  apt-get purge -y homelab-control >/dev/null 2>&1
  check "after purge: /etc/homelab-control removed" 0 "$([ -e /etc/homelab-control ] && echo 1 || echo 0)"
  check "after purge: dpkg has no record" 0 "$(dpkg-query -W homelab-control >/dev/null 2>&1 && echo 1 || echo 0)"
  check "after purge: upstream Webmin still answers" 1 "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:10000/ | grep -cE '^(200|302|401)$')"
  echo "rollback: $n_pass passed, $n_fail failed"
} > "$OUT/02-rollback.log" 2>&1
record "rollback / coexistence" "$(tail -1 "$OUT/02-rollback.log")"
rb_fail=$n_fail

step "3. browser e2e + axe (Playwright, Chromium) against the packaged stack"
(
  set -e
  # set -e: the first failing prerequisite stops this step with its own error at the end of the log.
  apt-get install -y python3-venv
  python3 -m venv $H/venv
  # >= 1.61.0 is the first Playwright that supports Ubuntu 26.04 (1.56 refuses to install Chromium there).
  $H/venv/bin/pip install -q playwright==1.63.0
  if ! $H/venv/bin/python -m playwright install --with-deps chromium; then
    echo "playwright --with-deps not supported on this release; installing Chromium deps from the archive"
    for p in libnss3 libnspr4 libatk1.0-0t64 libatk-bridge2.0-0t64 libcups2t64 libxkbcommon0 libxcomposite1 \
             libxdamage1 libxfixes3 libxrandr2 libgbm1 libpango-1.0-0 libcairo2 libasound2t64 libdrm2 fonts-liberation; do
      apt-get install -y "$p" >/dev/null 2>&1 || echo "  (package $p not available)"
    done
    $H/venv/bin/python -m playwright install chromium
  fi
  ensure_webmin
  apt-get install -y "$H/pkg.deb"
  pw="e2e-$(head -c 9 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  printf '%s\n' "$pw" | hlctl user add --file /etc/homelab-control/users.json --username admin >/dev/null
  chown homelab-control:homelab-control /etc/homelab-control/users.json
  # Loopback-only dev HTTP for the in-guest browser (no TLS proxy in the VM); never exposed off-host.
  sed -i "s|^HC_API_ARGS=.*|HC_API_ARGS=-listen 127.0.0.1:9180 -dev-insecure-http -legacy-url http://127.0.0.1:10000/|" /etc/homelab-control/hc-api.env
  systemctl enable --now homelab-control-broker.socket homelab-control.service >/dev/null 2>&1
  for _ in $(seq 50); do curl -fsS http://127.0.0.1:9180/api/v1/health >/dev/null 2>&1 && break; sleep 0.2; done
  # hc_m1_e2e.py loads axe from <repo>/frontend/app/node_modules/axe-core relative to itself.
  mkdir -p $H/frontend/app/node_modules/axe-core && cp $H/axe.min.js $H/frontend/app/node_modules/axe-core/axe.min.js
  HC_E2E_URL=http://127.0.0.1:9180 HC_E2E_PASSWORD="$pw" HC_E2E_ARTIFACTS="$OUT/screenshots" HC_E2E_REQUIRE_AXE=1 \
  HC_E2E_BROKER_STOP="systemctl stop homelab-control-broker.socket homelab-control-broker.service" \
    $H/venv/bin/python $H/tests/e2e/hc_m1_e2e.py
) > "$OUT/03-browser-axe.log" 2>&1
rc=$?; record "browser e2e + axe" "exit=$rc $(grep -E '[0-9]+ passed, [0-9]+ failed' "$OUT/03-browser-axe.log" | tail -1)"
$H/venv/bin/python -c 'import playwright, importlib.metadata as m; print("playwright", m.version("playwright"))' >> "$OUT/environment.txt" 2>/dev/null
grep -oE 'axe-core v?[0-9.]+|axe v[0-9.]+' $H/axe.min.js | head -1 | sed 's/^/axe_core=/' >> "$OUT/environment.txt"

step "4. final removal and residue check"
{
  n_pass=0; n_fail=0
  apt-get purge -y homelab-control >/dev/null 2>&1
  check "package purged" 0 "$(dpkg-query -W homelab-control >/dev/null 2>&1 && echo 1 || echo 0)"
  check "no hc processes" 0 "$(pgrep -c -f '/usr/(bin|lib/homelab-control)/hc-' || true)"
  check "no units left" 0 "$(find /usr/lib/systemd/system /etc/systemd/system -name 'homelab-control*' 2>/dev/null | wc -l)"
  check "nothing listening on 9180" 0 "$(ss -Hltn 'sport = :9180' | wc -l)"
  echo "final-removal: $n_pass passed, $n_fail failed"
} > "$OUT/04-final-removal.log" 2>&1
record "final removal" "$(tail -1 "$OUT/04-final-removal.log")"

systemd-analyze security --no-pager 2>/dev/null | grep -i homelab > "$OUT/systemd-analyze-after.txt" || true
journalctl -b --no-pager -u 'homelab-control*' > "$OUT/journal-homelab-control.log" 2>&1 || true
chmod -R a+rX "$OUT"
echo; cat "$summary"
if grep -qE 'exit=[1-9]| [1-9][0-9]* failed' "$summary" || [ "$rb_fail" -ne 0 ]; then exit 1; fi
