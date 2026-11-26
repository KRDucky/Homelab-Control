// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ops is the closed registry of broker operations (ADR 0008).
//
// Rules for every operation (review checklist):
//   - read-only unless registered as a write op (Write != nil; ADR 0011) with
//     plan/apply, a risk class, a lock domain and reviewed root templates;
//   - no caller-supplied paths, commands or patterns;
//   - typed, strictly decoded args (M1 ops take none);
//   - fixed file paths opened with O_NOFOLLOW and size caps, or a fixed argv
//     through internal/broker/runner (children run unprivileged);
//   - results contain only allow-listed, non-secret fields.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/mergerfsapply"
	"homelab-control/backend/internal/storagetools"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"homelab-control/backend/internal/aptapply"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/modules"
)

// Roots lets tests point file-reading ops at a fixture tree. Root is "/" in
// production; every op path is an absolute host path joined under it.
type Roots struct {
	Root string
}

// Path maps an absolute host path into the configured root.
func (r Roots) Path(abs string) string {
	if r.Root == "" {
		return abs
	}
	return filepath.Join(r.Root, abs)
}

// Env is passed to every handler.
type Env struct {
	Runner  *runner.Runner
	Roots   Roots
	Version string
	// JournalGID is the gid of the journal reader group (systemd-journal),
	// added to journalctl children; 0 when the group does not exist.
	JournalGID uint32
	// Writes is the plan/apply framework (tokens, locks, journal); nil when
	// the operation journal could not be opened (write ops then refuse).
	Writes *writes.Framework
	// Units overrides the systemd controller (tests); nil = systemctl.
	Units UnitController
	// Enablement overrides the unit-file controller (tests); nil = systemctl.
	Enablement EnablementController
	// RootExec is the root-exec handle. The server sets it in the per-call copy of
	// Env ONLY for ops whose Privilege is PrivRootExec; it is nil otherwise.
	RootExec *runner.Root
	// Systemctl overrides root systemctl execution in tests. Production leaves
	// it nil and services.systemctl uses RootExec.
	Systemctl systemctlExec
	// SnapraidStatus overrides `snapraid status` in tests. Production leaves
	// it nil. The production path runs Runner with argv exactly "status".
	SnapraidStatus func(ctx context.Context) (stdout []byte, exitCode int, err error)
	// PortsSystemctl overrides the two unprivileged systemctl reads of
	// network.ports.list (list-sockets, then show Id,Listen) in tests; it
	// receives the argv. Production leaves it nil and runs the runner.
	PortsSystemctl func(ctx context.Context, args []string) (stdout []byte, exitCode int, err error)
	// FirewallShow overrides the firewall unit-state query in tests.
	// Production leaves it nil and runs the fixed firewallShowArgs argv.
	FirewallShow func(ctx context.Context) (stdout []byte, exitCode int, err error)
	// MergerFSApply overrides the socket helper in tests. Production dials
	// mergerfs-apply.sock. The broker never executes mergerfs or snapraid sync.
	MergerFSApply func(ctx context.Context, req mergerfsapply.Request) (mergerfsapply.Result, error)
	// AptApply overrides the hc-apt-apply socket helper in tests. Production
	// dials apt-apply.sock. The broker never writes APT files itself.
	AptApply func(ctx context.Context, req aptapply.Request) (aptapply.Result, error)
	// AptConfigDump overrides `apt-config dump` in tests (stdout).
	AptConfigDump func(ctx context.Context) (string, error)
	// Actor is the caller as asserted by hc-api (per-call copy).
	Actor proto.Actor
	// Modules is the administrator's module setting (zero value: all enabled).
	Modules modules.Set
	// HostSettings overrides the hostnamectl/timedatectl controller (tests);
	// nil = hostnamectl and timedatectl.
	HostSettings HostSettingsController
	// Ext4Format overrides ext4 format device inspection (tests). Nil reads
	// the host. It never formats.
	Ext4Format Ext4FormatView
	// Ext4Mkfs overrides the mkfs.ext4 child (tests). Nil runs RootMkfsExt4
	// through RootExec. The argv is the closed template's argv.
	Ext4Mkfs func(ctx context.Context, argv []string) (runner.Result, error)
	// Ext4Tooling overrides the mkfs.ext4 presence probe (tests).
	Ext4Tooling *ext4ToolFact
	// PartDisk overrides partition device inspection (tests). Nil reads the host.
	PartDisk Ext4FormatView
	// PartGeometry overrides the sysfs logical block size and disk size (tests).
	PartGeometry func(rdev string) (sector, diskBytes uint64, err error)
	// PartSfdisk overrides the sfdisk child (tests). Nil runs RootSfdiskGPT.
	PartSfdisk func(ctx context.Context, argv []string, stdin []byte) (runner.Result, error)
	// PartTooling overrides the sfdisk presence probe (tests).
	PartTooling *partToolFact
}

// WriteSpec documents a write op for review and the registry snapshot test
// (M2-SECURITY-ACCEPTANCE W1).
type WriteSpec struct {
	Phase      string   // "plan" or "apply"
	Risks      []string // risk classes the op can return
	LockDomain string   // lock key pattern
	Templates  []runner.RootTemplate
}

// Op is one registered operation.
type Op struct {
	Name        string
	Scope       string // required scope; "" = any authenticated actor
	Privilege   Privilege
	Timeout     time.Duration
	Description string
	Handle      func(ctx context.Context, env *Env, args json.RawMessage) (any, error)
	// AuditArgs, for ops that take arguments, summarises validated arguments
	// for the broker audit record ("" if they do not validate). Never secrets.
	AuditArgs func(args json.RawMessage) string
	// Write is set for write ops only.
	Write *WriteSpec
	// Admission selects the broker's admission-control bucket. Every op has a
	// known non-empty class (see the constants below); the registry test pins
	// the exact set and the server refuses an unclassified op.
	Admission AdmissionClass
}

// AdmissionClass is the broker admission bucket for an operation. Each class
// has a fixed nonblocking capacity; the global in-flight cap is the backstop.
type AdmissionClass string

const (
	// AdmissionControlPlan covers service/host-settings plans (read-only, mint
	// a token). Separate from applies so plans cannot occupy every apply slot.
	AdmissionControlPlan AdmissionClass = "control-plan"
	// AdmissionControlApply covers service/host-settings applies (root-exec).
	AdmissionControlApply AdmissionClass = "control-apply"
	// AdmissionServicesRead covers the long read-only service views.
	AdmissionServicesRead AdmissionClass = "services-read"
	// AdmissionSystemRead covers short root-read system metadata views.
	AdmissionSystemRead AdmissionClass = "system-read"
	// AdmissionPackages covers the package queries.
	AdmissionPackages AdmissionClass = "packages"
	// AdmissionProbe covers the storage probe (dedicated so package queries
	// cannot occupy it).
	AdmissionProbe AdmissionClass = "probe"
	// AdmissionInfo covers broker.info (a tiny dedicated slot so it stays
	// available under read-class saturation).
	AdmissionInfo AdmissionClass = "info"
)

// Registry returns the compiled-in operation table.
func Registry() map[string]Op {
	ops := []Op{
		{Name: "broker.info", Scope: "", Privilege: PrivNone, Timeout: time.Second, Admission: AdmissionInfo,
			Description: "Broker version and the list of registered operations", Handle: brokerInfo},
		{Name: "hardware.dmi.identity", Scope: "hardware.read", Privilege: PrivRootRead, Timeout: 2 * time.Second, Admission: AdmissionSystemRead,
			Description: "Root-only SMBIOS identifiers (system/board/chassis serials, product UUID)", Handle: dmiIdentity},
		{Name: "services.systemd.list", Scope: "services.read", Privilege: PrivUnprivilegedExec, Timeout: 10 * time.Second, Admission: AdmissionServicesRead,
			Description: "systemd units with load/active/sub state and unit-file enablement", Handle: systemdList},
		{Name: "services.unit.logs", Scope: "services.logs", Privilege: PrivUnprivilegedExec, Timeout: 15 * time.Second, Admission: AdmissionServicesRead,
			Description: "Recent journal entries of one listed unit (args: unit, lines 1-500)", Handle: unitLogs,
			AuditArgs: logsAuditArgs},
		// M2-2 (ADR 0011; D-2…D-5). Plan is read-only (unprivileged systemctl
		// show) and mints the token; apply is the only root-exec op.
		{Name: "services.unit.plan", Scope: "services.operate", Privilege: PrivUnprivilegedExec, Timeout: 10 * time.Second, Admission: AdmissionControlPlan,
			Description: "Review start/stop/restart of one .service unit: policy check, observed state, risk class and a single-use plan token (args: unit, action)",
			Handle:      unitPlan, AuditArgs: writeAuditArgs,
			Write: &WriteSpec{Phase: "plan", Risks: []string{RiskReversible, RiskDisruptive}, LockDomain: "service:<unit id>"}},
		{Name: "services.unit.apply", Scope: "services.operate", Privilege: PrivRootExec, Timeout: 55 * time.Second, Admission: AdmissionControlApply,
			Description: "Execute a reviewed start/stop/restart with its plan token; journaled, verified by reading the state back (args: unit, action, token)",
			Handle:      unitApply, AuditArgs: writeAuditArgs,
			Write: &WriteSpec{Phase: "apply", Risks: []string{RiskReversible, RiskDisruptive}, LockDomain: "service:<unit id>",
				Templates: []runner.RootTemplate{runner.RootSystemctlStart, runner.RootSystemctlStop, runner.RootSystemctlRestart, runner.RootSystemctlShow}}},
		// M2-3 read-only views (unprivileged children, fixed argv).
		{Name: "services.timers.list", Scope: "services.read", Privilege: PrivUnprivilegedExec, Timeout: 15 * time.Second, Admission: AdmissionServicesRead,
			Description: "systemd timer units: schedule, last and next run, activated unit", Handle: timersList},
		{Name: "services.sockets.list", Scope: "services.read", Privilege: PrivUnprivilegedExec, Timeout: 15 * time.Second, Admission: AdmissionServicesRead,
			Description: "systemd socket units: listen addresses and activated units", Handle: socketsList},
		{Name: "services.unit.detail", Scope: "services.read", Privilege: PrivUnprivilegedExec, Timeout: 10 * time.Second, Admission: AdmissionServicesRead,
			Description: "One unit from the inventory: identity, unit-file origin, dependencies, last exit and restart counter (args: unit)",
			Handle:      unitDetail, AuditArgs: viewAuditArgs},
		{Name: "services.unit.contents", Scope: "services.files", Privilege: PrivUnprivilegedExec, Timeout: 10 * time.Second, Admission: AdmissionServicesRead,
			Description: "Read-only, redacted and size-capped unit file and drop-ins of one unit, as the service account can read them (args: unit)",
			Handle:      unitContents, AuditArgs: viewAuditArgs},
		// M2-3 boot-time enable/disable (ADR 0011; docs/m2/M2-3-DESIGN-REVIEW.md).
		// Plan reads [Install] as root in-process; apply is root-exec.
		{Name: "services.enablement.plan", Scope: "services.configure", Privilege: PrivRootRead, Timeout: 15 * time.Second, Admission: AdmissionControlPlan,
			Description: "Review enabling or disabling one .service unit at boot: policy, [Install] links, risk and a single-use plan token (args: unit, action)",
			Handle:      unitEnablementPlan, AuditArgs: enablementAuditArgs,
			Write: &WriteSpec{Phase: "plan", Risks: []string{RiskDisruptive}, LockDomain: "service:<unit id>+unit-files"}},
		{Name: "services.enablement.apply", Scope: "services.configure", Privilege: PrivRootExec, Timeout: 55 * time.Second, Admission: AdmissionControlApply,
			Description: "Execute a reviewed boot-time enable/disable with its plan token (no --now, no reload); journaled, verified by is-enabled and the links (args: unit, action, token)",
			Handle:      unitEnablementApply, AuditArgs: enablementAuditArgs,
			Write: &WriteSpec{Phase: "apply", Risks: []string{RiskDisruptive}, LockDomain: "service:<unit id>+unit-files",
				Templates: []runner.RootTemplate{runner.RootSystemctlEnable, runner.RootSystemctlDisable, runner.RootSystemctlIsEnabled,
					runner.RootSystemctlShowInstall, runner.RootSystemctlShow, runner.RootSystemctlShowManager}}},
		// Owner 2026-11-21: systemctl/systemd functions are in the production
		// admin. One scope, closed templates, not a shell. services.operate
		// and services.configure do not imply it.
		{Name: "services.systemctl.plan", Scope: "services.systemctl", Privilege: PrivNone, Timeout: 10 * time.Second, Admission: AdmissionControlPlan,
			Description: "Review one closed systemctl verb (unit, drop-in or host). Nothing is changed (args: action and, when the verb needs them, unit, wants, name, content)",
			Handle:      systemctlPlan, AuditArgs: systemctlAuditArgs,
			Write: &WriteSpec{Phase: "plan", Risks: []string{RiskDisruptive}, LockDomain: "systemd-manager"}},
		{Name: "services.systemctl.apply", Scope: "services.systemctl", Privilege: PrivRootExec, Timeout: 55 * time.Second, Admission: AdmissionControlApply,
			Description: "Execute a reviewed systemctl verb with its plan token. File-level verbs do not reload PID 1. Host power and daemon-reload are their own verbs (args: action, token, and the same fields as the plan)",
			Handle:      systemctlApply, AuditArgs: systemctlAuditArgs,
			Write: &WriteSpec{Phase: "apply", Risks: []string{RiskDisruptive}, LockDomain: "systemd-manager",
				Templates: systemctlTemplates}},
		{Name: "operations.journal.list", Scope: "audit.read", Privilege: PrivRootRead, Timeout: 5 * time.Second, Admission: AdmissionSystemRead,
			Description: "The 100 most recent privileged operations from the broker journal, including interrupted ones", Handle: journalList},
		// M2-6 (ADR 0011 extension, proposed): hostname and timezone.
		// Plan is unprivileged hostnamectl/timedatectl status; apply is root-exec.
		{Name: "host.settings.plan", Scope: "host.settings.apply", Privilege: PrivUnprivilegedExec, Timeout: 10 * time.Second, Admission: AdmissionControlPlan,
			Description: "Review setting the static host name, pretty host name and/or time zone: observed hostnamectl/timedatectl values, reversible risk, TLS/mDNS//etc/hosts warning and a single-use plan token",
			Handle:      hostSettingsPlan, AuditArgs: hostSettingsAuditArgs,
			Write: &WriteSpec{Phase: "plan", Risks: []string{RiskReversible}, LockDomain: "host-settings"}},
		{Name: "host.settings.apply", Scope: "host.settings.apply", Privilege: PrivRootExec, Timeout: 55 * time.Second, Admission: AdmissionControlApply,
			Description: "Execute a reviewed hostname/timezone change with its plan token; journaled, verified by reading hostnamectl/timedatectl back (args: requested fields and token)",
			Handle:      hostSettingsApply, AuditArgs: hostSettingsAuditArgs,
			Write: &WriteSpec{Phase: "apply", Risks: []string{RiskReversible}, LockDomain: "host-settings",
				Templates: []runner.RootTemplate{runner.RootHostnamectlSetStatic, runner.RootHostnamectlSetPretty, runner.RootTimedatectlSetTimezone,
					runner.RootHostnamectlStatus, runner.RootTimedatectlStatus, runner.RootTimedatectlListTimezones}}},
		// M4-1b (ADR 0008 Amendment 5, Proposed): read-only Software Center
		// queries, cache-only, unprivileged closed argv templates. No write.
		{Name: "packages.installed", Scope: "packages.read", Privilege: PrivUnprivilegedExec, Timeout: 30 * time.Second, Admission: AdmissionPackages,
			Description: "Installed packages of one distribution family, from the local package database (args: family)", Handle: packagesInstalled},
		{Name: "packages.updates", Scope: "packages.read", Privilege: PrivUnprivilegedExec, Timeout: 30 * time.Second, Admission: AdmissionPackages,
			Description: "Pending upgrades with old and new versions from the local cache, labelled with the metadata refresh time (args: family)", Handle: packagesUpdates},
		{Name: "packages.sources", Scope: "packages.read", Privilege: PrivUnprivilegedExec, Timeout: 30 * time.Second, Admission: AdmissionPackages,
			Description: "Configured repositories and package sources with third-party and unsigned flags (args: family)", Handle: packagesSources},
		{Name: "packages.search", Scope: "packages.read", Privilege: PrivUnprivilegedExec, Timeout: 30 * time.Second, Admission: AdmissionPackages,
			Description: "Search the local package cache of one family (args: family, q)", Handle: packagesSearch, AuditArgs: packagesAuditArgs},
		// M4 (owner 2026-11-24): APT settings and mirror writes. Plans read
		// files only; applies send the reviewed change to hc-apt-apply.
		{Name: "packages.aptconfig.read", Scope: "packages.read", Privilege: PrivUnprivilegedExec, Timeout: 15 * time.Second, Admission: AdmissionPackages,
			Description: "Effective APT tunables (apt-config dump) and the Homelab Control managed settings file", Handle: aptConfigRead},
		{Name: "packages.aptconfig.plan", Scope: "packages.configure", Privilege: PrivNone, Timeout: 10 * time.Second, Admission: AdmissionControlPlan,
			Description: "Review writing or removing the managed APT settings file; nothing is changed (args: tunables or reset)",
			Handle:      aptConfigPlan, AuditArgs: aptConfigAuditArgs,
			Write: &WriteSpec{Phase: "plan", Risks: []string{RiskReversible}, LockDomain: aptFilesLock}},
		{Name: "packages.aptconfig.apply", Scope: "packages.configure", Privilege: PrivUnprivilegedExec, Timeout: 40 * time.Second, Admission: AdmissionControlApply,
			Description: "Write or remove the reviewed managed APT settings file through hc-apt-apply, then read the effective values back (args: tunables or reset, token)",
			Handle:      aptConfigApply, AuditArgs: aptConfigAuditArgs,
			Write: &WriteSpec{Phase: "apply", Risks: []string{RiskReversible}, LockDomain: aptFilesLock}},
		{Name: "packages.mirrors.inventory", Scope: "packages.read", Privilege: PrivNone, Timeout: 10 * time.Second, Admission: AdmissionPackages,
			Description: "Vendor APT source entries with their role (archive, security, ports, other) and whether their mirror can be changed", Handle: aptMirrorInventory},
		{Name: "packages.mirror.plan", Scope: "packages.configure", Privilege: PrivNone, Timeout: 10 * time.Second, Admission: AdmissionControlPlan,
			Description: "Review changing the archive mirror of one vendor source file; nothing is changed (args: file, from, to)",
			Handle:      aptMirrorPlan, AuditArgs: aptMirrorAuditArgs,
			Write: &WriteSpec{Phase: "plan", Risks: []string{RiskReversible}, LockDomain: aptFilesLock}},
		{Name: "packages.mirror.apply", Scope: "packages.configure", Privilege: PrivNone, Timeout: 40 * time.Second, Admission: AdmissionControlApply,
			Description: "Change the reviewed archive URI through hc-apt-apply; signing keys, suites and security entries are not touched (args: file, from, to, token)",
			Handle:      aptMirrorApply, AuditArgs: aptMirrorAuditArgs,
			Write: &WriteSpec{Phase: "apply", Risks: []string{RiskReversible}, LockDomain: aptFilesLock}},
		{Name: "packages.simulation", Scope: "packages.read", Privilege: PrivUnprivilegedExec, Timeout: 30 * time.Second, Admission: AdmissionPackages,
			Description: "Simulate one Debian/Ubuntu APT action without root, downloads or package writes", Handle: packagesSimulation, AuditArgs: packagesSimulationAuditArgs},
		// MergerFS: read-only snapraid status through the root helper socket
		// (SnapRAID locks the array for status). No client path, no --conf, no
		// sync/diff/fix/scrub/smart. Shares the services-read admission bucket.
		{Name: "mergerfs.snapraid.status", Scope: "mergerfs.read", Privilege: PrivRootRead, Timeout: 60 * time.Second, Admission: AdmissionServicesRead,
			Description: "Read snapraid status through the helper socket (argv exactly status; default configuration). Per-disk table and summary; the scrub graph and paths are omitted", Handle: snapraidStatus},
		// MergerFS writes go to the opt-in helper socket. The broker does not
		// mount and does not run snapraid sync/scrub/fix. mergerfs.read does
		// not grant these. configure and maintain do not grant each other.
		{Name: "mergerfs.pool.plan", Scope: "mergerfs.configure", Privilege: PrivNone, Timeout: 10 * time.Second, Admission: AdmissionControlPlan,
			Description: "Review mounting existing directories or saving an fstab line and snapraid.conf. Nothing is changed",
			Handle:      mergerfsPlan(mergerfsPoolPlan), AuditArgs: mergerfsAuditArgs,
			Write: &WriteSpec{Phase: "plan", Risks: []string{RiskDisruptive}, LockDomain: mergerfsArrayLock}},
		{Name: "mergerfs.pool.apply", Scope: "mergerfs.configure", Privilege: PrivRootRead, Timeout: 90 * time.Second, Admission: AdmissionControlApply,
			Description: "Mount a reviewed pool or save its definition through the helper socket. Disks are not formatted",
			Handle:      mergerfsApply(mergerfsPoolOp), AuditArgs: mergerfsAuditArgs,
			Write: &WriteSpec{Phase: "apply", Risks: []string{RiskDisruptive}, LockDomain: mergerfsArrayLock}},
		{Name: "mergerfs.maintain.plan", Scope: "mergerfs.maintain", Privilege: PrivNone, Timeout: 10 * time.Second, Admission: AdmissionControlPlan,
			Description: "Review snapraid sync, scrub, or fix -m -f. Nothing is changed",
			Handle:      mergerfsPlan(mergerfsKeepPlan), AuditArgs: mergerfsAuditArgs,
			Write: &WriteSpec{Phase: "plan", Risks: []string{RiskDisruptive}, LockDomain: mergerfsArrayLock}},
		{Name: "mergerfs.maintain.apply", Scope: "mergerfs.maintain", Privilege: PrivRootRead, Timeout: 90 * time.Second, Admission: AdmissionControlApply,
			Description: "Run a reviewed snapraid sync, scrub, or filtered recovery through the helper socket. Sync and scrub stop when a disk is missing or files were removed",
			Handle:      mergerfsApply(mergerfsMaintainOp), AuditArgs: mergerfsAuditArgs,
			Write: &WriteSpec{Phase: "apply", Risks: []string{RiskDisruptive}, LockDomain: mergerfsArrayLock}},
		// M2-4 O-1 (owner-approved implementation/isolated validation): the
		// broker is only a bounded client of the root-only socket-activated
		// probe helper. It enumerates block names and relays a closed request;
		// it never opens a device or runs blkid itself. The helper's native LVM
		// reader is the default LVM path (no lvs/pvs execution).
		{Name: "storage.probe", Scope: "storage.probe", Privilege: PrivRootRead, Timeout: helperOpTimeout(""), Admission: AdmissionProbe,
			Description: "On-demand read-only storage tools through hc-storage-probe: lsblk, smartctl (never wakes a disk in standby), pvs/lvs --readonly", Handle: storageProbe},
		// M7 ZFS: zpool list/status and zfs list through the read-only helper
		// (owner 2026-11-25). Module "zfs" gates it by the zfs. prefix.
		{Name: "zfs.status", Scope: "zfs.read", Privilege: PrivRootRead, Timeout: helperOpTimeout(storagetools.ScopeZFS), Admission: AdmissionSystemRead,
			Description: "OpenZFS pools (health, capacity, scrub, vdev tree with error counters) and datasets: zpool list/status and zfs list through hc-storage-probe", Handle: zfsStatus},
		// M7 Btrfs: filesystem usage, device stats, scrub status and subvolume
		// list per mounted filesystem through the read-only helper (owner
		// 2026-11-25). Module "btrfs" gates it by the btrfs. prefix.
		{Name: "btrfs.status", Scope: "btrfs.read", Privilege: PrivRootRead, Timeout: helperOpTimeout(storagetools.ScopeBtrfs), Admission: AdmissionSystemRead,
			Description: "Btrfs filesystems (allocation by profile and device, device error counters, scrub status, subvolumes): btrfs-progs through hc-storage-probe", Handle: btrfsStatus},
		// M7 Software RAID: mdadm --detail for each array in /proc/mdstat
		// through the read-only helper (owner 2026-11-25). Module "mdadm"
		// gates it by the mdadm. prefix.
		{Name: "mdadm.status", Scope: "mdadm.read", Privilege: PrivRootRead, Timeout: helperOpTimeout(storagetools.ScopeMd), Admission: AdmissionSystemRead,
			Description: "Software RAID arrays (state, active/failed/spare counts, rebuild progress, member roles): mdadm --detail through hc-storage-probe", Handle: mdadmStatus},
		// M7 LVM: pvs, lvs and vgs through the read-only helper (owner
		// 2026-11-25). Module "lvm" gates it by the lvm. prefix.
		{Name: "lvm.status", Scope: "lvm.read", Privilege: PrivRootRead, Timeout: helperOpTimeout(storagetools.ScopeLVM), Admission: AdmissionSystemRead,
			Description: "LVM volume groups, logical volumes and physical volumes: pvs, lvs and vgs --readonly --nolocking through hc-storage-probe", Handle: lvmStatus},
		// M7 ext4 and XFS: tune2fs -l and xfs_info through the read-only
		// helper. Modules "ext4" and "xfs" gate them by prefix.
		{Name: "ext4.status", Scope: "ext4.read", Privilege: PrivRootRead, Timeout: helperOpTimeout(storagetools.ScopeExt4), Admission: AdmissionSystemRead,
			Description: "Mounted ext4 superblocks (state, error record, mount counts, features, block and inode counts): tune2fs -l through hc-storage-probe", Handle: ext4Status},
		{Name: "xfs.status", Scope: "xfs.read", Privilege: PrivRootRead, Timeout: helperOpTimeout(storagetools.ScopeXFS), Admission: AdmissionSystemRead,
			Description: "Mounted XFS geometry and features: xfs_info (xfs_spaceman -c info) through hc-storage-probe", Handle: xfsStatus},
		// M11 (ADR 0008 Amendment 9, Proposed): read-only networking. In-process
		// root reads of fixed /proc and config paths; the firewall op also runs
		// one fixed unprivileged systemctl show. No live ruleset, no writes.
		{Name: "network.ports.list", Scope: "networking.ports.read", Privilege: PrivRootRead, Timeout: 10 * time.Second, Admission: AdmissionSystemRead,
			Description: "Host-wide listening TCP and bound UDP sockets of the host network namespace with owning account, process, unit and container", Handle: networkPortsList},
		{Name: "network.firewall.config", Scope: "networking.firewall.read", Privilege: PrivRootRead, Timeout: 10 * time.Second, Admission: AdmissionSystemRead,
			Description: "Active firewall backend (firewalld, ufw, nftables or iptables) and its saved configuration; the live kernel ruleset is not read", Handle: networkFirewallConfig},
		// ext4 format (owner 2026-11-23): the closed mkfs.ext4 template is
		// this pair's apply, and only this pair's apply. Quiet, one by-id
		// target, no -F. ext4.read does not grant it. The main broker unit
		// stays PrivateDevices=yes; a broker that cannot see the device
		// refuses before exec.
		{Name: "ext4.format.plan", Scope: "ext4.format", Privilege: PrivRootRead, Timeout: 15 * time.Second, Admission: AdmissionControlPlan,
			Description: "Review a quiet ext4 format of one by-id disk: persist identity, block device, unmounted, no holders, destructive risk and a single-use plan token. Nothing is formatted",
			Handle:      ext4FormatPlan, AuditArgs: ext4FormatAuditArgs,
			Write: &WriteSpec{Phase: "plan", Risks: []string{RiskDestructive}, LockDomain: "ext4-format:<identity>+ext4-format-dev:<rdev>"}},
		{Name: "ext4.format.apply", Scope: "ext4.format", Privilege: PrivRootExec, Timeout: 9 * time.Minute, Admission: AdmissionControlApply,
			Description: "Format one reviewed by-id disk with mkfs.ext4 -q. Requires the plan token, password re-entry, and a fresh block-device check immediately before exec. No -F",
			Handle:      ext4FormatApply, AuditArgs: ext4FormatAuditArgs,
			Write: &WriteSpec{Phase: "apply", Risks: []string{RiskDestructive}, LockDomain: "ext4-format:<identity>+ext4-format-dev:<rdev>",
				Templates: []runner.RootTemplate{runner.RootMkfsExt4}}},
		// GPT partition write. The script is generated, not caller argv.
		// The device-number lock is shared with ext4 format.
		{Name: "storage.partition.plan", Scope: "storage.partition", Privilege: PrivRootRead, Timeout: 15 * time.Second, Admission: AdmissionControlPlan,
			Description: "Review a GPT partition of one whole by-id disk. Nothing is partitioned or formatted",
			Handle:      partApplyPlan, AuditArgs: partApplyAuditArgs,
			Write: &WriteSpec{Phase: "plan", Risks: []string{RiskDestructive}, LockDomain: "storage-partition:<identity>+ext4-format-dev:<rdev>"}},
		{Name: "storage.partition.apply", Scope: "storage.partition", Privilege: PrivRootExec, Timeout: 3 * time.Minute, Admission: AdmissionControlApply,
			Description: "Write one reviewed GPT layout with sfdisk. Requires the plan token, password re-entry, and a fresh block-device check immediately before exec. No format",
			Handle:      partApplyApply, AuditArgs: partApplyAuditArgs,
			Write: &WriteSpec{Phase: "apply", Risks: []string{RiskDestructive}, LockDomain: "storage-partition:<identity>+ext4-format-dev:<rdev>",
				Templates: []runner.RootTemplate{runner.RootSfdiskGPT}}},
	}
	m := make(map[string]Op, len(ops))
	for _, o := range ops {
		if !proto.ValidOpName(o.Name) {
			panic("invalid op name " + o.Name)
		}
		m[o.Name] = o
	}
	return m
}

// noArgs is the typed argument struct for argument-free ops: any field is rejected.
type noArgs struct{}

func requireNoArgs(raw json.RawMessage) error {
	var a noArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return &proto.Error{Code: proto.CodeBadRequest, Message: "this operation takes no arguments"}
	}
	return nil
}

type infoResult struct {
	Version  string   `json:"version"`
	Protocol int      `json:"protocol"`
	Ops      []OpInfo `json:"ops"`
	Exec     []string `json:"exec_available"`
	// SystemdBooted mirrors sd_booted(): /run/systemd/system exists only when
	// systemd is PID 1. systemctl may be installed on hosts that do not run it.
	SystemdBooted bool `json:"systemd_booted"`
	// WritesAvailable is false when the operation journal could not be opened.
	WritesAvailable bool `json:"writes_available"`
	// DisabledModules lists modules the administrator disabled; their ops are
	// omitted from Ops and refused.
	DisabledModules []string `json:"disabled_modules,omitempty"`
	ModulesReason   string   `json:"modules_reason,omitempty"`
}

func brokerInfo(_ context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	res := infoResult{Version: env.Version, Protocol: proto.Version}
	res.DisabledModules, res.ModulesReason = env.Modules.Disabled(), env.Modules.Reason
	for _, o := range Registry() {
		if !env.Modules.Enabled(modules.ForOp(o.Name)) {
			continue
		}
		res.Ops = append(res.Ops, OpInfo{Name: o.Name, Scope: o.Scope, Privilege: o.Privilege, Description: o.Description})
	}
	sort.Slice(res.Ops, func(i, j int) bool { return res.Ops[i].Name < res.Ops[j].Name })
	res.WritesAvailable = env.Writes != nil
	for _, b := range []string{"systemctl", "journalctl", "hostnamectl", "timedatectl", "dpkg-query", "apt-cache", "apt", "rpm", "dnf", "zypper", "pacman"} {
		if env.Runner != nil && env.Runner.Available(b) {
			res.Exec = append(res.Exec, b)
		}
	}
	if fi, err := os.Stat(env.Roots.Path("/run/systemd/system")); err == nil && fi.IsDir() {
		res.SystemdBooted = true
	}
	return res, nil
}

// readFixed reads a fixed path without following a final symlink, requiring a
// regular file, bounded by limit bytes.
func readFixed(path string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, limit)
	}
	return b, nil
}

func isNotExist(err error) bool { return errors.Is(err, os.ErrNotExist) }

func clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if len(s) > max {
		s = s[:max]
	}
	return s
}
