<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors

SPDX-License-Identifier: GPL-3.0-or-later
-->

# SELinux module for the M2-4 O-1 storage probe (source only)

`homelab_control_storage_probe.{te,fc}` define the confined domain
`hc_storage_probe_t` for `/usr/lib/homelab-control/hc-storage-probe`.

- **Not shipped, not loaded.** No package scriptlet compiles, installs or loads
  it. Owner approval (2026-11-17) covers implementation and disposable
  validation only.
- **Fail closed.** The unit sets `SELinuxContext=system_u:system_r:hc_storage_probe_t:s0`.
  With SELinux active and no module loaded the instance cannot start.
- **Unvalidated.** It has not been compiled here. The first compile and the AVC
  capture in a disposable Fedora VM are the acceptance evidence
  (`docs/m2/M2-4-O1-DEVICE-POLICY.md`, section "Validation checklist").
- Build in the VM only: `./build-selinux-policy.sh` (compile; it never loads).
  Load with `semodule -i` and `restorecon -v /usr/lib/homelab-control/hc-storage-probe`
  by hand, in the VM only.
- Do not use `audit2allow` output as policy. Review each new AVC against the
  design: read-only raw access to `fixed_disk_device_t` only.
