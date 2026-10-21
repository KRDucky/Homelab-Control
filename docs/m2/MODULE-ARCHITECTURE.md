# Module architecture, coordinated workflows and the physical drive layout model

**Date:** 2026-10-21 · **Status:** design proposal implementing the owner's product decisions of
2026-10-21:
- the technology scope;
- independent modules plus coordinated workflows;
- the Physical Drive Layout Designer.

These are **design authorization only**; nothing here is implemented. Builds on ADR 0001/0003/0008 and
specs 02, 03 and 05.

## 1. Principles (owner decisions, 2026-10-21)
1. **Modules are independently useful.**
   - Each module works when its own host dependencies are present.
   - A missing tool disables only that module, with an explanation and an approved install path (spec 06/07).
   - No module requires another module to be configured.
2. **Coordinated workflows are optional conveniences.**
   - They exist only when every participating module is installed and enabled.
   - They never bypass a module's own validation, scopes, broker ops or locks.
3. **Native tools and established APIs** are used: systemd, journald, `zpool`/`zfs`, `mdadm`, LVM2 JSON reports, `btrfs`, Docker Engine API, libvirt, Samba tools, LIO, OpenSSH, `bolt`. Nothing is re-implemented. No Webmin code is executed or copied (ADR 0003, ADR 0005 boundaries).
4. **Read-only first.** Every module ships discovery and monitoring before any configuration change.
5. **Adopt, don't recreate.** Existing pools, arrays, filesystems, shares, containers and VMs are discovered and managed in place. Nothing requires reformatting or recreation. Unmanaged configuration is never rewritten without explicit import or takeover.
6. **Risk classes on every action:**
   - *reversible*: e.g. enable a share, start a container;
   - *disruptive*: e.g. restart a service, stop a VM, unmount;
   - *destructive*: e.g. wipe, destroy pool, remove LV.

   Each class has its own confirmation and step-up (D-3) and its own scope level (D-7).

## 2. Module model (proposed; D-10: built-in first-party modules)
Each module is a Go package pair plus a UI package, registered in one module registry.

| Element | Content |
|---|---|
| Manifest | `id` (e.g. `storage.zfs`), title, version, category (Storage, Containers, Virtualization, Sharing, System), required host capabilities, optional capabilities, scopes it defines, routes, nav entry |
| Capability probe | Unprivileged where possible (binary presence through the broker's allow-list resolver, sysfs, kernel modules, sockets); result `available` / `missing: <component>` / `unsupported: <reason>` with the distro-native package suggestion |
| Read API | `GET /api/v1/<module>/…`, scope `<module>.read`; data labelled with source and confidence |
| Broker ops | Namespaced `<module>.<object>.<verb>`; read ops first; write ops follow the plan/apply framework (ADR 0011, proposed) |
| Resource types | Contribution to the shared resource graph (spec 02): nodes and typed edges with provenance and confidence |
| Lock domains | Declared per op; storage ops lock **device identities** (§3), not module-local names |
| UI | Route set and nav entry shown only when the module is enabled and its probe says available; otherwise a single "not available — why / how to install" card |
| Enable/disable | Owner setting in `state.db`. Disabling hides routes and refuses its ops; it never uninstalls host software or touches data |

**Distribution adapters.** Package names, service names, config paths and SELinux/AppArmor/firewall specifics come from a per-family adapter (Debian/Ubuntu, Fedora, openSUSE, Arch) selected from `os-release` `ID`/`ID_LIKE`. They are detected, never hardcoded to Ubuntu. Package installs go through the Software Center module, with no silent repositories (spec 06).

## 3. Device ownership and conflict prevention (storage)
Several modules can claim the same block device (whole disk → partition → md member → LVM PV → filesystem → mount → share). A shared **ownership resolver** is built from the read-only resource graph:
- **Claims** are derived from on-disk signatures and live state, not from module guesses. Each claim names the claiming technology, the stable identity of the device and the evidence. Sources:
  - `blkid` type and UUIDs;
  - md superblocks;
  - LVM PV labels;
  - ZFS labels;
  - Btrfs device membership;
  - mergerfs branch paths;
  - SnapRAID `snapraid.conf` entries;
  - mounts.
- A device with an active claim is **read-only to every other module**. Proposing a write against it returns `owned_by: <module/resource>` with the chain of dependencies.
- **Unknown or conflicting signatures** (e.g. stale md superblock plus a filesystem) mark the device **ambiguous**. Ambiguous devices are excluded from every write plan until the user resolves them through the owning module's guided flow.
- **Locks** are taken on the device identity, and on its ancestors and descendants in the graph, for the duration of an apply. Two modules can therefore never operate on overlapping devices at the same time.
- Identity is always WWN / serial / NVMe EUI/NGUID / by-id with confidence (existing `hwinfo` ranking). `/dev/sdX` is display-only.

## 4. Coordinated cross-module workflows
A workflow is an ordered list of **module plan steps**. The workflow engine only orchestrates: it never calls the broker directly.

**Prerequisites.** Each workflow declares its modules. It is offered only when all of them are enabled and available, and the user holds every step's scope.

**Plan.** The engine requests each module's plan in order, passing forward only declared outputs (e.g. the dataset mountpoint). The combined preview shows:
- every step with its risk class;
- the affected resources by stable identity (and physical bay where mapped);
- destructive steps highlighted;
- the recovery note for each step.

**Confirm.** The user confirms once for the workflow; step-up covers the whole workflow if any step is disruptive or destructive. Each step's broker plan token is still minted and checked individually.

**Execute.** Steps run in order. Each step:
- takes its own locks;
- revalidates its target;
- writes its own journal records.

**Partial failure.** Execution stops at the first failed step. Completed steps are listed as completed. Then:
- If a completed step declares a *safe compensating action* (e.g. remove the share export it just added), it is **offered**, never run automatically.
- Otherwise the step's documented recovery procedure is shown.
- **No automatic rollback is promised for non-reversible steps** (CLAUDE.md).

**Dry run.** Where every module supports it, the whole workflow can be dry-run. Steps that cannot be simulated are labelled "not simulated".

| Example workflow (owner) | Steps (module) | Destructive? | Notes |
|---|---|---|---|
| ZFS dataset → SMB/SFTP/WebDAV share | create dataset (zfs) → set ownership/ACL on mountpoint (filesystem permissions) → add export (samba/sftp/webdav) → validate and reload (service) | No (new dataset) | Effective-access check across layers (addon 03) before apply |
| Existing filesystem → Docker bind-mount storage | verify mount and ownership (storage) → create or modify container/compose bind mount (docker) | No | Compose files edited only if the project is managed or imported; otherwise shown as a diff to apply manually |
| LVM2 LV → format → persistent mount | create LV (lvm) → mkfs (filesystem, **destructive** on the new LV only) → mountpoint and fstab or systemd `.mount` (mounts) | mkfs on new LV | Ownership resolver proves the LV is new and unclaimed |
| mergerfs pool + optional SnapRAID | define branches from adopted filesystems (mergerfs) → optional: parity/content layout and config (snapraid) → first sync **only after** user review | No | The two modules stay separately usable (owner requirement) |
| libvirt VM on an existing storage pool | select pool/volume (libvirt storage) → define domain (libvirt) → optional start | No | Through libvirt API/`virsh`, never raw QEMU |
| Provision storage → iSCSI target | create zvol/LV (zfs/lvm) → LIO backstore + target + ACL + CHAP (iscsi) | No | Never auto-format a LUN; exclusive-use warning (addon 01) |

## 5. Physical Drive Layout Designer (owner requirement, 2026-10-21)
One shared physical-location model, consumed by every storage module. There is no per-technology designer.

### 5.0 Built from the existing HBA Viewer concept (owner direction, 2026-10-21)
The owner directed that the existing **HBA Viewer** concept be made reusable inside Homelab Control, instead
of building a separate drive-location system.

**What was found.** The only matching prior work available to this review is the **chassis "Layout" tab of
the owner's *Linux Storage Manager*** (`~/Storage-Manager`, Flask + SQLite, reviewed read-only on
2026-10-21). It contains:
- `chassis` and `chassis_bays` tables;
- size presets (towers, NAS, 12/24-bay rackmount);
- a bay grid with a health border per bay;
- click to assign or clear a drive from an "unassigned drives" list;
- bay labels.

The owner should confirm that this is the HBA Viewer meant, or point to the other source (D-12). The
design below treats it as the base.

**Reused (concept, data model and interaction; re-implemented in Go/React, not copied):**
- **Data model:** chassis with rows × cols plus bays with label and drive assignment. This becomes `Chassis`/`Cell`/`Assignment` (§5.1), extended rather than replaced.
- **Presets:** 4/6/8-bay tower, 4/6/8/12-bay NAS, 12/24-bay rackmount, custom. They are offered as starting layouts, then edited freely.
- **Interactions:** bay grid with a health border; click an empty bay to assign; click an occupied bay to reassign or clear; unassigned-drives list; labels.
- **Discovery:** the background drive scan with SMART health and temperature, as the source for bay status; this becomes the read-only SMART op in M3.

**One component, shared by every module.** The viewer becomes a single **Hardware → Drives & Bays** view.
It shows the controller (HBA) → enclosure or chassis → bay → drive path where known, or manual chassis
grids where not. Every storage module (ZFS, mdadm, LVM, Btrfs, mergerfs, SnapRAID, SMART, replacement
flows) embeds the same `BayLocation` badge and links into this view. No module draws its own layout.

**Changes required against the prior art** (Homelab Control rules):

| Prior art (Storage Manager) | Homelab Control |
|---|---|
| Drives keyed by `device_path` (`/dev/sda`), so a bay follows a kernel name | Keyed by stable identity (WWN > serial > NVMe EUI/NGUID > by-id) with provenance and confidence; never re-associated because a kernel name moved (CLAUDE.md) |
| Rectangular grid only | Cell kinds incl. empty, disabled and reserved for irregular layouts; several chassis, shelves and enclosures; HDD/SSD/NVMe/tray kinds |
| No controller or enclosure link | Optional HBA/enclosure/slot association (Stage C), shown only when verified |
| No mismatch handling | Present / missing / replaced / mismatch states; manual mappings labelled *unverified* |
| Unauthenticated Flask API, shell-string command builders | Scoped API (`layout.manage`), CSRF; layout edits never touch the host; no command strings anywhere |
| No export | Export/import with identifiers redacted by default |

The Storage Manager's mergerfs, SnapRAID, SMART and LVM parsers are also useful **reference fixtures**
for the M3 read-only storage modules. Their command builders produce shell strings and are not reused.

### 5.1 Data model (layout data is separate from live discovery)
| Entity | Fields | Notes |
|---|---|---|
| `Chassis` | id, name, kind (server, shelf, external enclosure, desktop), grid rows × cols, notes | Several chassis per host |
| `Cell` | chassis, row, col, kind (`hdd-3.5`, `ssd-2.5`, `nvme-m2`, `nvme-u2`, `tray`, `empty`, `disabled`, `reserved`), label, bay number | Irregular layouts use `empty`/`disabled` cells |
| `Assignment` | cell → drive identity `{kind: wwn\|serial\|nvme-eui\|nvme-nguid\|scsi-vpd\|by-id, value}`, provenance (`manual` \| `ses` \| `controller`), confidence, assigned_at, assigned_by, last_seen | Never keyed by `/dev/sdX` |
| `Controller` (read-only) | HBA PCI address, driver, model, SAS address (when available) | From discovery, not stored as truth |
| `Observation` (derived, not stored as layout) | current device at identity, health, temperature, pool/array/filesystem membership | Joined at read time from modules |

**States shown per bay:**
- assigned and present;
- **assigned but missing**;
- **identity mismatch**: a different drive appears where SES/controller data says this slot is, or the user is asked to confirm a replacement;
- unassigned drive;
- empty, reserved or disabled.

Health alerts are a separate visual layer from location.

**Safety rules:**
- A bay is never re-associated automatically because a kernel name moved.
- Manual assignments are labelled *manual, unverified* until corroborated, e.g. a drive LED blink acknowledged by the user, or SES slot data.
- Destructive and replacement flows show identity, location, provenance and dependencies, and warn on stale, missing, conflicting or unverified mappings (owner §6).

**Export/import:** JSON with the layout and assignments.
- Serials and WWNs are **excluded by default**: an assignment exports as a salted hash plus the last 4 characters, which re-matches on the same host.
- Full identifiers only on explicit opt-in.
- Imports are validated and never overwrite without a preview.

**Persistence:** `state.db` (D-1), non-secret. It survives reboots, renumbering, replacements, HBA changes and upgrades, because keys are stable identities.

### 5.2 Stages
| Stage | Content | Privileges | Proposed milestone |
|---|---|---|---|
| A | Chassis grid editor (rows/cols, labels, cell kinds, drag-and-drop assign from the existing disk inventory), persisted, export/import; clicking a bay opens the disk detail | None (layout in API DB; discovery already unprivileged) | **M2 (PR M2-5)** |
| B | Stable identity mapping across modules; replacement/mismatch detection; location shown in ZFS/mdadm/LVM/Btrfs/mergerfs/SnapRAID/SMART views and replacement guidance | Read ops only (SMART needs broker read op) | Storage read-only milestone (M3 proposed) |
| C | Automatic slot discovery: `/sys/class/enclosure` (SES via kernel), `sg_ses` where installed, SAS addresses, `storcli` **only if user-installed** (proprietary, never bundled) | Broker read ops | Hardware/enclosure milestone |
| D | Guarded identify LED (`/sys/class/enclosure/*/*/locate`, `ledctl`, or controller tool), only after verifying the capability on that enclosure; guided physical replacement | Broker write op (`reversible` class, `storage.locate` scope) | Replacement milestone |

### 5.3 Example rendering (from owner requirement)
> **ZFS Pool:** tank · **Device:** ST16000NM001G · **Serial:** *(detected)* · **Location:** Main NAS → Row 2 → Column 3 → Bay 7 *(manual, unverified)*

## 6. Frontend implications (no rewrite; existing React app)
- **Navigation:**
  - The registry drives nav groups (System, Storage, Containers, Virtualization, Sharing).
  - Each module contributes routes.
  - The current hard-coded `Services`/`Hardware` entries move into this model.
- **Shared components** to add in M2:
  - `ConfirmDialog` (native `<dialog>`, focus trap, risk-class styling, typed-identity confirmation for destructive actions);
  - `StepUpPrompt`;
  - `PlanPreview` (steps, resources, reversible/destructive badges);
  - `JobStatus` (polling first; SSE per spec 02 later);
  - `PermissionGate` (hide/disable with reason, based on session scopes returned by `/auth/session`).
- **Theme compatibility:** new components use CSS custom properties from the design tokens only, so the planned `.hltheme` engine (ADR 0010) applies without rework. No Theme Studio work.
- **Accessibility:** keyboard-complete flows, axe 0 violations, and responsive layout are kept as CI gates. The drive grid needs a keyboard alternative to drag-and-drop (select bay → choose drive).
