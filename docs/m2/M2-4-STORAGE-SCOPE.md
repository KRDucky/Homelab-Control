# M2-4 storage inventory — implementation scope

Status: **Accepted 2026-11-18** for the scoped read-only storage inventory and explicit on-demand probe. The helper remains disabled by default and requires a separate owner decision before host or production activation. This slice performs no storage configuration writes. Acceptance evidence and limits are recorded in `M2-4-ACCEPTANCE.md`.

## LVM2 coverage (required)

LVM2 RAID and SSD caching are part of the inventory, not optional omissions:

- Physical volumes, volume groups, logical volumes and segment relationships, identified by native UUIDs with provenance.
- LVM RAID segment types and underlying data/metadata devices, observed RAID layout, and metadata relationships. Runtime degraded/rebuild/health state is not observed and remains `unknown`; do not infer protection from a name or device-mapper prefix.
- Existing LVM cache/cache-pool and cachevol relationships, origin and fast-tier devices, and metadata mode/policy. Runtime dirty-block, health and usage observations are not available. Distinguish dm-cache from dm-writecache; unsupported fields/layouts are explicitly partial.
- Show which disks/partitions supply RAID components and cache tiers through the same graph, including hidden/internal LVs where required to explain ownership.
- Capability-detect installed LVM tooling and report unavailable/partial observations. Fixture and integration evidence must support every claimed field. Never label missing health data healthy or an unobserved device unused.

Creation, conversion, cache attachment/detachment, mode changes, RAID reshape/repair and removal remain later privileged storage work, with their own plan/apply authorization and acceptance. This read-only slice does not authorize them.

## Drive label choice (required)

Users can choose **Serial/ID** or **WWN**, defaulting to Serial/ID to help match physically accessible labels. Do not assume a serial is printed in any particular location or constitutes a verified bay association.

Keep all observed identifiers and provenance regardless of display preference. Internal persistent identity and collision handling remain separate from the selected display label. Missing or duplicate preferred identifiers require visible warnings and an explicit alternative; kernel names are volatile and never persistent mapping keys. The detail view must make the alternative identifiers available for corroboration. A preference change must not reassign bays, graph ownership or disk identity.

The label preference is implemented in the Storage UI (browser-local) with fixtures. Native LVM2 metadata is read by the isolated helper and merged into the graph; it does not observe live device-mapper health. The helper's runtime path has been exercised on synthetic LVM layouts in disposable Fedora and Debian guests.

## LVM metadata contract research and runtime reader

The runtime reader parses bounded native LVM2 metadata from the already pinned read-only device descriptors; it does not execute `lvs`, `pvs` or `lvm`. LVM report parsers remain compatibility/test oracles only. The reader validates label/MDA/text CRCs and bounds, selects committed metadata, handles supported linear/striped/RAID/mirror/thin/cache/snapshot layouts, and returns typed partial results for unsupported or malformed data. Missing health/synchronization observations stay unknown. UUID-backed pool/origin/data/metadata references are joined without guessing from device strings. Multi-segment LVs remain one LV with several segments.

Primary references include the LVM2 format source and the report manuals above. No third-party parser implementation was copied. The portable fixture corpus records its LVM2/Fedora provenance in `backend/internal/lvmmeta/testdata/README.md`; fixture and guest evidence are summarized in the acceptance record.

## API implementation (accepted)

`GET /api/v1/storage` requires the independent `storage.read` scope, never a default grant. It returns a timestamped, read-only metadata graph with explicit limitations; no caller can select an OS root, device, executable or mountpoint. The separate `POST /api/v1/storage/probe` requires the non-default `storage.probe` scope, session CSRF token, audit, rate limit and single-flight controls. It invokes the root-only socket helper; the native LVM graph is merged with observed `blkid` signatures. The UI exposes an explicit, scope-gated Probe action and never triggers probing from polling GET requests. New scope grants must be removed before downgrading to builds that reject unknown scopes.

## Storage page (accepted)

The HCIS page now consumes the real `/storage` API, with capability-gated navigation, explicit partial-data warnings, resource filtering and an accessible textual relationship table. Serial/ID is the default display preference; WWN is selectable and the preference stays in this browser, independently of theme. Details retain all corroborating identifiers and ambiguity warnings; physical bay location remains unknown. Selection is bound to the observation timestamp so a refreshed/reused kernel device number cannot silently keep an old selection.

The UI displays the current graph and explicit partial/unknown states, and the explicit Probe action is covered by browser tests. Probe results replace the displayed inventory; polling GET is read-only. Physical drive identity and live LVM health retain the limitations stated above.

## Bounded observation persistence (accepted)

With the existing state database enabled, fresh authorized observations are saved in a new migration-2 table, capped at four live rows and 8 MiB per graph. API responses identify whether an observation was persisted; a configured store failure returns an error instead of pretending a snapshot exists. No stale graph substitutes for a failed fresh collection. A staleness helper classifies unknown/different boot identifiers, future timestamps and expired observations; no snapshot-history API is exposed yet. Without a state database, responses explicitly identify ephemeral mode.

Migration 1 stays unchanged. Upgrade takes a consistent backup, failure rolls back, and same-second retries retain distinct recovery copies rather than colliding. Downgrading requires restoring a verified older-schema backup; old binaries refuse schema 2. These are application-state writes, not storage-device configuration operations.

## Browser fixture validation (2026-11-05)

An opt-in test server exercises the actual API, authentication/session machinery and private SQLite state, with only a labelled synthetic sysfs tree and LVM JSON report. The headless Chromium test makes no mocked browser requests and invokes no native tool or privileged operation. It covers drive label preference, keyboard filtering, text relationships, RAID/cache report rendering, persisted observations, refresh selection, 401/403 denial and sign-out. Desktop and mobile accessibility scans and screenshots are collected separately from private runtime credentials/state.

This is API/UI integration evidence, not actual host storage, LVM/blkid, distribution, SELinux or KVM acceptance. Normal Go tests skip the opt-in server. Runtime directories must never be copied into published evidence. The first attempts retain the ambiguous fixture-label locator, exact accessible-name problem and an unsettled viewport measurement; the label is now explicitly associated, selectors are scoped, and viewport checks wait for rendered layout.

## Review follow-up (Claude, 2026-11-05) — identity, LVM PV/RAID composition, md state

Identity/display behavior is fixture-tested and was exercised against synthetic virtio devices in disposable guests; no physical vendor hardware was available for this acceptance run.

- **WWID classification.** sysfs `wwid` is validated against the kernel writers, not just its prefix: SCSI VPD page 0x83 (`drivers/scsi/scsi_lib.c` `scsi_vpd_lun_id`: `naa.%8phN`/`%16phN`, `eui.%8phN`/`%12phN`/`%16phN`, `t10.*`, and a verbatim SCSI name string, whose SPC spelling uses uppercase hex) and NVMe (`drivers/nvme/host/sysfs.c` `wwid_show`: `uuid.%pU`, `eui.` NGUID/EUI-64, else synthesized `nvme.*`). Only a well-formed, non-zero NAA type 2/5 (8 bytes) or 6 (16 bytes), or an 8/12/16-byte EUI, is a `wwn`; NAA type 3 is locally assigned and is not. Hex case is canonicalized (lowercase) so equivalent spellings are detected as duplicates. A well-formed non-null `uuid.` is `nvme-uuid`. `t10.` and `nvme.` are vendor-derived (`t10-vendor-id`, `nvme-vendor-id`), rank below a device serial and are flagged ambiguous when shared. Anything else, including malformed or all-zero `naa.`/`eui.`/`uuid.` values, is kept verbatim as `wwid-unclassified` raw evidence; such a failed `naa.`/`eui.`/`uuid.` designator (including well-formed NAA type 3) never becomes a persistent identity at all, because placeholder values are shared by unrelated devices. Genuine `t10.`, `nvme.` and unprefixed values keep the lowest identity tier. Fixture WWIDs are synthetic, labelled examples; they are not production validation of any vendor's values.
- **Serial/ID sources.** Read only from world-readable sysfs, with the exact source recorded on each identifier: SCSI/libata `device/vpd_pg80` (unit serial page, `S_IRUGO` in `drivers/scsi/scsi_sysfs.c`), virtio-blk `<blk>/serial` (`DEVICE_ATTR_RO` in `drivers/block/virtio_blk.c`; reading it issues a virtio GET_ID identification request, not a data read), then `device/serial`. `/dev/disk/by-id` is **not** read: hc-api runs with `PrivateDevices=yes`, so that tree does not exist in its namespace. Serial/ID therefore remains partial for device types outside these sources, and the API notes say so.
- **NVMe controller serial.** `device/serial` of an NVMe namespace is the controller's serial and is recorded as `controller-serial` with `scope: controller`. It is a valid Serial/ID display label, is not a per-namespace identity, and is flagged duplicated only when it appears under more than one controller.
- **LVM metadata mode.** The helper labels the inventory `metadata-only`; live device-mapper health is not read. Health and synchronization remain unknown. The legacy `lvs`/`pvs` JSON parsers are retained as fixture/compatibility oracles, not runtime collectors.
- **PV identity and links.** A PV UUID identifies its LVM metadata record, not a disk. The native reader derives the PV, VG, LV, extent and segment relationships from validated metadata; `blkid` signatures corroborate candidates. Because the current collector does not take a second same-boot sysfs observation after parsing, PV-to-disk links remain provisional or withheld rather than `verified`. Conflicting signatures, sizes, duplicate/missing PVs or ambiguous device mappings withhold the edge.
- **RAID/cache composition.** Native metadata joins supported internal LV data/metadata devices, pool/origin/cache relationships and segments by UUID within a VG. Unsupported layouts are partial; runtime degraded/rebuild/dirty-block state is not inferred.
- **Join status.** Each LV records `kernel_join` (`joined`, `no-kernel-node`, `ambiguous`, `not-checked`); any unjoined LV makes the graph partial.
- **VG consistency.** A VG UUID reported with different names in one report is rejected.
- **md runtime state.** For md arrays the collector reads `md/array_state`, `degraded`, `raid_disks`, `sync_action`, `sync_completed`, `mismatch_cnt` and each `md/dev-*/{slot,state}` with the member device number via its `block` link. Values are relayed only if they are short kernel keywords; no health summary is derived. Nothing is written to md control files.
- **Snapshot comparison.** `SameDevice` compares only unique observed persistent identities; node IDs and device numbers (reused after hotplug) are never compared across snapshots.
- The Go `Label` helper was removed; the display-label policy lives only in `frontend/app/src/storageModel.ts` and is pinned by UI tests, so the two copies cannot drift.

### Live LVM RAID/cache health needs device-mapper control

Linux `drivers/md/dm-ioctl.c` `ctl_ioctl()` refuses **every** device-mapper control ioctl, including read-only `DM_TABLE_STATUS` and `DM_LIST_DEVICES` and even `DM_VERSION`, unless the caller has `CAP_SYS_ADMIN`. dm-raid publishes no md-style sysfs. A "live" LVM health observer would therefore hold full device-mapper control (suspend, remove, table load). That is an owner/ADR decision and is not implemented or requested by this slice; until then LVM RAID/cache runtime health is reported as not observed.
