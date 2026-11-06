// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"strings"
	"testing"
)

func TestLVMRAIDCacheAndInternalUUIDRelationships(t *testing.T) {
	// Labelled synthetic report, not captured host output. Field contract follows
	// lvmteam/lvm2 lib/report/columns.h; live collector validation is still needed.
	input := `{"report":[{"lv":[
 {"lv_uuid":"LV-RAID","lv_name":"data","vg_uuid":"VG-A","vg_name":"vg","lv_size":"1048576","segtype":"raid1","raid_sync_action":"recover","sync_percent":"52.50","lv_health_status":"partial","seg_start":"0","seg_size":"1048576","devices":"data_rimage_0(0),data_rimage_1(0)"},
 {"lv_uuid":"LV-ORIGIN","lv_name":"[origin]","vg_uuid":"VG-A","vg_name":"vg","segtype":"linear"},
 {"lv_uuid":"LV-CACHE","lv_name":"[cachepool]","vg_uuid":"VG-A","vg_name":"vg","segtype":"cache-pool"},
 {"lv_uuid":"LV-CACHED","lv_name":"cached","vg_uuid":"VG-A","vg_name":"vg","segtype":"cache","origin_uuid":"LV-ORIGIN","pool_lv_uuid":"LV-CACHE","cache_mode":"writeback","cache_policy":"smq","cache_dirty_blocks":"17","cache_total_blocks":"100","cache_used_blocks":"42"},
 {"lv_uuid":"LV-WRITECACHE","lv_name":"fast","vg_uuid":"VG-A","vg_name":"vg","segtype":"writecache","origin_uuid":"LV-ORIGIN"}
 ]}]}`
	inv, e := ParseLVM(strings.NewReader(input), ModeLive)
	if e != nil {
		t.Fatal(e)
	}
	if len(inv.Volumes) != 5 {
		t.Fatal(inv)
	}
	raid, cached := inv.Volumes[0], inv.Volumes[3]
	if raid.Segments[0].Type != "raid1" || raid.SyncPercent == nil || *raid.SyncPercent != 52.5 || raid.Health != "partial" {
		t.Fatal(raid)
	}
	if cached.Health != "unknown" || cached.CacheDirtyBlocks == nil || *cached.CacheDirtyBlocks != 17 || cached.Segments[0].CacheMode != "writeback" {
		t.Fatal(cached)
	}
	g := Build(nil, nil)
	AddLVM(&g, inv)
	if len(g.Nodes) != 6 || len(g.Edges) != 8 {
		t.Fatalf("unexpected logical relationships %+v", g)
	}
}
func TestLVMMultiSegmentAndMissingHealth(t *testing.T) {
	input := `{"report":[{"lv":[{"lv_uuid":"LV-A","lv_name":"data","vg_uuid":"VG-A","segtype":"linear","seg_start":"0"},{"lv_uuid":"LV-A","lv_name":"data","vg_uuid":"VG-A","segtype":"striped","seg_start":"100"}]}]}`
	inv, e := ParseLVM(strings.NewReader(input), ModeLive)
	if e != nil {
		t.Fatal(e)
	}
	if len(inv.Volumes) != 1 || len(inv.Volumes[0].Segments) != 2 || inv.Volumes[0].SyncPercent != nil || inv.Volumes[0].Health != "unknown" {
		t.Fatal(inv)
	}
}
func TestLVMRejectsMalformedAndInconsistentReports(t *testing.T) {
	for _, s := range []string{`{}`, `{"report":[]} {}`, `{"report":[{"lv":[{"lv_name":"missing-id"}]}]}`, `{"report":[{"lv":[{"lv_uuid":"LV-A","vg_uuid":"VG-A","lv_name":"x","segtype":"raid1","sync_percent":"NaN"}]}]}`, `{"report":[{"lv":[{"lv_uuid":"LV-A","vg_uuid":"VG-A","lv_name":"x","segtype":"raid1","lv_size":"-1"}]}]}`, `{"report":[{"lv":[{"lv_uuid":"LV-A","vg_uuid":"VG-A","lv_name":"x","segtype":"linear"},{"lv_uuid":"LV-A","vg_uuid":"VG-B","lv_name":"x","segtype":"linear"}]}]}`} {
		if _, e := ParseLVM(strings.NewReader(s), ModeLive); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
func TestLVMUnknownComponentIsPartialNotGuessed(t *testing.T) {
	g := Build(nil, nil)
	AddLVM(&g, LVMInventory{Source: "fixture", Volumes: []LogicalVolume{{UUID: "LV-A", VGUUID: "VG-A", Name: "cached", PoolUUID: "MISSING"}}})
	if g.Status != "partial" || len(g.Notes) == 0 {
		t.Fatal(g)
	}
}

func TestLVMKernelJoinUsesUUIDNotNamesOrLayerSuffix(t *testing.T) {
	vg := "ABCDEF-1234-5678-90ab-cdef-1234-ABCDEF"
	lv := "GHIJKL-1234-5678-90ab-cdef-1234-GHIJKL"
	native := "LVM-" + strings.ReplaceAll(vg, "-", "") + strings.ReplaceAll(lv, "-", "")
	inv := LVMInventory{Source: "fixture report", Volumes: []LogicalVolume{{UUID: lv, VGUUID: vg, Name: "data"}}}
	for _, tc := range []struct {
		native string
		want   bool
	}{{native, true}, {native + "-real", false}, {"not-the-same-uuid", false}} {
		g := Build([]Block{{Name: "dm-0", DeviceNumber: "253:0", NativeKind: "dm-uuid", NativeUUID: tc.native}}, nil)
		AddLVM(&g, inv)
		joined := false
		for _, edge := range g.Edges {
			joined = joined || edge.Kind == "represents"
		}
		if joined != tc.want {
			t.Fatal(tc, joined)
		}
	}
}

// Synthetic UUIDs in LVM's 32-character format (6-4-4-4-4-4-6).
const (
	exVG     = "EXVGAA-0000-0000-0000-0000-0000-000001"
	exRAID   = "EXRAID-0000-0000-0000-0000-0000-000001"
	exImage0 = "EXIMG0-0000-0000-0000-0000-0000-000001"
	exImage1 = "EXIMG1-0000-0000-0000-0000-0000-000001"
	exPV0    = "EXPV00-0000-0000-0000-0000-0000-000001"
	exPV1    = "EXPV01-0000-0000-0000-0000-0000-000001"
)

const exRAIDReport = `{"report":[{"lv":[
 {"lv_uuid":"` + exRAID + `","lv_name":"data","vg_uuid":"` + exVG + `","vg_name":"vg","segtype":"raid1","lv_attr":"rwi-aor---","lv_health_status":"refresh needed","raid_sync_action":"recover","sync_percent":"10.00"},
 {"lv_uuid":"` + exImage0 + `","lv_name":"[data_rimage_0]","lv_parent":"data","vg_uuid":"` + exVG + `","vg_name":"vg","segtype":"linear"},
 {"lv_uuid":"` + exImage1 + `","lv_name":"[data_rimage_1]","lv_parent":"data","vg_uuid":"` + exVG + `","vg_name":"vg","segtype":"linear"}
]}]}`

const exPVReport = `{"report":[{"pvseg":[
 {"pv_uuid":"` + exPV0 + `","pv_name":"/dev/sda1","vg_uuid":"` + exVG + `","vg_name":"vg","pv_major":"8","pv_minor":"1","pvseg_start":"0","pvseg_size":"100","lv_uuid":"` + exImage0 + `"},
 {"pv_uuid":"` + exPV0 + `","pv_name":"/dev/sda1","vg_uuid":"` + exVG + `","vg_name":"vg","pv_major":"8","pv_minor":"1","pvseg_start":"100","pvseg_size":"20","lv_uuid":""},
 {"pv_uuid":"` + exPV1 + `","pv_name":"/dev/sdb1","vg_uuid":"` + exVG + `","vg_name":"vg","pv_major":"8","pv_minor":"17","pvseg_start":"0","pvseg_size":"100","lv_uuid":"` + exImage1 + `"}
]}]}`

func edgeKinds(g Graph) map[string]int {
	out := map[string]int{}
	for _, e := range g.Edges {
		out[e.Kind]++
	}
	return out
}

func TestLVMMetadataOnlyModeHidesRuntimeCounters(t *testing.T) {
	inv, e := ParseLVM(strings.NewReader(exRAIDReport), ModeMetadataOnly)
	if e != nil {
		t.Fatal(e)
	}
	raid := inv.Volumes[0]
	if raid.Health != "unknown" || raid.SyncAction != "" || raid.SyncPercent != nil || raid.Attributes != "rwi-??r-?-" {
		t.Fatalf("metadata-only report surfaced runtime state: %+v", raid)
	}
	if raid.Segments[0].Type != "raid1" {
		t.Fatal("metadata (segment type) lost")
	}
	live, e := ParseLVM(strings.NewReader(exRAIDReport), ModeLive)
	if e != nil || live.Volumes[0].Health != "refresh needed" || live.Volumes[0].SyncPercent == nil {
		t.Fatal("live mode lost runtime state", e)
	}
	if _, e := ParseLVM(strings.NewReader(exRAIDReport), ""); e == nil {
		t.Fatal("parser accepted a report without a caller-stated mode")
	}
	g := Build(nil, nil)
	AddLVM(&g, inv)
	if g.Status != "partial" || !strings.Contains(strings.Join(g.Notes, " "), "metadata-only") {
		t.Fatal("metadata-only limitation not noted", g.Notes)
	}
}

func TestLVMRejectsVGNameMismatch(t *testing.T) {
	in := `{"report":[{"lv":[{"lv_uuid":"LV-A","lv_name":"a","vg_uuid":"VG-A","vg_name":"one","segtype":"linear"},{"lv_uuid":"LV-B","lv_name":"b","vg_uuid":"VG-A","vg_name":"two","segtype":"linear"}]}]}`
	if _, e := ParseLVM(strings.NewReader(in), ModeLive); e == nil {
		t.Fatal("torn VG name accepted")
	}
}

func TestLVMSegKeyAccepted(t *testing.T) {
	in := `{"report":[{"seg":[{"lv_uuid":"LV-A","lv_name":"a","vg_uuid":"VG-A","segtype":"linear"}]}]}`
	if inv, e := ParseLVM(strings.NewReader(in), ModeLive); e != nil || len(inv.Volumes) != 1 {
		t.Fatal(inv, e)
	}
}

func TestLVMRAIDCompositionPVsAndDisks(t *testing.T) {
	inv, e := ParseLVM(strings.NewReader(exRAIDReport), ModeMetadataOnly)
	if e != nil {
		t.Fatal(e)
	}
	if inv.PhysicalVolumes, e = ParsePVS(strings.NewReader(exPVReport), ModeMetadataOnly); e != nil {
		t.Fatal(e)
	}
	if len(inv.PhysicalVolumes) != 2 || len(inv.PhysicalVolumes[0].Segments) != 2 || inv.PhysicalVolumes[0].DeviceNumber != "8:1" {
		t.Fatalf("PV report: %+v", inv.PhysicalVolumes)
	}
	g := Build([]Block{{Name: "sda1", DeviceNumber: "8:1", Kind: "partition"}, {Name: "sdb1", DeviceNumber: "8:17", Kind: "partition"}}, nil)
	AddLVM(&g, inv)
	k := edgeKinds(g)
	// Without a signature or a revalidating observation the disk links are
	// provisional only: no "used-by" edge and no claim.
	if k["component-of"] != 2 || k["allocated-to"] != 2 || k["member-of"] != 2 || k["possibly-used-by"] != 2 || k["used-by"] != 0 || len(g.Claims) != 0 {
		t.Fatalf("disk -> PV -> rimage -> raid chain incomplete: %v", k)
	}
	for _, e := range g.Edges {
		if e.Kind == "possibly-used-by" && e.Evidence.Confidence != "provisional" {
			t.Fatal("device-number join presented as more than provisional")
		}
		if e.Kind == "component-of" && !strings.Contains(e.Evidence.Source, "lv_parent") {
			t.Fatal("name-scope join lacks its provenance")
		}
	}
	for _, v := range g.LVM.Volumes {
		if v.KernelJoin != "no-kernel-node" {
			t.Fatalf("LV join status: %s=%q", v.Name, v.KernelJoin)
		}
	}
}

func TestLVMDuplicatePVUUIDIsAmbiguousAndUnjoined(t *testing.T) {
	in := `{"report":[{"pv":[
 {"pv_uuid":"` + exPV0 + `","pv_name":"/dev/sda1","vg_uuid":"` + exVG + `","vg_name":"vg","pv_major":"8","pv_minor":"1"},
 {"pv_uuid":"` + exPV0 + `","pv_name":"/dev/sdc1","vg_uuid":"` + exVG + `","vg_name":"vg","pv_major":"8","pv_minor":"33"}]}]}`
	pvs, e := ParsePVS(strings.NewReader(in), ModeMetadataOnly)
	if e != nil || len(pvs) != 1 || !pvs[0].Ambiguous || pvs[0].DeviceNumber != "" {
		t.Fatal(pvs, e)
	}
	g := Build([]Block{{Name: "sda1", DeviceNumber: "8:1", Kind: "partition"}, {Name: "sdc1", DeviceNumber: "8:33", Kind: "partition"}}, nil)
	AddLVM(&g, LVMInventory{Mode: ModeMetadataOnly, Source: "fixture", Volumes: []LogicalVolume{}, PhysicalVolumes: pvs})
	if edgeKinds(g)["used-by"] != 0 || len(g.Claims) != 0 {
		t.Fatal("cloned PV UUID associated with a disk")
	}
}

func TestLVMParentNameMustBeUniqueInVG(t *testing.T) {
	inv := LVMInventory{Mode: ModeLive, Source: "fixture", Volumes: []LogicalVolume{
		{UUID: "LV-A", VGUUID: "VG-A", Name: "data"},
		{UUID: "LV-B", VGUUID: "VG-B", Name: "data"},
		{UUID: "LV-C", VGUUID: "VG-A", Name: "[data_rimage_0]", Parent: "missing"},
	}}
	g := Build(nil, nil)
	AddLVM(&g, inv)
	if edgeKinds(g)["component-of"] != 0 || g.Status != "partial" {
		t.Fatal("unresolved parent guessed")
	}
}

func TestPVSRejectsMalformed(t *testing.T) {
	for _, s := range []string{`{}`, `{"report":[{"pv":[{"pv_name":"/dev/sda"}]}]}`, `{"report":[{"pv":[{"pv_uuid":"PV-A","pv_major":"x","pv_minor":"1"}]}]}`, `{"report":[{"pv":[{"pv_uuid":"PV-A","lv_uuid":"bad uuid"}]}]}`} {
		if _, e := ParsePVS(strings.NewReader(s), ModeLive); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}

// --- disk-to-PV links need corroboration and revalidation. ---

const pvLinkUUID = "EXPVLK-0000-0000-0000-0000-0000-000001"

func pvLinkReport(major, minor, devSize string) []PhysicalVolume {
	in := `{"report":[{"pv":[{"pv_uuid":"` + pvLinkUUID + `","pv_name":"/dev/sda1","vg_uuid":"` + exVG + `","vg_name":"vg","pv_major":"` + major + `","pv_minor":"` + minor + `","dev_size":"` + devSize + `","pv_attr":"a--","pv_missing":""}]}]}`
	pvs, e := ParsePVS(strings.NewReader(in), ModeMetadataOnly)
	if e != nil {
		panic(e)
	}
	return pvs
}

// sda1 as sysfs (and, when sig is set, an observed signature) sees it.
func pvLinkBlock(sigType, sigUUID string) Block {
	return Block{Name: "sda1", DeviceNumber: "8:1", Kind: "partition", PartUUID: "example-part-1", DiskSeq: "7", SizeBytes: 1048576, FSType: sigType, UUID: sigUUID}
}

func pvLinkGraph(t *testing.T, before Block, pvs []PhysicalVolume, after *PVRevalidation) (Graph, PhysicalVolume) {
	t.Helper()
	g := Build([]Block{before}, nil)
	g.BootID = "boot-a"
	inv := LVMInventory{Mode: ModeMetadataOnly, Source: "fixture", Volumes: []LogicalVolume{}, PhysicalVolumes: pvs}
	if after == nil {
		AddLVM(&g, inv)
	} else {
		AddLVMRevalidated(&g, inv, *after)
	}
	return g, g.LVM.PhysicalVolumes[0]
}

func TestPVLinkVerifiedOnlyWithSignatureSizeAndRevalidation(t *testing.T) {
	block := pvLinkBlock("LVM2_member", pvLinkUUID)
	g, pv := pvLinkGraph(t, block, pvLinkReport("8", "1", "1048576"), &PVRevalidation{BootID: "boot-a", Blocks: []Block{block}})
	k := edgeKinds(g)
	if pv.DeviceLink != PVLinkVerified || k["used-by"] != 1 || k["possibly-used-by"] != 0 || len(g.Claims) != 2 {
		t.Fatalf("corroborated PV link not verified: %+v %v %+v", pv, k, g.Claims)
	}
	for _, e := range g.Edges {
		if e.Kind == "used-by" && e.Evidence.Confidence != "corroborated" {
			t.Fatal(e)
		}
	}
}

func TestPVLinkProvisionalWithoutSignatureOrRevalidation(t *testing.T) {
	for name, tc := range map[string]struct {
		block Block
		after bool
	}{
		"no signature, revalidated":  {pvLinkBlock("", ""), true},
		"signature, no revalidation": {pvLinkBlock("LVM2_member", pvLinkUUID), false},
		"neither":                    {pvLinkBlock("", ""), false},
	} {
		var after *PVRevalidation
		if tc.after {
			after = &PVRevalidation{BootID: "boot-a", Blocks: []Block{tc.block}}
		}
		g, pv := pvLinkGraph(t, tc.block, pvLinkReport("8", "1", "1048576"), after)
		k := edgeKinds(g)
		if pv.DeviceLink != PVLinkProvisional || k["used-by"] != 0 || k["possibly-used-by"] != 1 || g.Status != "partial" {
			t.Fatalf("%s: %+v %v", name, pv, k)
		}
		for _, c := range g.Claims {
			if c.Technology == "lvm-pv" {
				t.Fatalf("%s: provisional link produced a claim", name)
			}
		}
	}
}

func TestPVLinkWithheldOnContradiction(t *testing.T) {
	good := pvLinkBlock("LVM2_member", pvLinkUUID)
	replaced := good
	replaced.DiskSeq = "8" // device number reused by a different disk
	renamedID := good
	renamedID.PartUUID = "example-part-2"
	resized := good
	resized.SizeBytes = 2097152
	other := pvLinkBlock("", "")
	other.Name = "sdb1"
	for name, tc := range map[string]struct {
		before Block
		pvs    []PhysicalVolume
		after  *PVRevalidation
	}{
		"replaced between observations":     {good, pvLinkReport("8", "1", "1048576"), &PVRevalidation{BootID: "boot-a", Blocks: []Block{replaced}}},
		"identity changed":                  {good, pvLinkReport("8", "1", "1048576"), &PVRevalidation{BootID: "boot-a", Blocks: []Block{renamedID}}},
		"resized between observations":      {good, pvLinkReport("8", "1", "1048576"), &PVRevalidation{BootID: "boot-a", Blocks: []Block{resized}}},
		"different name at the same number": {good, pvLinkReport("8", "1", "1048576"), &PVRevalidation{BootID: "boot-a", Blocks: []Block{other}}},
		"disappeared before revalidation":   {good, pvLinkReport("8", "1", "1048576"), &PVRevalidation{BootID: "boot-a"}},
		"stale report from another boot":    {good, pvLinkReport("8", "1", "1048576"), &PVRevalidation{BootID: "boot-b", Blocks: []Block{good}}},
		"unknown revalidation boot":         {good, pvLinkReport("8", "1", "1048576"), &PVRevalidation{Blocks: []Block{good}}},
		"mismatched signature UUID":         {pvLinkBlock("LVM2_member", "EXPVLK-0000-0000-0000-0000-0000-000002"), pvLinkReport("8", "1", "1048576"), nil},
		"non-LVM signature":                 {pvLinkBlock("ext4", "example-fs"), pvLinkReport("8", "1", "1048576"), nil},
		"dev_size differs from sysfs":       {good, pvLinkReport("8", "1", "4096"), nil},
		"no block at that number":           {good, pvLinkReport("8", "2", "1048576"), nil},
	} {
		g, pv := pvLinkGraph(t, tc.before, tc.pvs, tc.after)
		k := edgeKinds(g)
		if pv.DeviceLink != PVLinkWithheld || pv.DeviceLinkReason == "" || k["used-by"] != 0 || k["possibly-used-by"] != 0 || g.Status != "partial" {
			t.Fatalf("%s: %+v %v", name, pv, k)
		}
	}
}

func TestPVLinkWithheldForDuplicateOrMissingPVs(t *testing.T) {
	block := pvLinkBlock("LVM2_member", pvLinkUUID)
	after := &PVRevalidation{BootID: "boot-a", Blocks: []Block{block}}
	two := `{"report":[{"pv":[{"pv_uuid":"` + pvLinkUUID + `","pv_major":"8","pv_minor":"1"},{"pv_uuid":"EXPVLK-0000-0000-0000-0000-0000-000002","pv_major":"8","pv_minor":"1"}]}]}`
	flagged := `{"report":[{"pv":[{"pv_uuid":"` + pvLinkUUID + `","pv_major":"8","pv_minor":"1","pv_attr":"d--"}]}]}`
	missing := `{"report":[{"pv":[{"pv_uuid":"` + pvLinkUUID + `","pv_name":"[unknown]","pv_major":"0","pv_minor":"0","pv_attr":"a-m","pv_missing":"missing"}]}]}`
	for name, in := range map[string]string{"two PVs on one device number": two, "LVM duplicate flag": flagged, "missing PV": missing} {
		pvs, e := ParsePVS(strings.NewReader(in), ModeMetadataOnly)
		if e != nil {
			t.Fatal(name, e)
		}
		g, pv := pvLinkGraph(t, block, pvs, after)
		if k := edgeKinds(g); k["used-by"] != 0 || k["possibly-used-by"] != 0 || pv.DeviceLink == PVLinkVerified || pv.DeviceLink == PVLinkProvisional {
			t.Fatalf("%s: %+v %v", name, pv, k)
		}
	}
}

// --- pv_missing / pv_major semantics and duplicate rows. ---

func TestPVSMissingSpellingsFollowLVMBinaryField(t *testing.T) {
	row := func(missing, attr string) string {
		return `{"report":[{"pv":[{"pv_uuid":"PV-A","pv_major":"8","pv_minor":"1","pv_attr":"` + attr + `"` + missing + `}]}]}`
	}
	for _, tc := range []struct{ in, state string }{
		{row(`,"pv_missing":""`, "a--"), "present"},
		{row(`,"pv_missing":"0"`, "a--"), "present"},
		{row(`,"pv_missing":"missing"`, "a-m"), "missing"},
		{row(`,"pv_missing":"1"`, "a-m"), "missing"},
		{row(``, "a--"), "not-reported"},
		{row(``, "a-m"), "missing-from-attr"},
	} {
		pvs, e := ParsePVS(strings.NewReader(tc.in), ModeLive)
		if e != nil || pvs[0].MissingState != tc.state || pvs[0].Missing != (tc.state == "missing" || tc.state == "missing-from-attr") {
			t.Fatalf("%s: %+v %v", tc.in, pvs, e)
		}
	}
	for _, in := range []string{
		row(`,"pv_missing":"unknown"`, "a--"),
		row(`,"pv_missing":"-1"`, "a--"),
		row(`,"pv_missing":"yes"`, "a--"),
		row(`,"pv_missing":"missing"`, "a--"),                     // attr says present
		row(`,"pv_missing":""`, "a-m"),                            // attr says missing
		`{"report":[{"pv":[{"pv_uuid":"PV-A","pv_missing":0}]}]}`, // json_std number
	} {
		if _, e := ParsePVS(strings.NewReader(in), ModeLive); e == nil {
			t.Fatalf("accepted %s", in)
		}
	}
}

func TestPVSDeviceNumberWithoutDevice(t *testing.T) {
	for _, mm := range [][2]string{{"0", "0"}, {"-1", "-1"}, {"", ""}} {
		in := `{"report":[{"pv":[{"pv_uuid":"PV-A","pv_major":"` + mm[0] + `","pv_minor":"` + mm[1] + `"}]}]}`
		pvs, e := ParsePVS(strings.NewReader(in), ModeLive)
		if e != nil || pvs[0].DeviceNumber != "" {
			t.Fatalf("%v: %+v %v", mm, pvs, e)
		}
	}
	for _, mm := range [][2]string{{"-2", "1"}, {"8", ""}, {"8", "-5"}} {
		in := `{"report":[{"pv":[{"pv_uuid":"PV-A","pv_major":"` + mm[0] + `","pv_minor":"` + mm[1] + `"}]}]}`
		if _, e := ParsePVS(strings.NewReader(in), ModeLive); e == nil {
			t.Fatalf("accepted device number %v", mm)
		}
	}
}

func TestPVSRejectsContradictoryDuplicateRows(t *testing.T) {
	base := `"pv_uuid":"PV-A","pv_name":"/dev/sda1","vg_uuid":"VG-A","vg_name":"vg","pv_major":"8","pv_minor":"1"`
	for name, second := range map[string]string{
		"size":     `,"pv_size":"200","dev_size":"300","pv_attr":"a--","pv_missing":""`,
		"dev_size": `,"pv_size":"100","dev_size":"400","pv_attr":"a--","pv_missing":""`,
		"attr":     `,"pv_size":"100","dev_size":"300","pv_attr":"ax-","pv_missing":""`,
		"missing":  `,"pv_size":"100","dev_size":"300","pv_attr":"a--"`,
	} {
		in := `{"report":[{"pvseg":[{` + base + `,"pv_size":"100","dev_size":"300","pv_attr":"a--","pv_missing":"","pvseg_start":"0"},{` + base + second + `,"pvseg_start":"10"}]}]}`
		if _, e := ParsePVS(strings.NewReader(in), ModeLive); e == nil {
			t.Fatalf("%s: contradictory duplicate rows accepted (first-wins)", name)
		}
	}
	in := `{"report":[{"pvseg":[{` + base + `,"pv_size":"100","pvseg_start":"0"},{` + base + `,"pv_size":"100","pvseg_start":"10"}]}]}`
	if pvs, e := ParsePVS(strings.NewReader(in), ModeLive); e != nil || len(pvs[0].Segments) != 2 {
		t.Fatal("consistent segment rows rejected", e)
	}
}
