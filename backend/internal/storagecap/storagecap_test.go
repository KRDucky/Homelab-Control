// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagecap

import "testing"

func TestProbeInstalledAndMissing(t *testing.T) {
	specs := []Spec{
		{ID: "mergerfs", Package: "mergerfs", Paths: []string{"/usr/bin/mergerfs", "/usr/sbin/mergerfs"}, Required: true},
		{ID: "snapraid", Package: "snapraid", Paths: []string{"/usr/bin/snapraid"}, Required: false},
	}
	stat := func(path string) bool { return path == "/usr/bin/mergerfs" }
	tool := Probe("debian", specs, stat)
	if tool.Family != "debian" || !tool.RequiredInstalled("mergerfs") {
		t.Fatalf("required: %+v", tool)
	}
	if tool.Required[0].Binary != "/usr/bin/mergerfs" {
		t.Fatalf("binary: %+v", tool.Required[0])
	}
	if tool.Optional[0].Installed {
		t.Fatalf("optional must be missing: %+v", tool.Optional[0])
	}
	if FromTooling(tool) != GateAvailable {
		t.Fatalf("gate: %s", FromTooling(tool))
	}
}

func TestProbeMissingRequiredIsDegraded(t *testing.T) {
	specs := []Spec{{ID: "zfs", Package: "zfsutils-linux", Paths: []string{"/usr/sbin/zfs"}, Required: true}}
	tool := Probe("debian", specs, func(string) bool { return false })
	if tool.RequiredInstalled("zfs") {
		t.Fatal("zfs installed")
	}
	if FromTooling(tool) != GateDegraded {
		t.Fatalf("gate: %s", FromTooling(tool))
	}
	if WritesAllowed(tool) {
		t.Fatal("writes must stay closed")
	}
}

func TestUnknownFamilyUnsupportedAndEmptyPackage(t *testing.T) {
	specs := []Spec{{ID: "mdadm", Package: "", Paths: []string{"/usr/sbin/mdadm"}, Required: true}}
	tool := Probe("unknown", specs, func(string) bool { return false })
	if FromTooling(tool) != GateUnsupported {
		t.Fatalf("gate: %s", FromTooling(tool))
	}
	plan := PlanFromTooling(tool)
	if plan.Executes {
		t.Fatal("plan must not execute")
	}
	if plan.Items[0].Query != "" {
		t.Fatalf("no query without package: %+v", plan.Items[0])
	}
	if plan.Reason != planNoPackage {
		t.Fatalf("reason: %q", plan.Reason)
	}
}

func TestPlanSoftwareCenterQuery(t *testing.T) {
	tool := Tooling{
		Family:   "debian",
		Required: []ToolItem{{ID: "mergerfs", Package: "mergerfs", Installed: false}},
		Optional: []ToolItem{{ID: "snapraid", Package: "snapraid", Installed: true}},
	}
	plan := PlanFromTooling(tool)
	if plan.Executes || plan.Reason != planNoExecute {
		t.Fatalf("plan: %+v", plan)
	}
	if plan.Items[0].Query != "/software?q=mergerfs" {
		t.Fatalf("required query: %+v", plan.Items[0])
	}
	if plan.Items[1].Query != "" {
		t.Fatalf("installed optional must not offer: %+v", plan.Items[1])
	}
}

func TestIdentityStrongest(t *testing.T) {
	if !IdentityWWN.Strongest() || IdentityKernel.Strongest() || IdentityByID.Strongest() {
		t.Fatal("persist/write is wwn/nvme-uuid/serial only")
	}
}

func TestDefaultReadOnly(t *testing.T) {
	if !DefaultReadOnly {
		t.Fatal("P0 default is read-only")
	}
}
