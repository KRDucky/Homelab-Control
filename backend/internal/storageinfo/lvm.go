// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Report modes. The caller states how a report was produced; the parser never
// infers it from the input. "lvs --readonly" does not query device-mapper, so
// a metadata-only report cannot carry live RAID sync, cache or health state.
const (
	ModeMetadataOnly = "metadata-only"
	ModeLive         = "live"
)

// LVMInventory describes lvs/pvs --segments JSON reports.
// It does not run LVM or infer that RAID/cache is healthy from its mere presence.
// The future collector must request explicit fields with --units b --nosuffix;
// unsupported report fields require capability detection, not invented values.
type LVMInventory struct {
	Mode            string           `json:"mode"`
	Volumes         []LogicalVolume  `json:"volumes"`
	PhysicalVolumes []PhysicalVolume `json:"physical_volumes"`
	Source          string           `json:"source"`
}

type LogicalVolume struct {
	UUID             string       `json:"uuid"`
	Name             string       `json:"name"`
	VGUUID           string       `json:"vg_uuid"`
	VGName           string       `json:"vg_name"`
	Parent           string       `json:"parent,omitempty"`
	SizeBytes        *uint64      `json:"size_bytes,omitempty"`
	Attributes       string       `json:"attributes,omitempty"`
	Health           string       `json:"health"`
	SyncAction       string       `json:"sync_action,omitempty"`
	SyncPercent      *float64     `json:"sync_percent,omitempty"`
	CacheTotalBlocks *uint64      `json:"cache_total_blocks,omitempty"`
	CacheUsedBlocks  *uint64      `json:"cache_used_blocks,omitempty"`
	CacheDirtyBlocks *uint64      `json:"cache_dirty_blocks,omitempty"`
	OriginUUID       string       `json:"origin_uuid,omitempty"`
	PoolUUID         string       `json:"pool_uuid,omitempty"`
	DataUUID         string       `json:"data_uuid,omitempty"`
	MetadataUUID     string       `json:"metadata_uuid,omitempty"`
	Segments         []LVMSegment `json:"segments"`
	// KernelJoin records whether this LV was matched to a device-mapper node:
	// joined, no-kernel-node, ambiguous or not-checked. It is set by AddLVM.
	KernelJoin string `json:"kernel_join,omitempty"`
}

type LVMSegment struct {
	Type        string  `json:"type"`
	StartBytes  *uint64 `json:"start_bytes,omitempty"`
	SizeBytes   *uint64 `json:"size_bytes,omitempty"`
	CacheMode   string  `json:"cache_mode,omitempty"`
	CachePolicy string  `json:"cache_policy,omitempty"`
	// Devices is display evidence only. It may contain paths or internal LV
	// names plus extent offsets; never use it as an executable target/identity.
	Devices         string `json:"devices,omitempty"`
	MetadataDevices string `json:"metadata_devices,omitempty"`
}

type lvmRow struct {
	UUID            string `json:"lv_uuid"`
	Name            string `json:"lv_name"`
	VGUUID          string `json:"vg_uuid"`
	VGName          string `json:"vg_name"`
	Parent          string `json:"lv_parent"`
	Size            string `json:"lv_size"`
	Attr            string `json:"lv_attr"`
	Health          string `json:"lv_health_status"`
	SyncAction      string `json:"raid_sync_action"`
	Sync            string `json:"sync_percent"`
	Total           string `json:"cache_total_blocks"`
	Used            string `json:"cache_used_blocks"`
	Dirty           string `json:"cache_dirty_blocks"`
	Origin          string `json:"origin_uuid"`
	Pool            string `json:"pool_lv_uuid"`
	Data            string `json:"data_lv_uuid"`
	Metadata        string `json:"metadata_lv_uuid"`
	Type            string `json:"segtype"`
	Start           string `json:"seg_start"`
	SegmentSize     string `json:"seg_size"`
	CacheMode       string `json:"cache_mode"`
	CachePolicy     string `json:"cache_policy"`
	Devices         string `json:"devices"`
	MetadataDevices string `json:"metadata_devices"`
}

func lvmIdentifier(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
func lvmUint(s string) (*uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	n, e := strconv.ParseUint(s, 10, 64)
	if e != nil {
		return nil, fmt.Errorf("invalid LVM integer observation")
	}
	return &n, nil
}
func lvmPercent(s string) (*float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	n, e := strconv.ParseFloat(s, 64)
	if e != nil || !(n >= 0 && n <= 100) {
		return nil, fmt.Errorf("invalid LVM percentage observation")
	}
	return &n, nil
}

func readReport(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, 4*1024*1024+1))
	if e != nil {
		return nil, e
	}
	if len(b) > 4*1024*1024 {
		return nil, fmt.Errorf("LVM report exceeds byte limit")
	}
	return b, nil
}

func decodeReport(b []byte, doc any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	if e := dec.Decode(doc); e != nil {
		return fmt.Errorf("invalid LVM report: %w", e)
	}
	var extra any
	if e := dec.Decode(&extra); e != io.EOF {
		return fmt.Errorf("trailing LVM report data")
	}
	return nil
}

// maskRuntime removes observations a metadata-only report cannot make, so a
// stale or default value is never shown as current state (review A10). The
// lv_attr state (5th), open (6th) and health (9th) characters are runtime.
func maskRuntime(v *LogicalVolume) {
	v.Health = "unknown"
	v.SyncAction, v.SyncPercent = "", nil
	v.CacheTotalBlocks, v.CacheUsedBlocks, v.CacheDirtyBlocks = nil, nil, nil
	if a := []byte(v.Attributes); len(a) >= 9 {
		a[4], a[5], a[8] = '?', '?', '?'
		v.Attributes = string(a)
	}
}

// ParseLVM accepts the original JSON report mode (string-valued columns), not
// json_std. Limits apply before decoding, with a separate report/row budget.
// mode is supplied by the trusted collector that produced the report.
func ParseLVM(r io.Reader, mode string) (LVMInventory, error) {
	if mode != ModeMetadataOnly && mode != ModeLive {
		return LVMInventory{}, fmt.Errorf("unknown LVM report mode")
	}
	b, e := readReport(r)
	if e != nil {
		return LVMInventory{}, e
	}
	var doc struct {
		Report []struct {
			LV  []lvmRow `json:"lv"`
			Seg []lvmRow `json:"seg"`
		} `json:"report"`
	}
	if e = decodeReport(b, &doc); e != nil {
		return LVMInventory{}, e
	}
	if doc.Report == nil || len(doc.Report) > 64 {
		return LVMInventory{}, fmt.Errorf("missing or excessive LVM report groups")
	}
	inv := LVMInventory{Mode: mode, Volumes: []LogicalVolume{}, PhysicalVolumes: []PhysicalVolume{}, Source: "lvs JSON report"}
	vgNames := map[string]string{}
	index := map[string]int{}
	first := map[string]LogicalVolume{}
	rows := 0
	for _, report := range doc.Report {
		if len(report.LV) > 0 && len(report.Seg) > 0 {
			return LVMInventory{}, fmt.Errorf("ambiguous LVM report group")
		}
		for _, row := range append(report.LV, report.Seg...) {
			rows++
			if rows > 4096 {
				return LVMInventory{}, fmt.Errorf("LVM segment report exceeds row limit")
			}
			if !lvmIdentifier(row.UUID) || !lvmIdentifier(row.VGUUID) || strings.TrimSpace(row.Name) == "" || row.Type == "" {
				return LVMInventory{}, fmt.Errorf("LVM row missing stable identity or segment type")
			}
			for _, id := range []string{row.Origin, row.Pool, row.Data, row.Metadata} {
				if id != "" && !lvmIdentifier(id) {
					return LVMInventory{}, fmt.Errorf("invalid LVM relationship UUID")
				}
			}
			if name, seen := vgNames[row.VGUUID]; seen && name != row.VGName {
				return LVMInventory{}, fmt.Errorf("one VG UUID is reported with different names")
			}
			vgNames[row.VGUUID] = row.VGName
			v := LogicalVolume{UUID: row.UUID, Name: row.Name, VGUUID: row.VGUUID, VGName: row.VGName, Parent: strings.TrimSpace(row.Parent), Attributes: row.Attr, Health: strings.TrimSpace(row.Health), SyncAction: strings.TrimSpace(row.SyncAction), OriginUUID: row.Origin, PoolUUID: row.Pool, DataUUID: row.Data, MetadataUUID: row.Metadata, Segments: []LVMSegment{}}
			if v.Health == "" {
				v.Health = "unknown"
			}
			if v.SizeBytes, e = lvmUint(row.Size); e != nil {
				return LVMInventory{}, e
			}
			if v.SyncPercent, e = lvmPercent(row.Sync); e != nil {
				return LVMInventory{}, e
			}
			if v.CacheTotalBlocks, e = lvmUint(row.Total); e != nil {
				return LVMInventory{}, e
			}
			if v.CacheUsedBlocks, e = lvmUint(row.Used); e != nil {
				return LVMInventory{}, e
			}
			if v.CacheDirtyBlocks, e = lvmUint(row.Dirty); e != nil {
				return LVMInventory{}, e
			}
			if mode == ModeMetadataOnly {
				maskRuntime(&v)
			}
			seg := LVMSegment{Type: row.Type, CacheMode: row.CacheMode, CachePolicy: row.CachePolicy, Devices: row.Devices, MetadataDevices: row.MetadataDevices}
			if seg.StartBytes, e = lvmUint(row.Start); e != nil {
				return LVMInventory{}, e
			}
			if seg.SizeBytes, e = lvmUint(row.SegmentSize); e != nil {
				return LVMInventory{}, e
			}
			i, ok := index[v.UUID]
			if !ok {
				i = len(inv.Volumes)
				index[v.UUID] = i
				inv.Volumes = append(inv.Volumes, v)
				first[v.UUID] = v
			} else {
				// The same LV appears once per segment. Identity and LV-wide observations
				// must agree; a torn report is not silently treated as a coherent snapshot.
				a, _ := json.Marshal(first[v.UUID])
				bb, _ := json.Marshal(v)
				if !bytes.Equal(a, bb) {
					return LVMInventory{}, fmt.Errorf("inconsistent duplicate LV observations")
				}
			}
			inv.Volumes[i].Segments = append(inv.Volumes[i].Segments, seg)
		}
	}
	return inv, nil
}

// AddLVM joins only UUID-backed logical relationships, plus explicitly
// labelled report-scope name joins for internal LVs. Unresolved components
// remain visible findings, never guessed from path strings or similar names.
// AddLVM adds an LVM report without a revalidating observation, so any
// PV-to-disk link is at best provisional.
func AddLVM(g *Graph, inv LVMInventory) { addLVM(g, inv, nil) }

// AddLVMRevalidated adds an LVM report bracketed by the graph's sysfs
// observation and a later one (after), which PV-to-disk links require.
func AddLVMRevalidated(g *Graph, inv LVMInventory, after PVRevalidation) { addLVM(g, inv, &after) }

func addLVM(g *Graph, inv LVMInventory, after *PVRevalidation) {
	byUUID := map[string]string{}
	groups := map[string]bool{}
	partial := func(s string) { g.Status = "partial"; g.Notes = append(g.Notes, s) }
	for _, v := range inv.Volumes {
		id := key("lvm-lv", v.UUID)
		byUUID[v.UUID] = id
		g.Nodes = append(g.Nodes, Node{ID: id, Kind: "lvm-lv", Name: v.Name, PersistentID: "lvm-lv:" + v.UUID, IdentityStatus: "observed", Evidence: []Evidence{{inv.Source, "observed"}}})
		vgID := key("lvm-vg", v.VGUUID)
		if !groups[v.VGUUID] {
			groups[v.VGUUID] = true
			g.Nodes = append(g.Nodes, Node{ID: vgID, Kind: "lvm-vg", Name: v.VGName, PersistentID: "lvm-vg:" + v.VGUUID, IdentityStatus: "observed", Evidence: []Evidence{{inv.Source, "observed"}}})
		}
		g.Edges = append(g.Edges, Edge{vgID, id, "contains", Evidence{inv.Source, "observed"}})
	}
	for _, v := range inv.Volumes {
		for _, rel := range []struct{ uuid, kind string }{{v.OriginUUID, "origin-for"}, {v.PoolUUID, "pool-for"}, {v.DataUUID, "data-for"}, {v.MetadataUUID, "metadata-for"}} {
			if rel.uuid == "" {
				continue
			}
			if from, ok := byUUID[rel.uuid]; ok {
				g.Edges = append(g.Edges, Edge{from, byUUID[v.UUID], rel.kind, Evidence{inv.Source, "observed"}})
			} else {
				partial("An LVM component UUID was reported without its internal LV; component membership is incomplete.")
			}
		}
	}
	joinLVMParents(g, inv, byUUID)
	addPhysicalVolumes(g, &inv, byUUID, groups, after)
	joinLVMKernelDevices(g, &inv)
	if inv.Mode == ModeMetadataOnly {
		partial("LVM was read in metadata-only mode: RAID synchronization, cache usage and health are not observed and are shown as unknown.")
	}
	g.LVM = &inv
	sort.Strings(g.Notes)
	g.Notes = compact(g.Notes)
}

// joinLVMParents links internal LVs (RAID _rimage/_rmeta, mirror legs) to the
// LV that lists them. lvs offers no UUID for this, so the join uses lv_parent
// within one VG UUID of one report, and only when that name is unique there.
func joinLVMParents(g *Graph, inv LVMInventory, byUUID map[string]string) {
	names := map[string][]string{}
	for _, v := range inv.Volumes {
		k := v.VGUUID + "\x00" + bareLVName(v.Name)
		names[k] = append(names[k], v.UUID)
	}
	for _, v := range inv.Volumes {
		if v.Parent == "" {
			continue
		}
		matches := names[v.VGUUID+"\x00"+bareLVName(v.Parent)]
		if len(matches) == 1 && matches[0] != v.UUID {
			g.Edges = append(g.Edges, Edge{byUUID[v.UUID], byUUID[matches[0]], "component-of", Evidence{inv.Source + " lv_parent (name within one VG of one report)", "reported"}})
		} else {
			g.Status = "partial"
			g.Notes = append(g.Notes, "An internal LV names a parent that is missing or not unique in its VG; its composition is not shown.")
		}
	}
}

// bareLVName strips the brackets lvs puts around hidden internal LV names.
func bareLVName(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		return s[1 : len(s)-1]
	}
	return s
}

// joinLVMKernelDevices connects an LV UUID to an observed device-mapper UUID,
// never a /dev/mapper name. The upstream LVM DM UUID encoding concatenates
// VG/LV UUIDs without separators; layer suffixes do not identify the top LV.
// Each LV records its join outcome (review A7).
func joinLVMKernelDevices(g *Graph, inv *LVMInventory) {
	unjoined := false
	for i := range inv.Volumes {
		volume := &inv.Volumes[i]
		vgID, lvID := strings.ReplaceAll(volume.VGUUID, "-", ""), strings.ReplaceAll(volume.UUID, "-", "")
		expected := "LVM-" + vgID + lvID
		if len(vgID) != 32 || len(lvID) != 32 {
			// actual VG/LV identifiers each contain 32 alphanumeric characters
			volume.KernelJoin = "not-checked"
			unjoined = true
			continue
		}
		matches := []string{}
		for _, node := range g.Nodes {
			if node.DeviceNumber == "" {
				continue
			}
			for _, id := range node.Identifiers {
				if id.Kind == "dm-uuid" && id.Value == expected && !id.Ambiguous {
					matches = append(matches, node.ID)
				}
			}
		}
		switch len(matches) {
		case 1:
			volume.KernelJoin = "joined"
			g.Edges = append(g.Edges, Edge{matches[0], key("lvm-lv", volume.UUID), "represents", Evidence{"sysfs dm/uuid and lvs UUIDs", "observed"}})
		case 0:
			volume.KernelJoin = "no-kernel-node"
			unjoined = true
		default:
			volume.KernelJoin = "ambiguous"
			g.Status = "partial"
			g.Notes = append(g.Notes, "An LV UUID corresponds to multiple device-mapper nodes; no physical association was made.")
		}
	}
	if unjoined {
		g.Status = "partial"
		g.Notes = append(g.Notes, "Some LVs have no matching device-mapper node (inactive, internal or layered); their kernel devices are not shown.")
	}
}
