# M7: Software RAID detail from mdadm (2026-11-25)

Part of the M5–M9 review ("use the standard tools"). The Software RAID page used to parse `/proc/mdstat` and
deliberately dropped the members, the `[n/m]` health field and resync progress. It now shows each array as
`mdadm --detail` describes it, through the read-only storage helper. This is a checkpoint, **not M7
acceptance**. ADR 0008 Amendment 15.

## Behaviour
- Helper request scope `md`: reads `/proc/mdstat` and runs `mdadm --detail /dev/<kernel name>` per array. An
  array mdadm cannot describe keeps its name and mdstat state with the error.
- Broker op `mdadm.status` (scope `mdadm.read`), shared per-scope cache and deadline chain. `GET /mdadm` adds
  `status`; audit records counts only.
- Page: per array mdadm's state as the badge (red for degraded/failed/inactive, amber for
  recovering/resyncing/reshaping/checking), level, size, device counts, progress, layout and chunk, UUID, and
  a member table (slot, state, device). The `/proc/mdstat` table is shown only without the report.

## Verification
- Parser tested on real mdadm 4.5 output (RAID1 clean, RAID5 degraded with a faulty member, RAID5 rebuilding)
  plus removed and spare rows. Helper tests: exact arguments per array, an mdadm failure kept per array, the
  missing-tool path, and refusal without block-device classes (the scope opens `/dev/mdN`). API and page
  tests for report, reason and no-mdstat paths; `ReadMdstatFile` bounds.
- `go test ./...` (non-root), frontend 318/318, ESLint, Prettier, `tsc`, REUSE, policy guard (94 mutations),
  `make lint` clean up to the cargo step (cargo is not installed on this host).
- Graphify at `3b9786f`: no definition-only symbols, seven baseline self-loops.

### Disposable VMs (installed `.deb`, package `0.1.0+dev.1+187+g3b9786f`)

`md-check.py` compares the API with `mdadm --detail` run as root in the same guest (state, active/failed/spare
counts, member devices and states) for a RAID1 and a RAID5 array on loop devices: clean; after `--fail` of a
RAID5 member (degraded, faulty member); during a throttled rebuild (recovering, "Rebuild N% complete", a
`spare rebuilding` member). Also: missing `mdadm.read` gives 403, a stopped helper gives 200 with a reason, no
`hc-storage-probe` AppArmor denial with the profile enforced, audit rows carry counts and no device path.

| Guest | Kernel | mdadm | Result |
|---|---|---|---|
| Ubuntu 26.04.1 LTS | 7.0.0-43-generic | 4.5 | 14/14 |
| Ubuntu 22.04.5 LTS | 5.15.0-203-generic | 4.2 | 14/14 |
| Debian 13 (trixie) | 6.12.118+deb13-amd64 | 4.4 | 14/14 |

VM finding fixed: the helper first read `/proc/mdstat` through `mdadminfo.ProbeMdstat`, which opens the root
directory; the AppArmor profile denied it and the scope saw no arrays. The helper now opens `/proc/mdstat`
directly (`mdadminfo.ReadMdstatFile`) instead of the profile being widened. Harness fix (not product): the
check now starts from idle arrays after an aborted run left one rebuilding.

Evidence: `/mnt/tank/claude-vms/artifacts/m7-mdadm-be02b40/` (harness, per-guest `check.log`, package
checksums). Not covered: Ubuntu 24.04, Debian 12, arrays on real disks, IMSM/DDF container arrays, and
reshape/check progress (parsed by the same rule as rebuild, unit level only).
