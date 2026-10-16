#!/usr/bin/env bash
# Boots a disposable Ubuntu container with systemd as PID 1 and runs the
# package smoke test inside it. Needs root, docker and debootstrap, and network
# access to archive.ubuntu.com. Used when no hypervisor is available; a VM
# (dev/vm) or a CI runner is preferred when you have one.
#
# Usage: dev/disposable/systemd-container.sh <resolute|noble|jammy> <package.deb> [extra smoke env...]
set -euo pipefail
rel=${1:?release (resolute|noble|jammy)}; pkg=${2:?package}; shift 2
[[ "$rel" =~ ^(resolute|noble|jammy)$ ]] || { echo "unsupported release $rel"; exit 2; }
# Older debootstrap packages (e.g. on a 24.04 host) predate 26.04; every Ubuntu
# release since 7.10 uses the same bootstrap script, so fall back to it.
dbs_script=()
[ -e "/usr/share/debootstrap/scripts/$rel" ] || dbs_script=(/usr/share/debootstrap/scripts/gutsy)
root=$(cd "$(dirname "$0")/../.." && pwd)
rootfs=/srv/hc-rootfs/$rel
img=hc-$rel-systemd:m1
if ! docker image inspect "$img" >/dev/null 2>&1; then
  if [ ! -f "$rootfs/.done" ]; then
    rm -rf "$rootfs"; mkdir -p "$rootfs"
    debootstrap --variant=minbase \
      --include=systemd,systemd-sysv,dbus,adduser,curl,ca-certificates,perl,python3,procps,iproute2,pciutils,usbutils,kmod,udev \
      "$rel" "$rootfs" http://archive.ubuntu.com/ubuntu "${dbs_script[@]}"
    touch "$rootfs/.done"
  fi
  tar -C "$rootfs" -c . | docker import - "$img" >/dev/null
fi
name=hc-sd-$rel-$$
vol=$(mktemp -d /srv/hc-vol.XXXXXX)   # real local filesystem (host ext4/xfs) bound in as /srv/data
trap 'docker rm -f "$name" >/dev/null 2>&1 || true; rm -rf "$vol"' EXIT
# cgroup v2 hosts (current GitHub runners, most distros): systemd needs a writable
# cgroup tree; bind-mounting the host tree into a private cgroup namespace makes it
# exit at boot, so share the host namespace. v1 hosts keep the old bind mount.
# (systemd >= 258, e.g. Ubuntu 26.04, supports only cgroup v2.)
if [ "$(stat -fc %T /sys/fs/cgroup)" = cgroup2fs ]; then cg=(--cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw)
else cg=(-v /sys/fs/cgroup:/sys/fs/cgroup:rw); fi
docker run -d --name "$name" --privileged -e container=docker --tmpfs /run --tmpfs /run/lock \
  "${cg[@]}" -v "$vol":/srv/data "$img" /sbin/init >/dev/null
state=""
for _ in $(seq 60); do
  state=$(docker exec "$name" systemctl is-system-running 2>/dev/null || true)
  [[ "$state" =~ ^(running|degraded)$ ]] && break; sleep 0.5
done
echo "container $name: systemd state=$state"
if [[ ! "$state" =~ ^(running|degraded)$ ]]; then
  echo "ERROR: systemd did not reach running/degraded in the container; init output:"
  docker logs "$name" 2>&1 | tail -n 40 || true
  exit 1
fi
docker exec "$name" systemctl list-units --failed --no-legend --no-pager || true
docker exec "$name" touch /etc/hc-disposable
docker cp "$pkg" "$name":/tmp/pkg.deb
docker cp "$root/tests/systemd" "$name":/tmp/hc-tests
# Upstream Webmin source for the legacy-detection checks (installed only inside this container).
docker cp "$root/compat/webmin" "$name":/tmp/hc-webmin
env_args=()
for kv in "$@"; do env_args+=(-e "$kv"); done
docker exec "${env_args[@]}" "$name" /tmp/hc-tests/systemd-smoke.sh /tmp/pkg.deb
