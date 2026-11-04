#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Runs a systemd probe script in a disposable systemd container (same isolation
# as docker-systemd.sh: private cgroup namespace, own network namespace,
# removed afterwards). The script runs with "pre", the container is restarted
# (a fresh PID 1 boots), then the script runs with "post".
#
# Usage: dev/disposable/probe-container.sh <image> <probe-script> [NAME=value ...]
# (the assignments are passed to the probe script's environment)
set -euo pipefail
image=${1:?image}; script=${2:?probe script}; shift 2
env_args=()
for kv in "$@"; do env_args+=(-e "$kv"); done
root=$(cd "$(dirname "$0")/../.." && pwd)
df=$root/dev/disposable/images/$image.Dockerfile
[ -f "$df" ] || { echo "no such image $image"; exit 2; }
tag=hc-disposable-$image:m2-3
docker build -q -t "$tag" -f "$df" "$root/dev/disposable/images" >/dev/null
name=hc-probe-$image-$$
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT
docker run -d --name "$name" --privileged --cgroupns=private --tmpfs /run --tmpfs /run/lock --tmpfs /tmp "$tag" >/dev/null
wait_up() {
  local s=""
  for _ in $(seq 90); do
    s=$(docker exec "$name" systemctl is-system-running 2>/dev/null || true)
    [[ "$s" =~ ^(running|degraded)$ ]] && break; sleep 0.5
  done
  echo "container $name ($image): systemd state=$s"
}
wait_up
docker exec "$name" touch /etc/hc-disposable
docker cp "$script" "$name:/probe.sh"
docker exec "${env_args[@]}" "$name" bash /probe.sh pre
docker restart -t 5 "$name" >/dev/null
wait_up
docker exec "${env_args[@]}" "$name" bash /probe.sh post
