# M2-4 storage inventory — acceptance checklist (not accepted)

Status 2026-11-17: **O-1 implementation integrated on the isolated readiness branch; M2-4 is not ready for acceptance or merge.** The owner authorized O-1 implementation and isolated validation. This checklist records evidence and remaining gates; it is not owner acceptance.

| # | Criterion | Status | Evidence |
|---|---|---|---|
| 1 | Read-only `GET /api/v1/storage`, separate `storage.read` scope, no default grant, audited before release | Done (tests) | Go API/guard tests; OpenAPI |
| 2 | Identity: WWN only from validated NAA (types 2/5/6, correct length, non-zero) or EUI designators, case-canonicalized for duplicate detection; malformed/locally-assigned values kept as raw `wwid-unclassified`; vendor-derived wwid ranked below serial; NVMe controller serial scoped; partitions never inherit disk IDs; duplicates ambiguous, never merged | Done (synthetic fixtures; formats from kernel `scsi_vpd_lun_id`/`wwid_show`) — not validated on real vendor hardware (row 4) | `TestCanonicalWWIDValidatesKernelDesignatorFormats`, `TestEquivalentWWNSpellingsAreDuplicatesAndMalformedIsNotWWNTier` |
| 3 | Serial/ID default with WWN choice, all identifiers retained, display never changes identity | Done (fixtures, browser) | UI tests; browser fixture |
| 4 | Serial/ID sources on real SATA/SAS/virtio/NVMe hosts | **Open** — synthetic virtio disk exercised in Fedora KVM; no real SATA/SAS/NVMe vendor hardware or host identity sources observed | O-1 Fedora VM logs; `M2-4-DESIGN-REVIEW.md` |
| 5 | LVM RAID / SSD cache: LV, PV, VG, segment, cache mode/policy, RAID composition, UUID joins for LV/VG/PV relationships, metadata-only vs live mode; `pv_missing`/`pv_major` semantics and duplicate-row consistency per lvm2 source; PV→disk link only `verified` with signature + size + same-boot revalidation, else `provisional` (no claim) or `withheld` | Parsers and link rules done (fixtures); native LVM collection remains disabled pending identity and real-tool validation; complete LVM graph joins are not claimed | `storageinfo` tests incl. `TestPVLink*`, `TestPVS*`; `M2-4-DESIGN-REVIEW.md` |
| 6 | Native LVM/blkid collection | blkid helper, broker op, explicit CSRF POST, non-default scope and UI action implemented; synthetic-disk Fedora VM probe passed. LVM remains disabled: reports are unusable inside the actual unit sandbox because `/dev/mapper/control` is inaccessible. Not accepted | O-1 Fedora VM logs; API/broker/storageprobe tests |
| 7 | Live LVM RAID/cache health | **Not observable without `CAP_SYS_ADMIN`** (O-2); reported as unknown | `M2-4-STORAGE-SCOPE.md` |
| 8 | md runtime state from sysfs | Done (fixtures) | `TestCollectMDRuntimeStateWithoutDerivingHealth` |
| 9 | ZFS, Btrfs multi-device, mergerfs | **Open** | — |
| 10 | Bounded snapshots (migration 2), boot staleness, dedup | Done (tests) | `state` tests |
| 11 | HCIS UI, keyboard, axe desktop/mobile, no overflow, real API/session/DB | Done for the synthetic fixture only (browser run 22/22 after integrating `main` `a548f52f`; axe 0 desktop/0 mobile; no overflow). Not native-probe, distro or KVM evidence | local evidence, not committed |
| 12 | `make check` | Pass locally 2026-11-05 (pinned Go 1.26.10, non-root) including ShellCheck 0.9.0, the version CI installs, also after integrating `main` `a548f52f` (uncommitted merge); CI not yet run on this change | local log |
| 13 | CI, distribution packaging, SELinux, KVM/systemd | **Partial, not accepted** — Fedora 44 KVM systemd/device-policy and fixed SELinux source passed on a synthetic disk; LVM unit-sandbox check failed; nspawn policy only held with `--keep-unit`; AppArmor, CI and cross-distro packaging remain unverified | `/mnt/tank/claude-vms/artifacts/hc-m24-o1-ce76000d/`; `/mnt/tank/claude-vms/artifacts/hc-m24-o1-ce76000d-policy-retest2/`; `M2-4-O1-DEVICE-POLICY.md` |
| 14 | GET side effects and request controls | Storage, service-log and unit-file reads require the session CSRF token before collection, broker access, audit or snapshot writes; residual audit growth remains for owner/supervisor review | see below; `TestStorageReadRequiresSessionCSRFToken`, `TestUnitLogsRequiresCSRFBeforeBrokerOrAudit`, `TestUnitContentsRequiresCSRFBeforeBrokerOrAudit`; e2e `cookie-only storage read refused` |
| 15 | Module registry (M2-3, merged from `main` `a548f52f`) | `storage` is **not** a registered module: `GET /api/v1/storage` is a core route, so `/etc/homelab-control/modules.json` cannot disable it and the fail-closed state (invalid setting) does not hide it. Registering it changes the administrator file contract (`modules_test` requires review for new modules); proposed, not done | `modules.ForRoute("/api/v1/storage") == ""` |

### Row 14 — reasoning (not an accepted risk)

A storage read is a GET with side effects inside the application: one audit row per authorized read (normal — reads of identifiers are audited before release, as `services.logs.read` already is) and, when the state database is attached, a snapshot save. `SameSite=Strict` does **not** make every unwanted trigger impossible:

- SameSite ignores ports and covers sibling subdomains, so another web application on the same host (e.g. `:8080`) or on `other.example.lan` is *same-site*: an `<img>`, `<link rel=prefetch>`, no-cors `fetch` or top-level navigation from it carries the session cookie. CORS only stops it reading the response.
- Browser prefetch/prerender and an authenticated user's reloads or concurrent tabs also issue the GET.

Controls:

1. **Session CSRF token required on this GET** (`X-CSRF-Token`, the same per-session token unsafe methods use). A cross-origin page cannot attach a custom header without a CORS preflight, which the server never grants, and cannot read the token. Navigation and prefetch cannot send it. A refused request returns `403 csrf_rejected` **before** collection, audit or snapshot save, so it cannot be used to grow the audit log. (Refusals are therefore not audited; that choice is for review.) Tests: `TestStorageReadRequiresSessionCSRFToken` (cookie only, wrong token, same-site image, prefetch, token in query string → refused with no collection/audit/save); browser fixture check.
2. **Concurrency** capped at one collection; others get `503 storage_busy`. 5-second deadline.
3. **Snapshot storage is bounded**: 4 rows, 8 MiB per graph, one-minute same-boot dedup (`state` tests). Responses are `Cache-Control: no-store`.
4. The UI polls once per 60 s only while the tab is visible (`useData` pauses on `visibilitychange`).

Residual, for owner/supervisor review: the audit log is an append-only hash chain with **no retention or rotation** found in code or packaging, so authorized reads grow it without bound (≈1,440 rows per day per continuously visible Storage tab, plus manual refreshes). Bounding that is an audit-subsystem decision (rotation with checkpoint continuity, or a per-user read rate limit), not something to drop silently from M2-4.

Follow-up hardening (2026-11-15): `GET /services/units/{unit}/logs` and `/services/units/{unit}/contents` require the per-session CSRF header through the shared `Server.handleCSRFRead` wrapper. Cookie-only, wrong-token and URL-token requests are rejected before the broker or audit writer; the Services UI sends the session token. This does not bound audit growth from authorized reads.

