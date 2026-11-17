// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvmmeta

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
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
	// StateMalformed marks a VG whose metadata has a numeric field that cannot
	// be converted safely (e.g. a dev_size whose sector count overflows when
	// multiplied to bytes); it must never be reported complete.
	StateMalformed VGState = "partial-malformed"
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
	// Malformed is set when a required numeric scalar (start_extent,
	// extent_count) is missing or invalid; the LV/VG must then never be
	// reported complete.
	Malformed   bool
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
	// Malformed is set when segment_count is missing/invalid, segment rows are
	// non-contiguous, or a segment scalar is malformed; the VG must then never
	// be reported complete.
	Malformed bool
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
	// Malformed is set when a numeric field cannot be converted safely (parse
	// error or overflow); the VG must then never be reported complete.
	Malformed bool
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
	// Malformed is set when a required VG-level scalar (seqno, extent_size) is
	// missing or invalid; the VG must then never be reported complete.
	Malformed bool
	// Notes accumulates every partial condition (missing PV, inconsistent,
	// malformed size, unsupported segment) so no single State value hides a
	// second one.
	Notes []string
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
	if v, ok := requiredUint(n, "seqno"); ok {
		vg.Seqno = v
	} else {
		vg.Malformed = true
	}
	if v, ok := requiredUint(n, "extent_size"); ok {
		vg.ExtentSize = v
	} else {
		vg.Malformed = true
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
	if d := n.child("dev_size"); d != nil && d.Kind == kindScalar {
		// Text dev_size is in 512-byte sectors (lvm2 internal unit); the graph
		// and identity checks use bytes, so convert. Overflow is rejected, never
		// wrapped (C9): a hostile sector count becomes a typed partial VG.
		sectors, err := strconv.ParseUint(d.Value, 10, 64)
		if err != nil {
			p.Malformed = true
		} else if bytes, ok := mulU64(sectors, 512); ok {
			p.DevSize = bytes
		} else {
			p.Malformed = true
		}
	} else {
		p.Malformed = true // dev_size is required
	}
	if v, ok := requiredUint(n, "pe_start"); ok {
		p.PEStart = v
	} else {
		p.Malformed = true
	}
	if v, ok := requiredUint(n, "pe_count"); ok {
		p.PECount = v
	} else {
		p.Malformed = true
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

	// segment_count is required; segments must be numbered 1..segment_count
	// contiguously. A gap or oversized count must not silently truncate the LV
	// while leaving the VG complete.
	count, ok := requiredUint(n, "segment_count")
	if !ok || count > MaxSegmentsPerLV {
		lv.Malformed = true
	}
	for i := uint64(1); i <= count && i <= MaxSegmentsPerLV; i++ {
		seg := n.child("segment" + strconv.FormatUint(i, 10))
		if seg == nil {
			lv.Malformed = true // gap in segment numbering
			break
		}
		s := parseSegment(seg)
		if s.Malformed {
			lv.Malformed = true
		}
		lv.Segments = append(lv.Segments, s)
	}
	return lv
}

func parseSegment(n *Node) Segment {
	s := Segment{Parsed: true}
	if v, ok := requiredUint(n, "start_extent"); ok {
		s.StartExtent = v
	} else {
		s.Malformed = true
	}
	if v, ok := requiredUint(n, "extent_count"); ok {
		s.ExtentCount = v
	} else {
		s.Malformed = true
	}
	if d := n.child("type"); d != nil {
		s.Type = d.Value
	}
	// LVM2 appends feature flags to the base type as "+FLAG" (e.g.
	// "cache-pool+METADATA_FORMAT", "cache+CACHE_USES_CACHEVOL"); the base
	// type is everything before the first '+'.
	base := s.Type
	if i := strings.IndexByte(base, '+'); i >= 0 {
		base = base[:i]
	}
	switch base {
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

// requiredUint reads a required scalar uint child; ok is false when the child
// is missing, wrong-kind, or not a valid base-10 uint64 (C9: a malformed
// required scalar must become typed partial, never a silent zero).
func requiredUint(n *Node, name string) (uint64, bool) {
	d := n.child(name)
	if d == nil || d.Kind != kindScalar {
		return 0, false
	}
	v, err := strconv.ParseUint(d.Value, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
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

	byUUID := map[string][]vgCandidate{}
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
		byUUID[id] = append(byUUID[id], vgCandidate{node: vgNode, text: pv.Text, seq: seq(vgNode)})
	}
	for uuid, cands := range byUUID {
		// Highest seqno wins; ties break on text bytes so the representative is
		// independent of input order. Equal-seqno candidates whose text differs
		// mark the VG inconsistent, deterministically regardless of order.
		best := cands[0]
		for _, c := range cands[1:] {
			if c.seq > best.seq || (c.seq == best.seq && bytes.Compare(c.text, best.text) < 0) {
				best = c
			}
		}
		inconsistent := false
		for _, c := range cands {
			if c.seq == best.seq && !bytes.Equal(c.text, best.text) {
				inconsistent = true
				break
			}
		}
		vg, err := ParseVG(best.node.Name, best.node)
		if err != nil {
			continue
		}
		vg.UUID = uuid
		// Mark presence and accumulate every partial condition as a typed note
		// so the dominant State never hides a second one (e.g. malformed size
		// plus unsupported segment).
		var duplicate, missing bool
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
				duplicate = true
			} else if seen[vg.PVs[i].UUID] == 1 {
				vg.PVs[i].Presence = PresencePresent
			} else if hasFlag(vg.PVs[i].Flags, "MISSING") {
				vg.PVs[i].Presence = PresenceMissingFlag
				missing = true
			} else {
				vg.PVs[i].Presence = PresenceNotFound
				missing = true
			}
		}
		malformed := vg.Malformed || hasMalformedPV(vg) || hasMalformedLV(vg)
		unsupported := hasUnsupportedSegment(vg)
		prefix := "vg " + vg.Name + ": "
		if missing {
			vg.Notes = append(vg.Notes, prefix+"partial (missing PV)")
		}
		if duplicate || inconsistent {
			vg.Notes = append(vg.Notes, prefix+"inconsistent metadata")
		}
		if malformed {
			vg.Notes = append(vg.Notes, prefix+"partial (malformed size)")
		}
		if unsupported {
			vg.Notes = append(vg.Notes, prefix+"partial (unsupported segment type)")
		}
		switch {
		case unsupported:
			vg.State = StateUnsupported
		case malformed:
			vg.State = StateMalformed
		case duplicate || inconsistent:
			vg.State = StateInconsistent
		case missing:
			vg.State = StatePartialMissing
		default:
			vg.State = StateComplete
		}
		inv.VGs = append(inv.VGs, *vg)
	}
	return inv
}

// vgCandidate is one device's VG section (with its raw text and seqno) for a
// VG UUID, used to pick the highest-seqno representative and detect
// equal-seqno content divergence order-independently.
type vgCandidate struct {
	node *Node
	text []byte
	seq  uint64
}

// hasMalformedPV reports whether any PV's numeric field could not be converted
// safely (parse error or overflow).
func hasMalformedPV(vg *VG) bool {
	for _, pv := range vg.PVs {
		if pv.Malformed {
			return true
		}
	}
	return false
}

// hasMalformedLV reports whether any LV has a malformed segment_count, a gap in
// segment numbering, or a malformed segment scalar.
func hasMalformedLV(vg *VG) bool {
	for _, lv := range vg.LVs {
		if lv.Malformed {
			return true
		}
	}
	return false
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
