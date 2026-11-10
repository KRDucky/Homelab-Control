// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Package management defines the contract for future container-platform
// mutations (deploy, restart, Compose/manifest changes, network and storage
// configuration, rollback). It is deliberately separate from discovery:
//
//   - Discovery adapters (platform.Adapter) never implement Executor and
//     their scopes (docker.compose.read, podman.read, kubernetes.read) never
//     imply a management scope.
//   - Every operation is planned first, then explicitly approved by a
//     principal holding the operation's management scope, then executed by
//     a separate, narrowly scoped component (ADR 0011 plan -> confirm ->
//     execute).
//
// Phase 9 ships the contract and a Disabled executor only. No live
// executor exists, and no API route can reach one.
package management

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ErrDisabled is returned for every execution attempt in this phase.
var ErrDisabled = errors.New("container management operations are not enabled")

// Kind of a management operation.
type Kind string

const (
	KindDeploy        Kind = "deploy"
	KindRestart       Kind = "restart"
	KindComposeUpdate Kind = "compose-update"
	KindManifestApply Kind = "manifest-apply"
	KindNetwork       Kind = "network-configure"
	KindStorage       Kind = "storage-configure"
	KindRollback      Kind = "rollback"
)

var kinds = map[Kind]bool{KindDeploy: true, KindRestart: true, KindComposeUpdate: true, KindManifestApply: true, KindNetwork: true, KindStorage: true, KindRollback: true}

// Scope returns the management scope an operation kind needs on a platform
// family. Management scopes are distinct from, and never implied by,
// discovery scopes.
func Scope(family string, k Kind) string {
	return "platforms.manage." + family + "." + string(k)
}

// Operation is a requested change, referencing resources only by
// server-issued platform reference keys.
type Operation struct {
	Platform string `json:"platform"`
	Kind     Kind   `json:"kind"`
	Target   string `json:"target"` // platform.Ref key
	// Desired is the platform-native configuration fragment (for example a
	// Compose service patch or Kubernetes manifest) produced by previews.
	Desired map[string]any `json:"desired"`
}

var platformID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)

// Validate checks the operation's shape. It never authorizes.
func (o Operation) Validate() error {
	if !platformID.MatchString(o.Platform) || !kinds[o.Kind] || o.Target == "" || len(o.Target) > 600 {
		return errors.New("invalid management operation")
	}
	return nil
}

// Plan is a reviewable description of exactly what an executor would do.
type Plan struct {
	ID          string        `json:"id"`
	Operation   Operation     `json:"operation"`
	Steps       []string      `json:"steps"`
	Risks       []string      `json:"risks"`
	Rollback    []string      `json:"rollback"`
	RequiresAck []string      `json:"requiresAcknowledgement"` // e.g. privileged groups, root-equivalent daemons
	Expires     time.Time     `json:"expires"`
	Fingerprint string        `json:"fingerprint"` // hash of the reviewed plan; execution must match it
	TTL         time.Duration `json:"-"`
}

// Approval binds a principal's explicit confirmation to one plan.
type Approval struct {
	PlanID      string    `json:"planId"`
	Fingerprint string    `json:"fingerprint"`
	Principal   string    `json:"principal"`
	Scope       string    `json:"scope"`
	Acknowledge []string  `json:"acknowledge"`
	At          time.Time `json:"at"`
}

// CheckApproval verifies an approval matches a plan exactly and is current.
// It does not execute anything.
func CheckApproval(p Plan, a Approval, holds func(scope string) bool, now time.Time) error {
	family := ""
	if fam, ok := familyOf(p.Operation.Platform); ok {
		family = fam
	}
	want := Scope(family, p.Operation.Kind)
	switch {
	case a.PlanID != p.ID || a.Fingerprint == "" || a.Fingerprint != p.Fingerprint:
		return errors.New("approval does not match the reviewed plan")
	case a.Scope != want || !holds(want):
		return fmt.Errorf("missing management scope %s", want)
	case now.After(p.Expires):
		return errors.New("plan expired")
	}
	acked := map[string]bool{}
	for _, x := range a.Acknowledge {
		acked[x] = true
	}
	for _, r := range p.RequiresAck {
		if !acked[r] {
			return fmt.Errorf("plan requires acknowledgement of %q", r)
		}
	}
	return nil
}

// familyOf is replaced by callers that know the registry; the contract
// keeps a conservative default (unknown family never matches a scope).
var familyOf = func(string) (string, bool) { return "", false }

// Executor performs approved plans. Only Disabled exists in Phase 9.
type Executor interface {
	Execute(p Plan, a Approval) error
}

// Disabled refuses every execution, regardless of approvals.
type Disabled struct{}

func (Disabled) Execute(Plan, Approval) error { return ErrDisabled }
