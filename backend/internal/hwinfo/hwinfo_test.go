package hwinfo

import (
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
	if d.Class != "Storage controller" || d.VendorName != "Intel Corporation" || d.Driver != "ahci" || d.IOMMUGroup != "7" ||
		d.DeviceName != "82801IR/IO/IH (ICH9R/DO/DH) 6 port SATA Controller [AHCI mode]" {
		t.Fatalf("%+v", d)
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
