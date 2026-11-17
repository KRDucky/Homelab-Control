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

func TestFromNativeSegmentRelations(t *testing.T) {
	vg := lvmmeta.VG{
		UUID:       "aaaaaaaabbbbccccddddeeeeffff0001",
		Name:       "vg1",
		ExtentSize: 8192,
		LVs: []lvmmeta.LV{
			{UUID: "11111111222233334444555566666666", Name: "data_lv"},
			{UUID: "22222222333344445555666677778888", Name: "meta_lv"},
			{UUID: "33333333444455556666777788889999", Name: "origin_lv"},
			{UUID: "44444444555566667777888899990000", Name: "pool_lv"},
			{UUID: "55555555666677778888999900001111", Name: "raid_lv", Segments: []lvmmeta.Segment{{Type: "raid1", Parsed: true, Raid: &lvmmeta.RaidSeg{DeviceCount: 2, Images: []lvmmeta.Area{{PVKey: "pv0"}, {PVKey: "pv1"}}, Metas: []lvmmeta.Area{{PVKey: "pv0"}, {PVKey: "pv1"}}}}}},
			{UUID: "66666666777788889999000011112222", Name: "thin_lv", Segments: []lvmmeta.Segment{{Type: "thin", Parsed: true, Thin: &lvmmeta.ThinSeg{ThinPool: "pool_lv", Origin: "origin_lv"}}}},
			{UUID: "77777777888899990000111122223333", Name: "cache_lv", Segments: []lvmmeta.Segment{{Type: "cache", Parsed: true, Cache: &lvmmeta.CacheSeg{CachePool: "pool_lv", Origin: "origin_lv"}}}},
		},
	}
	out, _ := FromNative(lvmmeta.Inventory{VGs: []lvmmeta.VG{vg}})
	byName := map[string]LogicalVolume{}
	for _, lv := range out.Volumes {
		byName[lv.Name] = lv
	}

	raid := byName["raid_lv"]
	if raid.Segments[0].Devices != "pv0,pv1" || raid.Segments[0].MetadataDevices != "pv0,pv1" {
		t.Fatalf("raid segment = %+v", raid.Segments[0])
	}
	thin := byName["thin_lv"]
	if thin.PoolUUID != "444444-4455-5566-6677-7788-8899-990000" || thin.OriginUUID != "333333-3344-4455-5566-6677-7788-889999" {
		t.Fatalf("thin lv = %+v", thin)
	}
	cache := byName["cache_lv"]
	if cache.PoolUUID != "444444-4455-5566-6677-7788-8899-990000" || cache.OriginUUID != "333333-3344-4455-5566-6677-7788-889999" {
		t.Fatalf("cache lv = %+v", cache)
	}
}
