// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"strings"

	"homelab-control/backend/internal/lvmmeta"
)

// FromNative converts a native-reader inventory into the storageinfo LVM
// inventory model. It does not build the graph: AddLVMRevalidated/linkPV remain
// the single graph-linking logic. Notes carry typed partial/unsupported/
// withheld outcomes. Unresolved segment types are never reported complete.
func FromNative(inv lvmmeta.Inventory) (LVMInventory, []string) {
	out := LVMInventory{Mode: "native", Source: "on-disk metadata", Volumes: []LogicalVolume{}, PhysicalVolumes: []PhysicalVolume{}}
	var notes []string

	for _, vg := range inv.VGs {
		vgUUID := dashUUID(vg.UUID)
		extentBytes := vg.ExtentSize * 512

		for _, pv := range vg.PVs {
			spv := PhysicalVolume{
				UUID:   dashUUID(pv.UUID),
				VGUUID: vgUUID,
				VGName: vg.Name,
			}
			if pv.DevSize > 0 {
				spv.DevSizeBytes = &pv.DevSize
			}
			switch pv.Presence {
			case lvmmeta.PresencePresent:
				spv.MissingState = "present"
				spv.DeviceLink = "verified"
			case lvmmeta.PresenceMissingFlag:
				spv.Missing = true
				spv.MissingState = "missing"
				spv.DeviceLink = "withheld"
				spv.DeviceLinkReason = "MISSING flag"
			case lvmmeta.PresenceDuplicate:
				spv.Ambiguous = true
				spv.MissingState = "not-reported"
				spv.DeviceLink = "withheld"
				spv.DeviceLinkReason = "duplicate PV UUID"
			default:
				spv.MissingState = "not-reported"
				spv.DeviceLink = "no-device"
				spv.DeviceLinkReason = "PV not found among probed devices"
			}
			out.PhysicalVolumes = append(out.PhysicalVolumes, spv)
		}

		for _, lv := range vg.LVs {
			slv := LogicalVolume{
				UUID:   dashUUID(lv.UUID),
				Name:   lv.Name,
				VGUUID: vgUUID,
				VGName: vg.Name,
				Health: "unknown", // runtime health is O-2
			}
			for _, seg := range lv.Segments {
				slv.Segments = append(slv.Segments, fromNativeSegment(seg, extentBytes))
				if !seg.Parsed {
					notes = append(notes, "unsupported-segment-type:"+seg.Type)
				}
			}
			out.Volumes = append(out.Volumes, slv)
		}

		switch vg.State {
		case lvmmeta.StateUnsupported:
			notes = append(notes, "vg "+vg.Name+": partial (unsupported segment type)")
		case lvmmeta.StatePartialMissing:
			notes = append(notes, "vg "+vg.Name+": partial (missing PV)")
		case lvmmeta.StateInconsistent:
			notes = append(notes, "vg "+vg.Name+": inconsistent metadata")
		case lvmmeta.StateNoMetadata:
			notes = append(notes, "vg "+vg.Name+": no metadata")
		}
	}
	return out, notes
}

func fromNativeSegment(s lvmmeta.Segment, extentBytes uint64) LVMSegment {
	o := LVMSegment{Type: s.Type}
	if extentBytes > 0 {
		start := s.StartExtent * extentBytes
		size := s.ExtentCount * extentBytes
		o.StartBytes = &start
		o.SizeBytes = &size
	}
	devs := make([]string, 0, len(s.Stripes))
	for _, a := range s.Stripes {
		devs = append(devs, a.PVKey)
	}
	o.Devices = strings.Join(devs, ",")
	if s.CachePool != nil {
		o.CacheMode = s.CachePool.CacheMode
		o.CachePolicy = s.CachePool.Policy
	}
	return o
}

// dashUUID formats a 32-char undashed hex UUID as 8-4-4-4-12. Non-UUID-length
// strings are returned unchanged.
func dashUUID(undashed string) string {
	if len(undashed) != 32 {
		return undashed
	}
	return undashed[0:8] + "-" + undashed[8:12] + "-" + undashed[12:16] + "-" + undashed[16:20] + "-" + undashed[20:32]
}
