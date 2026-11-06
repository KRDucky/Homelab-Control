#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 final safety review: installs the package in a disposable systemd
# container (same isolation as docker-systemd.sh), runs
# tests/systemd/m2_3_boot_repro.py pre through the product, restarts the
# container (a fresh PID 1; /run is a tmpfs, so runtime configuration is gone)
# and runs the post phase. The container is removed afterwards.
#
# Usage: dev/disposable/boot-repro.sh <image> <package>
set -euo pipefail
image=${1:?image}; pkg=${2:?package}
root=$(cd "$(dirname "$0")/../.." && pwd)
df=$root/dev/disposable/images/$image.Dockerfile
[ -f "$df" ] || { echo "no such image $image"; exit 2; }
tag=hc-disposable-$image:m2-3
docker build -q -t "$tag" -f "$df" "$root/dev/disposable/images" >/dev/null
name=hc-rr-$image-$$
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT
docker run -d --name "$name" --privileged --cgroupns=private --tmpfs /run --tmpfs /run/lock --tmpfs /tmp "$tag" >/dev/null
wait_up() {
  local s=""
  for _ in $(seq 240); do
    s=$(docker exec "$name" systemctl is-system-running 2>/dev/null || true)
    [[ "$s" =~ ^(running|degraded)$ ]] && break; sleep 0.5
  done
  echo "container $name ($image): systemd state=$s"
}
wait_up
docker exec "$name" touch /etc/hc-disposable
docker exec "$name" mkdir -p /opt/hc
kind=${pkg##*.}
docker cp "$pkg" "$name:/opt/hc/pkg.$kind"
docker cp "$root/tests/systemd" "$name":/opt/hc/tests
docker exec -e HC_PKG_KIND="$kind" -e HC_SMOKE_EXTRA_UNITS=homelab-control-broker.socket \
  -e HC_SMOKE_EXTRA_CHECKS=/opt/hc/tests/m2-3-boot-repro.sh \
  -e HC_SMOKE_SCOPES=system.read,services.read,services.logs,hardware.read \
  "$name" /opt/hc/tests/systemd-smoke.sh "/opt/hc/pkg.$kind" | grep -E '^(DECISION|EXPECT|STATE|INFO|HARNESS|PRODUCT|systemd|==|FAIL)' || true
echo "== /run is a tmpfs: $(docker exec "$name" findmnt -n -o FSTYPE /run)"
docker restart -t 10 "$name" >/dev/null
wait_up
docker exec "$name" python3 /opt/hc/tests/m2_3_boot_repro.py post
