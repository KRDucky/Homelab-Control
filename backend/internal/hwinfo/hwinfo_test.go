// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package hwinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) Inventory {
	t.Helper()
	root, err := filepath.Abs("testdata/host")
	if err != nil {
		t.Fatal(err)
	}
	return (&Collector{Roots: Roots{Root: root}}).Collect()
}

func TestSystem(t *testing.T) {
	s := fixture(t).System
	if s.Vendor != "ASRock" || s.Product != "B550M Pro4" || s.ChassisType != "Desktop" || s.BIOSVersion != "P2.10" {
		t.Fatalf("%+v", s)
	}
}

func TestPCIWithNames(t *testing.T) {
	inv := fixture(t)
	if inv.PCI.Status != "ok" || len(inv.PCI.Devices) != 1 {
		t.Fatalf("%+v", inv.PCI)
	}
	d := inv.PCI.Devices[0]
	if d.Class != "Storage controller" || d.VendorName != "Example Vendor (synthetic)" || d.Driver != "ahci" || d.IOMMUGroup != "7" ||
		d.DeviceName != "Example SATA Controller (synthetic)" {
		t.Fatalf("%+v", d)
	}
}

// The fixture is synthetic data in pci.ids format (not an excerpt of the PCI ID Repository); this
// pins the parser rules it exercises: vendor and device lines, subsystem lines ignored, class
// section ends parsing.
func TestParsePCIIDsFixture(t *testing.T) {
	f, err := os.Open("testdata/host/usr/share/misc/pci.ids")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ids := parsePCIIDs(f)
	if len(ids.vendors) != 2 || ids.vendors["f0e2"] != "Second Example Vendor (synthetic)" {
		t.Fatalf("vendors: %v", ids.vendors)
	}
	if len(ids.devices) != 3 || ids.devices["f0e10a01"] != "Example Device Without Driver (synthetic)" ||
		ids.devices["f0e20b01"] != "Unrelated Device (synthetic)" {
		t.Fatalf("devices: %v", ids.devices)
	}
	if _, ok := ids.vendors["f0e3"]; ok {
		t.Fatal("vendor after the class section was parsed")
	}
	if v, d := ids.lookup("f0e1", "0001"); v == "" || d != "" {
		t.Fatalf("subsystem line parsed as a device: %q %q", v, d)
	}
}

func TestUSBSkipsInterfaces(t *testing.T) {
	inv := fixture(t)
	if len(inv.USB.Devices) != 1 || inv.USB.Devices[0].Product != "Ultra Fit" {
		t.Fatalf("%+v", inv.USB.Devices)
	}
}

func TestDiskIdentity(t *testing.T) {
	inv := fixture(t)
	if len(inv.Disks.Devices) != 2 {
		t.Fatalf("%+v", inv.Disks.Devices)
	}
	by := map[string]Disk{}
	for _, d := range inv.Disks.Devices {
		by[d.KernelName] = d
	}
	sda := by["sda"]
	if sda.Transport != "sata" || sda.Confidence != "high" || sda.Identity != "naa.5000cca252c8d2a1" || sda.SizeBytes != 15628053168*512 {
		t.Fatalf("sda: %+v", sda)
	}
	if sda.Rotational == nil || !*sda.Rotational {
		t.Fatal("rotational flag")
	}
	for _, id := range sda.Identifiers {
		if id.Value == "sda" || id.Value == "/dev/sda" {
			t.Fatal("kernel name used as identifier")
		}
		if id.Kind == "by-id" && filepath.Ext(id.Value) != "" && id.Value[len(id.Value)-6:] == "-part1" {
			t.Fatal("partition link counted as disk identity")
		}
	}
	nv := by["nvme0n1"]
	if nv.Transport != "nvme" || nv.Confidence != "medium" || nv.Identity != "S5GXNX0R123456A" {
		t.Fatalf("nvme: %+v", nv)
	}
	if sda.KernelNote == "" {
		t.Fatal("volatile kernel name must be labelled")
	}
}

func TestSensors(t *testing.T) {
	inv := fixture(t)
	if len(inv.Sensors.Readings) != 2 {
		t.Fatalf("%+v", inv.Sensors.Readings)
	}
	r := inv.Sensors.Readings
	if r[0].Label != "Tctl" && r[1].Label != "Tctl" {
		t.Fatalf("%+v", r)
	}
	for _, s := range r {
		if s.Kind == "temperature" && s.Value != 45.25 {
			t.Fatalf("millidegree conversion: %+v", s)
		}
	}
}

func TestEmptyHostIsLabelledNotInvented(t *testing.T) {
	inv := (&Collector{Roots: Roots{Root: t.TempDir()}}).Collect()
	for name, st := range map[string]string{"system": inv.System.Status, "pci": inv.PCI.Status, "usb": inv.USB.Status, "disks": inv.Disks.Status, "sensors": inv.Sensors.Status} {
		if st != "unavailable" {
			t.Errorf("%s: %s", name, st)
		}
	}
	if len(inv.Disks.Devices) != 0 || inv.System.Vendor != "" {
		t.Fatal("values invented for empty host")
	}
}

func TestRankIdentity(t *testing.T) {
	if c, v := rankIdentity(nil); c != "none" || v != "" {
		t.Fatal(c, v)
	}
	if c, _ := rankIdentity([]Identifier{{Kind: "by-id", Value: "x"}}); c != "low" {
		t.Fatal(c)
	}
}

func TestVirtualDiskMediaTypeUnknown(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "sys/block/vda")
	for f, v := range map[string]string{"size": "100", "queue/rotational": "1", "serial": "abc"} {
		p := filepath.Join(d, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inv := (&Collector{Roots: Roots{Root: root}}).Collect()
	if len(inv.Disks.Devices) != 1 || inv.Disks.Devices[0].Rotational != nil {
		t.Fatalf("virtio rotational flag must not be reported as media type: %+v", inv.Disks.Devices)
	}
}
