#!/usr/bin/env bash
# One-command M1 validation in a disposable libvirt VM (host side).
#   dev/vm/run-m1-validation.sh <vm-name> <release> <package.deb>
#   e.g. dev/vm/run-m1-validation.sh hc-test-u2604 26.04 build/homelab-control_*.deb
# Creates the VM if it does not exist (hc-vm.sh enforces the hc-test-/pool/network limits),
# stages the package and tests, runs dev/vm/guest-validate.sh as root inside the guest,
# and copies results to docs/evidence/m1/vm-<release>/. The VM is left running for
# inspection; remove it with: dev/vm/hc-vm.sh destroy <vm-name> --yes
set -euo pipefail
name=${1:?vm name (hc-test-...)}; rel=${2:?release}; pkg=${3:?package.deb}
root=$(cd "$(dirname "$0")/../.." && pwd); vm="$root/dev/vm/hc-vm.sh"
[ -f "$pkg" ] || { echo "no package $pkg"; exit 1; }
axe="$root/frontend/app/node_modules/axe-core/axe.min.js"
[ -f "$axe" ] || { echo "missing $axe — run 'make deps' (npm ci --ignore-scripts) first"; exit 1; }
out="$root/docs/evidence/m1/vm-$rel"; tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT

"$vm" preflight
virsh --connect qemu:///system dominfo "$name" >/dev/null 2>&1 || "$vm" create "$name" "$rel"

# Stage inputs (no git checkout or toolchain needed in the guest).
mkdir -p "$tmp/hc/tests"
cp "$pkg" "$tmp/hc/pkg.deb"; cp "$axe" "$tmp/hc/axe.min.js"
cp -r "$root/tests/systemd" "$root/tests/e2e" "$tmp/hc/tests/"
rm -rf "$tmp/hc/tests/e2e/artifacts"
cp "$root/dev/vm/guest-validate.sh" "$tmp/hc/"
cp -r "$root/compat/webmin" "$tmp/hc/webmin"
"$vm" ssh "$name" 'sudo rm -rf /opt/hc && sudo install -d /opt/hc'
tar -C "$tmp/hc" -cf - . | "$vm" ssh "$name" 'sudo tar -C /opt/hc --no-same-owner -xf - && sudo chown -R root:root /opt/hc'

set +e
"$vm" ssh "$name" 'sudo bash /opt/hc/guest-validate.sh' 2>&1 | tee "$tmp/guest-console.log"
rc=${PIPESTATUS[0]}
set -e

# Collect evidence written outside the repo first, then move it in whole.
mkdir -p "$tmp/ev"
"$vm" ssh "$name" 'sudo tar -C /opt/hc -cf - out' | tar -C "$tmp/ev" -xf -
cp "$tmp/guest-console.log" "$tmp/ev/out/guest-console.log"
cn=$(case "$rel" in 26.04) echo resolute;; 24.04) echo noble;; esac)
{
  echo "host_kernel=$(uname -r)"
  echo "host_os=$(. /etc/os-release && echo "$PRETTY_NAME")"
  echo "host_cpu=$(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | sed 's/^ //')"
  echo "libvirt=$(virsh --connect qemu:///system version 2>/dev/null | tr '\n' ' ')"
  echo "qemu=$(qemu-system-x86_64 --version 2>/dev/null | head -1)"
  echo "virt_install=$(virt-install --version 2>/dev/null)"
  echo "vm_name=$name release=$rel"
  echo "== verified cloud image"; cat "${HC_VM_STATE:-$root/.dev/vm}/images/$cn/VERIFIED" 2>/dev/null
  echo "== domain"; virsh --connect qemu:///system dumpxml "$name" | grep -E '<(name|memory|vcpu|source |model |type )' | sed 's/^ *//'
} > "$tmp/ev/out/host-environment.txt"
echo "guest_validate_exit=$rc" >> "$tmp/ev/out/host-environment.txt"
rm -rf "$out"; mkdir -p "$(dirname "$out")"; mv "$tmp/ev/out" "$out"
echo "evidence: $out (guest exit $rc)"
exit "$rc"
