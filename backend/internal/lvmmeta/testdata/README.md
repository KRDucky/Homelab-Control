# lvmmeta fixtures

Real LVM2-written disk images, captured in a disposable Fedora VM and reduced to
the smallest committed prefix the reader needs. These are read-only test data;
the full 512 MiB images stay on the capture host.

## Provenance

- Capture root: `/mnt/tank/claude-vms/artifacts/m24-lvmmeta-fixtures-20261117b`
  (`raw/` = 27 case dirs, 64 disks; `json/` = per-case reference reports;
  `attempt-b1-INVALID-raw-sizes/` is excluded — it was an invalid first run).
- LVM2 version: `LVM version: 2.03.38(2) (2025-12-15)` (`lvm2-2.03.38-2.fc44.x86_64`).
- Host: `Linux lvmmeta-fx 6.19.10-300.fc44.x86_64` (Fedora 44).
- Reference capture commands (per `json/<case>/commands.txt`):
  - `pvs --reportformat json --units b --nosuffix -o all`
  - `pvs --reportformat json --units b --nosuffix --segments -o all`
  - `vgs --reportformat json --units b --nosuffix -o all`
  - `lvs --reportformat json --units b --nosuffix -a --segments -o all`
  - `lvs --reportformat json --units b --nosuffix --segments -o all`
  - `vgcfgbackup <vgname>` (text) — the differential reference used here.

## Extraction

Each `<case>.raw` is the first 32 KiB of one disk (max observed metadata extent
ends at byte 22771; the full image is never committed):

    dd if=<raw disk> of=<case>.raw bs=1 count=32768 status=none

Each `<case>.vgcfg` is the matching `vgcfgbackup -f` text. `SHA256SUMS` covers
the `.raw` prefixes; `SHA256SUMS.vgcfg` covers the reference texts.

## Coverage

- Differential (parse on-disk vs `vgcfgbackup`, field-by-field): every RAID
  level (0/1/4/5/6/10), mirror, thin pool/LV/snapshot, cache pool/origin/
  cachevol, snapshot, striped 3-way, and multi-PV (empty, spanning+pinned,
  six-PV).
- Unsupported: `writecache` is parsed as `Parsed=false` and reported partial,
  never complete.
- Missing PV: `multi-04` has one disk blanked; the reader reports the VG
  partial-missing (the committed metadata names the disk without a MISSING flag;
  the later `vgcfgbackup` reference shows the MISSING flag and is therefore not
  used for that case's presence assertion).
