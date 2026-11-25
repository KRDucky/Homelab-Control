// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagetools

import (
	"fmt"
	"strings"
	"testing"
)

// Real btrfs-progs 6.17.1 output (Ubuntu 26.04 guest).
func TestParseBtrfsUsageReal(t *testing.T) {
	o, p, err := ParseBtrfsUsage(fixture(t, "btrfs-usage-raid1-6.17.txt"))
	if err != nil || *o.DeviceSize != 2147483648 || *o.Allocated != 513277952 || *o.Missing != 0 || *o.Used != 294912 ||
		*o.FreeEstimated != 924450816 || *o.DataRatio != 2 || *o.MetadataRatio != 2 {
		t.Fatalf("%+v %v", o, err)
	}
	if len(p) != 4 || p[0].Type != "Data" || p[0].Profile != "RAID1" || *p[0].Size != 107347968 || *p[0].Used != 0 ||
		len(p[0].Devices) != 2 || p[0].Devices[1] != (BtrfsDeviceUsed{Path: "/dev/loop1", Bytes: 107347968}) ||
		p[3].Type != "Unallocated" || p[3].Devices[0].Bytes != 817102848 {
		t.Fatalf("%+v", p)
	}
	_, p, err = ParseBtrfsUsage(fixture(t, "btrfs-usage-single-6.17.txt"))
	if err != nil || p[0].Profile != "single" || p[1].Type != "Metadata" || p[1].Profile != "DUP" || p[1].Devices[0].Bytes != 214695936 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, _, err := ParseBtrfsUsage([]byte("ERROR: not a btrfs filesystem\n")); err == nil {
		t.Error("non-report accepted")
	}
}

func TestParseBtrfsDeviceStatsReal(t *testing.T) {
	d, err := ParseBtrfsDeviceStats(fixture(t, "btrfs-stats-raid1-6.17.txt"))
	if err != nil || len(d) != 2 || d[1].Path != "/dev/loop1" || *d[1].CorruptionErrs != 0 || d[0].GenerationErrs == nil {
		t.Fatalf("%+v %v", d, err)
	}
	d, err = ParseBtrfsDeviceStats([]byte("[/dev/sdb].write_io_errs    3\n[/dev/sdb].corruption_errs  17\n"))
	if err != nil || *d[0].WriteIOErrs != 3 || *d[0].CorruptionErrs != 17 || d[0].ReadIOErrs != nil {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := ParseBtrfsDeviceStats([]byte("garbage\n")); err == nil {
		t.Error("garbage accepted")
	}
}

func TestParseBtrfsScrubReal(t *testing.T) {
	uuid, s, err := ParseBtrfsScrub(fixture(t, "btrfs-scrub-done-6.17.txt"))
	if err != nil || uuid != "f857c26f-273a-46ba-9ae9-65996efe0a50" || s.Status != "finished" || s.Started != "Wed Nov 25 11:02:35 2026" ||
		s.ErrorSummary != "no errors found" || s.NeverRun {
		t.Fatalf("%q %+v %v", uuid, s, err)
	}
	uuid, s, err = ParseBtrfsScrub(fixture(t, "btrfs-scrub-never-6.17.txt"))
	if err != nil || uuid != "ee94af32-cf43-4216-9d07-98a259d9b634" || !s.NeverRun || s.Status != "" {
		t.Fatalf("%q %+v %v", uuid, s, err)
	}
	if _, _, err := ParseBtrfsScrub([]byte("Status: finished\n")); err == nil {
		t.Error("report without UUID accepted")
	}
}

func TestParseBtrfsSubvolumes(t *testing.T) {
	s, trunc, err := ParseBtrfsSubvolumes(fixture(t, "btrfs-subvols-6.17.txt"))
	if err != nil || trunc || len(s) != 2 || s[1] != (BtrfsSubvol{ID: 257, TopLevel: 5, Path: "@data"}) {
		t.Fatalf("%+v %v %v", s, trunc, err)
	}
	var b strings.Builder
	for i := 0; i < MaxBtrfsSubvolumes+3; i++ {
		fmt.Fprintf(&b, "ID %d gen 9 top level 5 path snaps/%d with space\n", 256+i, i)
	}
	s, trunc, err = ParseBtrfsSubvolumes([]byte(b.String()))
	if err != nil || !trunc || len(s) != MaxBtrfsSubvolumes || s[0].Path != "snaps/0 with space" {
		t.Fatalf("len=%d trunc=%v err=%v", len(s), trunc, err)
	}
	if _, _, err := ParseBtrfsSubvolumes([]byte("nope\n")); err == nil {
		t.Error("garbage accepted")
	}
}

func TestBtrfsTemplatesAreReadOnly(t *testing.T) {
	for _, args := range [][]string{BtrfsUsageArgs("/m"), BtrfsDeviceStatsArgs("/m"), BtrfsScrubStatusArgs("/m"), BtrfsSubvolumeListArgs("/m")} {
		joined := strings.Join(args, " ")
		for _, w := range []string{"start", "cancel", "resume", "-z", "--reset", "create", "delete", "balance", "set"} {
			if strings.Contains(" "+joined+" ", " "+w+" ") {
				t.Fatalf("template %q contains %q", joined, w)
			}
		}
	}
}
