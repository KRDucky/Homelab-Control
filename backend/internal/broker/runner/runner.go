// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package runner is the ONLY place in the code base allowed to execute
// programs (enforced by internal/guard). It runs allow-listed binaries with
// argv fixed by the calling operation, a replaced environment, stdin closed,
// size-capped output, a timeout that kills the whole process group, and
// as the unprivileged service user. An op may add supplementary groups (e.g.
// the journal reader group); the uid stays the service user.
//
// One exception exists (ADR 0008 Amendment 2, owner decision D-4): the
// root-exec class. A Root handle runs one of a closed set of argv templates
// that are fixed in this file (RootTemplate); the caller supplies only the
// template ID and a target that the template's own validator accepts. There
// is no way to pass a binary, flags or free-form arguments to a root child.
// The broker hands a Root handle only to ops registered as root-exec.
// Stdin stays closed except for systemctl.edit, which reads one size-capped
// drop-in and still cannot take a free-form argument.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"homelab-control/backend/internal/hostinfo"
)

// Binary names the runner may execute, with candidate absolute paths.
// Adding an entry requires security review (ADR 0008).
var allowlist = map[string][]string{
	"systemctl":   {"/usr/bin/systemctl", "/bin/systemctl"},
	"journalctl":  {"/usr/bin/journalctl", "/bin/journalctl"},
	"hostnamectl": {"/usr/bin/hostnamectl", "/bin/hostnamectl"},
	"timedatectl": {"/usr/bin/timedatectl", "/bin/timedatectl"},
	// M4-1b (ADR 0008 Amendment 5, Proposed): read-only package-manager
	// queries, cache-only, closed argv templates. No write command here.
	"dpkg-query": {"/usr/bin/dpkg-query"},
	"apt-cache":  {"/usr/bin/apt-cache"},
	"apt":        {"/usr/bin/apt"},
	"rpm":        {"/usr/bin/rpm", "/bin/rpm"},
	"dnf":        {"/usr/bin/dnf"},
	"zypper":     {"/usr/bin/zypper"},
	"pacman":     {"/usr/bin/pacman", "/usr/bin/pacman.static"},
	// ext4.format.apply is the only caller. Quiet, no -F.
	"mkfs.ext4": {"/usr/sbin/mkfs.ext4", "/sbin/mkfs.ext4"},
	// MergerFS SnapRAID status (ADR 0008): only mergerfs.snapraid.status calls
	// this binary, and only with argv "status". No sync, scrub, fix, diff, or smart.
	"snapraid": {"/usr/bin/snapraid", "/usr/sbin/snapraid"},
}

// argRE limits fixed arguments to a conservative character set; arguments
// are compile-time constants in ops, this is a second line of defence. A bare
// "--" (end of options, before unit names) is allowed.
var argRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9=,._:@/+-]*$|^--?[A-Za-z0-9][A-Za-z0-9=,._:@/+-]*$|^--$`)

// RootTemplate identifies one fixed root argv template. Adding a template
// requires security review (ADR 0008 Amendment 2) and the registry snapshot
// test in this package to be updated.
type RootTemplate string

// The root templates approved for M2-2: systemd service start/stop/restart,
// and the read-only state query that services.unit.apply uses to revalidate
// and to verify the outcome (so that its decision does not rest on a child
// process running as the same uid as hc-api, which hc-api could tamper with).
const (
	RootSystemctlStart   RootTemplate = "systemctl.start"
	RootSystemctlStop    RootTemplate = "systemctl.stop"
	RootSystemctlRestart RootTemplate = "systemctl.restart"
	RootSystemctlShow    RootTemplate = "systemctl.show"
)

// The root templates approved for M2-3 (ADR 0008 Amendment 3): boot-time
// enable/disable of one .service unit WITHOUT reloading the manager
// (--no-reload: no implicit global daemon-reload) and WITHOUT --now, plus two
// read-only queries apply uses to revalidate and to verify the outcome:
// is-enabled (computed from disk, fresh after --no-reload) and the fixed
// unit-file property list.
const (
	RootSystemctlEnable      RootTemplate = "systemctl.enable"
	RootSystemctlDisable     RootTemplate = "systemctl.disable"
	RootSystemctlIsEnabled   RootTemplate = "systemctl.is-enabled"
	RootSystemctlShowInstall RootTemplate = "systemctl.show-install"
	// RootSystemctlShowManager reads when the manager last loaded its unit
	// files (no target; PR #36 review, probe 8).
	RootSystemctlShowManager RootTemplate = "systemctl.show-manager"
)

// The root templates proposed for M2-6 (ADR 0008 Amendment 4): set static
// hostname, pretty hostname (empty = remove) and timezone, plus the read-only
// status and list-timezones queries apply uses to revalidate and read back.
const (
	RootHostnamectlSetStatic     RootTemplate = "hostnamectl.set-static"
	RootHostnamectlSetPretty     RootTemplate = "hostnamectl.set-pretty"
	RootHostnamectlStatus        RootTemplate = "hostnamectl.status"
	RootTimedatectlSetTimezone   RootTemplate = "timedatectl.set-timezone"
	RootTimedatectlStatus        RootTemplate = "timedatectl.status"
	RootTimedatectlListTimezones RootTemplate = "timedatectl.list-timezones"
)

// Closed mkfs.ext4 template. ext4.format.apply is the only caller.
// Quiet only; no -F. The argv stays one by-id target.
const (
	RootMkfsExt4 RootTemplate = "mkfs.ext4"
)

// Closed systemctl templates for services.systemctl (owner 2026-11-21).
// Unit templates accept every unit type validQueryUnit allows. They do not
// replace systemctl.start/stop/restart/enable/disable, which stay .service
// only. Host templates take no target. systemctl.edit reads stdin.
const (
	RootSystemctlStartUnit       RootTemplate = "systemctl.start-unit"
	RootSystemctlStopUnit        RootTemplate = "systemctl.stop-unit"
	RootSystemctlRestartUnit     RootTemplate = "systemctl.restart-unit"
	RootSystemctlReload          RootTemplate = "systemctl.reload"
	RootSystemctlTryRestart      RootTemplate = "systemctl.try-restart"
	RootSystemctlReloadOrRestart RootTemplate = "systemctl.reload-or-restart"
	RootSystemctlKill            RootTemplate = "systemctl.kill"
	RootSystemctlResetFailed     RootTemplate = "systemctl.reset-failed"
	RootSystemctlMask            RootTemplate = "systemctl.mask"
	RootSystemctlUnmask          RootTemplate = "systemctl.unmask"
	RootSystemctlReenable        RootTemplate = "systemctl.reenable"
	RootSystemctlPreset          RootTemplate = "systemctl.preset"
	RootSystemctlIsolate         RootTemplate = "systemctl.isolate"
	RootSystemctlRevert          RootTemplate = "systemctl.revert"
	RootSystemctlSetDefault      RootTemplate = "systemctl.set-default"
	RootSystemctlFreeze          RootTemplate = "systemctl.freeze"
	RootSystemctlThaw            RootTemplate = "systemctl.thaw"
	RootSystemctlEnableUnit      RootTemplate = "systemctl.enable-unit"
	RootSystemctlDisableUnit     RootTemplate = "systemctl.disable-unit"
	RootSystemctlDaemonReload    RootTemplate = "systemctl.daemon-reload"
	RootSystemctlDaemonReexec    RootTemplate = "systemctl.daemon-reexec"
	RootSystemctlPoweroff        RootTemplate = "systemctl.poweroff"
	RootSystemctlReboot          RootTemplate = "systemctl.reboot"
	RootSystemctlHalt            RootTemplate = "systemctl.halt"
	RootSystemctlSuspend         RootTemplate = "systemctl.suspend"
	RootSystemctlHibernate       RootTemplate = "systemctl.hibernate"
	RootSystemctlHybridSleep     RootTemplate = "systemctl.hybrid-sleep"
	RootSystemctlKexec           RootTemplate = "systemctl.kexec"
	RootSystemctlSoftReboot      RootTemplate = "systemctl.soft-reboot"
	RootSystemctlResetFailedAll  RootTemplate = "systemctl.reset-failed-all"
	RootSystemctlAddWants        RootTemplate = "systemctl.add-wants"
	RootSystemctlAddRequires     RootTemplate = "systemctl.add-requires"
	RootSystemctlEdit            RootTemplate = "systemctl.edit"
)

// managerShowProperties are the manager's unit-load timestamps: realtime
// (printed in UTC with microseconds) and monotonic. UnitsLoadTimestamp
// (systemd >= 255) is set at every reload; UnitsLoadStartTimestamp at boot.
// UnitPath is the manager's unit search path, in precedence order (final
// review: checked against the documented load path instead of assumed).
var managerShowProperties = []string{
	"UnitsLoadStartTimestamp", "UnitsLoadStartTimestampMonotonic",
	"UnitsLoadTimestamp", "UnitsLoadTimestampMonotonic", "UnitPath",
}

// ManagerShowArgs is the fixed manager query (no unit).
func ManagerShowArgs() []string {
	a := []string{"show", "--no-pager", "--timestamp=us+utc"}
	for _, p := range managerShowProperties {
		a = append(a, "--property="+p)
	}
	return a
}

// installShowProperties is the fixed property list of the unit-file query
// (identity and where the unit's definition comes from).
var installShowProperties = []string{
	"Id", "Names", "LoadState", "FragmentPath", "SourcePath", "DropInPaths",
	"Transient", "NeedDaemonReload", "UnitFilePreset",
}

// ShowInstallArgs is the argv (without the binary) of the unit-file query.
func ShowInstallArgs(units ...string) []string {
	a := []string{"show", "--no-pager"}
	for _, p := range installShowProperties {
		a = append(a, "--property="+p)
	}
	return append(append(a, "--"), units...)
}

// IsEnabledArgs is the argv (without the binary) of the enablement query.
func IsEnabledArgs(unit string) []string { return []string{"is-enabled", "--", unit} }

type rootTemplate struct {
	binary     string   // allow-list key
	argv       []string // fixed arguments; the validated targets follow "--"
	valid      func(target string) bool
	maxTargets int
}

// templateFlags holds the few templates that are not "targets after --".
// Kept off rootTemplate so the closed argv rows stay a 4-field literal.
type templateFlags struct {
	// leadPrefix turns targets[0] into one flag inserted before "--".
	leadPrefix string
	leadValid  func(string) bool
	// readsStdin is set only for systemctl.edit.
	readsStdin bool
	// exact requires len(targets) == maxTargets. The default allows 1..max
	// so systemctl.show can take up to 32 units.
	exact bool
}

// systemctlShowProperties is the fixed property list of every `systemctl
// show` the broker runs (root or unprivileged): identity, state, safety
// properties, dependencies and the *Action= properties.
var systemctlShowProperties = []string{
	"Id", "Names", "Description", "LoadState", "ActiveState", "SubState", "Result",
	"UnitFileState", "FragmentPath", "MainPID", "ExecMainStatus", "InvocationID", "Type",
	"StateChangeTimestamp", "RefuseManualStart", "RefuseManualStop", "CanStart", "CanStop",
	"OnFailureJobMode", "OnSuccessJobMode", "StopWhenUnneeded", "DropInPaths",
	"Requires", "Requisite", "Wants", "BindsTo", "Upholds", "PartOf",
	"Conflicts", "ConflictedBy", "RequiredBy", "RequisiteOf", "BoundBy",
	"ConsistsOf", "PropagatesStopTo", "OnFailure", "OnSuccess", "Triggers",
	"FailureAction", "SuccessAction", "StartLimitAction", "JobTimeoutAction",
}

// SystemctlShowProperties returns a copy of the fixed property list.
func SystemctlShowProperties() []string { return append([]string{}, systemctlShowProperties...) }

// SystemctlShowArgs is the argv (without the binary) of the state query for
// units; the unprivileged path uses the same argv as the root template.
func SystemctlShowArgs(units ...string) []string {
	a := []string{"show", "--no-pager"}
	for _, p := range systemctlShowProperties {
		a = append(a, "--property="+p)
	}
	return append(append(a, "--"), units...)
}

// serviceUnitRE mirrors servicepolicy.UnitRE (kept here, independently, as the
// runner's own last line of defence): a .service unit, no leading '-', no
// escapes, separators or whitespace.
var serviceUnitRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.@-]{0,99}\.service$`)

func validServiceUnit(t string) bool {
	return serviceUnitRE.MatchString(t) && !strings.Contains(t, "@.service") && !strings.Contains(t, "..")
}

// rootTemplates is the closed table of root children. --no-ask-password: never
// wait for an agent; --job-mode=fail: fail instead of replacing another queued
// job on the unit (a concurrent change made elsewhere is not overridden).
var rootTemplates = map[RootTemplate]rootTemplate{
	RootSystemctlStart:   {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "start", "--"}, validServiceUnit, 1},
	RootSystemctlStop:    {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "stop", "--"}, validServiceUnit, 1},
	RootSystemctlRestart: {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "restart", "--"}, validServiceUnit, 1},
	// Read-only: SystemctlShowArgs (ending in "--"), then up to 32 units.
	RootSystemctlShow: {"systemctl", SystemctlShowArgs(), validQueryUnit, 32},
	// M2-3: persistent boot-time enablement, one non-instance .service unit.
	RootSystemctlEnable:      {"systemctl", []string{"--no-ask-password", "--no-reload", "enable", "--"}, validEnablementUnit, 1},
	RootSystemctlDisable:     {"systemctl", []string{"--no-ask-password", "--no-reload", "disable", "--"}, validEnablementUnit, 1},
	RootSystemctlIsEnabled:   {"systemctl", []string{"is-enabled", "--"}, validServiceUnit, 1},
	RootSystemctlShowInstall: {"systemctl", ShowInstallArgs(), validServiceUnit, 1},
	// Exactly zero targets.
	RootSystemctlShowManager: {"systemctl", ManagerShowArgs(), nil, 0},
	// M2-6 (ADR 0008 Amendment 4, proposed): hostname and timezone. Values
	// after --; empty pretty hostname is removal. Status and list-timezones
	// are read-only (revalidation and read-back).
	RootHostnamectlSetStatic:     {"hostnamectl", []string{"--no-ask-password", "set-hostname", "--static", "--"}, validStaticHostname, 1},
	RootHostnamectlSetPretty:     {"hostnamectl", []string{"--no-ask-password", "set-hostname", "--pretty", "--"}, validPrettyHostname, 1},
	RootHostnamectlStatus:        {"hostnamectl", []string{"--no-ask-password", "status"}, nil, 0},
	RootTimedatectlSetTimezone:   {"timedatectl", []string{"--no-ask-password", "set-timezone", "--"}, validTimezoneName, 1},
	RootTimedatectlStatus:        {"timedatectl", []string{"--no-ask-password", "status"}, nil, 0},
	RootTimedatectlListTimezones: {"timedatectl", []string{"--no-ask-password", "list-timezones"}, nil, 0},
	// Quiet ext4 format of one by-id path. Called only by ext4.format.apply.
	RootMkfsExt4: {"mkfs.ext4", []string{"-q", "--"}, validStorageByID, 1},
	// services.systemctl. Job-mode verbs queue one job and fail if another is
	// already queued. File-level verbs pass --no-reload so they do not reload
	// PID 1; daemon-reload is its own template. No --now, --force, --runtime,
	// or --user.
	RootSystemctlStartUnit:       {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "start", "--"}, validQueryUnit, 1},
	RootSystemctlStopUnit:        {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "stop", "--"}, validQueryUnit, 1},
	RootSystemctlRestartUnit:     {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "restart", "--"}, validQueryUnit, 1},
	RootSystemctlReload:          {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "reload", "--"}, validQueryUnit, 1},
	RootSystemctlTryRestart:      {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "try-restart", "--"}, validQueryUnit, 1},
	RootSystemctlReloadOrRestart: {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "reload-or-restart", "--"}, validQueryUnit, 1},
	RootSystemctlKill:            {"systemctl", []string{"--no-ask-password", "kill", "--signal=SIGTERM", "--"}, validQueryUnit, 1},
	RootSystemctlResetFailed:     {"systemctl", []string{"--no-ask-password", "reset-failed", "--"}, validQueryUnit, 1},
	RootSystemctlFreeze:          {"systemctl", []string{"--no-ask-password", "freeze", "--"}, validQueryUnit, 1},
	RootSystemctlThaw:            {"systemctl", []string{"--no-ask-password", "thaw", "--"}, validQueryUnit, 1},
	RootSystemctlIsolate:         {"systemctl", []string{"--no-ask-password", "isolate", "--"}, validQueryUnit, 1},
	RootSystemctlMask:            {"systemctl", []string{"--no-ask-password", "--no-reload", "mask", "--"}, validQueryPlain, 1},
	RootSystemctlUnmask:          {"systemctl", []string{"--no-ask-password", "--no-reload", "unmask", "--"}, validQueryPlain, 1},
	RootSystemctlReenable:        {"systemctl", []string{"--no-ask-password", "--no-reload", "reenable", "--"}, validQueryPlain, 1},
	RootSystemctlPreset:          {"systemctl", []string{"--no-ask-password", "--no-reload", "preset", "--"}, validQueryPlain, 1},
	RootSystemctlRevert:          {"systemctl", []string{"--no-ask-password", "--no-reload", "revert", "--"}, validQueryPlain, 1},
	RootSystemctlSetDefault:      {"systemctl", []string{"--no-ask-password", "set-default", "--"}, validQueryPlain, 1},
	RootSystemctlEnableUnit:      {"systemctl", []string{"--no-ask-password", "--no-reload", "enable", "--"}, validQueryPlain, 1},
	RootSystemctlDisableUnit:     {"systemctl", []string{"--no-ask-password", "--no-reload", "disable", "--"}, validQueryPlain, 1},
	RootSystemctlDaemonReload:    {"systemctl", []string{"--no-ask-password", "daemon-reload"}, nil, 0},
	RootSystemctlDaemonReexec:    {"systemctl", []string{"--no-ask-password", "daemon-reexec"}, nil, 0},
	RootSystemctlPoweroff:        {"systemctl", []string{"--no-ask-password", "poweroff"}, nil, 0},
	RootSystemctlReboot:          {"systemctl", []string{"--no-ask-password", "reboot"}, nil, 0},
	RootSystemctlHalt:            {"systemctl", []string{"--no-ask-password", "halt"}, nil, 0},
	RootSystemctlSuspend:         {"systemctl", []string{"--no-ask-password", "suspend"}, nil, 0},
	RootSystemctlHibernate:       {"systemctl", []string{"--no-ask-password", "hibernate"}, nil, 0},
	RootSystemctlHybridSleep:     {"systemctl", []string{"--no-ask-password", "hybrid-sleep"}, nil, 0},
	RootSystemctlKexec:           {"systemctl", []string{"--no-ask-password", "kexec"}, nil, 0},
	RootSystemctlSoftReboot:      {"systemctl", []string{"--no-ask-password", "soft-reboot"}, nil, 0},
	RootSystemctlResetFailedAll:  {"systemctl", []string{"--no-ask-password", "reset-failed"}, nil, 0},
	RootSystemctlAddWants:        {"systemctl", []string{"--no-ask-password", "--no-reload", "add-wants", "--"}, validQueryPlain, 2},
	RootSystemctlAddRequires:     {"systemctl", []string{"--no-ask-password", "--no-reload", "add-requires", "--"}, validQueryPlain, 2},
	RootSystemctlEdit:            {"systemctl", []string{"--no-ask-password", "--no-reload", "edit", "--stdin", "--"}, validQueryPlain, 2},
}

// templateFlagsByID is empty for every template that is only argv plus targets.
var templateFlagsByID = map[RootTemplate]templateFlags{
	RootSystemctlAddWants:    {exact: true},
	RootSystemctlAddRequires: {exact: true},
	RootSystemctlEdit:        {leadPrefix: "--drop-in=", leadValid: validDropInName, readsStdin: true, exact: true},
}

func validStaticHostname(t string) bool { return hostinfo.ValidStaticHostname(t) == "" }

func validPrettyHostname(t string) bool { return hostinfo.ValidPrettyHostname(t) == "" }

func validTimezoneName(t string) bool { return hostinfo.ValidTimezoneName(t) == "" }

// validEnablementUnit additionally refuses template instances: enabling an
// instance acts on the template's [Install] section (M2-3 does not support it).
func validEnablementUnit(t string) bool { return validServiceUnit(t) && !strings.Contains(t, "@") }

// queryUnitRE is the runner's own grammar for state-query targets: any unit
// type, no leading '-', no escapes, separators or whitespace.
var queryUnitRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.@-]{0,99}\.(service|socket|timer|mount|automount|path|target|swap|slice|scope|device)$`)

func validQueryUnit(t string) bool {
	return queryUnitRE.MatchString(t) && !strings.Contains(t, "..") && !strings.Contains(t, "@.")
}

// validQueryPlain is validQueryUnit without a template or instance marker.
// Enablement, mask and drop-in targets use it. Instances stay on the job verbs.
func validQueryPlain(t string) bool {
	return validQueryUnit(t) && !strings.Contains(t, "@")
}

// dropInNameRE is the only file name systemctl.edit may write. It is a single
// path segment, not a path.
var dropInNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,40}\.conf$`)

func validDropInName(t string) bool { return dropInNameRE.MatchString(t) }

// storageByIDRE is the exec-time discovery path for a storage root template.
// Persist/write identity stays wwn / nvme-uuid / serial; kernel names, mapper
// names, md/lvm/dm, by-path, and path traversal are refused. virtio- is for
// guest MMV disks. Optional -partN is a udev partition symlink.
var storageByIDRE = regexp.MustCompile(`^/dev/disk/by-id/(wwn-0x[0-9a-fA-F]{16}|wwn-0x[0-9a-fA-F]{32}|nvme-eui\.[0-9a-fA-F]{16}|nvme-uuid\.[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|ata-[A-Za-z0-9._-]+|scsi-[A-Za-z0-9._-]+|virtio-[A-Za-z0-9._-]+)(-part[0-9]{1,3})?$`)

func validStorageByID(t string) bool {
	if len(t) < 20 || len(t) > 128 {
		return false
	}
	if strings.Contains(t, "..") || strings.Contains(t, "//") || strings.ContainsAny(t, " \t\n\r\x00") {
		return false
	}
	return storageByIDRE.MatchString(t)
}

// Identity is the uid/gid children run as.
type Identity struct {
	UID, GID uint32
}

// Runner executes allow-listed binaries.
type Runner struct {
	paths     map[string]string
	identity  Identity
	maxStdout int
	maxStderr int
	// skipOwnerCheck is only for tests running as non-root against a
	// fixture binary; it cannot be set outside this package.
	skipOwnerCheck bool
}

// Errors returned by Run.
var (
	ErrNotAllowed = errors.New("binary not in allow-list")
	ErrBadArg     = errors.New("argument rejected")
	ErrTooLarge   = errors.New("output exceeded limit")
	ErrTimeout    = errors.New("timed out")
)

// New resolves the allow-list. Binaries that are missing are simply
// unavailable; binaries that exist but are unsafe (not root-owned, or
// group/world-writable) are an error.
func New(id Identity) (*Runner, error) {
	r := &Runner{paths: map[string]string{}, identity: id, maxStdout: 4 << 20, maxStderr: 64 << 10}
	for name, cands := range allowlist {
		for _, c := range cands {
			p, err := filepath.EvalSymlinks(c)
			if err != nil {
				continue
			}
			if err := checkBinary(p, false); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			r.paths[name] = p
			break
		}
	}
	return r, nil
}

func checkBinary(p string, skipOwner bool) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", p)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is group/world-writable", p)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Uid != 0 && !skipOwner {
		return fmt.Errorf("%s is not owned by root", p)
	}
	return nil
}

// Available reports whether a binary was found.
func (r *Runner) Available(name string) bool { _, ok := r.paths[name]; return ok }

type capped struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.limit {
		c.over = true
		return 0, ErrTooLarge
	}
	return c.buf.Write(p)
}

// Result of a run.
type Result struct {
	Stdout   []byte
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Run executes name with args. ctx should carry the op deadline.
func (r *Runner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return r.RunWithGroups(ctx, nil, name, args...)
}

// RunWithGroups is Run with additional supplementary groups for the child
// (still the unprivileged service uid/gid; never root).
func (r *Runner) RunWithGroups(ctx context.Context, groups []uint32, name string, args ...string) (Result, error) {
	for _, g := range groups {
		if g == 0 {
			return Result{}, fmt.Errorf("group 0: %w", ErrBadArg)
		}
	}
	path, ok := r.paths[name]
	if !ok {
		if _, known := allowlist[name]; known {
			return Result{}, fmt.Errorf("%s: %w", name, os.ErrNotExist)
		}
		return Result{}, fmt.Errorf("%q: %w", name, ErrNotAllowed)
	}
	for _, a := range args {
		if !argRE.MatchString(a) || len(a) > 128 {
			return Result{}, fmt.Errorf("%q: %w", a, ErrBadArg)
		}
	}
	if err := checkBinary(path, r.skipOwnerCheck); err != nil { // re-check at use time
		return Result{}, err
	}
	return r.exec(ctx, path, args, &syscall.Credential{Uid: r.identity.UID, Gid: r.identity.GID, Groups: append([]uint32{}, groups...)}, nil)
}

// exec runs path with args. cred is the child identity when the broker runs
// as root; nil means "run as root" and is only passed by Root. stdin nil
// is /dev/null. A non-nil body is only used by systemctl.edit.
func (r *Runner) exec(ctx context.Context, path string, args []string, cred *syscall.Credential, stdin []byte) (Result, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"LC_ALL=C.UTF-8",
		"HOME=/nonexistent",
		"SYSTEMD_COLORS=0",
		"SYSTEMD_PAGER=",
		"SYSTEMD_LESS=",
		"TERM=dumb",
	}
	cmd.Dir = "/"
	if stdin == nil {
		cmd.Stdin = nil // /dev/null
	} else {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out := &capped{limit: r.maxStdout}
	errb := &capped{limit: r.maxStderr}
	cmd.Stdout, cmd.Stderr = out, errb
	cmd.SysProcAttr = childSysProcAttr(cred)
	// kill(-pid) targets the child's process group. After Setsid the
	// session id and pgid both equal the child's pid (setsid(2)), so this
	// is the same W9 contract as Setpgid: true.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	start := time.Now()
	err := cmd.Run()
	res := Result{Stdout: out.buf.Bytes(), Stderr: errb.buf.String(), Duration: time.Since(start)}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	switch {
	case out.over || errb.over:
		return res, ErrTooLarge
	case ctx.Err() != nil:
		return res, ErrTimeout
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return res, nil // non-zero exit is reported via ExitCode
		}
		return res, &ExecError{Class: errnoClass(err), err: err}
	}
	return res, nil
}

// childSysProcAttr is the SysProcAttr of every child (unprivileged and root-exec).
//
// Setsid, not Setpgid: after fork the child is not a session or process-group
// leader, so setsid() succeeds and makes sid = pgid = pid. Go's
// forkAndExecInChild (go 1.26, syscall/exec_linux.go) calls setsid before
// Credential and execve; it calls setpgid only when Setpgid is set, and
// setpgid on a session leader is EPERM, so Setpgid must stay false.
// SELinux init_t denies process:setpgid and allows this setsid path (P2).
func childSysProcAttr(cred *syscall.Credential) *syscall.SysProcAttr {
	attr := &syscall.SysProcAttr{Setsid: true}
	if os.Geteuid() == 0 && cred != nil {
		attr.Credential = cred
	}
	return attr
}

// ExecError is a failed fork/exec (not a child exit). Class is a closed errno
// name. Error() never includes a path, argv, token, or environment.
type ExecError struct {
	Class string
	err   error
}

func (e *ExecError) Error() string {
	if e == nil {
		return "exec"
	}
	if e.Class == "" {
		return "exec"
	}
	return "exec " + e.Class
}

func (e *ExecError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

var errnoNames = map[syscall.Errno]string{
	syscall.EACCES: "EACCES",
	syscall.EPERM:  "EPERM",
	syscall.ENOENT: "ENOENT",
	syscall.EAGAIN: "EAGAIN",
	syscall.ENOMEM: "ENOMEM",
	syscall.ENOSYS: "ENOSYS",
	syscall.EINVAL: "EINVAL",
	syscall.EIO:    "EIO",
	syscall.EINTR:  "EINTR",
	syscall.ENFILE: "ENFILE",
	syscall.EMFILE: "EMFILE",
	syscall.EFAULT: "EFAULT",
	syscall.ENOSPC: "ENOSPC",
}

func errnoClass(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if n, ok := errnoNames[errno]; ok {
			return n
		}
		return "errno"
	}
	return "exec"
}

// ClassFromMessage returns a closed errno class if msg ends with " (CLASS)".
func ClassFromMessage(msg string) string {
	for _, n := range errnoNames {
		if strings.HasSuffix(msg, " ("+n+")") {
			return n
		}
	}
	if strings.HasSuffix(msg, " (errno)") {
		return "errno"
	}
	if strings.HasSuffix(msg, " (exec)") {
		return "exec"
	}
	return ""
}

// PackageQuery identifies one fixed package-manager read command (ADR 0008
// Amendment 5, Proposed). The argv is compile-time constant; the only caller
// input is a validated search query placed in the query slot. No write,
// refresh, root or network command is represented here.
type PackageQuery string

// The closed package-manager read templates. Format strings carry literal
// backslash escapes (\t, \n) that the tool interprets; the runner never uses
// a shell, so no metacharacter is ever evaluated.
const (
	PkgDebianInstalled PackageQuery = "debian.installed"
	PkgDebianSearch    PackageQuery = "debian.search"
	PkgDebianUpdates   PackageQuery = "debian.updates"
	PkgFedoraInstalled PackageQuery = "fedora.installed"
	PkgFedoraSearch    PackageQuery = "fedora.search"
	PkgFedoraUpdates   PackageQuery = "fedora.updates"
	PkgSUSEInstalled   PackageQuery = "suse.installed"
	PkgSUSESearch      PackageQuery = "suse.search"
	PkgSUSEUpdates     PackageQuery = "suse.updates"
	PkgSUSESources     PackageQuery = "suse.sources"
	PkgArchInstalled   PackageQuery = "arch.installed"
	PkgArchSearch      PackageQuery = "arch.search"
	PkgArchUpdates     PackageQuery = "arch.updates"
)

type packageQuery struct {
	binary  string
	argv    []string // fixed argv; the query slot is a placeholder replaced at run time
	queryAt int      // index of the query slot in argv; -1 = no query
}

var packageQueries = map[PackageQuery]packageQuery{
	PkgDebianInstalled: {"dpkg-query", []string{"-W", "-f", "${binary:Package}\\t${Version}\\t${Architecture}\\n"}, -1},
	PkgDebianSearch:    {"apt-cache", []string{"search", "--", ""}, 2},
	PkgDebianUpdates:   {"apt", []string{"list", "--upgradable"}, -1},
	PkgFedoraInstalled: {"rpm", []string{"-qa", "--queryformat", "%{NAME}\\t%{VERSION}-%{RELEASE}\\t%{ARCH}\\n"}, -1},
	PkgFedoraSearch:    {"dnf", []string{"-C", "repoquery", "--queryformat", "%{NAME}\\t%{VERSION}-%{RELEASE}\\t%{ARCH}\\t%{REPONAME}\\t%{SUMMARY}\\n", "--", ""}, 5},
	PkgFedoraUpdates:   {"dnf", []string{"-C", "check-update"}, -1},
	PkgSUSEInstalled:   {"zypper", []string{"--no-refresh", "-x", "search", "--installed-only"}, -1},
	PkgSUSESearch:      {"zypper", []string{"--no-refresh", "-x", "search", "--", ""}, 4},
	PkgSUSEUpdates:     {"zypper", []string{"--no-refresh", "-x", "list-updates"}, -1},
	PkgSUSESources:     {"zypper", []string{"--no-refresh", "-x", "lr"}, -1},
	PkgArchInstalled:   {"pacman", []string{"-Q"}, -1},
	PkgArchSearch:      {"pacman", []string{"-Ss", "--", ""}, 2},
	PkgArchUpdates:     {"pacman", []string{"-Qu"}, -1},
}

// packageQueryRE is the grammar for a caller-supplied search query: ASCII
// alphanumeric start (never "-"), then a bounded set with no whitespace,
// shell metacharacters, NUL or option-leading dash. ".." is refused as well.
var packageQueryRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+._-]{0,63}$`)

// ValidPackageQuery reports whether a search query is safe to place in a
// package-query template slot.
func ValidPackageQuery(q string) bool {
	return packageQueryRE.MatchString(q)
}

// PackageQueryArgv returns the exact argv (without the binary) a template
// produces for a query, or an error. Exposed for tests and audit records.
func PackageQueryArgv(q PackageQuery, query string) ([]string, error) {
	tpl, ok := packageQueries[q]
	if !ok {
		return nil, fmt.Errorf("%q: %w", q, ErrUnknownTemplate)
	}
	argv := append([]string{}, tpl.argv...)
	if tpl.queryAt >= 0 {
		if !ValidPackageQuery(query) {
			return nil, fmt.Errorf("query %q: %w", query, ErrBadArg)
		}
		argv[tpl.queryAt] = query
	} else if query != "" {
		return nil, fmt.Errorf("query: %w", ErrBadArg)
	}
	return argv, nil
}

// RunPackageQuery executes a fixed package-manager read command. The binary is
// the template's allow-listed path, re-checked at use time; the environment is
// replaced (LC_ALL=C); stdin is /dev/null; output is capped; the process group
// is killed when ctx expires; the child runs as the unprivileged service user.
func (r *Runner) RunPackageQuery(ctx context.Context, q PackageQuery, query string) (Result, error) {
	tpl, ok := packageQueries[q]
	if !ok {
		return Result{}, fmt.Errorf("%q: %w", q, ErrUnknownTemplate)
	}
	argv, err := PackageQueryArgv(q, query)
	if err != nil {
		return Result{}, err
	}
	path, ok := r.paths[tpl.binary]
	if !ok {
		if _, known := allowlist[tpl.binary]; known {
			return Result{}, fmt.Errorf("%s: %w", tpl.binary, os.ErrNotExist)
		}
		return Result{}, fmt.Errorf("%q: %w", tpl.binary, ErrNotAllowed)
	}
	if err := checkBinary(path, r.skipOwnerCheck); err != nil {
		return Result{}, err
	}
	return r.exec(ctx, path, argv, &syscall.Credential{Uid: r.identity.UID, Gid: r.identity.GID}, nil)
}

// Root runs the fixed root argv templates (root-exec class). The broker
// creates exactly one and passes it only to ops registered as root-exec.
type Root struct {
	r *Runner
}

// RootExec returns the root-exec handle of this runner.
func (r *Runner) RootExec() *Root { return &Root{r: r} }

// ErrUnknownTemplate is returned for a template ID that is not in the table.
var ErrUnknownTemplate = errors.New("root template not registered")

// RootArgv returns the exact argv (without the binary) a template produces
// for targets, or an error. Exposed for tests and audit records.
func RootArgv(t RootTemplate, targets ...string) ([]string, error) {
	tpl, ok := rootTemplates[t]
	if !ok {
		return nil, fmt.Errorf("%q: %w", t, ErrUnknownTemplate)
	}
	flags := templateFlagsByID[t]
	if flags.leadPrefix != "" {
		if len(targets) != tpl.maxTargets {
			return nil, fmt.Errorf("%d targets: %w", len(targets), ErrBadArg)
		}
		if flags.leadValid == nil || !flags.leadValid(targets[0]) {
			return nil, fmt.Errorf("target %q: %w", targets[0], ErrBadArg)
		}
		for _, target := range targets[1:] {
			if tpl.valid == nil || !tpl.valid(target) {
				return nil, fmt.Errorf("target %q: %w", target, ErrBadArg)
			}
		}
		if len(tpl.argv) == 0 || tpl.argv[len(tpl.argv)-1] != "--" {
			return nil, fmt.Errorf("template %s: %w", t, ErrBadArg)
		}
		argv := append([]string{}, tpl.argv[:len(tpl.argv)-1]...)
		argv = append(argv, flags.leadPrefix+targets[0], "--")
		return append(argv, targets[1:]...), nil
	}
	if tpl.maxTargets == 0 {
		if len(targets) != 0 {
			return nil, fmt.Errorf("%d targets: %w", len(targets), ErrBadArg)
		}
		return append([]string{}, tpl.argv...), nil
	}
	if len(targets) == 0 || len(targets) > tpl.maxTargets || (flags.exact && len(targets) != tpl.maxTargets) {
		return nil, fmt.Errorf("%d targets: %w", len(targets), ErrBadArg)
	}
	for _, target := range targets {
		if tpl.valid == nil || !tpl.valid(target) {
			return nil, fmt.Errorf("target %q: %w", target, ErrBadArg)
		}
	}
	return append(append([]string{}, tpl.argv...), targets...), nil
}

// maxDropInBytes is the largest body systemctl.edit will read.
const maxDropInBytes = 32768

// Run executes template t on target as root. The binary is the template's
// allow-listed path, re-checked (root-owned, not group/world-writable) at use
// time; the environment is replaced; stdin is /dev/null; output is capped and
// the process group is killed when ctx expires.
func (x *Root) Run(ctx context.Context, t RootTemplate, targets ...string) (Result, error) {
	return x.run(ctx, t, nil, targets...)
}

// RunStdin is Run for the one template that reads a drop-in (systemctl.edit).
// stdin is capped and must not contain a NUL. It is not an argument vector.
func (x *Root) RunStdin(ctx context.Context, t RootTemplate, stdin []byte, targets ...string) (Result, error) {
	if _, ok := rootTemplates[t]; !ok {
		return Result{}, fmt.Errorf("%q: %w", t, ErrUnknownTemplate)
	}
	if !templateFlagsByID[t].readsStdin {
		return Result{}, ErrBadArg
	}
	if stdin == nil {
		stdin = []byte{}
	}
	if len(stdin) > maxDropInBytes || bytes.Contains(stdin, []byte{0}) {
		return Result{}, ErrBadArg
	}
	return x.run(ctx, t, stdin, targets...)
}

func (x *Root) run(ctx context.Context, t RootTemplate, stdin []byte, targets ...string) (Result, error) {
	if x == nil || x.r == nil {
		return Result{}, ErrNotAllowed
	}
	tpl, ok := rootTemplates[t]
	if !ok {
		return Result{}, fmt.Errorf("%q: %w", t, ErrUnknownTemplate)
	}
	if templateFlagsByID[t].readsStdin != (stdin != nil) {
		return Result{}, ErrBadArg
	}
	argv, err := RootArgv(t, targets...)
	if err != nil {
		return Result{}, err
	}
	name := tpl.binary
	path, ok := x.r.paths[name]
	if !ok {
		return Result{}, fmt.Errorf("%s: %w", name, os.ErrNotExist)
	}
	if err := checkBinary(path, x.r.skipOwnerCheck); err != nil {
		return Result{}, err
	}
	return x.r.exec(ctx, path, argv, nil, stdin)
}
