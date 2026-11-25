# M7: LVM page from pvs, lvs and vgs (2026-11-25)

Part of the M5–M9 review ("use the standard tools"). The LVM page reconstructed logical volumes from sysfs
`dm-*` names and their `LVM-` uuids, about 300 lines plus 13 tests redoing what `lvs` reports. It now shows
LVM's own reports through the read-only storage helper, and the walk is removed. This is a checkpoint, **not
M7 acceptance**. ADR 0008 Amendment 16.

## Behaviour
- Helper request scope `lvm`: `pvs`, `lvs` (existing templates) and the new `vgs` template, `--readonly
  --nolocking`, through the same function as the full probe (which still runs only `pvs` and `lvs`).
- Broker op `lvm.status` (scope `lvm.read`), shared cache and deadline chain. `GET /lvm` decodes the reports
  with `storageinfo.ParseLVM`/`ParsePVS` and `ParseVGS`; a report that does not parse becomes a reason. Audit
  records `vgs=N lvs=N pvs=N`, or `status=unavailable`.
- Page: volume groups (size, free, PVs, LVs, missing PVs in red), logical volumes (volume group, size,
  segment type, LVM's health, sync percent) and physical volumes (device, VG, size, missing). `thin_check` is
  offered only when `lvs` reports a thin pool. `GET /lvm` no longer returns the `lvm` device list.

## Verification
- `ParseVGS` tested on real LVM 2.03.31 output and malformed rows. Helper tests: the scope runs exactly
  `pvs`, `lvs`, `vgs` with their templates; the full probe does not run `vgs`; missing tools. API tests:
  decoded report and counts-only audit, an unparsable report as a reason with `status=unavailable` in the
  audit, no broker call without lvm2, no deadline shorter than the client's. Page tests for the three tables,
  the missing-PV and missing-VG markers, the thin_check offer and the reason path.
- `go test ./...` (non-root), frontend 318/318, ESLint, Prettier, `tsc`, REUSE, policy guard (94 mutations),
  `make lint` clean up to the cargo step (cargo is not installed on this host).
- Graphify at `1a53cd9`: no definition-only symbols, seven baseline self-loops.

### Disposable VMs (installed `.deb`, package `0.1.0+dev.1+187+g1a53cd9`)

`lvm-check.py` compares the API with `vgs`, `lvs` and `pvs` run as root in the same guest (VG size, free and
counts; LV name, size and segment type; PV device, VG and size) for a volume group with two linear LVs and a
thin pool on a SATA virtual disk. Also: the thin pool is reported, missing `lvm.read` gives 403, a stopped
helper gives 200 with a reason, no `hc-storage-probe` AppArmor denial with the profile enforced, audit rows
carry counts and no names.

| Guest | Kernel | LVM | Result |
|---|---|---|---|
| Ubuntu 26.04.1 LTS | 7.0.0-43-generic | 2.03.31 | 10/10 |
| Ubuntu 22.04.5 LTS | 5.15.0-203-generic | 2.03.11 | 10/10 |
| Debian 13 (trixie) | 6.12.118+deb13-amd64 | 2.03.31 | 10/10 |

Finding fixed on the way: with the report unavailable the audit said `vgs=0 lvs=0 pvs=0`; it now says
`status=unavailable`.

Evidence: `/mnt/tank/claude-vms/artifacts/m7-lvm-3800c6e/`. Not covered: Ubuntu 24.04, Debian 12, a missing
PV, RAID and cache LVs on real disks, and PVs on loop devices (the helper's device policy has no loop class,
by design).
