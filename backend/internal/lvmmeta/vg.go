// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvmmeta

import (
	"fmt"
	"strconv"
)

// VGState is the closed per-VG state.
type VGState string

const (
	StateComplete       VGState = "complete"
	StatePartialMissing VGState = "partial-missing-pv"
	StateInconsistent   VGState = "inconsistent"
	StateNoMetadata     VGState = "no-metadata"
	// StateUnsupported marks a VG whose metadata contains a segment type the
	// parser does not yet cover; it must never be reported as a complete
	// inventory.
	StateUnsupported VGState = "partial-unsupported-segment"
)

// Presence describes whether a VG's PV was found among the probed devices.
type Presence string

const (
	PresencePresent     Presence = "present"
	PresenceMissingFlag Presence = "missing-flag"
	PresenceNotFound    Presence = "not-found"
	PresenceDuplicate   Presence = "duplicate"
)

// Area is one striped/linear data area (PV key + extent offset).
type Area struct {
	PVKey  string
	Offset uint64
}

// RaidSeg is a raid0/1/4/5/6/10 segment: data images and metadata images,
// each as an area (PV key or hidden LV name + offset).
type RaidSeg struct {
	DeviceCount uint64
	RegionSize  uint64
	StripeSize  uint64
	Images      []Area
	Metas       []Area
}

// ThinPoolSeg is a thin-pool segment.
type ThinPoolSeg struct {
	MetadataLV    string
	DataLV        string
	ChunkSize     uint64
	TransactionID uint64
	Discards      string
	ZeroNewBlocks string
}

// ThinSeg is a thin (thinly-provisioned LV) segment.
type ThinSeg struct {
	ThinPool      string
	Origin        string
	DeviceID      uint64
	TransactionID uint64
}

// CachePoolSeg is a cache-pool segment.
type CachePoolSeg struct {
	DataLV     string
	MetadataLV string
	CacheMode  string
	Policy     string
	ChunkSize  uint64
}

// CacheSeg is a cache segment.
type CacheSeg struct {
	CachePool string
	Origin    string
}

// MirrorSeg is a legacy mirror segment.
type MirrorSeg struct {
	Legs []Area
	Log  string
}

// SnapshotSeg is an old-style snapshot segment.
type SnapshotSeg struct {
	Origin    string
	COWStore  string
	ChunkSize uint64
}

// Segment is one LV segment. Unknown segment types are kept with Parsed=false.
type Segment struct {
	StartExtent uint64
	ExtentCount uint64
	Type        string
	Parsed      bool
	Stripes     []Area
	StripeSize  uint64
	Raid        *RaidSeg
	ThinPool    *ThinPoolSeg
	Thin        *ThinSeg
	CachePool   *CachePoolSeg
	Cache       *CacheSeg
	Mirror      *MirrorSeg
	Snapshot    *SnapshotSeg
	Unsupported string
}

// LV is one logical volume.
type LV struct {
	UUID     string
	Name     string
	Status   []string
	Flags    []string
	Segments []Segment
}

// VGPV is one physical volume in a VG's metadata.
type VGPV struct {
	Key        string
	UUID       string
	DeviceHint string
	DevSize    uint64 // bytes (text dev_size is sectors; converted)
	PEStart    uint64 // sectors
	PECount    uint64 // extents
	Flags      []string
	Device     string
	Presence   Presence
}

// VG is one volume group assembled from metadata text.
type VG struct {
	UUID       string
	Name       string
	Seqno      uint64
	ExtentSize uint64
	Status     []string
	Flags      []string
	PVs        []VGPV
	LVs        []LV
	State      VGState
}

// Inventory is the assembled result for a probe request.
type Inventory struct {
	VGs     []VG
	PVs     []PVRead
	Orphans []PVRead
	// Notes carries typed aggregate outcomes (e.g. metadata text over cap).
	Notes []string
}

// ParseVG maps the parsed VG section into a typed VG.
func ParseVG(name string, n *Node) (*VG, error) {
	if n == nil || n.Kind != kindSection {
		return nil, fmt.Errorf("VG %q is not a section", name)
	}
	vg := &VG{Name: name}
	if id := n.child("id"); id != nil && id.Kind == kindScalar {
		vg.UUID = undash(id.Value)
	}
	if seq := n.child("seqno"); seq != nil && seq.Kind == kindScalar {
		vg.Seqno, _ = strconv.ParseUint(seq.Value, 10, 64)
	}
	if es := n.child("extent_size"); es != nil && es.Kind == kindScalar {
		vg.ExtentSize, _ = strconv.ParseUint(es.Value, 10, 64)
	}
	vg.Status = stringArray(n.child("status"))
	vg.Flags = stringArray(n.child("flags"))

	if pvs := n.child("physical_volumes"); pvs != nil {
		for _, c := range pvs.Children {
			if len(vg.PVs) >= MaxPVsPerVG {
				break
			}
			vg.PVs = append(vg.PVs, parseVGPV(c.Name, c))
		}
	}
	if lvs := n.child("logical_volumes"); lvs != nil {
		for _, c := range lvs.Children {
			if len(vg.LVs) >= MaxLVsPerVG {
				break
			}
			vg.LVs = append(vg.LVs, parseLV(c.Name, c))
		}
	}
	return vg, nil
}

func parseVGPV(key string, n *Node) VGPV {
	p := VGPV{Key: key}
	if id := n.child("id"); id != nil {
		p.UUID = undash(id.Value)
	}
	if d := n.child("device"); d != nil {
		p.DeviceHint = d.Value
	}
	if d := n.child("dev_size"); d != nil {
		// Text dev_size is in 512-byte sectors (lvm2 internal unit); the graph
		// and identity checks use bytes, so convert.
		sectors, _ := strconv.ParseUint(d.Value, 10, 64)
		p.DevSize = sectors * 512
	}
	if d := n.child("pe_start"); d != nil {
		p.PEStart, _ = strconv.ParseUint(d.Value, 10, 64)
	}
	if d := n.child("pe_count"); d != nil {
		p.PECount, _ = strconv.ParseUint(d.Value, 10, 64)
	}
	p.Flags = stringArray(n.child("flags"))
	p.Presence = PresenceMissingFlag
	for _, f := range p.Flags {
		if f == "MISSING" {
			p.Presence = PresenceMissingFlag
		}
	}
	if !hasFlag(p.Flags, "MISSING") {
		p.Presence = PresenceNotFound
	}
	return p
}

func parseLV(name string, n *Node) LV {
	lv := LV{Name: name}
	if id := n.child("id"); id != nil {
		lv.UUID = undash(id.Value)
	}
	lv.Status = stringArray(n.child("status"))
	lv.Flags = stringArray(n.child("flags"))
	for i := 1; i <= MaxSegmentsPerLV; i++ {
		seg := n.child("segment" + strconv.Itoa(i))
		if seg == nil {
			break
		}
		lv.Segments = append(lv.Segments, parseSegment(seg))
	}
	return lv
}

func parseSegment(n *Node) Segment {
	s := Segment{Parsed: true}
	if d := n.child("start_extent"); d != nil {
		s.StartExtent, _ = strconv.ParseUint(d.Value, 10, 64)
	}
	if d := n.child("extent_count"); d != nil {
		s.ExtentCount, _ = strconv.ParseUint(d.Value, 10, 64)
	}
	if d := n.child("type"); d != nil {
		s.Type = d.Value
	}
	switch s.Type {
	case "striped":
		s.StripeSize = uintChild(n, "stripe_size")
		s.Stripes = areaList(n.child("stripes"))
	case "raid0", "raid1", "raid4", "raid5", "raid6", "raid10":
		r := &RaidSeg{DeviceCount: uintChild(n, "device_count"), RegionSize: uintChild(n, "region_size"), StripeSize: uintChild(n, "stripe_size")}
		r.Images = areaList(n.child("raids"))
		r.Metas = areaList(n.child("meta_devices"))
		s.Raid = r
	case "thin-pool":
		s.ThinPool = &ThinPoolSeg{
			MetadataLV:    strChild(n, "metadata"),
			DataLV:        strChild(n, "pool"),
			ChunkSize:     uintChild(n, "chunk_size"),
			TransactionID: uintChild(n, "transaction_id"),
			Discards:      strChild(n, "discards"),
			ZeroNewBlocks: strChild(n, "zero_new_blocks"),
		}
	case "thin":
		s.Thin = &ThinSeg{
			ThinPool:      strChild(n, "thin_pool"),
			Origin:        strChild(n, "origin"),
			DeviceID:      uintChild(n, "device_id"),
			TransactionID: uintChild(n, "transaction_id"),
		}
	case "cache-pool":
		s.CachePool = &CachePoolSeg{
			DataLV:     strChild(n, "data"),
			MetadataLV: strChild(n, "metadata"),
			CacheMode:  strChild(n, "cache_mode"),
			Policy:     strChild(n, "policy"),
			ChunkSize:  uintChild(n, "chunk_size"),
		}
	case "cache":
		s.Cache = &CacheSeg{CachePool: strChild(n, "cache_pool"), Origin: strChild(n, "origin")}
	case "mirror":
		s.Mirror = &MirrorSeg{Legs: areaList(n.child("mirrors")), Log: strChild(n, "mirror_log")}
	case "snapshot":
		s.Snapshot = &SnapshotSeg{Origin: strChild(n, "origin"), COWStore: strChild(n, "cow_store"), ChunkSize: uintChild(n, "chunk_size")}
	default:
		s.Parsed = false
		s.Unsupported = "unsupported-segment-type:" + s.Type
	}
	return s
}

// uintChild reads a child's integer value, 0 when absent.
func uintChild(n *Node, name string) uint64 {
	if d := n.child(name); d != nil && d.Kind == kindScalar {
		v, _ := strconv.ParseUint(d.Value, 10, 64)
		return v
	}
	return 0
}

// strChild reads a child's string value, "" when absent.
func strChild(n *Node, name string) string {
	if d := n.child(name); d != nil && d.Kind == kindScalar {
		return d.Value
	}
	return ""
}

// areaList parses a [name, offset, name, offset, ...] array into areas.
func areaList(n *Node) []Area {
	if n == nil || n.Kind != kindArray {
		return nil
	}
	var out []Area
	for i := 0; i+1 < len(n.Values); i += 2 {
		off, _ := strconv.ParseUint(n.Values[i+1], 10, 64)
		out = append(out, Area{PVKey: n.Values[i], Offset: off})
	}
	return out
}

func stringArray(n *Node) []string {
	if n == nil || n.Kind != kindArray {
		return nil
	}
	return append([]string(nil), n.Values...)
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

// undash removes dashes from a UUID string.
func undash(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

// vgUUID returns the lowercased undashed VG UUID from a VG section's id child,
// and whether it is a valid scalar UUID. Missing or wrong-kind id is not valid.
func vgUUID(n *Node) (string, bool) {
	id := n.child("id")
	if id == nil || id.Kind != kindScalar || id.Value == "" {
		return "", false
	}
	u := uuidString([]byte(undash(id.Value)))
	return u, u != ""
}

// Assemble groups PV reads by VG UUID, chooses the highest-seqno copy and
// marks missing/duplicate PVs. It never drops unknown data; inconsistent
// copies are flagged.
func Assemble(pvs []PVRead) Inventory {
	inv := Inventory{PVs: pvs}

	// Enforce the aggregate metadata-text budget before grouping: a device
	// whose retained text no longer fits is classified orphan/limit-exceeded and
	// its text is dropped, so no unbounded aggregate is kept.
	budget := int64(MaxTotalText)
	overCap := false
	for i := range pvs {
		pv := &pvs[i]
		if pv.Text == nil {
			continue
		}
		if int64(len(pv.Text)) > budget {
			pv.Text = nil
			pv.Status = StatusLimitExceeded
			inv.Orphans = append(inv.Orphans, *pv)
			overCap = true
			continue
		}
		budget -= int64(len(pv.Text))
	}
	if overCap {
		inv.Notes = append(inv.Notes, "metadata text exceeds MaxTotalText")
	}

	byUUID := map[string][]*Node{}
	for _, pv := range pvs {
		if pv.Text == nil {
			continue // already classified (e.g. over-cap) in the budget pass
		}
		root, err := parseText(pv.Text)
		if err != nil {
			inv.Orphans = append(inv.Orphans, pv)
			continue
		}
		vgNode := findVGNode(root)
		if vgNode == nil {
			inv.Orphans = append(inv.Orphans, pv)
			continue
		}
		id, ok := vgUUID(vgNode)
		if !ok {
			// Malformed VG section: missing or wrong-kind id. Never dereference it.
			inv.Orphans = append(inv.Orphans, pv)
			continue
		}
		byUUID[id] = append(byUUID[id], vgNode)
	}
	for uuid, nodes := range byUUID {
		best := nodes[0]
		for _, n := range nodes[1:] {
			if seq(n) > seq(best) {
				best = n
			}
		}
		vg, err := ParseVG(best.Name, best)
		if err != nil {
			continue
		}
		vg.UUID = uuid
		vg.State = StateComplete
		// Mark presence.
		seen := map[string]int{}
		for i := range vg.PVs {
			for _, pv := range pvs {
				if pv.Label != nil && pv.Label.PVUUID == vg.PVs[i].UUID {
					vg.PVs[i].Device = pv.Device
					seen[vg.PVs[i].UUID]++
				}
			}
			if seen[vg.PVs[i].UUID] > 1 {
				vg.PVs[i].Presence = PresenceDuplicate
				vg.State = StateInconsistent
			} else if seen[vg.PVs[i].UUID] == 1 {
				vg.PVs[i].Presence = PresencePresent
			} else if hasFlag(vg.PVs[i].Flags, "MISSING") {
				vg.PVs[i].Presence = PresenceMissingFlag
				vg.State = StatePartialMissing
			} else {
				vg.PVs[i].Presence = PresenceNotFound
				vg.State = StatePartialMissing
			}
		}
		// Any unsupported segment type makes the VG partial, never complete.
		if hasUnsupportedSegment(vg) {
			vg.State = StateUnsupported
		}
		inv.VGs = append(inv.VGs, *vg)
	}
	return inv
}

// hasUnsupportedSegment reports whether any LV has a segment the parser does
// not yet cover.
func hasUnsupportedSegment(vg *VG) bool {
	for _, lv := range vg.LVs {
		for _, seg := range lv.Segments {
			if !seg.Parsed {
				return true
			}
		}
	}
	return false
}

// findVGNode returns the single VG section among the top-level keys, ignoring
// the wrapper keys contents/version/description/creation_host/creation_time.
func findVGNode(root map[string]*Node) *Node {
	for name, n := range root {
		switch name {
		case "contents", "version", "description", "creation_host", "creation_time":
			continue
		}
		if n.Kind == kindSection {
			return n
		}
	}
	return nil
}

func seq(n *Node) uint64 {
	if n == nil {
		return 0
	}
	if s := n.child("seqno"); s != nil {
		v, _ := strconv.ParseUint(s.Value, 10, 64)
		return v
	}
	return 0
}
