// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// PhysicalVolume is one PV from a pvs --segments JSON report (review A8).
// The PV UUID is its identity. Name is display evidence only: a /dev path is
// never used to join a PV to a block device.
type PhysicalVolume struct {
	UUID         string  `json:"uuid"`
	Name         string  `json:"name,omitempty"`
	VGUUID       string  `json:"vg_uuid,omitempty"`
	VGName       string  `json:"vg_name,omitempty"`
	SizeBytes    *uint64 `json:"size_bytes,omitempty"`
	DevSizeBytes *uint64 `json:"device_size_bytes,omitempty"`
	// DeviceNumber is LVM's pv_major:pv_minor for the device it read. It is a
	// separate observation from sysfs, not a persistent ID: the kernel reuses
	// numbers, so it only nominates a candidate block device (see DeviceLink).
	DeviceNumber string `json:"device_number,omitempty"`
	Attributes   string `json:"attributes,omitempty"`
	// Missing is true only when LVM reported the PV missing. MissingState is
	// "present", "missing" (pv_missing), "missing-from-attr" (pv_missing absent,
	// pv_attr bit 3 is "m") or "not-reported".
	Missing      bool   `json:"missing"`
	MissingState string `json:"missing_state"`
	Ambiguous    bool   `json:"ambiguous"`
	// DeviceLink is set by AddLVM: verified, provisional, withheld or no-device.
	// DeviceLinkReason explains a provisional or withheld outcome.
	DeviceLink       string      `json:"device_link,omitempty"`
	DeviceLinkReason string      `json:"device_link_reason,omitempty"`
	Segments         []PVSegment `json:"segments"`
}

// PVSegment is an extent range of a PV. LVUUID is empty for free space.
type PVSegment struct {
	StartExtent *uint64 `json:"start_extent,omitempty"`
	SizeExtents *uint64 `json:"size_extents,omitempty"`
	LVUUID      string  `json:"lv_uuid,omitempty"`
}

type pvRow struct {
	UUID    string  `json:"pv_uuid"`
	Name    string  `json:"pv_name"`
	VGUUID  string  `json:"vg_uuid"`
	VGName  string  `json:"vg_name"`
	Size    string  `json:"pv_size"`
	DevSize string  `json:"dev_size"`
	Major   string  `json:"pv_major"`
	Minor   string  `json:"pv_minor"`
	Attr    string  `json:"pv_attr"`
	Missing *string `json:"pv_missing"`
	Start   string  `json:"pvseg_start"`
	SegSize string  `json:"pvseg_size"`
	LVUUID  string  `json:"lv_uuid"`
}

// pvMissingState decodes the BIN field pv_missing. Upstream lvm2
// (lib/report/report.c _pvmissing_disp -> _binary_disp; values.h
// FIELD_RESERVED_BINARY_VALUE(pv_missing, ..., "missing")) prints "missing"
// or "" by default and "1" or "0" with --binary. pv_missing is computed from
// metadata flags and is never undefined, so "unknown"/"-1" are refused.
func pvMissingState(v *string) (string, error) {
	if v == nil {
		return "not-reported", nil
	}
	switch strings.TrimSpace(*v) {
	case "", "0":
		return "present", nil
	case "missing", "1":
		return "missing", nil
	}
	return "", fmt.Errorf("unrecognized pv_missing value")
}

// pvDeviceNumber decodes pv_major/pv_minor (signed in lvm2). A PV without a
// device is reported against a dummy device (0:0, report_object); "-1" is
// also treated as no device. Other negative or non-numeric values are refused.
func pvDeviceNumber(majorText, minorText string) (string, error) {
	majorText, minorText = strings.TrimSpace(majorText), strings.TrimSpace(minorText)
	if majorText == "-1" || minorText == "-1" {
		return "", nil
	}
	major, e1 := lvmUint(majorText)
	minor, e2 := lvmUint(minorText)
	if e1 != nil || e2 != nil || (major == nil) != (minor == nil) {
		return "", fmt.Errorf("invalid PV device number")
	}
	if major == nil || (*major == 0 && *minor == 0) {
		return "", nil
	}
	return fmt.Sprintf("%d:%d", *major, *minor), nil
}

func sameUint(a, b *uint64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// ParsePVS parses a pvs --segments original-JSON report (every value quoted;
// json_std numeric output is refused by the string decoder). A PV UUID
// reported for more than one device (cloned disks, unmanaged multipath legs,
// or pv_attr duplicate flag "d") is kept as one ambiguous PV with no device
// association, as LVM itself refuses to pick. Repeated rows for one PV on one
// device must agree on every PV-level field.
func ParsePVS(r io.Reader, mode string) ([]PhysicalVolume, error) {
	if mode != ModeMetadataOnly && mode != ModeLive {
		return nil, fmt.Errorf("unknown LVM report mode")
	}
	b, e := readReport(r)
	if e != nil {
		return nil, e
	}
	var doc struct {
		Report []struct {
			PV    []pvRow `json:"pv"`
			PVSeg []pvRow `json:"pvseg"`
		} `json:"report"`
	}
	if e = decodeReport(b, &doc); e != nil {
		return nil, e
	}
	if doc.Report == nil || len(doc.Report) > 64 {
		return nil, fmt.Errorf("missing or excessive LVM report groups")
	}
	out := []PhysicalVolume{}
	index := map[string]int{}
	device := map[string]string{}
	rows := 0
	for _, report := range doc.Report {
		if len(report.PV) > 0 && len(report.PVSeg) > 0 {
			return nil, fmt.Errorf("ambiguous LVM report group")
		}
		for _, row := range append(report.PV, report.PVSeg...) {
			rows++
			if rows > 4096 {
				return nil, fmt.Errorf("PV segment report exceeds row limit")
			}
			if !lvmIdentifier(row.UUID) || (row.VGUUID != "" && !lvmIdentifier(row.VGUUID)) || (row.LVUUID != "" && !lvmIdentifier(row.LVUUID)) {
				return nil, fmt.Errorf("PV row missing stable identity or has an invalid UUID")
			}
			pv := PhysicalVolume{UUID: row.UUID, Name: strings.TrimSpace(row.Name), VGUUID: row.VGUUID, VGName: row.VGName, Attributes: strings.TrimSpace(row.Attr), Segments: []PVSegment{}}
			if pv.MissingState, e = pvMissingState(row.Missing); e != nil {
				return nil, e
			}
			pv.Missing = pv.MissingState == "missing"
			// pv_attr (pvs(8) NOTES): 1 (d)uplicate/(a)llocatable/(u)sed,
			// 2 e(x)ported, 3 (m)issing. A contradiction refuses the report.
			if len(pv.Attributes) >= 3 {
				if pv.MissingState == "not-reported" {
					// pv_attr alone is still LVM's missing evidence.
					if pv.Attributes[2] == 'm' {
						pv.MissingState, pv.Missing = "missing-from-attr", true
					}
				} else if (pv.Attributes[2] == 'm') != pv.Missing {
					return nil, fmt.Errorf("pv_attr contradicts pv_missing")
				}
			}
			if pv.SizeBytes, e = lvmUint(row.Size); e != nil {
				return nil, e
			}
			if pv.DevSizeBytes, e = lvmUint(row.DevSize); e != nil {
				return nil, e
			}
			if pv.DeviceNumber, e = pvDeviceNumber(row.Major, row.Minor); e != nil {
				return nil, e
			}
			seg := PVSegment{LVUUID: row.LVUUID}
			if seg.StartExtent, e = lvmUint(row.Start); e != nil {
				return nil, e
			}
			if seg.SizeExtents, e = lvmUint(row.SegSize); e != nil {
				return nil, e
			}
			deviceKey := pv.Name + "\x00" + pv.DeviceNumber
			i, ok := index[pv.UUID]
			if !ok {
				index[pv.UUID] = len(out)
				device[pv.UUID] = deviceKey
				if len(pv.Attributes) > 0 && pv.Attributes[0] == 'd' {
					pv.Ambiguous = true
				}
				out = append(out, pv)
				i = len(out) - 1
			} else if device[pv.UUID] != deviceKey {
				out[i].Ambiguous = true
			} else {
				prev := out[i]
				if prev.VGUUID != pv.VGUUID || prev.VGName != pv.VGName || !sameUint(prev.SizeBytes, pv.SizeBytes) || !sameUint(prev.DevSizeBytes, pv.DevSizeBytes) || prev.Attributes != pv.Attributes || prev.MissingState != pv.MissingState {
					return nil, fmt.Errorf("inconsistent duplicate PV observations")
				}
			}
			if out[i].Ambiguous {
				out[i].Name, out[i].DeviceNumber = "", ""
			}
			if row.Start != "" || row.SegSize != "" || row.LVUUID != "" {
				out[i].Segments = append(out[i].Segments, seg)
			}
		}
	}
	return out, nil
}

// PVRevalidation is a second sysfs block observation taken after the LVM
// report completed, in the boot named by BootID. A disk-to-PV link needs the
// candidate device unchanged between the observation in the graph and this
// one, so a device number reused by hotplug between scans is not followed.
type PVRevalidation struct {
	BootID string
	Blocks []Block
}

// Device-link outcomes for a PV.
const (
	PVLinkVerified    = "verified"
	PVLinkProvisional = "provisional"
	PVLinkWithheld    = "withheld"
	PVLinkNoDevice    = "no-device"
)

// addPhysicalVolumes adds PV nodes, PV-to-VG membership and PV-to-LV extent
// allocation by UUID. Those joins use only LVM UUIDs. The PV-to-disk link does
// not: the PV UUID says nothing about which disk supplies it, and LVM's device
// number is an independent observation that the kernel can reuse. So a link is
//   - verified (edge "used-by", confidence "corroborated", plus a claim) only
//     when the device number names exactly one block, LVM's dev_size equals
//     the sysfs size, the block carries an observed LVM2_member signature
//     whose UUID equals the PV UUID, and a later sysfs observation in the same
//     boot shows the block unchanged;
//   - provisional (edge "possibly-used-by", confidence "provisional", no
//     claim) when nothing contradicts the candidate but the signature or the
//     revalidation is not available; the production collector observes no
//     signatures, so this is its best outcome today;
//   - withheld (no edge) on any contradiction: a different signature, a size
//     mismatch, a change or disappearance between observations, another boot,
//     an ambiguous block identity, a PV reported missing or duplicated, or a
//     device number claimed by more than one PV.
func addPhysicalVolumes(g *Graph, inv *LVMInventory, byUUID map[string]string, groups map[string]bool, after *PVRevalidation) {
	if len(inv.PhysicalVolumes) == 0 {
		g.Status = "partial"
		g.Notes = append(g.Notes, "No PV report was supplied; which disks supply each VG is not shown.")
		return
	}
	partial := func(s string) { g.Status = "partial"; g.Notes = append(g.Notes, s) }
	blocks := map[string][]int{}
	for i, n := range g.Nodes {
		if n.DeviceNumber != "" {
			blocks[n.DeviceNumber] = append(blocks[n.DeviceNumber], i)
		}
	}
	signatures := map[string][]Node{}
	nodeByID := map[string]Node{}
	for _, n := range g.Nodes {
		nodeByID[n.ID] = n
	}
	for _, e := range g.Edges {
		if e.Kind == "has-signature" {
			signatures[e.From] = append(signatures[e.From], nodeByID[e.To])
		}
	}
	claimed := map[string]int{}
	for _, pv := range inv.PhysicalVolumes {
		if pv.DeviceNumber != "" && !pv.Ambiguous {
			claimed[pv.DeviceNumber]++
		}
	}
	afterByDev := map[string][]Block{}
	if after != nil {
		for _, b := range after.Blocks {
			afterByDev[b.DeviceNumber] = append(afterByDev[b.DeviceNumber], b)
		}
	}
	for k := range inv.PhysicalVolumes {
		pv := &inv.PhysicalVolumes[k]
		id := key("lvm-pv", pv.UUID)
		n := Node{ID: id, Kind: "lvm-pv", Name: pv.Name, PersistentID: "lvm-pv:" + pv.UUID, IdentityStatus: "observed", Evidence: []Evidence{{inv.Source, "observed"}}}
		if pv.Name == "" {
			n.Name = "PV " + pv.UUID
		}
		if pv.Ambiguous {
			n.PersistentID, n.IdentityStatus = "", "ambiguous"
			n.Notes = []string{"This PV UUID was reported on more than one device or flagged duplicate by LVM; no device association is safe."}
			partial("Duplicate LVM PV UUIDs were reported; affected PVs are not associated with a device.")
		}
		if pv.Missing {
			n.Notes = append(n.Notes, "LVM reports this PV as missing.")
		}
		g.Nodes = append(g.Nodes, n)
		if pv.VGUUID != "" {
			vgID := key("lvm-vg", pv.VGUUID)
			if !groups[pv.VGUUID] {
				groups[pv.VGUUID] = true
				g.Nodes = append(g.Nodes, Node{ID: vgID, Kind: "lvm-vg", Name: pv.VGName, PersistentID: "lvm-vg:" + pv.VGUUID, IdentityStatus: "observed", Evidence: []Evidence{{inv.Source, "observed"}}})
			}
			g.Edges = append(g.Edges, Edge{id, vgID, "member-of", Evidence{inv.Source, "observed"}})
		}
		seen := map[string]bool{}
		for _, seg := range pv.Segments {
			if seg.LVUUID == "" || seen[seg.LVUUID] {
				continue
			}
			seen[seg.LVUUID] = true
			if lv, ok := byUUID[seg.LVUUID]; ok {
				g.Edges = append(g.Edges, Edge{id, lv, "allocated-to", Evidence{"pvs segment lv_uuid", "observed"}})
			} else {
				partial("A PV segment references an LV missing from the LV report.")
			}
		}
		link, reason, block := linkPV(g, *pv, blocks, signatures, claimed, after, afterByDev)
		pv.DeviceLink, pv.DeviceLinkReason = link, reason
		switch link {
		case PVLinkVerified:
			evidence := Evidence{"pvs pv_major:pv_minor and dev_size, LVM2_member signature UUID, sysfs revalidated after the report", "corroborated"}
			g.Edges = append(g.Edges, Edge{block, id, "used-by", evidence})
			g.Claims = append(g.Claims, Claim{block, "lvm-pv", evidence})
		case PVLinkProvisional:
			g.Edges = append(g.Edges, Edge{block, id, "possibly-used-by", Evidence{"pvs pv_major:pv_minor names this device; " + reason, "provisional"}})
			partial("Some PV-to-disk links are provisional: LVM's device number is not corroborated by an on-disk signature and a later observation.")
		case PVLinkWithheld:
			partial("Some PV-to-disk links were withheld because observations disagree; affected disks are not shown as PVs.")
		case PVLinkNoDevice:
			if !pv.Ambiguous && !pv.Missing {
				partial("A PV was reported without a device number; its disk is not shown.")
			}
		}
	}
	sort.SliceStable(g.Edges, func(i, j int) bool {
		a, b := g.Edges[i], g.Edges[j]
		return a.From+a.To+a.Kind < b.From+b.To+b.Kind
	})
}

// linkPV decides one PV's device link; see addPhysicalVolumes.
func linkPV(g *Graph, pv PhysicalVolume, blocks map[string][]int, signatures map[string][]Node, claimed map[string]int, after *PVRevalidation, afterByDev map[string][]Block) (link, reason, block string) {
	switch {
	case pv.Ambiguous:
		return PVLinkWithheld, "PV UUID reported on more than one device", ""
	case pv.Missing:
		return PVLinkWithheld, "LVM reports the PV missing", ""
	case pv.DeviceNumber == "":
		return PVLinkNoDevice, "", ""
	case claimed[pv.DeviceNumber] > 1:
		return PVLinkWithheld, "device number reported for more than one PV", ""
	}
	matches := blocks[pv.DeviceNumber]
	if len(matches) != 1 {
		return PVLinkWithheld, "device number did not match exactly one observed block device", ""
	}
	n := g.Nodes[matches[0]]
	if n.IdentityStatus == "ambiguous" {
		return PVLinkWithheld, "candidate block device has an ambiguous identity", ""
	}
	if pv.DevSizeBytes != nil && n.SizeBytes != 0 && *pv.DevSizeBytes != n.SizeBytes {
		return PVLinkWithheld, "LVM dev_size differs from the sysfs size", ""
	}
	signature := "none"
	for _, s := range signatures[n.ID] {
		if s.FSType != "LVM2_member" {
			return PVLinkWithheld, "candidate block device carries a non-LVM signature", ""
		}
		if s.SignatureUUID != pv.UUID {
			return PVLinkWithheld, "LVM2_member signature UUID differs from the PV UUID", ""
		}
		signature = "match"
	}
	revalidated := "unavailable"
	if after != nil {
		if after.BootID == "" || g.BootID == "" || after.BootID != g.BootID {
			return PVLinkWithheld, "revalidation was not taken in the same known boot", ""
		}
		later := afterByDev[n.DeviceNumber]
		if len(later) != 1 {
			return PVLinkWithheld, "candidate block device disappeared or was duplicated before revalidation", ""
		}
		b := later[0]
		kind := b.Kind
		if kind == "" {
			kind = "block"
		}
		if b.Name != n.Name || kind != n.Kind || b.SizeBytes != n.SizeBytes || b.DiskSeq != n.DiskSeq || persistent(b) != n.PersistentID {
			return PVLinkWithheld, "candidate block device changed between observations", ""
		}
		revalidated = "unchanged"
	}
	switch {
	case signature == "match" && revalidated == "unchanged" && pv.DevSizeBytes != nil && n.SizeBytes != 0:
		return PVLinkVerified, "", n.ID
	case signature != "match" && revalidated != "unchanged":
		return PVLinkProvisional, "no on-disk signature observed and no revalidation", n.ID
	case signature != "match":
		return PVLinkProvisional, "no on-disk signature observed", n.ID
	case revalidated != "unchanged":
		return PVLinkProvisional, "no later observation revalidated the device", n.ID
	}
	return PVLinkProvisional, "device size not reported by both observations", n.ID
}
