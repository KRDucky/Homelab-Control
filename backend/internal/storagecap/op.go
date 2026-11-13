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
// reaches it (storage root template registered unused; DefaultReadOnly).
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
	BlockOverlapNoStack   Block = "overlapping-claim"
	BlockKernelIdentity   Block = "identity-kernel"
	BlockWeakIdentity     Block = "identity-not-strongest"
	BlockExtraConfirm     Block = "extra-confirm-missing"
	BlockIdentityChanged  Block = "identity-changed"
	BlockNoExecute        Block = "no-storage-execute"
	BlockGate             Block = "gate-not-available"
	BlockEmptyDevice      Block = "device-required"
	BlockPreflightMissing Block = "preflight-missing"
	BlockNotBlockDevice   Block = "not-block-device"
	BlockMounted          Block = "device-mounted"
	BlockHasHolders       Block = "device-has-holders"
	BlockRdevInvalid      Block = "rdev-invalid"
	BlockDuplicateDevice  Block = "device-duplicate"
)

const blockNoExecuteReason = "P0 storage writes are not implemented; no storage apply op and no device helper unit (G3). ADR 0011 apply is unused here."

// DevicePreflight is necessary, not sufficient, observed device state from a
// later privileged helper (not hc-api). This package does not stat, open, or
// format block devices (G1/G2/G4). Signature, host-root/boot/tank, and
// md/ZFS/LVM/btrfs membership are later helper facts.
type DevicePreflight struct {
	Device      DeviceRef // must match one Op.Devices entry by kind:value
	BlockDevice bool      // S_ISBLK after resolving the planned path
	Mounted     bool      // mounted, swap, a mounted partition of the disk, or otherwise busy
	HasHolders  bool      // sysfs holders/slaves or a stack member
	Rdev        string    // st_rdev as "major:minor"; required when BlockDevice
}

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
	PlannedRdev    []string // one major:minor per Devices entry, recorded at plan
	Preflight      []DevicePreflight
	StackingPlan   bool
	Block          Block
	Reason         string
}

func deviceKey(d DeviceRef) string {
	v := d.Value
	switch d.Kind {
	case IdentityWWN, IdentityNVMeUUID:
		v = strings.ToLower(v)
	}
	return string(d.Kind) + ":" + v
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

func duplicateDevice(devs []DeviceRef) bool {
	seen := map[string]bool{}
	for _, d := range devs {
		if d.Value == "" {
			continue
		}
		k := deviceKey(d)
		if seen[k] {
			return true
		}
		seen[k] = true
	}
	return false
}

func identityBlock(devs []DeviceRef) Block {
	if len(devs) == 0 {
		return BlockEmptyDevice
	}
	if duplicateDevice(devs) {
		return BlockDuplicateDevice
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

func rdevDigits(s string, allowZero bool, maxLen int) bool {
	if len(s) < 1 || len(s) > maxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	if s[0] == '0' {
		return allowZero && len(s) == 1
	}
	return true
}

func validRdev(s string) bool {
	maj, min, ok := strings.Cut(s, ":")
	if !ok || strings.Contains(min, ":") {
		return false
	}
	return rdevDigits(maj, false, 5) && rdevDigits(min, true, 7)
}

func sameRdevs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func duplicateRdev(vals []string) bool {
	seen := map[string]bool{}
	for _, v := range vals {
		if !validRdev(v) {
			continue
		}
		if seen[v] {
			return true
		}
		seen[v] = true
	}
	return false
}

func factsByDevice(facts []DevicePreflight) (map[string]DevicePreflight, bool) {
	out := map[string]DevicePreflight{}
	for _, p := range facts {
		if p.Device.Value == "" {
			return nil, false
		}
		k := deviceKey(p.Device)
		if _, dup := out[k]; dup {
			return nil, false
		}
		out[k] = p
	}
	return out, true
}

func plannedRdevFromFacts(devs []DeviceRef, facts []DevicePreflight) []string {
	if len(facts) != len(devs) || len(devs) == 0 {
		return nil
	}
	byKey, ok := factsByDevice(facts)
	if !ok {
		return nil
	}
	out := make([]string, len(devs))
	for i, d := range devs {
		p, ok := byKey[deviceKey(d)]
		if !ok || !validRdev(p.Rdev) {
			return nil
		}
		out[i] = p.Rdev
	}
	return out
}

func preflightBlock(devs []DeviceRef, facts []DevicePreflight, planned []string) (Block, string) {
	if len(devs) == 0 {
		return BlockEmptyDevice, "mutating storage ops require at least one device"
	}
	if duplicateDevice(devs) {
		return BlockDuplicateDevice, "each target must appear once"
	}
	if len(facts) != len(devs) || len(planned) != len(devs) {
		return BlockPreflightMissing, "execute needs a fresh block-device preflight per target"
	}
	byKey, ok := factsByDevice(facts)
	if !ok {
		return BlockPreflightMissing, "execute needs a fresh block-device preflight per target"
	}
	seenRdev := map[string]bool{}
	for i, d := range devs {
		p, ok := byKey[deviceKey(d)]
		if !ok {
			return BlockPreflightMissing, "execute needs a fresh block-device preflight per target"
		}
		if !p.BlockDevice {
			return BlockNotBlockDevice, "target must be a block device immediately before exec"
		}
		if !validRdev(p.Rdev) || !validRdev(planned[i]) {
			return BlockRdevInvalid, "target rdev must be major:minor immediately before exec"
		}
		if p.Rdev != planned[i] {
			return BlockIdentityChanged, "identity changed since plan; revalidate before action"
		}
		if seenRdev[p.Rdev] {
			return BlockDuplicateDevice, "each target must appear once"
		}
		seenRdev[p.Rdev] = true
		if p.Mounted {
			return BlockMounted, "target is mounted"
		}
		if p.HasHolders {
			return BlockHasHolders, "target has holders or is a stack member"
		}
	}
	return "", ""
}

func identityFail(op Op, b Block) Op {
	switch b {
	case BlockKernelIdentity:
		return fail(op, b, "/dev/sdX is display-only; persist/write needs wwn, nvme-uuid, or serial")
	case BlockEmptyDevice:
		return fail(op, b, "mutating storage ops require at least one device")
	case BlockDuplicateDevice:
		return fail(op, b, "each target must appear once")
	default:
		return fail(op, b, "persist/write identity is wwn, nvme-uuid, or serial; by-id is discovery only")
	}
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
		if duplicateDevice(op.Devices) {
			return fail(op, BlockDuplicateDevice, "each target must appear once")
		}
		op.IdentityDigest = DigestDevices(op.Devices)
		op.Phase = PhasePlan
		return op
	case PhasePlan:
		if b := identityBlock(op.Devices); b != "" {
			return identityFail(op, b)
		}
		fromFacts := plannedRdevFromFacts(op.Devices, op.Preflight)
		if len(op.PlannedRdev) != 0 && len(op.PlannedRdev) != len(op.Devices) {
			return fail(op, BlockPreflightMissing, "execute needs a fresh block-device preflight per target")
		}
		if duplicateRdev(op.PlannedRdev) || duplicateRdev(fromFacts) {
			return fail(op, BlockDuplicateDevice, "each target must appear once")
		}
		if len(op.PlannedRdev) == 0 {
			op.PlannedRdev = fromFacts
		} else if fromFacts != nil && !sameRdevs(op.PlannedRdev, fromFacts) {
			return fail(op, BlockIdentityChanged, "identity changed since plan; revalidate before action")
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
		if Overlap(op.Claims) && !op.StackingPlan {
			return fail(op, BlockOverlapNoStack, "device already claimed by another stack; stacking plan required")
		}
		if b := identityBlock(op.Devices); b != "" {
			return identityFail(op, b)
		}
		if op.ObservedDigest == "" {
			op.ObservedDigest = DigestDevices(op.Devices)
		}
		if op.IdentityDigest == "" || op.ObservedDigest != op.IdentityDigest {
			return fail(op, BlockIdentityChanged, "identity changed since plan; revalidate before action")
		}
		if b, reason := preflightBlock(op.Devices, op.Preflight, op.PlannedRdev); b != "" {
			return fail(op, b, reason)
		}
		return fail(op, BlockNoExecute, blockNoExecuteReason)
	default:
		return fail(op, BlockNoExecute, blockNoExecuteReason)
	}
}
