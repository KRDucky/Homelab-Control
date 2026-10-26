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

// Unit is one systemd unit as reported by systemctl.
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
