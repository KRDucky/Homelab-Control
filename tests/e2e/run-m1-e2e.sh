#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M1 browser e2e against the real stack: package installed in a disposable
# Ubuntu systemd container (host network so the browser can reach loopback),
# broker socket-activated.
# Needs root, docker, python3 + playwright on the host. Disposable hosts only.
#
# Usage: tests/e2e/run-m1-e2e.sh <package.deb> [artifact-dir]
set -euo pipefail
pkg=${1:?package}; art=${2:-$(pwd)/tests/e2e/artifacts}
root=$(cd "$(dirname "$0")/../.." && pwd)
img=hc-noble-systemd:m1
docker image inspect "$img" >/dev/null 2>&1 || { echo "build the image first: dev/disposable/systemd-container.sh noble <pkg>"; exit 1; }
if (exec 3<>"/dev/tcp/127.0.0.1/9180") 2>/dev/null; then echo "port 9180 busy on host"; exit 1; fi
name=hc-e2e-$$
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT
docker run -d --name "$name" --privileged --network host --tmpfs /run --tmpfs /run/lock \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw "$img" /sbin/init >/dev/null
for _ in $(seq 60); do s=$(docker exec "$name" systemctl is-system-running 2>/dev/null || true); [[ "$s" =~ running|degraded ]] && break; sleep 0.5; done
# Stage under /opt: systemd mounts a tmpfs over /tmp at boot on newer releases
# (e.g. 26.04), which hides files docker cp writes to the image layer.
docker exec "$name" mkdir -p /opt/hc
docker cp "$pkg" "$name":/opt/hc/pkg.deb
pw="e2e-$(head -c 9 /dev/urandom | od -An -tx1 | tr -d ' \n')"
docker exec -e PW="$pw" "$name" bash -euc '
  touch /etc/hc-disposable
  dpkg -i /opt/hc/pkg.deb >/dev/null
  printf "%s\n" "$PW" | hlctl user add --file /etc/homelab-control/users.json --username admin \
    --scopes system.read,services.read,services.logs,hardware.read,audit.read >/dev/null
  chown homelab-control:homelab-control /etc/homelab-control/users.json
  # Browser test over loopback without a TLS proxy: dev mode (loopback only).
  sed -i "s|^HC_API_ARGS=.*|HC_API_ARGS=-listen 127.0.0.1:9180 -dev-insecure-http|" /etc/homelab-control/hc-api.env
  systemctl enable --now homelab-control-broker.socket homelab-control.service >/dev/null 2>&1
'
for _ in $(seq 50); do curl -fsS http://127.0.0.1:9180/api/v1/health >/dev/null 2>&1 && break; sleep 0.2; done
mkdir -p "$art"; chmod 777 "$art"
HC_E2E_URL=http://127.0.0.1:9180 HC_E2E_PASSWORD="$pw" HC_E2E_ARTIFACTS="$art" \
HC_E2E_BROKER_STOP="docker exec $name systemctl stop homelab-control-broker.socket homelab-control-broker.service" \
  python3 "$root/tests/e2e/hc_m1_e2e.py"
