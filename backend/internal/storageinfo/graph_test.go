// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLayeringAndClaims(t *testing.T) {
	g := Build([]Block{{Name: "sda", DeviceNumber: "8:0", Kind: "disk", WWID: "naa.5000000000000a0a"}, {Name: "sda1", DeviceNumber: "8:1", Kind: "partition", Parent: "8:0", PartUUID: "part-A", FSType: "LVM2_member"}, {Name: "dm-0", DeviceNumber: "253:0", Kind: "lvm", Lower: []string{"8:1"}, FSType: "ext4", UUID: "fs-A"}}, []Mount{{ID: "20", DeviceNumber: "253:0", Mountpoint: "/data", FSType: "ext4", View: "host"}})
	if g.Status != "ok" || len(g.Nodes) != 6 || len(g.Edges) != 5 {
		t.Fatalf("unexpected graph: %+v", g)
	}
	var lowerClaim, mountedClaim bool
	for _, c := range g.Claims {
		lowerClaim = lowerClaim || c.Technology == "lvm"
		mountedClaim = mountedClaim || c.Technology == "mounted:ext4"
	}
	if !lowerClaim || !mountedClaim {
		t.Fatal("ownership evidence missing")
	}
}
func TestDuplicateIdentifiersNotMergedOrPersisted(t *testing.T) {
	g := Build([]Block{{Name: "sda", DeviceNumber: "8:0", Serial: "clone"}, {Name: "sdb", DeviceNumber: "8:16", Serial: "clone"}}, nil)
	if len(g.Nodes) != 2 || g.Status != "partial" {
		t.Fatal(g)
	}
	for _, n := range g.Nodes {
		if n.PersistentID != "" || n.IdentityStatus != "ambiguous" {
			t.Fatal(n)
		}
	}
}
func TestKernelRenameKeepsPersistentIdentity(t *testing.T) {
	a := Build([]Block{{Name: "sda", DeviceNumber: "8:0", WWID: "naa.5000000000000d15"}}, nil)
	b := Build([]Block{{Name: "sdc", DeviceNumber: "8:32", WWID: "naa.5000000000000d15"}}, nil)
	if a.Nodes[0].PersistentID != b.Nodes[0].PersistentID || a.Nodes[0].ID == b.Nodes[0].ID {
		t.Fatal("persistent identity confused with snapshot node ID")
	}
}
func TestFilesystemUUIDNotDiskIdentity(t *testing.T) {
	g := Build([]Block{{Name: "sda", DeviceNumber: "8:0", UUID: "cloned-fs", FSType: "ext4"}}, nil)
	for _, n := range g.Nodes {
		if n.PersistentID != "" {
			t.Fatal(n)
		}
	}
}
func TestMountParserDoesNotRetainNetworkCredentials(t *testing.T) {
	in := "37 1 0:42 / /media/My\\040Files rw - cifs //user:password@host/share rw,password=secret\n"
	m, e := ParseMountInfo(strings.NewReader(in))
	if e != nil {
		t.Fatal(e)
	}
	g := Build(nil, m)
	b, _ := json.Marshal(g)
	if len(m) != 1 || m[0].Mountpoint != "/media/My Files" {
		t.Fatal(m)
	}
	for _, secret := range []string{"password", "secret", "user:"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("network credentials retained")
		}
	}
}
func TestMalformedMountInfoRefused(t *testing.T) {
	for _, s := range []string{"garbage", "1 0 8:0 / /bad\\999 rw - ext4 /dev/sda rw", "1 0 8:0junk / / rw - ext4 /dev/sda rw", "1 0 8:0 / relative rw - ext4 x rw"} {
		if _, e := ParseMountInfo(strings.NewReader(s)); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestConflictsDoNotFlagRepeatedSameSignature(t *testing.T) {
	if Conflict([]string{"ext4", "ext4"}) || !Conflict([]string{"ext4", "LVM2_member"}) {
		t.Fatal("incorrect conflict classification")
	}
}

func TestOverlapClaimsLayeredLVMIsOneStack(t *testing.T) {
	g := Build([]Block{
		{Name: "sda", DeviceNumber: "8:0", Kind: "disk", WWID: "naa.5000000000000a0a"},
		{Name: "sda1", DeviceNumber: "8:1", Kind: "partition", Parent: "8:0", PartUUID: "part-A", FSType: "LVM2_member"},
		{Name: "dm-0", DeviceNumber: "253:0", Kind: "lvm", Lower: []string{"8:1"}, FSType: "ext4", UUID: "fs-A"},
	}, []Mount{{ID: "20", DeviceNumber: "253:0", Mountpoint: "/data", FSType: "ext4", View: "host"}})
	if OverlapClaims(g) {
		t.Fatal("layered LVM+ext4 treated as overlapping stacks")
	}
}

func TestOverlapClaimsTwoStacksOnOneDisk(t *testing.T) {
	g := Build([]Block{
		{Name: "sda", DeviceNumber: "8:0", Kind: "disk", WWID: "naa.5000000000000b0b"},
		{Name: "md0", DeviceNumber: "9:0", Kind: "mdraid", Lower: []string{"8:0"}},
		{Name: "dm-0", DeviceNumber: "253:0", Kind: "lvm", Lower: []string{"8:0"}},
	}, nil)
	if !OverlapClaims(g) {
		t.Fatal("mdraid and lvm on the same disk not reported as overlap")
	}
}

func TestOverlapClaimsMatchingMountIsSameStack(t *testing.T) {
	g := Build([]Block{
		{Name: "sda", DeviceNumber: "8:0", Kind: "disk", WWID: "naa.5000000000000c0c", FSType: "ext4"},
	}, []Mount{{ID: "20", DeviceNumber: "8:0", Mountpoint: "/data", FSType: "ext4", View: "host"}})
	if OverlapClaims(g) {
		t.Fatal("ext4 signature plus matching mount treated as overlap")
	}
}

func TestServiceNamespaceMountsRetainRootsWithoutInventingHostClaims(t *testing.T) {
	mounts, e := ParseMountInfo(strings.NewReader("10 1 8:0 /tmp/private /tmp rw - ext4 /dev/sda rw\n11 1 8:0 /@data /data rw - btrfs /dev/sda rw\n"))
	if e != nil {
		t.Fatal(e)
	}
	g := Build([]Block{{DeviceNumber: "8:0", Name: "sda", WWID: "naa.5000000000000f01"}}, mounts)
	if len(g.Nodes) != 3 || g.Status != "partial" {
		t.Fatal(g)
	}
	roots := map[string]bool{}
	for _, node := range g.Nodes {
		if node.Kind == "mount" {
			roots[node.MountRoot] = true
			if node.MountView != "service" {
				t.Fatal("invented host mount", node)
			}
		}
	}
	if !roots["/tmp/private"] || !roots["/@data"] {
		t.Fatal("bind/subvolume roots discarded")
	}
	for _, edge := range g.Edges {
		if edge.Kind == "mounted-as" {
			t.Fatal("service namespace promoted to host")
		}
	}
	for _, claim := range g.Claims {
		if strings.HasPrefix(claim.Technology, "mounted:") {
			t.Fatal("service mount presented as host claim")
		}
	}
}
func TestPartitionDoesNotInheritWholeDiskIdentity(t *testing.T) {
	g := Build([]Block{{Name: "sda", DeviceNumber: "8:0", Kind: "disk", WWID: "naa.5000000000000f01", Serial: "serial"}, {Name: "sda1", DeviceNumber: "8:1", Kind: "partition", Parent: "8:0", WWID: "naa.5000000000000f01", Serial: "serial"}}, nil)
	for _, node := range g.Nodes {
		if node.Kind == "partition" && (node.PersistentID != "" || len(node.Identifiers) != 0) {
			t.Fatal("partition inherited disk identity", node)
		}
		if node.Kind == "disk" && node.IdentityStatus != "observed" {
			t.Fatal("partition poisoned disk identity", node)
		}
	}
}
func TestDroppedRecordsDoNotPoisonSurvivingIdentity(t *testing.T) {
	g := Build([]Block{{Name: "sda", DeviceNumber: "8:0", WWID: "naa.5000000000000001"}, {Name: "invalid", DeviceNumber: "garbage", WWID: "naa.5000000000000001"}}, nil)
	if len(g.Nodes) != 1 || g.Nodes[0].IdentityStatus != "observed" {
		t.Fatal(g)
	}
	duplicate := Build([]Block{{Name: "first", DeviceNumber: "8:0", WWID: "naa.5000000000000001"}, {Name: "second", DeviceNumber: "8:0", WWID: "naa.5000000000000002"}}, nil)
	if len(duplicate.Nodes) != 0 || duplicate.Status != "partial" {
		t.Fatal("conflicting same-device records silently chosen", duplicate)
	}
}
