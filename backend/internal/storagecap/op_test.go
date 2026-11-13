// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagecap

import (
	"strings"
	"testing"
)

func debianReady() Tooling {
	return Tooling{
		Family:   "debian",
		Required: []ToolItem{{ID: "mergerfs", Package: "mergerfs", Installed: true, Binary: "/usr/bin/mergerfs"}},
	}
}

func wwnDisk() DeviceRef {
	return DeviceRef{Kind: IdentityWWN, Value: "naa.5000cca252c8d2a1"}
}

func advanceTo(t *testing.T, op Op, want Phase) Op {
	t.Helper()
	for op.Phase != want && op.Block == "" {
		next := Advance(op)
		if next.Phase == op.Phase && next.Block == "" {
			t.Fatalf("stuck at %s: %+v", op.Phase, next)
		}
		op = next
	}
	return op
}

func TestAdvanceDiscoveryToPlan(t *testing.T) {
	op := Op{Devices: []DeviceRef{wwnDisk()}, Tooling: debianReady()}
	op = Advance(op)
	if op.Phase != PhaseDiscovery {
		t.Fatalf("empty starts at discovery: %s", op.Phase)
	}
	op = Advance(op)
	if op.Phase != PhasePreflight {
		t.Fatalf("preflight: %s", op.Phase)
	}
	op = Advance(op)
	if op.Phase != PhasePlan || op.Block != "" {
		t.Fatalf("plan: %+v", op)
	}
	if op.IdentityDigest != "wwn:naa.5000cca252c8d2a1" {
		t.Fatalf("digest: %q", op.IdentityDigest)
	}
}

func TestOverlapWithoutStackingPlanBlocks(t *testing.T) {
	d := wwnDisk()
	op := Op{
		Phase:   PhasePreflight,
		Devices: []DeviceRef{d},
		Claims: []Claim{
			{Device: d, Stack: "zfs:tank"},
			{Device: d, Stack: "md:md0"},
		},
	}
	op = Advance(op)
	if op.Block != BlockOverlapNoStack || op.Phase != PhasePreflight {
		t.Fatalf("overlap: %+v", op)
	}
}

func TestOverlapWithStackingPlanMayPlan(t *testing.T) {
	d := wwnDisk()
	op := Op{
		Phase:        PhasePreflight,
		Devices:      []DeviceRef{d},
		Claims:       []Claim{{Device: d, Stack: "lvm:vg0"}, {Device: d, Stack: "xfs:/srv/data"}},
		StackingPlan: true,
		Tooling:      debianReady(),
	}
	op = Advance(op)
	if op.Phase != PhasePlan || op.Block != "" {
		t.Fatalf("stacking plan: %+v", op)
	}
}

func TestKernelIdentityCannotConfirm(t *testing.T) {
	op := Op{
		Phase:   PhasePlan,
		Devices: []DeviceRef{{Kind: IdentityKernel, Value: "/dev/sda"}},
		Tooling: debianReady(),
	}
	op = Advance(op)
	if op.Block != BlockKernelIdentity || op.Phase != PhasePlan {
		t.Fatalf("kernel: %+v", op)
	}
}

func TestByIDCannotConfirm(t *testing.T) {
	op := Op{
		Phase:   PhasePlan,
		Devices: []DeviceRef{{Kind: IdentityByID, Value: "ata-WDC_WD80EFAX"}},
		Tooling: debianReady(),
	}
	op = Advance(op)
	if op.Block != BlockWeakIdentity {
		t.Fatalf("by-id: %+v", op)
	}
}

func TestMissingExtraConfirmBlocks(t *testing.T) {
	d := wwnDisk()
	op := Op{
		Phase:          PhaseConfirm,
		Tooling:        debianReady(),
		Devices:        []DeviceRef{d},
		Extra:          []ExtraConfirm{ConfirmFormat},
		IdentityDigest: DigestDevices([]DeviceRef{d}),
	}
	op = Advance(op)
	if op.Block != BlockExtraConfirm || op.Phase != PhaseConfirm {
		t.Fatalf("extra: %+v", op)
	}
}

func TestIdentityChangedBlocksTOCTOU(t *testing.T) {
	d := wwnDisk()
	op := Op{
		Phase:          PhaseConfirm,
		Tooling:        debianReady(),
		Devices:        []DeviceRef{d},
		IdentityDigest: "wwn:other",
		Acked:          nil,
	}
	op = Advance(op)
	if op.Block != BlockIdentityChanged {
		t.Fatalf("toctou: %+v", op)
	}
}

func TestDegradedGateCannotExecute(t *testing.T) {
	d := wwnDisk()
	op := Op{
		Phase:          PhaseConfirm,
		Tooling:        Tooling{Family: "debian", Required: []ToolItem{{ID: "zfs", Installed: false}}},
		Devices:        []DeviceRef{d},
		IdentityDigest: DigestDevices([]DeviceRef{d}),
	}
	op = Advance(op)
	if op.Block != BlockGate {
		t.Fatalf("gate: %+v", op)
	}
}

func TestConfirmNeverEntersExecute(t *testing.T) {
	d := wwnDisk()
	op := Op{
		Phase:          PhaseConfirm,
		Tooling:        debianReady(),
		Devices:        []DeviceRef{d},
		Extra:          []ExtraConfirm{ConfirmFormat},
		Acked:          []ExtraConfirm{ConfirmFormat},
		IdentityDigest: DigestDevices([]DeviceRef{d}),
	}
	op = Advance(op)
	if op.Phase != PhaseConfirm || op.Block != BlockNoExecute {
		t.Fatalf("execute must stay closed: %+v", op)
	}
	if !strings.Contains(op.Reason, "no storage broker templates") {
		t.Fatalf("reason: %q", op.Reason)
	}
}

func TestAdvanceStuckWhenBlocked(t *testing.T) {
	op := Op{Phase: PhasePreflight, Block: BlockOverlapNoStack}
	next := Advance(op)
	if next.Phase != PhasePreflight || next.Block != BlockOverlapNoStack {
		t.Fatalf("stuck: %+v", next)
	}
}

func TestAdvanceToConfirmThenClosed(t *testing.T) {
	d := wwnDisk()
	op := advanceTo(t, Op{Devices: []DeviceRef{d}, Tooling: debianReady(), Extra: []ExtraConfirm{ConfirmForce}, Acked: []ExtraConfirm{ConfirmForce}}, PhaseConfirm)
	if op.Block != "" {
		t.Fatalf("reached confirm: %+v", op)
	}
	op = Advance(op)
	if op.Block != BlockNoExecute {
		t.Fatalf("still closed: %+v", op)
	}
}
