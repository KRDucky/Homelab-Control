#!/usr/bin/env bash
# Installs the pinned upstream Webmin (compat/webmin) into a throwaway prefix and
# starts it on 127.0.0.1:10000 with SSL off. Intended ONLY for disposable
# environments (container, VM). It never touches /etc/webmin or any disk device.
#
# Env: HC_DISPOSABLE_DIR (default /tmp/hc-disposable), WEBMIN_USER (default admin),
#      WEBMIN_PASS (required), WEBMIN_PORT (default 10000)
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
D=${HC_DISPOSABLE_DIR:-/tmp/hc-disposable}
: "${WEBMIN_PASS:?set WEBMIN_PASS for the throwaway admin account}"
if [ -e /etc/webmin ]; then
  echo "refusing: /etc/webmin exists — this looks like a real Webmin host, not a disposable environment" >&2
  exit 1
fi
[ "$(id -u)" -eq 0 ] || { echo "miniserv runs as root by upstream design; run inside a disposable VM/container as root" >&2; exit 1; }
rm -rf "$D"; mkdir -p "$D"
cp -a "$root/compat/webmin" "$D/webmin"
[ -x /usr/local/bin/perl ] || ln -s "$(command -v perl)" /usr/local/bin/perl   # upstream shebang
perl_bin=$(command -v perl)
(
  cd "$D/webmin"
  export config_dir="$D/etc-webmin" var_dir="$D/var-webmin" perl="$perl_bin" \
         port="${WEBMIN_PORT:-10000}" login="${WEBMIN_USER:-admin}" password="$WEBMIN_PASS" \
         ssl=0 atboot=0 nostart=1 nouninstall=1 nopostinstall=1 noperlpath=1 autoos=1 autothird=1
  ./setup.sh > "$D/setup.log" 2>&1
)
echo "bind=127.0.0.1" >> "$D/etc-webmin/miniserv.conf"
"$D/etc-webmin/start"
for _ in $(seq 30); do curl -fsS -o /dev/null "http://127.0.0.1:${WEBMIN_PORT:-10000}/" && break; sleep 0.5; done
echo "Upstream Webmin $(cat "$D/etc-webmin/version") running at http://127.0.0.1:${WEBMIN_PORT:-10000}/ (stop: $D/etc-webmin/stop)"
