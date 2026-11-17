// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvmmeta

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

// These fixtures are the first 32 KiB of real LVM2-written disks (lvm2 2.03.38)
// captured in a disposable Fedora VM. Provenance and reference captures are in
// /mnt/tank/claude-vms/artifacts/m24-lvmmeta-fixtures-20261117b/{raw,json}/.
// The 32 KiB prefix holds the label (sector 1), pv_header, MDA header and the
// metadata text (max observed extent ends at byte 22771), which is all the
// reader needs; the full 512 MiB image is never committed. Each <case>.vgcfg is
// the same VM's `vgcfgbackup -f` text, used as the differential reference.

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("checked-in fixture %s is missing: %v", name, err)
	}
	return b
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	return readTestdata(t, name+".raw")
}

func parseVGFromText(t *testing.T, b []byte) *VG {
	t.Helper()
	root, err := parseText(b)
	if err != nil {
		t.Fatalf("parse text: %v", err)
	}
	vgNode := findVGNode(root)
	if vgNode == nil {
		t.Fatal("no VG section")
	}
	vg, err := ParseVG(vgNode.Name, vgNode)
	if err != nil {
		t.Fatalf("ParseVG: %v", err)
	}
	return vg
}

func parseVGFromPV(t *testing.T, pv PVRead) *VG {
	t.Helper()
	if pv.Status != StatusOK {
		t.Fatalf("status = %s", pv.Status)
	}
	return parseVGFromText(t, pv.Text)
}

// assertSameVG compares a VG parsed from on-disk metadata against the VG parsed
// from the same case's vgcfgbackup reference, field by field.
func assertSameVG(t *testing.T, got, ref *VG) {
	t.Helper()
	if got.Name != ref.Name {
		t.Errorf("Name: %q != %q", got.Name, ref.Name)
	}
	if got.UUID != ref.UUID {
		t.Errorf("UUID: %q != %q", got.UUID, ref.UUID)
	}
	if got.Seqno != ref.Seqno {
		t.Errorf("Seqno: %d != %d", got.Seqno, ref.Seqno)
	}
	if got.ExtentSize != ref.ExtentSize {
		t.Errorf("ExtentSize: %d != %d", got.ExtentSize, ref.ExtentSize)
	}
	if !reflect.DeepEqual(got.Status, ref.Status) {
		t.Errorf("Status: %v != %v", got.Status, ref.Status)
	}
	if !reflect.DeepEqual(got.Flags, ref.Flags) {
		t.Errorf("Flags: %v != %v", got.Flags, ref.Flags)
	}
	if !reflect.DeepEqual(got.PVs, ref.PVs) {
		t.Errorf("PVs differ:\n got = %+v\n ref = %+v", got.PVs, ref.PVs)
	}
	if !reflect.DeepEqual(got.LVs, ref.LVs) {
		t.Errorf("LVs differ:\n got = %+v\n ref = %+v", got.LVs, ref.LVs)
	}
}

// diffCases covers one fixture per supported layout: every RAID level, mirror,
// thin pool/LV/snapshot, cache pool/origin/cachevol, snapshot, striped, and
// multi-PV (empty/spanning+pinned/six-PV). writecache and missing-PV are
// asserted separately (unsupported/partial).
var diffCases = []string{
	"lin-03-lv",
	"lin-04-two-lvs-lv0-extended-noncontiguous",
	"stripe-01-3way-64k",
	"raid0-01-created",
	"raid1-02-synced",
	"raid4-01-synced",
	"raid5-01-synced",
	"raid6-01-synced",
	"raid10-01-synced",
	"mirror-01-synced",
	"thin-01-pool-only",
	"thin-02-thin-lv",
	"thin-03-thin-snapshot",
	"cache-01-origin-and-unattached-cachepool",
	"cache-02-cached-origin",
	"cache-03-cachevol-writeback",
	"snap-01-cow-snapshot",
	"multi-02-vg-empty",
	"multi-03-spanning-and-pinned-lv",
	"multi-05-six-pv-final",
}

func TestRealFixturesDifferential(t *testing.T) {
	for _, c := range diffCases {
		t.Run(c, func(t *testing.T) {
			raw := readFixture(t, c)
			pv := ReadPV(bytes.NewReader(raw), uint64(len(raw)), "vdb")
			got := parseVGFromPV(t, pv)
			ref := parseVGFromText(t, readTestdata(t, c+".vgcfg"))
			assertSameVG(t, got, ref)
		})
	}
}

func TestRealFixtureLin03LV(t *testing.T) {
	b := readFixture(t, "lin-03-lv")
	pv := ReadPV(bytes.NewReader(b), uint64(len(b)), "vdb")
	if pv.Status != StatusOK {
		t.Fatalf("status = %s", pv.Status)
	}
	if pv.Label == nil || pv.Label.PVUUID != "nV0beL5jP21S4b6t9zB5b5YmeWwXGrJm" {
		t.Fatalf("label = %+v", pv.Label)
	}
	if pv.Label.DevSize != 536870912 {
		t.Fatalf("dev size = %d, want 536870912", pv.Label.DevSize)
	}
	vg := parseVGFromPV(t, pv)
	if vg.Name != "fxlin" || vg.Seqno != 2 || vg.ExtentSize != 8192 {
		t.Fatalf("vg = %+v", vg)
	}
	if len(vg.PVs) != 1 || vg.PVs[0].UUID != "nV0beL5jP21S4b6t9zB5b5YmeWwXGrJm" {
		t.Fatalf("pvs = %+v", vg.PVs)
	}
	if vg.PVs[0].DevSize != 536870912 || vg.PVs[0].PEStart != 2048 || vg.PVs[0].PECount != 127 {
		t.Fatalf("pv0 = %+v", vg.PVs[0])
	}
	if len(vg.LVs) != 1 || vg.LVs[0].Name != "lv0" {
		t.Fatalf("lvs = %+v", vg.LVs)
	}
	seg := vg.LVs[0].Segments[0]
	if seg.Type != "striped" || !seg.Parsed || len(seg.Stripes) != 1 || seg.Stripes[0].PVKey != "pv0" {
		t.Fatalf("segment = %+v", seg)
	}

	inv := Assemble([]PVRead{pv})
	if len(inv.VGs) != 1 || inv.VGs[0].State != StateComplete {
		t.Fatalf("assemble = %+v", inv.VGs)
	}
}

func TestRealFixtureLin01PVOnly(t *testing.T) {
	b := readFixture(t, "lin-01-pv-only")
	pv := ReadPV(bytes.NewReader(b), uint64(len(b)), "vdb")
	if pv.Status != StatusOK {
		t.Fatalf("status = %s", pv.Status)
	}
	if pv.Label == nil {
		t.Fatal("no label")
	}
	if pv.Label.PVUUID != "nV0beL5jP21S4b6t9zB5b5YmeWwXGrJm" {
		t.Fatalf("label uuid = %q", pv.Label.PVUUID)
	}
	if pv.Label.DevSize != 536870912 {
		t.Fatalf("dev size = %d, want 536870912", pv.Label.DevSize)
	}
	if len(pv.Copies) != 0 || pv.Text != nil {
		t.Fatalf("PV-only must have no metadata copies/text: copies=%d text=%d", len(pv.Copies), len(pv.Text))
	}
	inv := Assemble([]PVRead{pv})
	if len(inv.VGs) != 0 {
		t.Fatalf("unexpected VG from PV-only: %+v", inv.VGs)
	}
	if len(inv.Orphans) != 0 {
		t.Fatalf("PV-only must not be orphaned: %+v", inv.Orphans)
	}
}

func TestRealFixtureWritecachePartial(t *testing.T) {
	// writecache is not yet supported: it must be typed partial, never complete.
	b := readFixture(t, "writecache-01-attached")
	pv := ReadPV(bytes.NewReader(b), uint64(len(b)), "vdb")
	vg := parseVGFromPV(t, pv)

	found := false
	for _, lv := range vg.LVs {
		for _, seg := range lv.Segments {
			if seg.Type == "writecache" {
				found = true
				if seg.Parsed {
					t.Fatalf("writecache segment reported Parsed=true: %+v", seg)
				}
			}
		}
	}
	if !found {
		t.Fatal("no writecache segment found in fixture")
	}

	inv := Assemble([]PVRead{pv})
	if len(inv.VGs) != 1 {
		t.Fatalf("vgs = %+v", inv.VGs)
	}
	if inv.VGs[0].State != StateUnsupported {
		t.Fatalf("state = %s, want partial-unsupported-segment", inv.VGs[0].State)
	}
}

func TestRealFixtureMissingPV(t *testing.T) {
	// multi-04 has one PV whose disk is blank in the fixture (its label is
	// absent). The on-disk metadata still names it as a normal PV (the
	// vgcfgbackup reference was taken later and shows the MISSING flag, but the
	// raw committed metadata does not), so the reader must classify it
	// not-found and the VG partial-missing.
	vdbRaw := readFixture(t, "multi-04-missing-pv-partial-vg.vdb")
	vdcRaw := readFixture(t, "multi-04-missing-pv-partial-vg.vdc")
	vddRaw := readFixture(t, "multi-04-missing-pv-partial-vg.vdd")
	vdb := ReadPV(bytes.NewReader(vdbRaw), uint64(len(vdbRaw)), "vdb")
	vdc := ReadPV(bytes.NewReader(vdcRaw), uint64(len(vdcRaw)), "vdc")
	vdd := ReadPV(bytes.NewReader(vddRaw), uint64(len(vddRaw)), "vdd")

	if vdc.Status != StatusNotLVM {
		t.Fatalf("vdc (blank/missing disk) status = %s, want not-lvm", vdc.Status)
	}
	inv := Assemble([]PVRead{vdb, vdc, vdd})
	if len(inv.VGs) != 1 {
		t.Fatalf("vgs = %+v", inv.VGs)
	}
	vg := inv.VGs[0]
	if vg.State != StatePartialMissing {
		t.Fatalf("state = %s, want partial-missing-pv", vg.State)
	}
	present, notFound := 0, 0
	for _, pv := range vg.PVs {
		switch pv.Presence {
		case PresencePresent:
			present++
		case PresenceNotFound:
			notFound++
		}
	}
	if present != 2 || notFound != 1 {
		t.Fatalf("presence = present:%d not-found:%d, want 2/1: %+v", present, notFound, vg.PVs)
	}
	hasNote := false
	for _, n := range vg.Notes {
		if strings.Contains(n, "missing PV") {
			hasNote = true
		}
	}
	if !hasNote {
		t.Fatalf("notes = %v, want missing-PV note", vg.Notes)
	}
}
