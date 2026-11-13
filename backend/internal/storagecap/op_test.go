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

func readyPreflight(d DeviceRef) []DevicePreflight {
	return []DevicePreflight{{Device: d, BlockDevice: true, Rdev: "8:1"}}
}

func readyRdev() []string {
	return []string{"8:1"}
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
		PlannedRdev:    readyRdev(),
		Preflight:      readyPreflight(d),
	}
	op = Advance(op)
	if op.Phase != PhaseConfirm || op.Block != BlockNoExecute {
		t.Fatalf("execute must stay closed: %+v", op)
	}
	if !strings.Contains(op.Reason, "no device helper unit") {
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
	op.PlannedRdev = readyRdev()
	op.Preflight = readyPreflight(d)
	op = Advance(op)
	if op.Block != BlockNoExecute {
		t.Fatalf("still closed: %+v", op)
	}
}

func TestConfirmRequiresPreflight(t *testing.T) {
	d := wwnDisk()
	base := Op{
		Phase:          PhaseConfirm,
		Tooling:        debianReady(),
		Devices:        []DeviceRef{d},
		IdentityDigest: DigestDevices([]DeviceRef{d}),
	}
	base.PlannedRdev = readyRdev()
	op := Advance(base)
	if op.Block != BlockPreflightMissing {
		t.Fatalf("missing: %+v", op)
	}
	base.Preflight = []DevicePreflight{{Device: d, BlockDevice: false, Rdev: ""}}
	op = Advance(base)
	if op.Block != BlockNotBlockDevice {
		t.Fatalf("not block: %+v", op)
	}
	base.Preflight = []DevicePreflight{{Device: d, BlockDevice: true, Rdev: "8:1", Mounted: true}}
	op = Advance(base)
	if op.Block != BlockMounted {
		t.Fatalf("mounted: %+v", op)
	}
	base.Preflight = []DevicePreflight{{Device: d, BlockDevice: true, Rdev: "8:1", HasHolders: true}}
	op = Advance(base)
	if op.Block != BlockHasHolders {
		t.Fatalf("holders: %+v", op)
	}
}

func TestConfirmZeroDevicesBlocked(t *testing.T) {
	op := Op{
		Phase:          PhaseConfirm,
		Tooling:        debianReady(),
		IdentityDigest: "x",
		ObservedDigest: "x",
	}
	op = Advance(op)
	if op.Block != BlockEmptyDevice {
		t.Fatalf("zero devices: %+v", op)
	}
}

func TestConfirmPreflightWrongDevice(t *testing.T) {
	d := wwnDisk()
	other := DeviceRef{Kind: IdentityWWN, Value: "naa.5000cca2deadbeef"}
	op := Op{
		Phase:          PhaseConfirm,
		Tooling:        debianReady(),
		Devices:        []DeviceRef{d},
		IdentityDigest: DigestDevices([]DeviceRef{d}),
		PlannedRdev:    readyRdev(),
		Preflight:      readyPreflight(other),
	}
	op = Advance(op)
	if op.Block != BlockPreflightMissing {
		t.Fatalf("wrong device: %+v", op)
	}
}

func TestConfirmRdevMalformed(t *testing.T) {
	d := wwnDisk()
	base := Op{
		Phase:          PhaseConfirm,
		Tooling:        debianReady(),
		Devices:        []DeviceRef{d},
		IdentityDigest: DigestDevices([]DeviceRef{d}),
		PlannedRdev:    readyRdev(),
	}
	base.Preflight = []DevicePreflight{{Device: d, BlockDevice: true, Rdev: "x"}}
	op := Advance(base)
	if op.Block != BlockRdevInvalid {
		t.Fatalf("malformed: %+v", op)
	}
	base.Preflight = []DevicePreflight{{Device: d, BlockDevice: true, Rdev: "0:0"}}
	op = Advance(base)
	if op.Block != BlockRdevInvalid {
		t.Fatalf("zero rdev: %+v", op)
	}
	base.Preflight = []DevicePreflight{{Device: d, BlockDevice: true, Rdev: "08:1"}}
	op = Advance(base)
	if op.Block != BlockRdevInvalid {
		t.Fatalf("leading zero: %+v", op)
	}
	base.Preflight = []DevicePreflight{{Device: d, BlockDevice: true, Rdev: "0:1"}}
	op = Advance(base)
	if op.Block != BlockRdevInvalid {
		t.Fatalf("major zero: %+v", op)
	}
}

func TestConfirmRdevChanged(t *testing.T) {
	d := wwnDisk()
	op := Op{
		Phase:          PhaseConfirm,
		Tooling:        debianReady(),
		Devices:        []DeviceRef{d},
		IdentityDigest: DigestDevices([]DeviceRef{d}),
		PlannedRdev:    readyRdev(),
		Preflight:      []DevicePreflight{{Device: d, BlockDevice: true, Rdev: "8:2"}},
	}
	op = Advance(op)
	if op.Block != BlockIdentityChanged {
		t.Fatalf("rdev changed: %+v", op)
	}
}

func TestConfirmKernelIdentityBlocked(t *testing.T) {
	op := Op{
		Phase:          PhaseConfirm,
		Tooling:        debianReady(),
		Devices:        []DeviceRef{{Kind: IdentityKernel, Value: "/dev/sda"}},
		IdentityDigest: "kernel:/dev/sda",
		ObservedDigest: "kernel:/dev/sda",
		PlannedRdev:    readyRdev(),
		Preflight:      readyPreflight(DeviceRef{Kind: IdentityKernel, Value: "/dev/sda"}),
	}
	op = Advance(op)
	if op.Block != BlockKernelIdentity {
		t.Fatalf("kernel at confirm: %+v", op)
	}
}

func TestConfirmExtraBeforePreflight(t *testing.T) {
	d := wwnDisk()
	op := Op{
		Phase:          PhaseConfirm,
		Tooling:        debianReady(),
		Devices:        []DeviceRef{d},
		Extra:          []ExtraConfirm{ConfirmFormat},
		IdentityDigest: DigestDevices([]DeviceRef{d}),
		PlannedRdev:    readyRdev(),
		Preflight:      readyPreflight(d),
	}
	op = Advance(op)
	if op.Block != BlockExtraConfirm {
		t.Fatalf("extra before preflight: %+v", op)
	}
}

func TestDuplicateDevicesBlocked(t *testing.T) {
	d := wwnDisk()
	other := DeviceRef{Kind: IdentityWWN, Value: "naa.5000cca2deadbeef"}
	op := Op{
		Phase:   PhasePreflight,
		Devices: []DeviceRef{d, d},
		Tooling: debianReady(),
	}
	op = Advance(op)
	if op.Block != BlockDuplicateDevice || op.Phase != PhasePreflight {
		t.Fatalf("preflight dup: %+v", op)
	}
	op = Op{
		Phase:          PhaseConfirm,
		Tooling:        debianReady(),
		Devices:        []DeviceRef{d, d},
		IdentityDigest: DigestDevices([]DeviceRef{d, d}),
		PlannedRdev:    []string{"8:1", "8:2"},
		Preflight:      []DevicePreflight{readyPreflight(d)[0], {Device: other, BlockDevice: true, Rdev: "8:2"}},
	}
	op = Advance(op)
	if op.Block != BlockDuplicateDevice {
		t.Fatalf("confirm dup: %+v", op)
	}
}

func TestPlanPlannedRdevDisagreesWithFacts(t *testing.T) {
	d := wwnDisk()
	op := Op{
		Phase:       PhasePlan,
		Tooling:     debianReady(),
		Devices:     []DeviceRef{d},
		PlannedRdev: []string{"8:2"},
		Preflight:   readyPreflight(d),
	}
	op = Advance(op)
	if op.Block != BlockIdentityChanged || op.Phase != PhasePlan {
		t.Fatalf("plan rdev mismatch: %+v", op)
	}
}
