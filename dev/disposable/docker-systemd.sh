#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Boots a disposable systemd container from dev/disposable/images/<image>.Dockerfile
# and runs the package smoke test with the M1, M2-2 and M2-3 checks inside it.
# Needs docker (no host root, no debootstrap). The container gets a PRIVATE cgroup
# namespace (it sees only its own subtree; the host cgroup tree is never bind-mounted),
# its own network namespace, masked udev, and is removed afterwards.
# It runs --privileged because systemd and the units' sandboxing need it: use only
# on a workstation where that is acceptable, never on a production host.
#
# Usage: dev/disposable/docker-systemd.sh <ubuntu-2604|ubuntu-2404|ubuntu-2204|fedora-44> <package> [extra smoke env...]
set -euo pipefail
image=${1:?image (ubuntu-2604|ubuntu-2404|ubuntu-2204|fedora-44)}; pkg=${2:?package}; shift 2
root=$(cd "$(dirname "$0")/../.." && pwd)
df=$root/dev/disposable/images/$image.Dockerfile
[ -f "$df" ] || { echo "no such image $image"; exit 2; }
tag=hc-disposable-$image:m2-3
docker build -q -t "$tag" -f "$df" "$root/dev/disposable/images" >/dev/null
name=hc-sd-$image-$$
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT
docker run -d --name "$name" --privileged --cgroupns=private --tmpfs /run --tmpfs /run/lock --tmpfs /tmp \
  "$tag" >/dev/null
state=""
for _ in $(seq 60); do
  state=$(docker exec "$name" systemctl is-system-running 2>/dev/null || true)
  [[ "$state" =~ ^(running|degraded)$ ]] && break; sleep 0.5
done
echo "container $name ($image): systemd state=$state; cgroup $(docker exec "$name" cat /proc/self/cgroup)"
[[ "$state" =~ ^(running|degraded)$ ]] || { docker logs "$name" 2>&1 | tail -n 40; exit 1; }
docker exec "$name" systemctl list-units --failed --no-legend --no-pager || true
docker exec "$name" touch /etc/hc-disposable
docker exec "$name" mkdir -p /opt/hc
kind=${pkg##*.}
docker cp "$pkg" "$name:/opt/hc/pkg.$kind"
docker cp "$root/tests/systemd" "$name":/opt/hc/tests
docker cp "$root/compat/webmin" "$name":/opt/hc/webmin
env_args=()
for kv in "$@"; do env_args+=(-e "$kv"); done
docker exec "${env_args[@]}" -e HC_PKG_KIND="$kind" "$name" /opt/hc/tests/systemd-smoke.sh "/opt/hc/pkg.$kind"
