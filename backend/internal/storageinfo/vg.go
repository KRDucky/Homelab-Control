// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"fmt"
	"io"
	"strings"
)

// VolumeGroup is one `vgs` row: LVM's own size, free space and counts.
type VolumeGroup struct {
	UUID           string  `json:"uuid"`
	Name           string  `json:"name"`
	SizeBytes      *uint64 `json:"size_bytes,omitempty"`
	FreeBytes      *uint64 `json:"free_bytes,omitempty"`
	Attributes     string  `json:"attributes,omitempty"`
	PVCount        *uint64 `json:"pv_count,omitempty"`
	LVCount        *uint64 `json:"lv_count,omitempty"`
	MissingPVCount *uint64 `json:"missing_pv_count,omitempty"`
}

type vgRow struct {
	UUID    string `json:"vg_uuid"`
	Name    string `json:"vg_name"`
	Size    string `json:"vg_size"`
	Free    string `json:"vg_free"`
	Attr    string `json:"vg_attr"`
	PVs     string `json:"pv_count"`
	LVs     string `json:"lv_count"`
	Missing string `json:"vg_missing_pv_count"`
}

// ParseVGS parses a `vgs --reportformat json --units b --nosuffix` report
// (every value quoted, as ParsePVS expects).
func ParseVGS(r io.Reader) ([]VolumeGroup, error) {
	b, err := readReport(r)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Report []struct {
			VG []vgRow `json:"vg"`
		} `json:"report"`
	}
	if err := decodeReport(b, &doc); err != nil {
		return nil, err
	}
	if doc.Report == nil || len(doc.Report) > 64 {
		return nil, fmt.Errorf("missing or excessive LVM report groups")
	}
	out := []VolumeGroup{}
	for _, rep := range doc.Report {
		for _, row := range rep.VG {
			if len(out) >= 1024 {
				return nil, fmt.Errorf("VG report exceeds row limit")
			}
			if !lvmIdentifier(row.UUID) {
				return nil, fmt.Errorf("VG row missing stable identity or has an invalid UUID")
			}
			vg := VolumeGroup{UUID: row.UUID, Name: strings.TrimSpace(row.Name), Attributes: strings.TrimSpace(row.Attr)}
			for _, f := range []struct {
				dst **uint64
				v   string
			}{{&vg.SizeBytes, row.Size}, {&vg.FreeBytes, row.Free}, {&vg.PVCount, row.PVs}, {&vg.LVCount, row.LVs}, {&vg.MissingPVCount, row.Missing}} {
				if *f.dst, err = lvmUint(f.v); err != nil {
					return nil, err
				}
			}
			out = append(out, vg)
		}
	}
	return out, nil
}
