// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import "testing"

func TestHonestyWarningsParityFileOnBranch(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/parity"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/parity/snapraid.parity"}, Data: []string{"/mnt/disk1"}}
	got := HonestyWarnings(pools, snap)
	if len(got) != 1 || got[0].Code != WarnParityInPool || got[0].Branch != "/mnt/parity" || got[0].Other != "/mnt/parity/snapraid.parity" {
		t.Fatalf("got %+v", got)
	}
}

func TestHonestyWarningsParityEqualsBranch(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/parity1"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/parity1"}}
	got := HonestyWarnings(pools, snap)
	if len(got) != 1 || got[0].Code != WarnParityInPool {
		t.Fatalf("got %+v", got)
	}
}

func TestHonestyWarningsParityOnFusedMount(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/media/snapraid.parity"}}
	got := HonestyWarnings(pools, snap)
	if len(got) != 1 || got[0].Code != WarnParityInPool || got[0].Branch != "/mnt/media" {
		t.Fatalf("got %+v", got)
	}
}

func TestHonestyWarningsDataBranchIsNotAWarning(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/disk2"}}}
	snap := SnapRAID{Present: true, Data: []string{"/mnt/disk1", "/mnt/disk2"}, Parity: []string{"/mnt/parity1"}}
	got := HonestyWarnings(pools, snap)
	if len(got) != 0 {
		t.Fatalf("data disks as branches must not warn: %+v", got)
	}
}

func TestHonestyWarningsFalsePrefix(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/parity"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/parity2/snapraid.parity"}}
	got := HonestyWarnings(pools, snap)
	if len(got) != 0 {
		t.Fatalf("/mnt/parity must not match /mnt/parity2: %+v", got)
	}
}

func TestHonestyWarningsDuplicateBranch(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/disk1", "/mnt/disk2"}}}
	got := HonestyWarnings(pools, SnapRAID{})
	if len(got) != 1 || got[0].Code != WarnDuplicateBranch || got[0].Branch != "/mnt/disk1" {
		t.Fatalf("got %+v", got)
	}
}

func TestHonestyWarningsEmptyWhenNoSnapRAID(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1"}}}
	got := HonestyWarnings(pools, SnapRAID{Present: false, Parity: []string{"/mnt/disk1/x"}})
	if len(got) != 0 {
		t.Fatalf("absent snapraid must not use listed parity: %+v", got)
	}
}

func TestPathCovers(t *testing.T) {
	if !pathCovers("/mnt/parity", "/mnt/parity") || !pathCovers("/mnt/parity/", "/mnt/parity/snapraid.parity") {
		t.Fatal("expected covers")
	}
	if pathCovers("/mnt/parity", "/mnt/parity2") || pathCovers("", "/mnt/x") || pathCovers("/mnt/disk1", "/mnt/disk10") {
		t.Fatal("false prefix")
	}
}
