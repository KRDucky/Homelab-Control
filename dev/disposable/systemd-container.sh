#!/usr/bin/env bash
# Boots a disposable Ubuntu container with systemd as PID 1 and runs the
# package smoke test inside it. Needs root, docker and debootstrap, and network
# access to archive.ubuntu.com. Used when no hypervisor is available; a VM
# (dev/vm) or a CI runner is preferred when you have one.
#
# Usage: dev/disposable/systemd-container.sh <noble|jammy> <package.deb> [extra smoke env...]
set -euo pipefail
rel=${1:?release (noble|jammy)}; pkg=${2:?package}; shift 2
[[ "$rel" =~ ^(noble|jammy)$ ]] || { echo "unsupported release $rel"; exit 2; }
root=$(cd "$(dirname "$0")/../.." && pwd)
rootfs=/srv/hc-rootfs/$rel
img=hc-$rel-systemd:m1
if ! docker image inspect "$img" >/dev/null 2>&1; then
  if [ ! -f "$rootfs/.done" ]; then
    rm -rf "$rootfs"; mkdir -p "$rootfs"
    debootstrap --variant=minbase \
      --include=systemd,systemd-sysv,dbus,adduser,curl,ca-certificates,perl,python3,procps,iproute2,pciutils,usbutils,kmod,udev \
      "$rel" "$rootfs" http://archive.ubuntu.com/ubuntu
    touch "$rootfs/.done"
  fi
  tar -C "$rootfs" -c . | docker import - "$img" >/dev/null
fi
name=hc-sd-$rel-$$
vol=$(mktemp -d /srv/hc-vol.XXXXXX)   # real local filesystem (host ext4/xfs) bound in as /srv/data
trap 'docker rm -f "$name" >/dev/null 2>&1 || true; rm -rf "$vol"' EXIT
docker run -d --name "$name" --privileged --tmpfs /run --tmpfs /run/lock \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw -v "$vol":/srv/data "$img" /sbin/init >/dev/null
for _ in $(seq 60); do
  state=$(docker exec "$name" systemctl is-system-running 2>/dev/null || true)
  [[ "$state" =~ ^(running|degraded)$ ]] && break; sleep 0.5
done
echo "container $name: systemd state=$state"
docker exec "$name" systemctl list-units --failed --no-legend --no-pager || true
docker exec "$name" touch /etc/hc-disposable
docker cp "$pkg" "$name":/tmp/pkg.deb
docker cp "$root/tests/systemd" "$name":/tmp/hc-tests
# Upstream Webmin source for the legacy-detection checks (installed only inside this container).
docker cp "$root/compat/webmin" "$name":/tmp/hc-webmin
env_args=()
for kv in "$@"; do env_args+=(-e "$kv"); done
docker exec "${env_args[@]}" "$name" /tmp/hc-tests/systemd-smoke.sh /tmp/pkg.deb
