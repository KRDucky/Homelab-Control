// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func identifierOf(n Node, kind string) (Identifier, bool) {
	for _, id := range n.Identifiers {
		if id.Kind == kind {
			return id, true
		}
	}
	return Identifier{}, false
}

func TestClassifyWWIDByDesignatorPrefix(t *testing.T) {
	// Synthetic values shaped like Linux sysfs wwid output.
	for _, tc := range []struct{ raw, kind string }{
		{"naa.5000c500a1b2c3d4", KindWWN},
		{"eui.0025388b71b2c3d4", KindWWN},
		{"EUI.0025388B71B2C3D4", KindWWN},
		{"uuid.6d1c3b6e-1f7a-4a8e-9c0b-1a2b3c4d5e6f", KindNamespaceUUID},
		{"t10.ATA     EXAMPLE DISK                        EXAMPLE123", KindT10VendorID},
		{"nvme.1e0f-4558414d504c45-4558414d504c45-00000001", KindNVMeVendorID},
		{"0x5000c500a1b2c3d4", KindWWIDUnclassified},
	} {
		kind, ok := ClassifyWWID(tc.raw)
		if !ok || kind != tc.kind {
			t.Errorf("ClassifyWWID(%q) = %q, want %q", tc.raw, kind, tc.kind)
		}
	}
	if _, ok := ClassifyWWID("  "); ok {
		t.Fatal("empty wwid classified")
	}
}

// Review designator prefixes alone do not make a WWN. Values are
// synthetic, shaped like the kernel's scsi_vpd_lun_id and NVMe wwid_show output.
func TestCanonicalWWIDValidatesKernelDesignatorFormats(t *testing.T) {
	for _, tc := range []struct{ raw, kind, value string }{
		{"naa.5000000000000001", KindWWN, "naa.5000000000000001"},                                 // NAA 5, 8 bytes
		{"naa.2000000000000001", KindWWN, "naa.2000000000000001"},                                 // NAA 2, 8 bytes
		{"naa.60000000000000000000000000000001", KindWWN, "naa.60000000000000000000000000000001"}, // NAA 6, 16 bytes
		{"NAA.5000ABCDEF000001", KindWWN, "naa.5000abcdef000001"},                                 // SCSI name string spelling
		{"eui.000000000000000a", KindWWN, "eui.000000000000000a"},                                 // EUI-64
		{"eui.00000000000000000000000a", KindWWN, "eui.00000000000000000000000a"},                 // 12 bytes
		{"eui.0000000000000000000000000000000a", KindWWN, "eui.0000000000000000000000000000000a"}, // NGUID
		{"uuid.6D1C3B6E-1F7A-4A8E-9C0B-1A2B3C4D5E6F", KindNamespaceUUID, "uuid.6d1c3b6e-1f7a-4a8e-9c0b-1a2b3c4d5e6f"},
		// Malformed or not globally identifying: raw, unclassified evidence.
		{"naa.", KindWWIDUnclassified, "naa."},
		{"naa.fixture", KindWWIDUnclassified, "naa.fixture"},
		{"naa.5000000000000", KindWWIDUnclassified, "naa.5000000000000"},       // wrong length
		{"naa.6000000000000001", KindWWIDUnclassified, "naa.6000000000000001"}, // NAA 6 needs 16 bytes
		{"naa.5000000000000000000000000000001", KindWWIDUnclassified, "naa.5000000000000000000000000000001"},
		{"naa.3000000000000001", KindWWIDUnclassified, "naa.3000000000000001"}, // locally assigned
		{"naa.0000000000000000", KindWWIDUnclassified, "naa.0000000000000000"},
		{"eui.0000000000000000", KindWWIDUnclassified, "eui.0000000000000000"}, // all-zero
		{"eui.00000000000001", KindWWIDUnclassified, "eui.00000000000001"},     // wrong length
		{"eui.g000000000000001", KindWWIDUnclassified, "eui.g000000000000001"}, // non-hex
		{"uuid.00000000-0000-0000-0000-000000000000", KindWWIDUnclassified, "uuid.00000000-0000-0000-0000-000000000000"},
		{"uuid.not-a-uuid", KindWWIDUnclassified, "uuid.not-a-uuid"},
	} {
		kind, value, ok := CanonicalWWID(tc.raw)
		if !ok || kind != tc.kind || value != tc.value {
			t.Errorf("CanonicalWWID(%q) = %q %q, want %q %q", tc.raw, kind, value, tc.kind, tc.value)
		}
	}
}

func TestEquivalentWWNSpellingsAreDuplicatesAndMalformedIsNotWWNTier(t *testing.T) {
	g := Build([]Block{{Name: "sda", DeviceNumber: "8:0", WWID: "naa.5000abcdef000001"}, {Name: "sdb", DeviceNumber: "8:16", WWID: "NAA.5000ABCDEF000001"}}, nil)
	for _, n := range g.Nodes {
		if n.IdentityStatus != "ambiguous" || n.PersistentID != "" {
			t.Fatalf("case-equivalent WWNs not treated as one shared identity: %+v", n)
		}
		if id, ok := identifierOf(n, KindWWN); !ok || !id.Ambiguous || id.Value != "naa.5000abcdef000001" {
			t.Fatalf("WWN identifier not canonical/ambiguous: %+v", n.Identifiers)
		}
	}
	g = Build([]Block{{Name: "sda", DeviceNumber: "8:0", WWID: "naa.fixture", Serial: "SER-A"}}, nil)
	n := g.Nodes[0]
	if _, ok := identifierOf(n, KindWWN); ok || n.PersistentID != "serial:SER-A" {
		t.Fatalf("malformed naa. treated as a WWN: %+v", n)
	}
	if id, ok := identifierOf(n, KindWWIDUnclassified); !ok || id.Value != "naa.fixture" {
		t.Fatalf("raw wwid evidence not retained: %+v", n.Identifiers)
	}
}

func TestFailedDesignatorNeverBecomesPersistentIdentity(t *testing.T) {
	for _, raw := range []string{"eui.0000000000000000", "naa.", "naa.fixture", "naa.3000000000000001", "uuid.00000000-0000-0000-0000-000000000000"} {
		a := Build([]Block{{Name: "sda", DeviceNumber: "8:0", WWID: raw}}, nil).Nodes[0]
		b := Build([]Block{{Name: "sdc", DeviceNumber: "8:32", WWID: raw}}, nil).Nodes[0]
		if a.PersistentID != "" || a.IdentityStatus != "unavailable" || SameDevice(a, b) {
			t.Fatalf("%q became a persistent identity: %+v", raw, a)
		}
		if id, ok := identifierOf(a, KindWWIDUnclassified); !ok || id.Value != raw {
			t.Fatalf("%q not kept as raw evidence: %+v", raw, a.Identifiers)
		}
	}
	// Genuine unprefixed vendor values keep the lowest identity tier.
	if n := Build([]Block{{Name: "sda", DeviceNumber: "8:0", WWID: "0x5000c500a1b2c3d4"}}, nil).Nodes[0]; n.PersistentID != "wwid-unclassified:0x5000c500a1b2c3d4" {
		t.Fatalf("unprefixed wwid lost its tier: %q", n.PersistentID)
	}
}

func TestVendorDerivedWWIDIsNotWWNAndRanksBelowSerial(t *testing.T) {
	g := Build([]Block{{Name: "sda", DeviceNumber: "8:0", WWID: "t10.ATA EXAMPLE", Serial: "SER-A"}}, nil)
	n := g.Nodes[0]
	if _, ok := identifierOf(n, KindWWN); ok {
		t.Fatal("t10 vendor ID labelled as a WWN")
	}
	if n.PersistentID != "serial:SER-A" {
		t.Fatalf("vendor-derived wwid outranked a device serial: %q", n.PersistentID)
	}
	g = Build([]Block{{Name: "sda", DeviceNumber: "8:0", WWID: "t10.ATA EXAMPLE"}}, nil)
	if g.Nodes[0].PersistentID != "t10-vendor-id:t10.ATA EXAMPLE" {
		t.Fatalf("vendor ID not kept as lowest-tier identity: %q", g.Nodes[0].PersistentID)
	}
}

func TestSharedVendorDerivedWWIDIsAmbiguous(t *testing.T) {
	for _, raw := range []string{"t10.ATA CLONE", "nvme.1e0f-00-00-00000001"} {
		g := Build([]Block{{Name: "a", DeviceNumber: "8:0", WWID: raw}, {Name: "b", DeviceNumber: "8:16", WWID: raw}}, nil)
		for _, n := range g.Nodes {
			if n.PersistentID != "" || n.IdentityStatus != "ambiguous" {
				t.Fatalf("%s: shared vendor ID became identity: %+v", raw, n)
			}
			for _, id := range n.Identifiers {
				if !id.Ambiguous {
					t.Fatalf("%s: shared identifier not flagged", raw)
				}
			}
		}
	}
}

func TestNVMeControllerSerialSharedByNamespacesIsNotDuplicate(t *testing.T) {
	ns := func(name, dev, wwid, ctrl string) Block {
		return Block{Name: name, DeviceNumber: dev, WWID: wwid, ControllerSerial: "CTRL-SERIAL", ControllerKey: ctrl}
	}
	g := Build([]Block{ns("nvme0n1", "259:0", "eui.0000000000000001", "nqn.example\x001"), ns("nvme0n2", "259:1", "eui.0000000000000002", "nqn.example\x001")}, nil)
	for _, n := range g.Nodes {
		id, ok := identifierOf(n, KindControllerSerial)
		if !ok || id.Ambiguous || id.Scope != "controller" {
			t.Fatalf("controller serial misreported for %s: %+v", n.Name, n.Identifiers)
		}
		if n.IdentityStatus != "observed" || n.PersistentID == "serial:CTRL-SERIAL" {
			t.Fatalf("controller serial used as namespace identity: %+v", n)
		}
	}
	// The same serial under two different controllers is a real duplicate.
	g = Build([]Block{ns("nvme0n1", "259:0", "eui.0000000000000001", "nqn.a\x001"), ns("nvme1n1", "259:2", "eui.0000000000000003", "nqn.b\x001")}, nil)
	for _, n := range g.Nodes {
		if id, _ := identifierOf(n, KindControllerSerial); !id.Ambiguous {
			t.Fatal("serial reused across controllers not flagged")
		}
	}
}

func TestStackedDevicesHaveNativeUUIDIdentityWithoutInventedWWN(t *testing.T) {
	g := Build([]Block{{Name: "dm-0", DeviceNumber: "253:0", NativeKind: "dm-uuid", NativeUUID: "LVM-fixture"}, {Name: "md0", DeviceNumber: "9:0", NativeKind: "md-uuid", NativeUUID: "md-fixture"}}, nil)
	for _, n := range g.Nodes {
		if n.PersistentID == "" || n.IdentityStatus != "observed" {
			t.Fatal("native UUID discarded", n)
		}
		if _, ok := identifierOf(n, KindWWN); ok {
			t.Fatal("invented a physical WWN for a stacked device")
		}
	}
}

func TestSameDeviceComparesPersistentIdentityNotNodeID(t *testing.T) {
	// The kernel reused 8:0 for a different disk after hotplug.
	before := Build([]Block{{Name: "sda", DeviceNumber: "8:0", WWID: "naa.5000000000000001"}}, nil).Nodes[0]
	after := Build([]Block{{Name: "sda", DeviceNumber: "8:0", WWID: "naa.5000000000000002"}}, nil).Nodes[0]
	if before.ID != after.ID {
		t.Fatal("fixture premise: node IDs derive from the reused device number")
	}
	if SameDevice(before, after) {
		t.Fatal("reused device number treated as the same device")
	}
	renamed := Build([]Block{{Name: "sdc", DeviceNumber: "8:32", WWID: "naa.5000000000000001"}}, nil).Nodes[0]
	if !SameDevice(before, renamed) {
		t.Fatal("same WWN under a new kernel name not recognized")
	}
	none := Build([]Block{{Name: "loop0", DeviceNumber: "7:0"}}, nil).Nodes[0]
	if SameDevice(none, none) {
		t.Fatal("device without persistent identity treated as comparable")
	}
}

func TestCollectIdentitySources(t *testing.T) {
	root := t.TempDir()
	put := func(p string, b []byte) {
		t.Helper()
		p = filepath.Join(root, p)
		if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, b, 0644); e != nil {
			t.Fatal(e)
		}
	}
	// Synthetic SATA disk behind libata: wwid from VPD 0x83, serial in VPD 0x80.
	put("sys/class/block/sda/dev", []byte("8:0"))
	put("sys/class/block/sda/size", []byte("8"))
	put("sys/class/block/sda/device/wwid", []byte("naa.5000000000000a01"))
	put("sys/class/block/sda/device/vpd_pg80", append([]byte{0x00, 0x80, 0x00, 0x0c}, []byte("  EXAMPLE-A1")...))
	// Synthetic virtio-blk disk: serial at the block level, no wwid.
	put("sys/class/block/vda/dev", []byte("252:0"))
	put("sys/class/block/vda/size", []byte("8"))
	put("sys/class/block/vda/serial", []byte("EXAMPLE-VIRTIO"))
	// Synthetic NVMe namespace: wwid at block level, controller serial in device/.
	put("sys/class/block/nvme0n1/dev", []byte("259:0"))
	put("sys/class/block/nvme0n1/size", []byte("8"))
	put("sys/class/block/nvme0n1/nsid", []byte("1"))
	put("sys/class/block/nvme0n1/wwid", []byte("eui.0000000000000001"))
	put("sys/class/block/nvme0n1/device/serial", []byte("EXAMPLE-NVME   "))
	put("sys/class/block/nvme0n1/device/cntlid", []byte("1"))
	// Malformed VPD page (wrong page code) must not yield a serial.
	put("sys/class/block/sdb/dev", []byte("8:16"))
	put("sys/class/block/sdb/size", []byte("8"))
	put("sys/class/block/sdb/device/vpd_pg80", append([]byte{0x00, 0x83, 0x00, 0x04}, []byte("BAD!")...))
	// Synthetic hidden NVMe multipath path device repeating the head's wwid.
	put("sys/class/block/nvme0c0n1/dev", []byte("259:9"))
	put("sys/class/block/nvme0c0n1/size", []byte("8"))
	put("sys/class/block/nvme0c0n1/hidden", []byte("1"))
	put("sys/class/block/nvme0c0n1/wwid", []byte("eui.0000000000000001"))
	g, e := Collect(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	byName := map[string]Node{}
	for _, n := range g.Nodes {
		byName[n.Name] = n
	}
	if id, ok := identifierOf(byName["sda"], KindSerial); !ok || id.Value != "EXAMPLE-A1" || id.Source != "sysfs device/vpd_pg80" {
		t.Fatalf("SATA VPD serial: %+v", byName["sda"].Identifiers)
	}
	if byName["sda"].PersistentID != "wwn:naa.5000000000000a01" {
		t.Fatal(byName["sda"].PersistentID)
	}
	if id, ok := identifierOf(byName["vda"], KindSerial); !ok || id.Value != "EXAMPLE-VIRTIO" || id.Source != "sysfs serial" {
		t.Fatalf("virtio serial: %+v", byName["vda"].Identifiers)
	}
	nvme := byName["nvme0n1"]
	if id, ok := identifierOf(nvme, KindControllerSerial); !ok || id.Value != "EXAMPLE-NVME" || id.Scope != "controller" {
		t.Fatalf("NVMe controller serial: %+v", nvme.Identifiers)
	}
	if _, ok := identifierOf(nvme, KindSerial); ok || nvme.PersistentID != "wwn:eui.0000000000000001" {
		t.Fatalf("NVMe identity: %+v", nvme)
	}
	if _, listed := byName["nvme0c0n1"]; listed || nvme.IdentityStatus != "observed" {
		t.Fatal("hidden multipath path device listed or made the namespace ambiguous")
	}
	if _, ok := identifierOf(byName["sdb"], KindSerial); ok {
		t.Fatal("malformed VPD page produced a serial")
	}
}
