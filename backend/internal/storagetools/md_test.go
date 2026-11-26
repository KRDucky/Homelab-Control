// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagetools

import (
	"fmt"
	"strings"
	"testing"
)

// Real mdadm 4.5 output (Ubuntu 26.04 guest).
func TestParseMdDetailReal(t *testing.T) {
	a, err := ParseMdDetail("/dev/md127", fixture(t, "mdadm-detail-raid1-clean-4.5.txt"))
	if err != nil || a.Level != "raid1" || a.State != "clean" || a.Metadata != "1.2" || *a.ArraySizeKB != 203776 || *a.RaidDevices != 2 ||
		*a.Active != 2 || *a.Failed != 0 || a.Name != "homelab-demo:lab1" || a.UUID != "bccae293:2e60f129:0f6160e0:9e2f00a5" || a.Progress != "" ||
		len(a.Members) != 2 || *a.Members[1].Slot != 1 || a.Members[1].State != "active sync" || a.Members[1].Device != "/dev/loop1" {
		t.Fatalf("%+v %v", a, err)
	}
	a, err = ParseMdDetail("/dev/md126", fixture(t, "mdadm-detail-raid5-degraded-4.5.txt"))
	if err != nil || a.State != "clean, degraded" || *a.Failed != 1 || *a.Active != 2 || a.Layout != "left-symmetric" || a.ChunkSize != "512K" ||
		a.Members[1].State != "faulty" || a.Members[1].Device != "/dev/loop3" {
		t.Fatalf("%+v %v", a, err)
	}
	a, err = ParseMdDetail("/dev/md126", fixture(t, "mdadm-detail-raid5-recovering-4.5.txt"))
	if err != nil || a.State != "clean, degraded, recovering" || *a.Spare != 1 || a.Progress != "Rebuild 1% complete" ||
		a.Members[1].State != "spare rebuilding" || *a.Members[1].Slot != 1 {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestParseMdDetailRemovedAndSpareRows(t *testing.T) {
	in := "/dev/md0:\n        Raid Level : raid1\n             State : clean, degraded\n\n" +
		"    Number   Major   Minor   RaidDevice State\n" +
		"       0       8       17        0      active sync   /dev/sdb1\n" +
		"       -       0        0        1      removed\n\n" +
		"       2       8       33        -      spare   /dev/sdc1\n"
	a, err := ParseMdDetail("/dev/md0", []byte(in))
	if err != nil || len(a.Members) != 3 || a.Members[1].State != "removed" || a.Members[1].Device != "" || *a.Members[1].Slot != 1 ||
		a.Members[2].Slot != nil || a.Members[2].State != "spare" || a.Members[2].Device != "/dev/sdc1" {
		t.Fatalf("%+v %v", a, err)
	}
	if _, err := ParseMdDetail("/dev/md0", []byte("mdadm: cannot open /dev/md0: No such file or directory\n")); err == nil {
		t.Error("error text accepted as a report")
	}
}

func TestValidMdName(t *testing.T) {
	for _, ok := range []string{"md0", "md127", "md_lab1", "md_d0"} {
		if !ValidMdName(ok) {
			t.Errorf("%s rejected", ok)
		}
	}
	for _, bad := range []string{"md", "sda", "md0/../x", "md 0", "md-1", ""} {
		if ValidMdName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if a := MdDetailArgs("md127"); len(a) != 2 || a[0] != "--detail" || a[1] != "/dev/md127" {
		t.Fatalf("%v", a)
	}
}

// A device table longer than MaxMdMembers keeps the first rows and says it
// was cut, so the page can say its member list is incomplete.
func TestParseMdDetailMarksTruncatedMembers(t *testing.T) {
	var b strings.Builder
	b.WriteString("/dev/md0:\n        Raid Level : raid10\n             State : clean\n\n    Number   Major   Minor   RaidDevice State\n")
	for i := 0; i < MaxMdMembers+3; i++ {
		fmt.Fprintf(&b, "    %d       8       %d        %d      active sync   /dev/sd%d\n", i, i, i, i)
	}
	a, err := ParseMdDetail("/dev/md0", []byte(b.String()))
	if err != nil || len(a.Members) != MaxMdMembers || !a.MembersTruncated {
		t.Fatalf("members=%d truncated=%v err=%v", len(a.Members), a.MembersTruncated, err)
	}
	a, _ = ParseMdDetail("/dev/md0", []byte("/dev/md0:\n        Raid Level : raid1\n\n    Number   Major   Minor   RaidDevice State\n       0       8       1        0      active sync   /dev/sda1\n"))
	if a.MembersTruncated {
		t.Fatal("a short table is not truncated")
	}
}
