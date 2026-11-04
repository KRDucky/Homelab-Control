// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package types holds broker result types shared by hc-broker and hc-api.
// It must stay free of privileged code: hc-api imports it.
package types

import (
	"regexp"
	"time"
)

// Privilege describes how an op touches the host.
type Privilege string

// Privilege classes.
const (
	PrivNone             Privilege = "none"
	PrivRootRead         Privilege = "root-read"
	PrivUnprivilegedExec Privilege = "unprivileged-exec"
	// PrivRootExec ops may run one of the fixed root argv templates of
	// internal/broker/runner (ADR 0008 Amendment 2, D-4). Only write ops
	// that went through plan/apply review carry it.
	PrivRootExec Privilege = "root-exec"
)

// Risk classes of write operations (ADR 0011).
const (
	RiskReversible  = "reversible"
	RiskDisruptive  = "disruptive"
	RiskDestructive = "destructive"
)

// OpInfo is the public description of an op.
type OpInfo struct {
	Name        string    `json:"name"`
	Scope       string    `json:"scope"`
	Privilege   Privilege `json:"privilege"`
	Description string    `json:"description"`
}

// Check is one posture finding.
type Check struct {
	ID      string `json:"id"`
	Level   string `json:"level"` // ok | warn | info
	Message string `json:"message"`
}

// WebminStatus is the allow-listed view of an installed upstream Webmin.
type WebminStatus struct {
	Installed  bool     `json:"installed"`
	ConfigDir  string   `json:"config_dir"`
	Version    string   `json:"version,omitempty"`
	Port       int      `json:"port,omitempty"`
	TLS        *bool    `json:"tls,omitempty"`
	Bind       string   `json:"bind,omitempty"`
	AllIfaces  bool     `json:"listens_on_all_interfaces"`
	Running    *bool    `json:"running,omitempty"`
	Users      int      `json:"user_count"`
	RPCCapable []string `json:"rpc_capable_users"`
	Checks     []Check  `json:"checks"`
	Notes      []string `json:"notes,omitempty"`
}

// DMIField is one SMBIOS identifier with provenance.
type DMIField struct {
	Value  string `json:"value,omitempty"`
	Status string `json:"status"` // present | placeholder | missing | unreadable
}

// DMIIdentity holds the root-only SMBIOS identifiers.
type DMIIdentity struct {
	Available bool                `json:"available"`
	Reason    string              `json:"reason,omitempty"`
	Fields    map[string]DMIField `json:"fields"`
	Source    string              `json:"source"`
}

// Unit is one systemd unit from runtime or installed-file inventory.
// File-only entries have Load="not-loaded" and Active/Sub="unknown"; these
// describe missing runtime observations, not states inferred from enablement.
type Unit struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Load          string `json:"load"`
	Active        string `json:"active"`
	Sub           string `json:"sub"`
	Description   string `json:"description"`
	UnitFileState string `json:"unit_file_state,omitempty"`
	Preset        string `json:"preset,omitempty"`
}

// LogEntry is one journal record of a unit (services.unit.logs).
type LogEntry struct {
	Time       *time.Time `json:"time,omitempty"`     // absent when journald gave no usable timestamp
	Priority   *int       `json:"priority,omitempty"` // syslog 0 (emerg) … 7 (debug)
	PID        int        `json:"pid,omitempty"`
	Identifier string     `json:"identifier,omitempty"`
	Message    string     `json:"message"`
	Truncated  bool       `json:"truncated,omitempty"`
}

// UnitLogs is the result of services.unit.logs.
type UnitLogs struct {
	Unit      string     `json:"unit"`
	Requested int        `json:"requested"`
	Entries   []LogEntry `json:"entries"`
	Truncated bool       `json:"truncated"` // older entries dropped to stay under the size cap
	Skipped   int        `json:"skipped,omitempty"`
	Source    string     `json:"source"`
}

// UnitList is the result of services.systemd.list.
type UnitList struct {
	SystemdVersion string         `json:"systemd_version,omitempty"`
	Units          []Unit         `json:"units"`
	Counts         map[string]int `json:"counts"`
	Truncated      bool           `json:"truncated"`
	Source         string         `json:"source"`
}

// LogsUnitRE is the unit-name grammar accepted by services.unit.logs: no
// leading '-', no escapes or path separators, a listed unit type, at most 100
// characters before the type. Shared by hc-api (early 400) and the broker.
var LogsUnitRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.@-]{0,99}\.(service|socket|timer|mount|automount|path|target|swap)$`)

// MaxLogLines bounds one services.unit.logs request.
const MaxLogLines = 500

// ValidLogsArgs reports whether unit and lines are acceptable.
func ValidLogsArgs(unit string, lines int) bool {
	return LogsUnitRE.MatchString(unit) && lines >= 1 && lines <= MaxLogLines
}

// UnitObserved is the allow-listed, observed state of one service unit as
// systemd reports it (systemctl show), read by the broker.
type UnitObserved struct {
	ID             string   `json:"id"`
	Names          []string `json:"names,omitempty"`
	Description    string   `json:"description,omitempty"`
	LoadState      string   `json:"load_state"`
	ActiveState    string   `json:"active_state"`
	SubState       string   `json:"sub_state"`
	Result         string   `json:"result,omitempty"`
	UnitFileState  string   `json:"unit_file_state,omitempty"`
	MainPID        int      `json:"main_pid,omitempty"`
	ExecMainStatus int      `json:"exec_main_status,omitempty"`
	InvocationID   string   `json:"invocation_id,omitempty"`
	Type           string   `json:"type,omitempty"`
	StateSince     string   `json:"state_since,omitempty"`
}

// ServicePlan is the result of services.unit.plan: what would happen, the
// risk class and a short-lived, single-use plan token (ADR 0011).
type ServicePlan struct {
	Unit           string       `json:"unit"`      // canonical unit ID
	Requested      string       `json:"requested"` // name as requested (may be an alias)
	Action         string       `json:"action"`
	Risk           string       `json:"risk"`
	StepUpRequired bool         `json:"step_up_required"`
	Token          string       `json:"token"`
	ExpiresAt      time.Time    `json:"expires_at"`
	Observed       UnitObserved `json:"observed"`
	Effect         string       `json:"effect"`
	Consequences   []string     `json:"consequences"`
	// AlsoAffected: other units systemd stops or restarts with it (if running).
	AlsoAffected []string `json:"also_affected,omitempty"`
	// AlsoStarted: other, currently inactive units a start pulls in.
	AlsoStarted []string `json:"also_started,omitempty"`
}

// Operation outcomes. "succeeded" and "failed" are verified by reading the
// unit state back; "uncertain" means the result could not be verified.
const (
	OutcomeSucceeded   = "succeeded"
	OutcomeFailed      = "failed"
	OutcomeUncertain   = "uncertain"
	OutcomeInterrupted = "interrupted"
	OutcomePending     = "pending" // intent recorded, no outcome yet
)

// ServiceOutcome is the result of services.unit.apply.
type ServiceOutcome struct {
	OperationID string        `json:"operation_id"`
	Unit        string        `json:"unit"`
	Action      string        `json:"action"`
	Outcome     string        `json:"outcome"`
	Message     string        `json:"message"`
	ExitCode    *int          `json:"exit_code,omitempty"`
	Observed    *UnitObserved `json:"observed,omitempty"`
	Recovery    []string      `json:"recovery,omitempty"`
	StartedAt   time.Time     `json:"started_at"`
	FinishedAt  time.Time     `json:"finished_at"`
}

// OperationRecord is one entry of the broker operation journal as listed by
// operations.journal.list. It never contains plan tokens or secrets.
type OperationRecord struct {
	ID         string        `json:"id"`
	Op         string        `json:"op"`
	Action     string        `json:"action"`
	Unit       string        `json:"unit"`
	Actor      string        `json:"actor"`
	RequestID  string        `json:"request_id,omitempty"`
	Risk       string        `json:"risk"`
	Outcome    string        `json:"outcome"`
	Message    string        `json:"message,omitempty"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
	Observed   *UnitObserved `json:"observed,omitempty"`
}

// OperationList is the result of operations.journal.list.
type OperationList struct {
	Operations []OperationRecord `json:"operations"`
	Source     string            `json:"source"`
}

// EnablementPlan is the result of services.enablement.plan: a boot-time
// enable or disable of one .service unit (M2-3), reviewed by the broker.
type EnablementPlan struct {
	Unit           string       `json:"unit"`
	Requested      string       `json:"requested"`
	Action         string       `json:"action"` // enable | disable
	Risk           string       `json:"risk"`
	StepUpRequired bool         `json:"step_up_required"`
	Token          string       `json:"token"`
	ExpiresAt      time.Time    `json:"expires_at"`
	Observed       UnitObserved `json:"observed"` // unit_file_state is the fresh is-enabled value
	// BootState is `systemctl is-enabled` (computed from disk) at review time.
	BootState    string   `json:"boot_state"`
	Preset       string   `json:"preset,omitempty"`
	Effect       string   `json:"effect"`
	Consequences []string `json:"consequences"`
	// Links are the symbolic links the change creates (enable) or removes (disable).
	Links    []string `json:"links"`
	WantedBy []string `json:"wanted_by,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`
	// AlsoAffected: running units the boot-time start would stop (Conflicts=).
	AlsoAffected []string `json:"also_affected,omitempty"`
	// AlsoStarted: inactive units the boot-time start pulls in.
	AlsoStarted []string `json:"also_started,omitempty"`
	Notes       []string `json:"notes,omitempty"`
}

// LinkObservation is one expected link, checked by the broker itself.
type LinkObservation struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
}

// EnablementOutcome is the result of services.enablement.apply. "succeeded"
// means systemctl exited 0, is-enabled reports the requested state and every
// expected link is in the requested state.
type EnablementOutcome struct {
	OperationID     string            `json:"operation_id"`
	Unit            string            `json:"unit"`
	Action          string            `json:"action"`
	Outcome         string            `json:"outcome"`
	Message         string            `json:"message"`
	ExitCode        *int              `json:"exit_code,omitempty"`
	BootStateBefore string            `json:"boot_state_before"`
	BootState       string            `json:"boot_state,omitempty"` // empty when it could not be read back
	Links           []LinkObservation `json:"links"`
	// Reported are systemctl's own "Created symlink"/"Removed" lines.
	Reported   []string      `json:"reported,omitempty"`
	Observed   *UnitObserved `json:"observed,omitempty"`
	Recovery   []string      `json:"recovery,omitempty"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
}

// UnitExit is the last recorded exit of a service's main process.
type UnitExit struct {
	Code      string `json:"code"` // exited | killed | dumped core | unknown
	Status    int    `json:"status"`
	ExitedAt  string `json:"exited_at"`
	StartedAt string `json:"started_at,omitempty"`
}

// UnitDetail is the result of services.unit.detail. Fields are what systemd
// reported; absent values are omitted, never guessed.
type UnitDetail struct {
	Requested string `json:"requested"`
	ID        string `json:"id,omitempty"` // canonical; empty for templates
	// Kind: unit | alias (requested is another name of ID) | template.
	Kind string `json:"kind"`
	// InRuntime: listed by `systemctl list-units --all`.
	InRuntime bool `json:"in_runtime"`
	// LoadedForQuery: installed but not loaded; systemd loaded it to answer.
	LoadedForQuery bool     `json:"loaded_for_query,omitempty"`
	Names          []string `json:"names,omitempty"`
	Description    string   `json:"description,omitempty"`
	LoadState      string   `json:"load_state,omitempty"`
	ActiveState    string   `json:"active_state,omitempty"`
	SubState       string   `json:"sub_state,omitempty"`
	Result         string   `json:"result,omitempty"`
	// UnitFileState is systemd's loaded (possibly stale) value; BootState is
	// list-unit-files, computed from disk.
	UnitFileState         string              `json:"unit_file_state,omitempty"`
	BootState             string              `json:"boot_state,omitempty"`
	Preset                string              `json:"preset,omitempty"`
	FragmentPath          string              `json:"fragment_path,omitempty"`
	SourcePath            string              `json:"source_path,omitempty"`
	DropInPaths           []string            `json:"drop_in_paths,omitempty"`
	Generated             bool                `json:"generated,omitempty"`
	Transient             bool                `json:"transient,omitempty"`
	NeedDaemonReload      bool                `json:"need_daemon_reload,omitempty"`
	ServiceType           string              `json:"service_type,omitempty"`
	Restart               string              `json:"restart,omitempty"`
	Restarts              *int                `json:"restarts,omitempty"` // NRestarts (services only)
	MainPID               int                 `json:"main_pid,omitempty"`
	LastExit              *UnitExit           `json:"last_exit,omitempty"`
	ActiveSince           string              `json:"active_since,omitempty"`
	InactiveSince         string              `json:"inactive_since,omitempty"`
	StateSince            string              `json:"state_since,omitempty"`
	Dependencies          map[string][]string `json:"dependencies"`
	DependenciesTruncated bool                `json:"dependencies_truncated,omitempty"`
	Notes                 []string            `json:"notes,omitempty"`
	Source                string              `json:"source"`
}

// UnitFileText is one file of a unit's definition, redacted.
type UnitFileText struct {
	Path       string `json:"path"`
	Text       string `json:"text"`
	Unreadable bool   `json:"unreadable,omitempty"`
}

// UnitContents is the result of services.unit.contents.
type UnitContents struct {
	Unit       string         `json:"unit"`
	Files      []UnitFileText `json:"files"`
	Unreadable []string       `json:"unreadable,omitempty"` // not readable by the service account
	Redactions int            `json:"redactions"`
	Truncated  bool           `json:"truncated"`
	Notes      []string       `json:"notes,omitempty"`
	Source     string         `json:"source"`
}

// TimerInfo is one timer unit (services.timers.list).
type TimerInfo struct {
	Unit          string     `json:"unit"`
	Activates     string     `json:"activates,omitempty"`
	Description   string     `json:"description,omitempty"`
	ActiveState   string     `json:"active_state"`
	SubState      string     `json:"sub_state,omitempty"`
	UnitFileState string     `json:"unit_file_state,omitempty"`
	Next          *time.Time `json:"next,omitempty"`
	Last          *time.Time `json:"last,omitempty"`
	Schedule      []string   `json:"schedule,omitempty"`
	Persistent    *bool      `json:"persistent,omitempty"`
}

// TimerList is the result of services.timers.list.
type TimerList struct {
	Timers    []TimerInfo `json:"timers"`
	Truncated bool        `json:"truncated"`
	Source    string      `json:"source"`
}

// SocketInfo is one socket unit (services.sockets.list).
type SocketInfo struct {
	Unit          string   `json:"unit"`
	Description   string   `json:"description,omitempty"`
	Listen        []string `json:"listen"`
	Activates     []string `json:"activates,omitempty"`
	ActiveState   string   `json:"active_state"`
	SubState      string   `json:"sub_state,omitempty"`
	UnitFileState string   `json:"unit_file_state,omitempty"`
}

// SocketList is the result of services.sockets.list.
type SocketList struct {
	Sockets   []SocketInfo `json:"sockets"`
	Truncated bool         `json:"truncated"`
	Source    string       `json:"source"`
}
