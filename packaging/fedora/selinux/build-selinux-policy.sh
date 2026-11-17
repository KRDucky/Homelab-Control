#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Compile the storage-probe SELinux module into a temporary directory. This
# script NEVER loads or installs the module: loading is a manual step in a
# disposable validation VM (docs/m2/M2-4-O1-DEVICE-POLICY.md).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
devel=${SELINUX_DEVEL_MAKEFILE:-/usr/share/selinux/devel/Makefile}
if [ ! -f "$devel" ]; then
  echo "selinux-policy-devel is not installed ($devel not found); nothing compiled" >&2
  exit 2
fi
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
cp "$here/homelab_control_storage_probe.te" "$here/homelab_control_storage_probe.fc" "$out/"
make -C "$out" -f "$devel" homelab_control_storage_probe.pp
mkdir -p "${1:-$PWD}"
cp "$out/homelab_control_storage_probe.pp" "${1:-$PWD}/"
echo "built ${1:-$PWD}/homelab_control_storage_probe.pp (not loaded)"
