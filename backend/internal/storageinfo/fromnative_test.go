// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"strings"
	"testing"

	"homelab-control/backend/internal/lvmmeta"
)

func TestFromNative(t *testing.T) {
	inv := lvmmeta.Inventory{VGs: []lvmmeta.VG{{
		UUID:       "aaaaaaaabbbbccccddddeeeeffff0001",
		Name:       "vg1",
		ExtentSize: 8192,
		State:      lvmmeta.StateUnsupported,
		PVs: []lvmmeta.VGPV{
			{Key: "pv0", UUID: "abcdef1234567890abcdef1234567890", Device: "vdb", DevSize: 10485760, Presence: lvmmeta.PresencePresent},
			{Key: "pv1", UUID: "00000000000000000000000000000000", Presence: lvmmeta.PresenceMissingFlag, Flags: []string{"MISSING"}},
		},
		LVs: []lvmmeta.LV{
			{UUID: "99999999888877776666555544444444", Name: "lv1", Segments: []lvmmeta.Segment{{Type: "striped", Parsed: true, StartExtent: 0, ExtentCount: 128, Stripes: []lvmmeta.Area{{PVKey: "pv0"}}}}},
			{UUID: "11112222333344445555666677778888", Name: "lv2", Segments: []lvmmeta.Segment{{Type: "vdo", Parsed: false}}},
		},
	}}}

	out, notes := FromNative(inv)
	if len(out.PhysicalVolumes) != 2 {
		t.Fatalf("pvs = %+v", out.PhysicalVolumes)
	}
	if out.PhysicalVolumes[0].DeviceLink != "verified" || out.PhysicalVolumes[0].MissingState != "present" {
		t.Fatalf("pv0 = %+v", out.PhysicalVolumes[0])
	}
	if !out.PhysicalVolumes[1].Missing || out.PhysicalVolumes[1].MissingState != "missing" {
		t.Fatalf("pv1 = %+v", out.PhysicalVolumes[1])
	}
	if len(out.Volumes) != 2 || out.Volumes[0].Health != "unknown" {
		t.Fatalf("volumes = %+v", out.Volumes)
	}
	if out.Volumes[0].Segments[0].Type != "striped" || out.Volumes[0].Segments[0].Devices != "pv0" {
		t.Fatalf("lv1 segments = %+v", out.Volumes[0].Segments)
	}
	found := false
	for _, n := range notes {
		if strings.Contains(n, "unsupported") {
			found = true
		}
	}
	if !found {
		t.Fatalf("notes = %v, want an unsupported note", notes)
	}
}
