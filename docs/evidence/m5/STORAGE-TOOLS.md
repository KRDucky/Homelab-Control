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
4. **Stale note.** After a probe, the collector's "On-disk signatures have not been probed" note stayed;
   it is now removed once lsblk supplied signatures (regression test in `storage_probe_test.go`).

## Known limitation: NVMe SMART needs CAP_SYS_ADMIN

The kernel releases the NVMe SMART/Health log (an admin command) only to a process with `CAP_SYS_ADMIN`.
Observed in the Ubuntu 26.04 guest (kernel 7.0): with `CAP_SYS_RAWIO` smartctl gets
`NVME_IOCTL_ADMIN_CMD: Permission denied`; with `CAP_SYS_ADMIN` (and no AppArmor profile) it reads the log.
The helper does not hold `CAP_SYS_ADMIN` (owner decision), so NVMe drives report SMART health as
`unavailable` with the reason `nvme-needs-cap-sys-admin`, and the UI says why. SATA/SAS SMART works.

## Graphify

Indexed at the tested commit, see the table below. No missing endpoints, no import cycles, no symbol
left with only its definition. `storageinfo.Build` still labels signatures from `Block.FSType` with
source `blkid`; no production collector sets that field today, so it never appears in a result. It is
replaced in the next M5 PR, which moves the base inventory itself to lsblk.

VM_RESULTS_PLACEHOLDER
