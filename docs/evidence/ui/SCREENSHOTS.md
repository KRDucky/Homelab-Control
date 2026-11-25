# README screenshots: how they were taken (2026-11-25)

The README gallery (`docs/screenshots/`, 19 PNGs) shows the build at commit `f8a259b` (footer
`v0.1.0-dev.1-180-gf8a259b`). It is a visual record, **not production acceptance** of any module.

## Environment
- Disposable libvirt VM `hcshots-u2604-3ed3271`, Ubuntu 26.04.1 LTS, kernel 7.0.0-43, overlay on the
  preserved cloud image in pool `claude-vms-zfs`. The package was installed with `apt` and upgraded in place
  to each fix commit; broker, hc-api and the read-only storage helper sockets were enabled.
- Tools: smartmontools 7.5, lvm2 2.03.31, zfsutils-linux 2.4.1, btrfs-progs 6.17.1, mergerfs 2.40.2,
  snapraid 13.0.
- Test data only, on blank virtual disks: LVM `datavg` (ext4 `media`, XFS `backups`) on a virtual SATA disk;
  ZFS mirror `tank` (`photos`, `documents`); Btrfs `archive` with two subvolumes; MergerFS `/mnt/storage` over
  two ext4 disks (`fsname=mergerfs`) with `/etc/snapraid.conf`; file-manager root `/srv/share` with three
  sample files. The NVMe and SATA disks are QEMU's emulated drives (serials `HCNVME0001`, `HCSATA0001`).
- Account `demo` with read scopes only (no write scope). hc-api listened on loopback with a throwaway
  self-signed certificate; the capture ran on the workstation through an SSH port forward.
- For the capture only, the VM's `HC_API_ARGS` set `-rate-limit-burst 100000`, as the CI system tests do:
  the default per-user burst (30) is exhausted after about six page loads in a row.

## Capture
`scripts/capture-readme-ui.py` (Playwright 1.63.0, Chromium headless shell 153, desktop 1440 × 900, phone
390 × 844 at 2×; Midnight theme, Daybreak for one image; animations disabled; waits for network idle,
fonts and the absence of loading states). PNGs were optimised losslessly with oxipng (3.2 MB → 2.2 MB).
Run with `HC_SCREENSHOT_STORAGE_FILTER=HC`, which types `HC` into the Storage page's own *Find a resource*
box so the table shows the test disks; the README caption says so.

## Checks (all passed on the final run)
- Every page's rendered text and every `href`/`src`/`action` attribute: no `webmin` or `miniserv`.
  The sidebar groups are Dashboard, System, Storage & files, Network & applications, Administration;
  no legacy entry, on desktop and in the phone drawer. `frontend/app/src/branding.test.ts` keeps Webmin
  text and the "HomeControl" misspelling out of the shipped UI sources.
- No browser console error after sign-in (the 401 of the session check before sign-in is expected).
- No error overlay; each page's title matched its module.
- SMART on the Storage page matches `smartctl -j` run as root in the guest: SATA and NVMe both passed;
  NVMe 50 °C, 0 % used, 0 media errors.
- No AppArmor denial for `hc-storage-probe` after the upgrade (the new postinst reloads the profiles).

## Fixed while capturing
1. **"HomeControl"** in five UI strings and several API messages → "Homelab Control" (regression test).
2. **MergerFS pools mounted with `fsname=`** were dropped ("No mergerfs pools found"); they are listed with
   branches from the matching fstab line or mount unit (regression tests).
3. **NVMe SMART** (owner: "grant that permission"): the helper holds `CAP_SYS_ADMIN` for NVMe admin
   commands; the policy guard allows exactly `CAP_SYS_RAWIO` and `CAP_SYS_ADMIN`.
4. **AppArmor profiles were not reloaded on upgrade**: postinst now reloads them, as dh_apparmor does.
5. **Broken screenshot workflow removed.** `.github/workflows/ui-screenshots.yml` (run 45557302814 failed:
   it asserted "Storage & files" against `inner_text()`, which returns CSS-uppercased text) ran without
   a broker, pushed to `main` with `contents: write` and used unpinned actions. The VM procedure replaces it.

## Limitations
- The workstation's CPU model appears on the dashboard and hardware page (`--cpu host-passthrough`).
- Traefik is shown only in its honest unavailable state (no Traefik extension in the VM). ext4, XFS, f2fs
  and Software RAID are not pictured.
- The Storage table lists nodes in snapshot-ID order, so disks are not first without a filter.
- One distribution (Ubuntu 26.04). The VM was left running for review; it is shut down afterwards.
