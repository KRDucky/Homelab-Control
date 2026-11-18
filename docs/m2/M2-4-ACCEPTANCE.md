# M2-4 storage inventory — acceptance record

Status 2026-11-18: **Accepted for the scoped read-only inventory and explicit on-demand probe.** The owner asked to bring M2-4 to accepted and close it after authorizing disposable-VM testing. Acceptance covers the implementation, native LVM metadata graph, broker/API/UI path, and the Fedora 44 SELinux and Debian 13 AppArmor guest runs recorded below. The helper remains disabled by default; acceptance is not host activation or production deployment approval.

| # | Criterion | Status | Evidence |
|---|---|---|---|
| 1 | Read-only `GET /api/v1/storage`, separate `storage.read` scope, no default grant, audited before release | Done (tests) | Go API/guard tests; OpenAPI |
| 2 | Identity: WWN only from validated NAA (types 2/5/6, correct length, non-zero) or EUI designators, case-canonicalized for duplicate detection; malformed/locally-assigned values kept as raw `wwid-unclassified`; vendor-derived wwid ranked below serial; NVMe controller serial scoped; partitions never inherit disk IDs; duplicates ambiguous, never merged | Done (synthetic fixtures; formats from kernel `scsi_vpd_lun_id`/`wwid_show`) — not validated on real vendor hardware (row 4) | `TestCanonicalWWIDValidatesKernelDesignatorFormats`, `TestEquivalentWWNSpellingsAreDuplicatesAndMalformedIsNotWWNTier` |
| 3 | Serial/ID default with WWN choice, all identifiers retained, display never changes identity | Done (fixtures, browser) | UI tests; browser fixture |
| 4 | Serial/ID sources across device types | Accepted with a documented validation limit: identifiers and source handling are fixture-tested; virtio was exercised in KVM, but physical SATA/SAS/NVMe vendor hardware was not available | storage identity tests; Fedora VM evidence |
| 5 | LVM RAID/cache metadata, component relationships, UUID joins, metadata-only health semantics, missing/duplicate PV handling, and conservative PV-to-disk links | Accepted: native reader is default in the helper, grounded by 22 checked-in raw fixtures and field-by-field comparisons across 20 supported layouts. Unsupported writecache reports partial. Metadata health remains unknown; links remain provisional/withheld absent signature and same-boot revalidation | `backend/internal/lvmmeta/testdata/README.md`; `lvmmeta`, `storageinfo` tests; Fedora VM evidence |
| 6 | Native LVM/blkid collection through the isolated helper | Accepted: exact helper returned the six-PV `fxsix` VG, one LV and three striped segments under enforcing Fedora SELinux; Debian AppArmor probe also returned the expected inventory. Runtime uses native LVM metadata parsing and executes only `blkid`; no `lvs`/`pvs` process is used | O-1 evidence records and artifact manifests; API/broker/storageprobe tests |
| 7 | Live LVM RAID/cache health | **Not observable without `CAP_SYS_ADMIN`** (O-2); reported as unknown | `M2-4-STORAGE-SCOPE.md` |
| 8 | md runtime state from sysfs | Done (fixtures) | `TestCollectMDRuntimeStateWithoutDerivingHealth` |
| 9 | ZFS, Btrfs multi-device, mergerfs topology | Accepted with explicit limit: signature nodes are reported; pool/member topology and mergerfs backing relationships are not inferred by this M2-4 slice and remain technology-specific M3 work | `storageinfo/collect.go` graph limitations; M2 roadmap M3 storage row |
| 10 | Bounded snapshots (migration 2), boot staleness, dedup | Done (tests) | `state` tests |
| 11 | HCIS UI, keyboard, axe desktop/mobile, no overflow, real API/session/DB | Done for the synthetic fixture only (browser run 22/22 after integrating `main` `a548f52f`; axe 0 desktop/0 mobile; no overflow). Not native-probe, distro or KVM evidence | local evidence, not committed |
| 12 | Repository checks | Accepted with a coverage-run limitation: the full Go suite (`go test -count=1 ./...`) passed at this source revision. In the aggregate Fedora guest `make check`, the packaging guard (77 mutations), `go vet`, Rust fmt/clippy, and frontend typecheck/lint/format all passed. Its final `go test -count=1 -cover ./...` was still compiling/linking test binaries after 26 minutes under guest I/O pressure; no failure was reported, but no final exit status was retrieved. | `/var/tmp/m24-fedorapolicy-20261118/go-verification/final/make-check.log` and guest run status |
| 13 | Distribution security and runtime containment | Accepted for tested Fedora 44 SELinux and Debian 13 AppArmor configurations. Not validated: Ubuntu LTS, physical vendor hardware, systemd-nspawn containment, other supported distro packaging, or CI on a remote branch | `M2-4-O1-DEVICE-POLICY.md`; Fedora and Debian artifact manifests |
| 14 | GET side effects and request controls | Storage, service-log and unit-file reads require the session CSRF token before collection, broker access, audit or snapshot writes; residual audit growth remains for owner/supervisor review | see below; `TestStorageReadRequiresSessionCSRFToken`, `TestUnitLogsRequiresCSRFBeforeBrokerOrAudit`, `TestUnitContentsRequiresCSRFBeforeBrokerOrAudit`; e2e `cookie-only storage read refused` |
| 15 | Module registry (M2-3, merged from `main` `a548f52f`) | `storage` is **not** a registered module: `GET /api/v1/storage` is a core route, so `/etc/homelab-control/modules.json` cannot disable it and the fail-closed state (invalid setting) does not hide it. Registering it changes the administrator file contract (`modules_test` requires review for new modules); proposed, not done | `modules.ForRoute("/api/v1/storage") == ""` |

### Row 14 — reasoning and follow-up

A storage read is a GET with side effects inside the application: one audit row per authorized read (normal — reads of identifiers are audited before release, as `services.logs.read` already is) and, when the state database is attached, a snapshot save. `SameSite=Strict` does **not** make every unwanted trigger impossible:

- SameSite ignores ports and covers sibling subdomains, so another web application on the same host (e.g. `:8080`) or on `other.example.lan` is *same-site*: an `<img>`, `<link rel=prefetch>`, no-cors `fetch` or top-level navigation from it carries the session cookie. CORS only stops it reading the response.
- Browser prefetch/prerender and an authenticated user's reloads or concurrent tabs also issue the GET.

Controls:

1. **Session CSRF token required on this GET** (`X-CSRF-Token`, the same per-session token unsafe methods use). A cross-origin page cannot attach a custom header without a CORS preflight, which the server never grants, and cannot read the token. Navigation and prefetch cannot send it. A refused request returns `403 csrf_rejected` **before** collection, audit or snapshot save, so it cannot be used to grow the audit log. (Refusals are therefore not audited; that choice is for review.) Tests: `TestStorageReadRequiresSessionCSRFToken` (cookie only, wrong token, same-site image, prefetch, token in query string → refused with no collection/audit/save); browser fixture check.
2. **Concurrency** capped at one collection; others get `503 storage_busy`. 5-second deadline.
3. **Snapshot storage is bounded**: 4 rows, 8 MiB per graph, one-minute same-boot dedup (`state` tests). Responses are `Cache-Control: no-store`.
4. The UI polls once per 60 s only while the tab is visible (`useData` pauses on `visibilitychange`).

Cross-cutting follow-up: the audit log is an append-only hash chain with no retention/rotation. Authorized reads can grow it; retention policy belongs to the audit subsystem. Probe rate limiting limits disk wakes, not long-term audit storage.

Follow-up hardening (2026-11-15): `GET /services/units/{unit}/logs` and `/services/units/{unit}/contents` require the per-session CSRF header through the shared `Server.handleCSRFRead` wrapper. Cookie-only, wrong-token and URL-token requests are rejected before the broker or audit writer; the Services UI sends the session token. This does not bound audit growth from authorized reads.

