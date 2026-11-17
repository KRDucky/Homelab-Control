// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvmmeta

import (
	"bytes"
	"os"
	"testing"
)

// These fixtures are the first 8 KiB of real LVM2-written disks (lvm2 2.03.38)
// captured in a disposable Fedora VM. Provenance and reference captures are in
// /mnt/tank/claude-vms/artifacts/m24-lvmmeta-fixtures-20261117b/{raw,json}/.
// The first 8 KiB hold the label (sector 1), pv_header, MDA header and the
// metadata text, which is all the reader needs.

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".raw")
	if err != nil {
		t.Skipf("fixture %s not present: %v", name, err)
	}
	return b
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
	root, err := parseText(pv.Text)
	if err != nil {
		t.Fatalf("parse text: %v", err)
	}
	vgNode := findVGNode(root)
	if vgNode == nil {
		t.Fatal("no VG section")
	}
	vg, err := ParseVG(vgNode.Name, vgNode)
	if err != nil {
		t.Fatal(err)
	}
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
	inv := Assemble([]PVRead{pv})
	if len(inv.VGs) != 0 {
		t.Fatalf("unexpected VG from PV-only: %+v", inv.VGs)
	}
}
