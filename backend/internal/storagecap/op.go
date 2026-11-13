// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagecap

import (
	"sort"
	"strings"
)

// Phase is one step of the SHARED-02 mutating storage state machine.
// Execute is typed here so later broker templates can plug in; P0 never
// reaches it (no storage root templates, DefaultReadOnly).
type Phase string

const (
	PhaseDiscovery Phase = "discovery"
	PhasePreflight Phase = "preflight"
	PhasePlan      Phase = "plan"
	PhaseConfirm   Phase = "confirm"
	PhaseExecute   Phase = "execute"
	PhaseProgress  Phase = "progress"
	PhasePostcheck Phase = "postcheck"
	PhaseRemediate Phase = "remediate"
)

// Block is why Advance refused to leave the current phase.
type Block string

const (
	BlockOverlapNoStack  Block = "overlapping-claim"
	BlockKernelIdentity  Block = "identity-kernel"
	BlockWeakIdentity    Block = "identity-not-strongest"
	BlockExtraConfirm    Block = "extra-confirm-missing"
	BlockIdentityChanged Block = "identity-changed"
	BlockNoExecute       Block = "no-storage-execute"
	BlockGate            Block = "gate-not-available"
	BlockEmptyDevice     Block = "device-required"
)

const blockNoExecuteReason = "P0 storage writes are not implemented; no storage broker templates. ADR 0011 apply is unused here."

// DeviceRef is one persist/write identity. Kernel names are display-only
// and cannot confirm.
type DeviceRef struct {
	Kind  IdentityKind
	Value string
}

// Claim is one stack using a device. Two stacks on one device without
// StackingPlan is overlapping-claim.
type Claim struct {
	Device DeviceRef
	Stack  string
}

// Op is one allowlisted storage operation. It does not mint hcp1 tokens
// and does not spawn processes.
type Op struct {
	Phase          Phase
	Tooling        Tooling
	Devices        []DeviceRef
	Claims         []Claim
	Extra          []ExtraConfirm
	Acked          []ExtraConfirm
	IdentityDigest string
	ObservedDigest string
	StackingPlan   bool
	Block          Block
	Reason         string
}

func deviceKey(d DeviceRef) string {
	return string(d.Kind) + ":" + d.Value
}

// DigestDevices is the TOCTOU identity digest (sorted kind:value).
func DigestDevices(devs []DeviceRef) string {
	keys := make([]string, 0, len(devs))
	for _, d := range devs {
		if d.Value == "" {
			continue
		}
		keys = append(keys, deviceKey(d))
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// Overlap reports two different stacks claiming the same device.
func Overlap(claims []Claim) bool {
	seen := map[string]string{}
	for _, c := range claims {
		if c.Device.Value == "" || c.Stack == "" {
			continue
		}
		k := deviceKey(c.Device)
		if prev, ok := seen[k]; ok && prev != c.Stack {
			return true
		}
		seen[k] = c.Stack
	}
	return false
}

func ackedSet(acked []ExtraConfirm) map[ExtraConfirm]bool {
	m := map[ExtraConfirm]bool{}
	for _, a := range acked {
		m[a] = true
	}
	return m
}

func missingAck(required, acked []ExtraConfirm) bool {
	have := ackedSet(acked)
	for _, r := range required {
		if !have[r] {
			return true
		}
	}
	return false
}

func identityBlock(devs []DeviceRef) Block {
	if len(devs) == 0 {
		return BlockEmptyDevice
	}
	for _, d := range devs {
		if d.Kind == IdentityKernel {
			return BlockKernelIdentity
		}
		if !d.Kind.Strongest() {
			return BlockWeakIdentity
		}
		if d.Value == "" {
			return BlockWeakIdentity
		}
	}
	return ""
}

func fail(op Op, b Block, reason string) Op {
	op.Block = b
	op.Reason = reason
	return op
}

// Advance moves one step, or stays and sets Block. Execute is never
// entered: WritesAllowed is still not authorization to write.
func Advance(op Op) Op {
	if op.Block != "" {
		return op
	}
	if op.Phase == "" {
		op.Phase = PhaseDiscovery
		return op
	}
	switch op.Phase {
	case PhaseDiscovery:
		op.Phase = PhasePreflight
		return op
	case PhasePreflight:
		if Overlap(op.Claims) && !op.StackingPlan {
			return fail(op, BlockOverlapNoStack, "device already claimed by another stack; stacking plan required")
		}
		if len(op.Devices) == 0 {
			return fail(op, BlockEmptyDevice, "mutating storage ops require at least one device")
		}
		op.IdentityDigest = DigestDevices(op.Devices)
		op.Phase = PhasePlan
		return op
	case PhasePlan:
		if b := identityBlock(op.Devices); b != "" {
			switch b {
			case BlockKernelIdentity:
				return fail(op, b, "/dev/sdX is display-only; persist/write needs wwn, nvme-uuid, or serial")
			case BlockEmptyDevice:
				return fail(op, b, "mutating storage ops require at least one device")
			default:
				return fail(op, b, "persist/write identity is wwn, nvme-uuid, or serial; by-id is discovery only")
			}
		}
		op.Phase = PhaseConfirm
		return op
	case PhaseConfirm:
		if !WritesAllowed(op.Tooling) {
			return fail(op, BlockGate, "write actions stay closed unless the discovery gate is available")
		}
		if missingAck(op.Extra, op.Acked) {
			return fail(op, BlockExtraConfirm, "extra confirmation kinds must be acknowledged before execute")
		}
		if op.ObservedDigest == "" {
			op.ObservedDigest = DigestDevices(op.Devices)
		}
		if op.IdentityDigest == "" || op.ObservedDigest != op.IdentityDigest {
			return fail(op, BlockIdentityChanged, "identity changed since plan; revalidate before action")
		}
		return fail(op, BlockNoExecute, blockNoExecuteReason)
	default:
		return fail(op, BlockNoExecute, blockNoExecuteReason)
	}
}
