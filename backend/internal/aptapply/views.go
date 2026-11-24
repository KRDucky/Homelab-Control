// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package aptapply

import "time"

// The broker returns these views; hc-api passes them to the UI unchanged.

// AptConfigView is the read view of the tunables.
type AptConfigView struct {
	Effective      *Effective `json:"effective,omitempty"`
	EffectiveError string     `json:"effective_error,omitempty"`
	Managed        Tunables   `json:"managed"`
	ManagedPresent bool       `json:"managed_present"`
	ManagedByHand  bool       `json:"managed_changed_by_hand"`
	Path           string     `json:"path"`
}

// AptPlan is the review of a settings or mirror change. Before/After are the
// whole managed settings file; for a mirror change Lines lists the changed
// lines only.
type AptPlan struct {
	Action               string        `json:"action"`
	Risk                 string        `json:"risk"`
	StepUpRequired       bool          `json:"step_up_required"`
	ConfirmationRequired bool          `json:"confirmation_required"`
	Token                string        `json:"token"`
	ExpiresAt            time.Time     `json:"expires_at"`
	File                 string        `json:"file"`
	Before               string        `json:"before,omitempty"`
	After                string        `json:"after,omitempty"`
	Lines                []AptLineDiff `json:"lines,omitempty"`
	Effect               string        `json:"effect"`
	Consequences         []string      `json:"consequences"`
}

// AptLineDiff is one changed line of a source file.
type AptLineDiff struct {
	Line   int    `json:"line"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// AptOutcome is the result of an apply.
type AptOutcome struct {
	OperationID string   `json:"operation_id"`
	Action      string   `json:"action"`
	Outcome     string   `json:"outcome"`
	Message     string   `json:"message"`
	Backup      string   `json:"backup,omitempty"`
	Overrides   []string `json:"overridden_by_other_files,omitempty"`
}

// MirrorInventory is the read view of the vendor source files.
type MirrorInventory struct {
	Distro  Distro       `json:"distro"`
	Files   []MirrorFile `json:"files"`
	Entries []Entry      `json:"entries"`
}

// MirrorFile is one vendor file and its digest.
type MirrorFile struct {
	Path   string `json:"path"`
	Format string `json:"format"`
	Digest string `json:"digest"`
	Error  string `json:"error,omitempty"`
}
