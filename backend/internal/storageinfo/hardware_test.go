// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Synthetic sysfs fixtures only (labelled example data): no real controller,
// enclosure or disk is read by these tests.

type sysfsTree struct {
	t    *testing.T
	root string
}

func newTree(t *testing.T) *sysfsTree { return &sysfsTree{t: t, root: t.TempDir()} }

func (s *sysfsTree) put(p, v string) {
	s.t.Helper()
	p = filepath.Join(s.root, p)
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		s.t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(v+"\n"), 0644); e != nil {
		s.t.Fatal(e)
	}
}

func (s *sysfsTree) dir(p string) {
	s.t.Helper()
	if e := os.MkdirAll(filepath.Join(s.root, p), 0755); e != nil {
		s.t.Fatal(e)
	}
}

// ln creates link -> target, where target is relative to the link's directory
// exactly as in the kernel's sysfs.
func (s *sysfsTree) ln(link, target string) {
	s.t.Helper()
	p := filepath.Join(s.root, link)
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		s.t.Fatal(e)
	}
	if e := os.Symlink(target, p); e != nil {
		s.t.Fatal(e)
	}
}

const (
	hbaPCI  = "devices/pci0000:00/0000:00:01.0/0000:01:00.0"
	hbaHost = hbaPCI + "/host0"
	expPort = hbaHost + "/port-0:0/expander-0:0"
	ahciPCI = "devices/pci0000:00/0000:00:17.0"
	nvmePCI = "devices/pci0000:00/0000:00:1d.0/0000:3d:00.0"
)

// sasDisk adds a SAS disk behind the expander at end_device-0:0:<n>.
func (s *sysfsTree) sasDisk(n, name, dev, seq, wwid string, phy string) string {
	sdev := expPort + "/port-0:0:" + n + "/end_device-0:0:" + n + "/target0:0:" + n + "/0:0:" + n + ":0"
	s.put(sdev+"/block/"+name+"/dev", dev)
	s.put(sdev+"/block/"+name+"/diskseq", seq)
	s.put(sdev+"/block/"+name+"/size", "2048")
	s.put(sdev+"/wwid", wwid)
	s.ln(sdev+"/block/"+name+"/device", "../..")
	s.ln("sys/class/block/"+name, "../../../"+sdev+"/block/"+name)
	s.put("sys/class/sas_device/end_device-0:0:"+n+"/phy_identifier", phy)
	return sdev
}

// hba adds an mpt3sas controller, a SATA AHCI controller and an NVMe drive.
func (s *sysfsTree) hba() {
	s.put(hbaHost+"/scsi_host/host0/proc_name", "mpt3sas")
	s.put(hbaHost+"/scsi_host/host0/host_sas_address", "0x500605b0000000aa")
	s.put(hbaHost+"/scsi_host/host0/board_name", "Example-HBA-8i")
	s.put(hbaHost+"/scsi_host/host0/version_fw", "16.00.01.00")
	s.ln("sys/class/scsi_host/host0", "../../../"+hbaHost+"/scsi_host/host0")
	for _, h := range []string{"host1", "host2"} {
		s.put(ahciPCI+"/ata"+h[4:]+"/"+h+"/scsi_host/"+h+"/proc_name", "ahci")
		s.ln("sys/class/scsi_host/"+h, "../../../"+ahciPCI+"/ata"+h[4:]+"/"+h+"/scsi_host/"+h)
	}
	sd := ahciPCI + "/ata1/host1/target1:0:0/1:0:0:0"
	s.put(sd+"/block/sdb/dev", "8:16")
	s.put(sd+"/block/sdb/size", "2048")
	s.put(sd+"/wwid", "naa.5000000000000b02")
	s.ln(sd+"/block/sdb/device", "../..")
	s.ln("sys/class/block/sdb", "../../../"+sd+"/block/sdb")
	s.put(nvmePCI+"/nvme/nvme0/nvme0n1/dev", "259:0")
	s.put(nvmePCI+"/nvme/nvme0/nvme0n1/size", "2048")
	s.put(nvmePCI+"/nvme/nvme0/nvme0n1/wwid", "eui.0000000000000c03")
	s.put(nvmePCI+"/nvme/nvme0/nvme0n1/nsid", "1")
	s.ln("sys/class/block/nvme0n1", "../../../"+nvmePCI+"/nvme/nvme0/nvme0n1")
	s.put("sys/class/nvme/nvme0/transport", "pcie")
	s.put("sys/class/nvme/nvme0/address", "0000:3d:00.0")
	s.put("sys/class/nvme/nvme0/model", "Example NVMe")
}

// enclosure adds an SES enclosure at end_device-0:0:<n> with the given
// logical id and elements; attach maps element name -> SCSI device path.
func (s *sysfsTree) enclosure(n, id string, elements []string, slots []string, attach map[string]string) {
	ses := expPort + "/port-0:0:" + n + "/end_device-0:0:" + n + "/target0:0:" + n + "/0:0:" + n + ":0"
	s.put(ses+"/vendor", "EXAMPLE")
	s.put(ses+"/model", "Example JBOD")
	edir := ses + "/enclosure/0:0:" + n + ":0"
	s.put(edir+"/id", id)
	s.put(edir+"/components", strconv.Itoa(len(elements)))
	s.ln(edir+"/device", "../..")
	s.dir(edir + "/power")
	for i, e := range elements {
		s.put(edir+"/"+e+"/type", "array device")
		s.put(edir+"/"+e+"/slot", slots[i])
		s.put(edir+"/"+e+"/locate", "0")
		if target, ok := attach[e]; ok {
			s.put(edir+"/"+e+"/status", "OK")
			rel := strings.Repeat("../", strings.Count(edir+"/"+e, "/")+1)
			s.ln(edir+"/"+e+"/device", rel+target)
		} else {
			s.put(edir+"/"+e+"/status", "not installed")
		}
	}
	s.ln("sys/class/enclosure/0:0:"+n+":0", "../../../"+edir)
}

func collectHW(t *testing.T, s *sysfsTree) (Graph, *Hardware) {
	t.Helper()
	g, err := Collect(context.Background(), s.root)
	if err != nil {
		t.Fatal(err)
	}
	if g.Hardware == nil {
		t.Fatal("hardware inventory missing")
	}
	return g, g.Hardware
}

func nodeByName(t *testing.T, g Graph, name string) Node {
	t.Helper()
	for _, n := range g.Nodes {
		if n.Name == name && n.DeviceNumber != "" {
			return n
		}
	}
	t.Fatalf("node %s not observed", name)
	return Node{}
}

func linkOf(h *Hardware, node string) *HardwareLink {
	for i := range h.Links {
		if h.Links[i].Node == node {
			return &h.Links[i]
		}
	}
	return nil
}

func TestHardwareControllersEnclosureAndLinks(t *testing.T) {
	s := newTree(t)
	s.hba()
	sda := s.sasDisk("0", "sda", "8:0", "7", "naa.5000000000000a01", "4")
	s.enclosure("1", "0x500a0b8000000001", []string{"Slot 00", "Slot 01"}, []string{"0", "1"}, map[string]string{"Slot 00": sda})
	g, h := collectHW(t, s)
	if h.Status != "observed" {
		t.Fatalf("status %q notes %v", h.Status, h.Notes)
	}
	want := map[string]string{"pci:0000:01:00.0": "sas", "pci:0000:00:17.0": "sata", "pci:0000:3d:00.0": "nvme"}
	if len(h.Controllers) != len(want) {
		t.Fatalf("controllers %+v", h.Controllers)
	}
	for _, c := range h.Controllers {
		if want[c.ID] != c.Kind {
			t.Fatalf("controller %+v", c)
		}
		if c.ID == "pci:0000:01:00.0" && (c.SASAddress != "500605b0000000aa" || c.Driver != "mpt3sas" || c.Model != "Example-HBA-8i") {
			t.Fatalf("hba attributes %+v", c)
		}
		if c.ID == "pci:0000:00:17.0" && len(c.Hosts) != 2 {
			t.Fatalf("ahci ports must group by PCI function: %+v", c)
		}
	}
	if len(h.Enclosures) != 1 {
		t.Fatalf("enclosures %+v", h.Enclosures)
	}
	enc := h.Enclosures[0]
	if enc.ID != "ses:500a0b8000000001" || enc.IdentityStatus != "observed" || enc.ControllerID != "pci:0000:01:00.0" || enc.Components != 2 || enc.Vendor != "EXAMPLE" {
		t.Fatalf("enclosure %+v", enc)
	}
	if len(enc.Slots) != 2 {
		t.Fatalf("power/device entries must not be elements: %+v", enc.Slots)
	}
	disk := nodeByName(t, g, "sda")
	s0, s1 := enc.Slots[0], enc.Slots[1]
	if s0.Match != "matched" || !s0.Occupied || s0.NodeID != disk.ID || s0.PersistentID != "wwn:naa.5000000000000a01" || s0.Slot == nil || *s0.Slot != 0 {
		t.Fatalf("slot 0 %+v", s0)
	}
	if s1.Match != "empty" || s1.Occupied || s1.NodeID != "" || s1.Status != "not installed" {
		t.Fatalf("slot 1 %+v", s1)
	}
	l := linkOf(h, disk.ID)
	if l == nil || l.ControllerID != "pci:0000:01:00.0" || l.Port != "phy 4" || l.EnclosureID != enc.ID || l.Element != "Slot 00" || l.Slot == nil || *l.Slot != 0 {
		t.Fatalf("sda link %+v", l)
	}
	if l := linkOf(h, nodeByName(t, g, "sdb").ID); l == nil || l.ControllerID != "pci:0000:00:17.0" || l.Port != "ata1" || l.EnclosureID != "" {
		t.Fatalf("sdb link %+v", l)
	}
	if l := linkOf(h, nodeByName(t, g, "nvme0n1").ID); l == nil || l.ControllerID != "pci:0000:3d:00.0" || l.Port != "nvme0" {
		t.Fatalf("nvme link %+v", l)
	}
}

// A renumbered kernel name must not move a drive's slot identity: the same
// physical disk now enumerated as sdq still carries the same persistent ID.
func TestHardwareSlotIdentityIgnoresKernelName(t *testing.T) {
	ids := map[string]string{}
	for _, name := range []string{"sda", "sdq"} {
		s := newTree(t)
		s.hba()
		sd := s.sasDisk("0", name, map[string]string{"sda": "8:0", "sdq": "65:0"}[name], "3", "naa.5000000000000a01", "4")
		s.enclosure("1", "0x500a0b8000000001", []string{"Slot 00"}, []string{"0"}, map[string]string{"Slot 00": sd})
		_, h := collectHW(t, s)
		ids[name] = h.Enclosures[0].Slots[0].PersistentID
	}
	if ids["sda"] == "" || ids["sda"] != ids["sdq"] {
		t.Fatalf("slot identity changed with the kernel name: %v", ids)
	}
}

func TestHardwareDualPathEnclosureIsAmbiguous(t *testing.T) {
	s := newTree(t)
	s.hba()
	sda := s.sasDisk("0", "sda", "8:0", "1", "naa.5000000000000a01", "4")
	s.enclosure("1", "0x500a0b8000000001", []string{"Slot 00"}, []string{"0"}, map[string]string{"Slot 00": sda})
	s.enclosure("2", "0x500a0b8000000001", []string{"Slot 00"}, []string{"0"}, map[string]string{"Slot 00": sda})
	g, h := collectHW(t, s)
	for _, enc := range h.Enclosures {
		if enc.IdentityStatus != "ambiguous" || enc.Slots[0].Match != "ambiguous" || enc.Slots[0].PersistentID != "" {
			t.Fatalf("dual-path enclosure must attach nothing: %+v", enc)
		}
	}
	if l := linkOf(h, nodeByName(t, g, "sda").ID); l == nil || l.EnclosureID != "" || l.Slot != nil {
		t.Fatalf("ambiguous slot leaked into link %+v", l)
	}
}

func TestHardwareSlotRefusals(t *testing.T) {
	cases := []struct {
		name, id, want string
		build          func(*sysfsTree) string
	}{
		{"zero enclosure id", "0x0", "no-enclosure-id", func(s *sysfsTree) string {
			return s.sasDisk("0", "sda", "8:0", "1", "naa.5000000000000a01", "4")
		}},
		{"duplicated drive identity", "0x500a0b8000000001", "unidentified", func(s *sysfsTree) string {
			s.sasDisk("3", "sdc", "8:32", "2", "naa.5000000000000a01", "5")
			return s.sasDisk("0", "sda", "8:0", "1", "naa.5000000000000a01", "4")
		}},
		{"weak identity tier", "0x500a0b8000000001", "unidentified", func(s *sysfsTree) string {
			return s.sasDisk("0", "sda", "8:0", "1", "", "4")
		}},
		{"device not in this observation", "0x500a0b8000000001", "changed", func(s *sysfsTree) string {
			sd := s.sasDisk("0", "sda", "8:0", "1", "naa.5000000000000a01", "4")
			s.put(sd+"/block/sda/dev", "8:0")
			// The enclosure reports a different, unobserved device number.
			other := expPort + "/port-0:0:9/end_device-0:0:9/target0:0:9/0:0:9:0"
			s.put(other+"/block/sdz/dev", "8:400")
			return other
		}},
		{"diskseq reuse", "0x500a0b8000000001", "changed", func(s *sysfsTree) string {
			sd := s.sasDisk("0", "sda", "8:0", "1", "naa.5000000000000a01", "4")
			other := expPort + "/port-0:0:9/end_device-0:0:9/target0:0:9/0:0:9:0"
			s.put(other+"/block/sda/dev", "8:0")
			s.put(other+"/block/sda/diskseq", "99")
			_ = sd
			return other
		}},
		{"attached device without block disk", "0x500a0b8000000001", "not-a-disk", func(s *sysfsTree) string {
			s.sasDisk("0", "sda", "8:0", "1", "naa.5000000000000a01", "4")
			tape := expPort + "/port-0:0:8/end_device-0:0:8/target0:0:8/0:0:8:0"
			s.put(tape+"/type", "1")
			return tape
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTree(t)
			s.hba()
			target := tc.build(s)
			s.enclosure("1", tc.id, []string{"Slot 00"}, []string{"0"}, map[string]string{"Slot 00": target})
			_, h := collectHW(t, s)
			sl := h.Enclosures[0].Slots[0]
			if sl.Match != tc.want || sl.PersistentID != "" || sl.NodeID != "" {
				t.Fatalf("want %s, got %+v", tc.want, sl)
			}
			for _, l := range h.Links {
				if l.EnclosureID != "" || l.Slot != nil {
					t.Fatalf("refused slot leaked into link %+v", l)
				}
			}
		})
	}
}

func TestHardwareLogicalIDLeadingZeros(t *testing.T) {
	s := newTree(t)
	s.hba()
	sda := s.sasDisk("0", "sda", "8:0", "1", "naa.5000000000000a01", "4")
	s.enclosure("1", "0x123", []string{"0"}, []string{"0"}, map[string]string{"0": sda})
	_, h := collectHW(t, s)
	if h.Enclosures[0].ID != "ses:0000000000000123" {
		t.Fatalf("logical id %q", h.Enclosures[0].ID)
	}
}

func TestHardwareUnavailableWithoutControllerClasses(t *testing.T) {
	s := newTree(t)
	s.put("sys/class/block/sda/dev", "8:0")
	s.put("sys/class/block/sda/size", "2048")
	_, h := collectHW(t, s)
	if h.Status != "unavailable" || len(h.Controllers) != 0 || len(h.Enclosures) != 0 || len(h.Links) != 0 || len(h.Notes) == 0 {
		t.Fatalf("hardware %+v", h)
	}
}

// Symlinks that leave the collector root are refused by os.Root; an escaping
// enclosure is skipped instead of being read.
func TestHardwareEscapingEnclosureLinkIgnored(t *testing.T) {
	s := newTree(t)
	s.hba()
	outside := t.TempDir()
	if e := os.WriteFile(filepath.Join(outside, "id"), []byte("0x500a0b8000000001\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(s.root, "sys/class/enclosure"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(s.root, "sys/class/enclosure/0:0:1:0")); e != nil {
		t.Fatal(e)
	}
	_, h := collectHW(t, s)
	if len(h.Enclosures) != 1 || h.Enclosures[0].ID != "" || len(h.Enclosures[0].Slots) != 0 {
		t.Fatalf("escaping enclosure was read: %+v", h.Enclosures)
	}
}

// Discovery never writes: the enclosure locate attribute is left untouched.
func TestHardwareNeverWritesLocate(t *testing.T) {
	s := newTree(t)
	s.hba()
	sda := s.sasDisk("0", "sda", "8:0", "1", "naa.5000000000000a01", "4")
	s.enclosure("1", "0x500a0b8000000001", []string{"Slot 00"}, []string{"0"}, map[string]string{"Slot 00": sda})
	locate := filepath.Join(s.root, hbaHost, "port-0:0/expander-0:0/port-0:0:1/end_device-0:0:1/target0:0:1/0:0:1:0/enclosure/0:0:1:0/Slot 00/locate")
	if e := os.Chmod(locate, 0444); e != nil {
		t.Fatal(e)
	}
	before, _ := os.Stat(locate)
	collectHW(t, s)
	after, _ := os.Stat(locate)
	b, _ := os.ReadFile(locate)
	if string(b) != "0\n" || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("locate attribute changed")
	}
}
