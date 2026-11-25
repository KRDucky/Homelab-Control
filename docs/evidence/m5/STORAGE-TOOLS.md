# M5: storage helper runs the standard tools (2026-11-25)

Owner decision: `docs/m2/OWNER-DECISIONS.md`, "Use the standard storage tools; activate the read-only
helper". Design: ADR 0008 Amendment 12. This is a partial M5 checkpoint, **not M5 acceptance**.

## What changed

| Before | Now |
|---|---|
| `internal/lvmmeta` (about 2,800 lines with tests): a native parser for the LVM2 on-disk format | `pvs`/`lvs --readonly --nolocking --reportformat json` (LVM2), decoded by the existing `storageinfo.ParsePVS`/`ParseLVM` |
| fd-pinned `blkid` on device names the broker sent | `lsblk --json --list --bytes` (util-linux); filesystem signatures from the udev database |
| no SMART | `smartctl --json=c --nocheck=standby --health --info --attributes` (smartmontools) on SATA/SAS/NVMe whole disks; a disk in standby is not woken |
| helper inactive, DynamicUser, no capabilities | package ships the marker; admin enables the socket; root with `CAP_SYS_RAWIO` only |

The helper takes no device names from its caller (protocol 2). smartmontools is a `Recommends`, lvm2 a
`Suggests`; when either is missing the result is partial and says which package provides it.

## VM findings fixed on the way

1. **LVM found nothing under DynamicUser.** `pvs` as a non-root user reports "Device open 8:0 has no path
   names" for every device; as root without capabilities it works. The helper now runs as root with
   `CapabilityBoundingSet=CAP_SYS_RAWIO`; root owns the block nodes, so no `CAP_DAC_*` is needed, and the
   read-only cgroup device policy, AppArmor and `InaccessiblePaths=` still confine it.
2. **LVM locking.** On the read-only filesystem `pvs` failed with "File locking initialisation failed";
   `--nolocking` is added to both LVM templates.
3. **AppArmor.** LVM lists `/dev` subdirectories and reads `/etc/multipath.conf`; both are allowed
   read-only.
4. **Device classes.** The startup self-check refused the whole probe when any reviewed class was
   missing; the Debian 13 guest has no `md` class (no software RAID). It now refuses only when no class is
   registered and notes the missing ones: systemd leaves an unregistered class's devices unreadable anyway.
5. **AppArmor for LVM 2.03.11.** Ubuntu 22.04's LVM lists `/sys/class` and `/sys/bus` and reads its hints
   cache; sysfs and `/run/lvm` are readable, writes there stay denied.
6. **Stale note.** After a probe, the collector's "On-disk signatures have not been probed" note stayed;
   it is now removed once lsblk supplied signatures (regression test in `storage_probe_test.go`).

## NVMe SMART needs CAP_SYS_ADMIN (granted later, owner 2026-11-25)

> **Update:** the owner granted `CAP_SYS_ADMIN`. NVMe SMART verification after the grant is recorded in
> `docs/evidence/ui/SCREENSHOTS.md`. The text below records the first checkpoint.

The kernel releases the NVMe SMART/Health log (an admin command) only to a process with `CAP_SYS_ADMIN`.
Observed in the Ubuntu 26.04 guest (kernel 7.0): with `CAP_SYS_RAWIO` smartctl gets
`NVME_IOCTL_ADMIN_CMD: Permission denied`; with `CAP_SYS_ADMIN` (and no AppArmor profile) it reads the log.
The helper does not hold `CAP_SYS_ADMIN` (owner decision), so NVMe drives report SMART health as
`unavailable` with the reason `nvme-needs-cap-sys-admin`, and the UI says why. SATA/SAS SMART works.

## Graphify

Indexed at the tested commit `1326fde` (654 code files, 6964 nodes, 24135 edges). No missing endpoints, no import cycles, no symbol
left with only its definition. `storageinfo.Build` still labels signatures from `Block.FSType` with
source `blkid`; no production collector sets that field today, so it never appears in a result. It is
replaced in the next M5 PR, which moves the base inventory itself to lsblk.

## Installed-package VM validation (commit `1326fde`)

Package `homelab-control_0.1.0+dev.1+174+g1326fde_amd64.deb`, installed with `apt` (smartmontools came
in through `Recommends`). Each disposable guest had a virtio system disk plus two blank virtual test disks:
SATA (`HCSATA0001`, AHCI) carrying a disposable LVM PV/VG/LV, and NVMe (`HCNVME0001`). Evidence:
`/mnt/tank/claude-vms/artifacts/m5-tools-1326fde/`.

| Guest | Kernel | smartctl | Result |
|---|---|---|---|
| Ubuntu 26.04.1 | 7.0.0-43 | 7.5 | 27/27 |
| Debian 13 (Secure Boot UEFI) | 6.12.118 | 7.4 | 27/27 |
| Ubuntu 22.04.5 | 5.15.0-203 | 7.2 | 27/27 |

Checks, each against the tool run directly as root in the guest:
- SATA SMART: model, serial and health match `smartctl -j`; NVMe reports `unavailable`
  (`nvme-needs-cap-sys-admin`) and its identity still comes from sysfs; the virtio disk has no SMART.
- Signatures match `lsblk` FSTYPE; LVM VG/LV/PV nodes match the disposable volume group (metadata-only).
- Nothing written: the VG metadata sequence number and the first MiB of both test disks are unchanged.
- Negative paths: `storage.read` alone gets 403; an immediate second probe gets 429; without smartmontools
  the result is partial and names the package; with the marker removed the probe returns 503.
- Standby: QEMU does not emulate disk standby, so the product's answer is compared with
  `smartctl -n standby` (both report the disk as active).
- AppArmor `hc-storage-probe` loaded in enforce mode, no denials.

Earlier rounds at `e3dce77` and `8090093` found the issues listed above; their evidence is in the
matching `m5-tools-*` artifact directories.

Not run: Ubuntu 24.04 (its base image is readable only by libvirt; changing that needs owner approval)
and Debian 12 (no base image). Real SATA/NVMe hardware was not used: the SMART data comes from QEMU's
emulated drives.
