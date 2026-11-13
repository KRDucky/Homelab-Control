// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package storageinfo describes observed storage relationships. It performs no
// block-device writes, commands, mounts, or repair. A snapshot-local node ID is
// never a persistent disk identity or authorization for a future write.
package storageinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Evidence distinguishes observations from labels and inferred relationships.
type Evidence struct {
	Source     string `json:"source"`
	Confidence string `json:"confidence"`
}

type Node struct {
	ID             string       `json:"id"`
	Kind           string       `json:"kind"`
	Name           string       `json:"name"`
	PersistentID   string       `json:"persistent_id,omitempty"`
	Identifiers    []Identifier `json:"identifiers,omitempty"`
	IdentityStatus string       `json:"identity_status"`
	DeviceNumber   string       `json:"device_number,omitempty"`
	// DiskSeq is the kernel's per-boot disk sequence number (sysfs diskseq;
	// a partition reports its disk's). It detects device-number reuse.
	DiskSeq    string `json:"disk_seq,omitempty"`
	SizeBytes  uint64 `json:"size_bytes,omitempty"`
	FSType     string `json:"filesystem_type,omitempty"`
	Mountpoint string `json:"mountpoint,omitempty"`
	// SignatureUUID is the UUID an observed signature reports (for
	// LVM2_member, the PV UUID). Set only on signature nodes.
	SignatureUUID string     `json:"signature_uuid,omitempty"`
	MountRoot     string     `json:"mount_root,omitempty"`
	MountView     string     `json:"mount_view,omitempty"`
	MD            *MDState   `json:"md,omitempty"`
	Evidence      []Evidence `json:"evidence"`
	Notes         []string   `json:"notes,omitempty"`
}

type Edge struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Kind     string   `json:"kind"`
	Evidence Evidence `json:"evidence"`
}

// Claim means observed use, not ownership of data or permission to mutate it.
type Claim struct {
	Node       string   `json:"node"`
	Technology string   `json:"technology"`
	Evidence   Evidence `json:"evidence"`
}

type Graph struct {
	LVM    *LVMInventory `json:"lvm,omitempty"`
	BootID string        `json:"boot_id,omitempty"`
	Nodes  []Node        `json:"nodes"`
	Edges  []Edge        `json:"edges"`
	Claims []Claim       `json:"claims"`
	Status string        `json:"status"`
	Notes  []string      `json:"notes"`
}

// Block is supplied only by trusted bounded collectors, never request paths.
// Lower records kernel slave device numbers (md/dm dependencies), not guesses
// from names. Parent records the containing disk for a partition.
type Block struct {
	Name, DeviceNumber, Kind, Parent string
	// WWID is the raw sysfs wwid; ClassifyWWID decides whether it is a WWN.
	WWID, WWIDSource     string
	Serial, SerialSource string
	// ControllerSerial is an NVMe controller serial shared by its namespaces;
	// ControllerKey identifies that controller within this observation.
	ControllerSerial, ControllerKey string
	PartUUID, UUID, FSType          string
	NativeKind, NativeUUID          string
	ByID                            []string
	Lower                           []string
	SizeBytes                       uint64
	DiskSeq                         string
	Source                          string
	MD                              *MDState
}

type Mount struct {
	ID, DeviceNumber, Root, Mountpoint, FSType string
	View                                       string
	// Network sources are deliberately omitted: mount source/options can contain
	// credentials. Mounts are listed from mountinfo, never contacted or stat'ed.
}

// key derives a snapshot-local node ID. Block node IDs hash the kernel device
// number, which is reused after hotplug; compare snapshots with SameDevice.
func key(kind, value string) string {
	h := sha256.Sum256([]byte(kind + "\x00" + value))
	return kind + ":" + hex.EncodeToString(h[:16])
}

// persistent picks the strongest observed identity. Tiers: world-wide names,
// a per-device serial, a native stacked-device UUID, then vendor-derived
// designators (t10.*, nvme.*) that are known to collide on cheap or cloned
// devices. A controller serial is never a per-namespace identity.
func persistent(b Block) string {
	if b.Kind == "partition" {
		if b.PartUUID != "" {
			return "partuuid:" + b.PartUUID
		}
		return ""
	}
	kind, wwid, hasWWID := CanonicalWWID(b.WWID)
	switch {
	case hasWWID && (kind == KindWWN || kind == KindNamespaceUUID):
		return kind + ":" + wwid
	case b.Serial != "":
		return "serial:" + b.Serial
	case (b.NativeKind == "dm-uuid" || b.NativeKind == "md-uuid") && b.NativeUUID != "":
		return b.NativeKind + ":" + b.NativeUUID
	case hasWWID && !failedDesignator(b.WWID):
		return kind + ":" + wwid
	}
	// A filesystem UUID is not a disk identity: cloned filesystems and Btrfs
	// multi-device filesystems can legitimately repeat it on different devices.
	ids := append([]string(nil), b.ByID...)
	sort.Strings(ids)
	if len(ids) > 0 {
		return "by-id:" + ids[0]
	}
	return ""
}

// SameDevice reports whether two nodes, typically from different snapshots,
// may be treated as the same device. Node IDs and device numbers are
// snapshot-local: the kernel reuses major:minor numbers after hotplug within
// one boot, so they are never compared. Only a unique observed persistent
// identity is comparable; missing or ambiguous identity is not (review A14).
func SameDevice(a, b Node) bool {
	return a.PersistentID != "" && a.IdentityStatus == "observed" && b.IdentityStatus == "observed" && a.PersistentID == b.PersistentID
}

// Build assembles a single observation; duplicates never silently merge disks.
func Build(blocks []Block, mounts []Mount) Graph {
	g := Graph{Nodes: []Node{}, Edges: []Edge{}, Claims: []Claim{}, Status: "ok", Notes: []string{}}
	note := func(s string) { g.Status = "partial"; g.Notes = append(g.Notes, s) }
	// Validate the observation set before counting identities. Invalid or
	// contradictory duplicate device-number records cannot poison a survivor.
	counts := map[string]int{}
	for _, b := range blocks {
		if validDeviceNumber(b.DeviceNumber) {
			counts[b.DeviceNumber]++
		}
	}
	kept := make([]Block, 0, len(blocks))
	for _, b := range blocks {
		if !validDeviceNumber(b.DeviceNumber) {
			note("An invalid block device number was omitted.")
			continue
		}
		if counts[b.DeviceNumber] != 1 {
			note("Conflicting duplicate device-number observations were omitted.")
			continue
		}
		kept = append(kept, b)
	}
	blocks = kept
	byDev := map[string]int{}
	identityCounts := map[string]int{}
	owners := map[string]map[string]bool{}
	for _, b := range blocks {
		for _, identifier := range identifiers(b) {
			k := identifier.Kind + ":" + identifier.Value
			if owners[k] == nil {
				owners[k] = map[string]bool{}
			}
			owners[k][identifierOwner(b, identifier)] = true
		}
		if p := persistent(b); p != "" {
			identityCounts[p]++
		}
	}
	for _, b := range blocks {
		if !validDeviceNumber(b.DeviceNumber) {
			note("A block device has an invalid device number and was omitted.")
			continue
		}
		if _, ok := byDev[b.DeviceNumber]; ok {
			note("Duplicate kernel device number; inventory is inconsistent.")
			continue
		}
		source := b.Source
		if source == "" {
			source = "/sys/class/block"
		}
		n := Node{ID: key("block", b.DeviceNumber), Kind: b.Kind, Name: b.Name, DeviceNumber: b.DeviceNumber, DiskSeq: b.DiskSeq, SizeBytes: b.SizeBytes, IdentityStatus: "unavailable", Evidence: []Evidence{{source, "observed"}}}
		n.Identifiers = identifiers(b)
		n.MD = b.MD
		for i := range n.Identifiers {
			n.Identifiers[i].Ambiguous = len(owners[n.Identifiers[i].Kind+":"+n.Identifiers[i].Value]) > 1
		}
		if n.Kind == "" {
			n.Kind = "block"
		}
		if p := persistent(b); p != "" {
			if identityCounts[p] > 1 {
				n.IdentityStatus = "ambiguous"
				n.Notes = []string{"Identifier is shared by multiple devices; no persistent association is safe."}
				note("Duplicate persistent block identifiers were found.")
			} else {
				n.PersistentID = p
				n.IdentityStatus = "observed"
			}
		} else {
			n.Notes = []string{"No persistent identity observed; device number and kernel name are snapshot-local only."}
		}
		byDev[b.DeviceNumber] = len(g.Nodes)
		g.Nodes = append(g.Nodes, n)
	}
	addEdge := func(from, to, kind, source string) {
		g.Edges = append(g.Edges, Edge{from, to, kind, Evidence{source, "observed"}})
	}
	for _, b := range blocks {
		idx, ok := byDev[b.DeviceNumber]
		if !ok {
			continue
		}
		id := g.Nodes[idx].ID
		if b.Parent != "" {
			if p, ok := byDev[b.Parent]; ok {
				addEdge(g.Nodes[p].ID, id, "contains", "/sys/class/block/partition")
			} else {
				note("A partition's containing device was not observed.")
			}
		}
		for _, lower := range b.Lower {
			if p, ok := byDev[lower]; ok {
				addEdge(g.Nodes[p].ID, id, "used-by", "/sys/class/block/slaves")
				g.Claims = append(g.Claims, Claim{g.Nodes[p].ID, b.Kind, Evidence{"/sys/class/block/slaves", "observed"}})
			} else {
				note("A stacked device references an unobserved member.")
			}
		}
		if b.FSType != "" {
			// Signatures are claims, not automatically active filesystems. In particular
			// an LVM PV, md member or LUKS signature must never be labelled mountable.
			technology := b.FSType
			g.Claims = append(g.Claims, Claim{id, technology, Evidence{"blkid", "observed"}})
			signatureID := key("signature", b.DeviceNumber+"\x00"+b.FSType+"\x00"+b.UUID)
			g.Nodes = append(g.Nodes, Node{ID: signatureID, Kind: "signature", Name: b.FSType, FSType: b.FSType, SignatureUUID: b.UUID, IdentityStatus: "snapshot-local", Evidence: []Evidence{{"blkid", "observed"}}, Notes: []string{"Observed signature is not proof of exclusive ownership or current use."}})
			addEdge(id, signatureID, "has-signature", "blkid")
		}
	}
	seenMount := map[string]bool{}
	for _, m := range mounts {
		if m.ID == "" || seenMount[m.ID] {
			note("Duplicate or missing mount ID was omitted.")
			continue
		}
		seenMount[m.ID] = true
		id := key("mount", m.ID)
		view, source, relation, claim := "service", "/proc/self/mountinfo (hc-api service namespace)", "mounted-in-service-view", "service-view-mount:"
		if m.View == "host" {
			view, source, relation, claim = "host", "verified host mount namespace", "mounted-as", "mounted:"
		} else {
			note("Mount relationships reflect the API service namespace, not a verified host mount view.")
		}
		// Preserve non-root mount roots: they can be legitimate subvolume mounts,
		// not just bind mounts. Never guess a primary host mount from this field.
		g.Nodes = append(g.Nodes, Node{ID: id, Kind: "mount", Name: m.Mountpoint, Mountpoint: m.Mountpoint, MountRoot: m.Root, MountView: view, FSType: m.FSType, IdentityStatus: "snapshot-local", Evidence: []Evidence{{source, "observed"}}})
		if p, ok := byDev[m.DeviceNumber]; ok {
			addEdge(g.Nodes[p].ID, id, relation, source)
			g.Claims = append(g.Claims, Claim{g.Nodes[p].ID, claim + m.FSType, Evidence{source, "observed"}})
		}
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool { a, b := g.Edges[i], g.Edges[j]; return a.From+a.To+a.Kind < b.From+b.To+b.Kind })
	sort.Strings(g.Notes)
	g.Notes = compact(g.Notes)
	return g
}

func validDeviceNumber(s string) bool {
	var major, minor uint64
	if _, err := fmt.Sscanf(s, "%d:%d", &major, &minor); err != nil {
		return false
	}
	return fmt.Sprintf("%d:%d", major, minor) == s
}
func compact(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}

// Conflict summarizes incompatible signature claims without treating normal
// layering (partition -> LVM -> filesystem -> mount) as a conflict.
func Conflict(types []string) bool {
	unique := map[string]bool{}
	for _, t := range types {
		if t = strings.TrimSpace(t); t != "" {
			unique[t] = true
		}
	}
	return len(unique) > 1
}

// stackFamily maps an observed claim technology onto one stack name so a PV
// signature and the LVM used-by edge count as one stack, and a matching
// mount counts with its filesystem. Unknown names stay distinct.
func stackFamily(tech string) string {
	t := strings.TrimSpace(tech)
	if t == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(t, "mounted:"); ok {
		t = rest
	} else if rest, ok := strings.CutPrefix(t, "service-view-mount:"); ok {
		t = rest
	}
	switch strings.ToLower(t) {
	case "lvm2_member", "lvm", "lvm-pv":
		return "lvm"
	case "linux_raid_member", "mdraid":
		return "mdraid"
	case "crypto_luks", "crypt":
		return "crypt"
	case "zfs_member", "zfs":
		return "zfs"
	case "btrfs":
		return "btrfs"
	}
	return t
}

// OverlapClaims reports two unrelated stacks observed on one block node.
// Layered LVM/md/crypt plus a matching filesystem on a different node is
// not overlap. Absence of overlap is not permission to write.
func OverlapClaims(g Graph) bool {
	byNode := map[string]map[string]bool{}
	for _, c := range g.Claims {
		fam := stackFamily(c.Technology)
		if fam == "" || c.Node == "" {
			continue
		}
		if byNode[c.Node] == nil {
			byNode[c.Node] = map[string]bool{}
		}
		byNode[c.Node][fam] = true
	}
	for _, fams := range byNode {
		if len(fams) > 1 {
			return true
		}
	}
	return false
}
