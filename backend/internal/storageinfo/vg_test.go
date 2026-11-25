// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"os"
	"strings"
	"testing"
)

// Real `vgs` output from LVM 2.03.31 (Ubuntu 26.04 guest).
func TestParseVGSReal(t *testing.T) {
	f, err := os.Open("testdata/vgs-2.03.31.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	vgs, err := ParseVGS(f)
	if err != nil || len(vgs) != 1 {
		t.Fatalf("%+v %v", vgs, err)
	}
	v := vgs[0]
	if v.Name != "datavg" || v.UUID != "oqeX38-G06Z-JQ5n-dkLB-y3W5-DR8N-KjepGN" || *v.SizeBytes != 4290772992 || *v.FreeBytes != 2680160256 ||
		v.Attributes != "wz--n-" || *v.PVCount != 1 || *v.LVCount != 2 || *v.MissingPVCount != 0 {
		t.Fatalf("%+v", v)
	}
}

func TestParseVGSRejects(t *testing.T) {
	for _, in := range []string{
		`{"report":[{"vg":[{"vg_uuid":"","vg_name":"x"}]}]}`,                                        // no identity
		`{"report":[{"vg":[{"vg_uuid":"oqeX38-G06Z-JQ5n-dkLB-y3W5-DR8N-KjepGN","vg_size":"-1"}]}]}`, // bad number
		`not json`,
	} {
		if _, err := ParseVGS(strings.NewReader(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
	vgs, err := ParseVGS(strings.NewReader(`{"report":[{"vg":[]}]}`))
	if err != nil || len(vgs) != 0 {
		t.Fatalf("empty: %+v %v", vgs, err)
	}
}
