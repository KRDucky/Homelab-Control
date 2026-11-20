// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"context"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Read-only storage hardware discovery (M2-5 Stage B). Everything here comes
// from world-readable sysfs beneath the collector's os.Root: SCSI hosts, NVMe
// controllers, SES enclosures (the kernel ses driver) and SAS end devices.
// Nothing is executed, no /dev node is opened, and no sysfs attribute is ever
// written (the enclosure "locate" and "fault" files are never opened).
//
// The concept of keeping a controller link as evidence, never as a drive
// identity, follows the owner-supplied HBA Viewer project
// (github.com/KRDucky/hbaviewer-cockpit, MIT); no code was copied from it.
//
// An enclosure slot is a SCSI enclosure element as the enclosure firmware
// numbers it. It is reported hardware evidence, not a verified front-panel bay
// and never a bay assignment: manual layout assignments stay the only source
// of a chassis/bay location.

// Hardware is the observed controller and enclosure inventory.
type Hardware struct {
	// Status is "observed" when sysfs controller classes were readable and
	// "unavailable" otherwise (for example in a container without /sys).
	Status      string         `json:"status"`
	Source      string         `json:"source"`
	Controllers []Controller   `json:"controllers"`
	Enclosures  []Enclosure    `json:"enclosures"`
	Links       []HardwareLink `json:"links"`
	Notes       []string       `json:"notes"`
}

// Controller is a storage controller function. ID is "pci:<address>" when the
// PCI function is known; otherwise it is snapshot-local ("scsi-host:hostN").
type Controller struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Driver     string   `json:"driver,omitempty"`
	PCIAddress string   `json:"pci_address,omitempty"`
	SASAddress string   `json:"sas_address,omitempty"`
	Model      string   `json:"model,omitempty"`
	Firmware   string   `json:"firmware,omitempty"`
	Hosts      []string `json:"hosts,omitempty"`
}

// Enclosure is one SES enclosure. ID is "ses:<logical id>" only when the
// logical identifier is reported, non-zero and unique in this observation;
// IdentityStatus is then "observed". A dual-path enclosure appears twice with
// the same logical id: both entries are "ambiguous" and no slot is matched.
type Enclosure struct {
	ID             string          `json:"id,omitempty"`
	IdentityStatus string          `json:"identity_status"`
	Name           string          `json:"name"`
	Vendor         string          `json:"vendor,omitempty"`
	Model          string          `json:"model,omitempty"`
	ControllerID   string          `json:"controller_id,omitempty"`
	Components     int             `json:"components"`
	Slots          []EnclosureSlot `json:"slots"`
}

// EnclosureSlot is one enclosure element. Match explains whether a drive
// identity is attached:
//
//	matched      exactly one observed whole disk with an assignable identity
//	empty        no device is attached to the element
//	not-a-disk   the attached device has no block disk
//	changed      the attached disk is not the one in this observation
//	unidentified the disk has no unambiguous wwn:/nvme-uuid:/serial: identity
//	ambiguous    the identity or enclosure is seen more than once
//	no-enclosure-id the enclosure reports no logical identifier
//
// NodeID and PersistentID are set only for "matched".
type EnclosureSlot struct {
	Element      string `json:"element"`
	Slot         *int   `json:"slot,omitempty"`
	Type         string `json:"type,omitempty"`
	Status       string `json:"status,omitempty"`
	Occupied     bool   `json:"occupied"`
	Match        string `json:"match"`
	NodeID       string `json:"node_id,omitempty"`
	PersistentID string `json:"persistent_id,omitempty"`
}

// HardwareLink places one observed whole disk on a controller and, when
// matched, an enclosure slot. Port is evidence such as "ata3", "phy 5" or
// "nvme0"; it is never a bay.
type HardwareLink struct {
	Node         string `json:"node"`
	ControllerID string `json:"controller_id,omitempty"`
	Port         string `json:"port,omitempty"`
	EnclosureID  string `json:"enclosure_id,omitempty"`
	Element      string `json:"element,omitempty"`
	Slot         *int   `json:"slot,omitempty"`
}

const (
	maxHosts          = 256
	maxNVMe           = 256
	maxEnclosures     = 64
	maxComponents     = 512
	hardwareSourceTag = "sysfs scsi_host, nvme, enclosure, sas_device"
)

var (
	pciAddress   = regexp.MustCompile(`[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]`)
	hostToken    = regexp.MustCompile(`/host([0-9]{1,5})/`)
	ataToken     = regexp.MustCompile(`/(ata[0-9]{1,5})/`)
	endDevice    = regexp.MustCompile(`/end_device-([0-9]{1,5}:[0-9]{1,5}(?::[0-9]{1,5})?)/`)
	nvmeCtrl     = regexp.MustCompile(`/nvme/(nvme[0-9]{1,5})/`)
	hostName     = regexp.MustCompile(`^host[0-9]{1,5}$`)
	nvmeName     = regexp.MustCompile(`^nvme[0-9]{1,5}$`)
	enclosureDir = regexp.MustCompile(`^[0-9]{1,5}:[0-9]{1,5}:[0-9]{1,5}:[0-9]{1,10}$`)
	sasAddress   = regexp.MustCompile(`^(?:0x)?[0-9a-fA-F]{16}$`)
	logicalID    = regexp.MustCompile(`^(?:0x)?[0-9a-f]{1,16}$`)
)

// sasDrivers are SCSI host drivers of SAS HBAs and RAID controllers.
var sasDrivers = map[string]bool{
	"mpt3sas": true, "mpt2sas": true, "mptsas": true, "mpi3mr": true, "megaraid_sas": true,
	"hpsa": true, "smartpqi": true, "aacraid": true, "isci": true, "pm80xx": true, "hisi_sas_v3_hw": true,
}

func controllerKind(driver, sas string) string {
	switch {
	case sasDrivers[driver] || sas != "":
		return "sas"
	case driver == "ahci" || driver == "ata_piix" || strings.HasPrefix(driver, "sata_") || strings.HasPrefix(driver, "pata_"):
		return "sata"
	case driver == "usb-storage" || driver == "uas":
		return "usb"
	case driver == "virtio_scsi":
		return "virtio-scsi"
	}
	return "scsi"
}

// lastPCI returns the last PCI function in a sysfs device path.
func lastPCI(path string) string {
	all := pciAddress.FindAllString(path, -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

func lastSubmatch(re *regexp.Regexp, path string) string {
	all := re.FindAllStringSubmatch(path+"/", -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1][1]
}

// attrText accepts a short printable sysfs attribute, else "".
func attrText(s string) string {
	s = printable(s)
	if len(s) > 128 {
		return ""
	}
	return s
}

// collectHardware reads controller and enclosure metadata and joins it to the
// whole-disk nodes of g by device number (and diskseq when both sides report
// it). It never fails the inventory: problems become notes.
func collectHardware(ctx context.Context, root *os.Root, g Graph, read func(string) string) *Hardware {
	hw := &Hardware{Status: "unavailable", Source: hardwareSourceTag, Controllers: []Controller{}, Enclosures: []Enclosure{}, Links: []HardwareLink{}, Notes: []string{}}
	note := func(s string) {
		for _, n := range hw.Notes {
			if n == s {
				return
			}
		}
		hw.Notes = append(hw.Notes, s)
	}
	list := func(dir string, limit int) ([]os.DirEntry, bool) {
		f, err := root.Open(dir)
		if err != nil {
			return nil, false
		}
		defer f.Close()
		entries, err := f.ReadDir(limit + 1)
		if err != nil && err != io.EOF {
			return nil, false
		}
		if len(entries) > limit {
			note("A sysfs hardware directory exceeds its entry limit; the remainder was not read.")
			entries = entries[:limit]
		}
		return entries, true
	}
	link := func(p string) string {
		target, err := root.Readlink(p)
		if err != nil || len(target) > 4096 {
			return ""
		}
		return target
	}

	byID := map[string]int{}
	hostController := map[string]string{} // "host3" -> controller ID
	addController := func(c Controller, host string) {
		if i, ok := byID[c.ID]; ok {
			if host != "" {
				hw.Controllers[i].Hosts = append(hw.Controllers[i].Hosts, host)
			}
			return
		}
		if host != "" {
			c.Hosts = []string{host}
		}
		byID[c.ID] = len(hw.Controllers)
		hw.Controllers = append(hw.Controllers, c)
	}

	hosts, hostsOK := list("sys/class/scsi_host", maxHosts)
	for _, e := range hosts {
		if ctx.Err() != nil {
			note("Hardware discovery stopped early: the collection deadline was reached.")
			break
		}
		name := e.Name()
		if !hostName.MatchString(name) {
			continue
		}
		base := "sys/class/scsi_host/" + name + "/"
		driver := attrText(read(base + "proc_name"))
		sas := strings.ToLower(read(base + "host_sas_address"))
		if !sasAddress.MatchString(sas) {
			sas = ""
		}
		sas = strings.TrimPrefix(sas, "0x")
		pci := lastPCI(link("sys/class/scsi_host/" + name))
		c := Controller{Kind: controllerKind(driver, sas), Driver: driver, PCIAddress: pci, SASAddress: sas,
			Model: attrText(read(base + "board_name")), Firmware: attrText(read(base + "version_fw"))}
		c.ID = "scsi-host:" + name
		if pci != "" {
			c.ID = "pci:" + pci
			if i, ok := byID[c.ID]; ok && hw.Controllers[i].Driver != driver {
				c.ID = "pci:" + pci + "/" + driver
			}
		}
		hostController[name] = c.ID
		addController(c, name)
	}
	nvmes, nvmeOK := list("sys/class/nvme", maxNVMe)
	nvmeController := map[string]string{}
	for _, e := range nvmes {
		if ctx.Err() != nil {
			break
		}
		name := e.Name()
		if !nvmeName.MatchString(name) {
			continue
		}
		base := "sys/class/nvme/" + name + "/"
		transport := attrText(read(base + "transport"))
		pci := ""
		if transport == "pcie" {
			if a := strings.ToLower(read(base + "address")); pciAddress.MatchString(a) && len(a) == 12 {
				pci = a
			} else {
				pci = lastPCI(link("sys/class/nvme/" + name))
			}
		}
		c := Controller{ID: "nvme:" + name, Kind: "nvme", Driver: "nvme", PCIAddress: pci,
			Model: attrText(read(base + "model")), Firmware: attrText(read(base + "firmware_rev"))}
		if pci != "" {
			c.ID = "pci:" + pci
		} else if transport != "" {
			note("An NVMe controller over " + transport + " has no PCI function; its ID is snapshot-local.")
		}
		nvmeController[name] = c.ID
		addController(c, "")
	}
	if !hostsOK && !nvmeOK {
		note("Controller metadata (sysfs scsi_host, nvme) is unavailable; controllers and enclosures are unknown.")
		return hw
	}
	hw.Status = "observed"

	// Whole disks by device number, for joins. Partitions and stacked devices
	// are never placed on a controller or in a slot.
	disks := map[string]int{}
	for i, n := range g.Nodes {
		if n.Kind == "block" && n.DeviceNumber != "" {
			disks[n.DeviceNumber] = i
		}
	}
	// linkIndex maps a node ID to its position in hw.Links. Positions stay
	// valid while hw.Links grows; pointers into the slice would not.
	linkIndex := map[string]int{}
	for _, i := range sortedDisks(disks) {
		n := g.Nodes[i]
		target := link("sys/class/block/" + n.Name)
		if target == "" {
			continue
		}
		l := &HardwareLink{Node: n.ID}
		switch {
		case nvmeCtrl.MatchString(target + "/"):
			ctrl := lastSubmatch(nvmeCtrl, target)
			l.ControllerID, l.Port = nvmeController[ctrl], ctrl
		case strings.Contains(target, "/nvme-subsystem/"):
			note("An NVMe multipath namespace is reached through several controllers; no single controller is shown.")
		default:
			if h := lastSubmatch(hostToken, target); h != "" {
				l.ControllerID = hostController["host"+h]
			}
			if ed := lastSubmatch(endDevice, target); ed != "" {
				if phy := read("sys/class/sas_device/end_device-" + ed + "/phy_identifier"); phy != "" {
					if p, err := strconv.Atoi(phy); err == nil && p >= 0 && p < 4096 {
						l.Port = "phy " + strconv.Itoa(p)
					}
				}
			} else if ata := lastSubmatch(ataToken, target); ata != "" {
				l.Port = ata
			}
		}
		if l.ControllerID == "" && l.Port == "" {
			continue
		}
		hw.Links = append(hw.Links, *l)
		linkIndex[n.ID] = len(hw.Links) - 1
	}

	encs, _ := list("sys/class/enclosure", maxEnclosures)
	idCount := map[string]int{}
	for _, e := range encs {
		if ctx.Err() != nil {
			note("Hardware discovery stopped early: the collection deadline was reached.")
			break
		}
		name := e.Name()
		if !enclosureDir.MatchString(name) {
			note("An enclosure with an unexpected sysfs name was skipped.")
			continue
		}
		base := "sys/class/enclosure/" + name + "/"
		enc := Enclosure{Name: name, IdentityStatus: "unavailable", Slots: []EnclosureSlot{},
			Vendor: attrText(read(base + "device/vendor")), Model: attrText(read(base + "device/model"))}
		// The ses driver prints the logical identifier with %#llx, which drops
		// leading zeros; normalise to 16 hex digits. Zero means "not reported".
		if id := strings.ToLower(read(base + "id")); logicalID.MatchString(id) && strings.Trim(strings.TrimPrefix(id, "0x"), "0") != "" {
			hex := strings.TrimPrefix(id, "0x")
			enc.ID = "ses:" + strings.Repeat("0", 16-len(hex)) + hex
			enc.IdentityStatus = "observed"
			idCount[enc.ID]++
		}
		if h := lastSubmatch(hostToken, link("sys/class/enclosure/"+name)); h != "" {
			enc.ControllerID = hostController["host"+h]
		}
		comps, ok := list(base, maxComponents+8)
		if !ok {
			note("An enclosure's elements could not be read.")
		}
		for _, c := range comps {
			// Elements are the real subdirectories; "device", "subsystem" are
			// links and "power" is the kernel's runtime-PM directory.
			cname := c.Name()
			if !c.IsDir() || cname == "power" || attrText(cname) != cname || cname == "" || len(enc.Slots) >= maxComponents {
				continue
			}
			cb := base + cname + "/"
			s := EnclosureSlot{Element: cname, Type: attrText(read(cb + "type")), Status: attrText(read(cb + "status")), Match: "empty"}
			if v, err := strconv.Atoi(read(cb + "slot")); err == nil && v >= 0 && v < 65536 {
				s.Slot = &v
			}
			blocks, attached := list(cb+"device/block", 4)
			if _, err := root.Stat(cb + "device"); err == nil {
				s.Occupied = true
				s.Match = "not-a-disk"
			}
			if attached && len(blocks) == 1 && blockName.MatchString(blocks[0].Name()) {
				bb := cb + "device/block/" + blocks[0].Name() + "/"
				dev, seq := read(bb+"dev"), diskSeq(read(bb+"diskseq"))
				i, found := disks[dev]
				switch {
				case !found || (seq != "" && g.Nodes[i].DiskSeq != "" && seq != g.Nodes[i].DiskSeq):
					s.Match = "changed"
				case !assignableIdentity(g.Nodes[i]):
					s.Match = "unidentified"
				default:
					s.Match, s.NodeID, s.PersistentID = "matched", g.Nodes[i].ID, g.Nodes[i].PersistentID
				}
			}
			enc.Slots = append(enc.Slots, s)
		}
		if n, err := strconv.Atoi(read(base + "components")); err == nil && n >= 0 && n <= 65535 {
			enc.Components = n
		} else {
			enc.Components = len(enc.Slots)
		}
		sort.SliceStable(enc.Slots, func(a, b int) bool { return slotLess(enc.Slots[a], enc.Slots[b]) })
		hw.Enclosures = append(hw.Enclosures, enc)
	}

	// Ambiguity: a repeated enclosure logical id (dual path) or a disk seen in
	// more than one slot attaches nothing.
	seen := map[string]int{}
	for _, enc := range hw.Enclosures {
		for _, s := range enc.Slots {
			if s.Match == "matched" {
				seen[s.PersistentID]++
			}
		}
	}
	for ei := range hw.Enclosures {
		enc := &hw.Enclosures[ei]
		if enc.ID != "" && idCount[enc.ID] > 1 {
			enc.IdentityStatus = "ambiguous"
			note("An enclosure is reported more than once (for example over two SAS paths); its slots are not matched to drives.")
		}
		for si := range enc.Slots {
			s := &enc.Slots[si]
			if s.Match != "matched" {
				continue
			}
			if enc.IdentityStatus == "unavailable" {
				note("An enclosure reports no logical identifier; its slots are shown but not matched to drives.")
				s.Match, s.NodeID, s.PersistentID = "no-enclosure-id", "", ""
				continue
			}
			if enc.IdentityStatus != "observed" || seen[s.PersistentID] > 1 {
				s.Match, s.NodeID, s.PersistentID = "ambiguous", "", ""
				continue
			}
			if i, ok := linkIndex[s.NodeID]; ok {
				hw.Links[i].EnclosureID, hw.Links[i].Element, hw.Links[i].Slot = enc.ID, s.Element, s.Slot
			} else {
				hw.Links = append(hw.Links, HardwareLink{Node: s.NodeID, EnclosureID: enc.ID, Element: s.Element, Slot: s.Slot})
				linkIndex[s.NodeID] = len(hw.Links) - 1
			}
		}
	}
	sort.Slice(hw.Controllers, func(a, b int) bool { return hw.Controllers[a].ID < hw.Controllers[b].ID })
	sort.Slice(hw.Enclosures, func(a, b int) bool { return hw.Enclosures[a].Name < hw.Enclosures[b].Name })
	sort.Slice(hw.Links, func(a, b int) bool { return hw.Links[a].Node < hw.Links[b].Node })
	if len(hw.Enclosures) == 0 {
		note("No SES enclosure is reported by the kernel; drives have no enclosure slot evidence.")
	}
	note("Enclosure slots are numbered by the enclosure firmware and are evidence only; they are not verified front-panel bays and never change a bay assignment.")
	return hw
}

// assignableIdentity mirrors the Stage A assignment rule: one observed whole
// disk with a wwn:, nvme-uuid: or serial: identity.
func assignableIdentity(n Node) bool {
	if n.Kind != "block" || n.IdentityStatus != "observed" {
		return false
	}
	for _, p := range []string{"wwn:", "nvme-uuid:", "serial:"} {
		if strings.HasPrefix(n.PersistentID, p) && len(n.PersistentID) > len(p) {
			return true
		}
	}
	return false
}

func sortedDisks(m map[string]int) []int {
	out := make([]int, 0, len(m))
	for _, i := range m {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

func slotLess(a, b EnclosureSlot) bool {
	switch {
	case a.Slot != nil && b.Slot != nil && *a.Slot != *b.Slot:
		return *a.Slot < *b.Slot
	case a.Slot != nil && b.Slot == nil:
		return true
	case a.Slot == nil && b.Slot != nil:
		return false
	}
	return a.Element < b.Element
}
