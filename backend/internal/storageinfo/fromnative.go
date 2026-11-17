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
	out := LVMInventory{Mode: ModeMetadataOnly, Source: "on-disk metadata", Volumes: []LogicalVolume{}, PhysicalVolumes: []PhysicalVolume{}}
	var notes []string

	for _, vg := range inv.VGs {
		vgUUID := dashUUID(vg.UUID)
		// extent_size is in 512-byte sectors; convert with overflow check so a
		// hostile value becomes a typed note, never a wrapped byte count.
		extentBytes, extentOK := mulChecked(vg.ExtentSize, 512)
		if !extentOK {
			notes = append(notes, "malformed-extent-size:"+vg.Name)
		}

		// VG-scoped LV name -> UUID, for resolving segment component names.
		lvUUID := make(map[string]string, len(vg.LVs))
		for _, lv := range vg.LVs {
			lvUUID[lv.Name] = dashUUID(lv.UUID)
		}

		for _, pv := range vg.PVs {
			spv := PhysicalVolume{
				UUID:         dashUUID(pv.UUID),
				VGUUID:       vgUUID,
				VGName:       vg.Name,
				DeviceNumber: pv.DeviceNumber,
			}
			if pv.DevSize > 0 {
				spv.DevSizeBytes = &pv.DevSize
			}
			// DeviceLink/DeviceLinkReason are decided by addPhysicalVolumes/
			// linkPV (verified/provisional/withheld/no-device), never here.
			switch pv.Presence {
			case lvmmeta.PresencePresent:
				spv.MissingState = "present"
			case lvmmeta.PresenceMissingFlag:
				spv.Missing = true
				spv.MissingState = "missing"
			case lvmmeta.PresenceDuplicate:
				spv.Ambiguous = true
				spv.MissingState = "not-reported"
			default:
				spv.MissingState = "not-reported"
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
				mapped, rel, malformed := fromNativeSegment(seg, extentBytes, extentOK, lvUUID)
				slv.Segments = append(slv.Segments, mapped)
				if malformed {
					notes = append(notes, "malformed-segment-size:"+lv.Name)
				}
				if slv.OriginUUID == "" {
					slv.OriginUUID = rel.Origin
				}
				if slv.PoolUUID == "" {
					slv.PoolUUID = rel.Pool
				}
				if slv.DataUUID == "" {
					slv.DataUUID = rel.Data
				}
				if slv.MetadataUUID == "" {
					slv.MetadataUUID = rel.Metadata
				}
				if !seg.Parsed {
					notes = append(notes, "unsupported-segment-type:"+seg.Type)
				}
			}
			out.Volumes = append(out.Volumes, slv)
		}

		// The VG's accumulated notes cover every partial condition; the single
		// State is only the dominant classification, so no reason is dropped.
		notes = append(notes, vg.Notes...)
	}
	return out, notes
}

// segRel carries UUID relations a segment contributes to its parent LV,
// resolved from VG-scoped LV names.
type segRel struct {
	Origin   string
	Pool     string
	Data     string
	Metadata string
}

// fromNativeSegment maps one native segment into an LVMSegment plus the UUID
// relations its component names resolve to. Unknown types stay Parsed=false and
// are not invented. The bool result is true when a size multiplication
// overflowed and the byte fields were omitted (typed partial, never wrapped).
func fromNativeSegment(s lvmmeta.Segment, extentBytes uint64, extentOK bool, lvUUID map[string]string) (LVMSegment, segRel, bool) {
	o := LVMSegment{Type: s.Type}
	malformed := false
	if extentOK && extentBytes > 0 {
		start, ok1 := mulChecked(s.StartExtent, extentBytes)
		size, ok2 := mulChecked(s.ExtentCount, extentBytes)
		if ok1 && ok2 {
			o.StartBytes = &start
			o.SizeBytes = &size
		} else {
			malformed = true
		}
	}

	var rel segRel
	resolve := func(name string) string { return lvUUID[name] }

	switch {
	case s.Raid != nil:
		o.Devices = areaNames(s.Raid.Images)
		o.MetadataDevices = areaNames(s.Raid.Metas)
	case s.ThinPool != nil:
		rel.Data = resolve(s.ThinPool.DataLV)
		rel.Metadata = resolve(s.ThinPool.MetadataLV)
	case s.Thin != nil:
		rel.Pool = resolve(s.Thin.ThinPool)
		rel.Origin = resolve(s.Thin.Origin)
	case s.CachePool != nil:
		rel.Data = resolve(s.CachePool.DataLV)
		rel.Metadata = resolve(s.CachePool.MetadataLV)
		o.CacheMode = s.CachePool.CacheMode
		o.CachePolicy = s.CachePool.Policy
	case s.Cache != nil:
		rel.Pool = resolve(s.Cache.CachePool)
		rel.Origin = resolve(s.Cache.Origin)
	case s.Mirror != nil:
		o.Devices = areaNames(s.Mirror.Legs)
	case s.Snapshot != nil:
		rel.Origin = resolve(s.Snapshot.Origin)
	default:
		// striped / linear
		o.Devices = areaNames(s.Stripes)
	}
	return o, rel, malformed
}

// mulChecked returns a*b and whether it overflowed (C9 checked arithmetic).
func mulChecked(a, b uint64) (uint64, bool) {
	if b != 0 && a > ^uint64(0)/b {
		return 0, false
	}
	return a * b, true
}

// areaNames joins the PV keys (or hidden LV names) of areas as display text.
func areaNames(areas []lvmmeta.Area) string {
	names := make([]string, 0, len(areas))
	for _, a := range areas {
		names = append(names, a.PVKey)
	}
	return strings.Join(names, ",")
}

// dashUUID formats a 32-char undashed LVM2 UUID as 6-4-4-4-4-4-6 (the
// id_write_format group sizes). A '#-prefixed' new-format UUID is returned
// undashed. Non-32-char strings are returned unchanged.
func dashUUID(undashed string) string {
	if len(undashed) != 32 {
		return undashed
	}
	if undashed[0] == '#' {
		return undashed
	}
	return undashed[0:6] + "-" + undashed[6:10] + "-" + undashed[10:14] + "-" + undashed[14:18] + "-" + undashed[18:22] + "-" + undashed[22:26] + "-" + undashed[26:32]
}
