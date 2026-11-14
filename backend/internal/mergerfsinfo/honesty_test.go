// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func mustWarnings(t *testing.T, pools []Pool, snap SnapRAID) []Warning {
	t.Helper()
	// Existing fixtures omit content. Two files outside the pool keep those
	// cases on the parity and duplicate assertions.
	if snap.Present && snap.Content == nil {
		snap.Content = []string{"/var/snapraid/content.1", "/var/snapraid/content.2"}
	}
	got, trunc := HonestyWarnings(context.Background(), pools, snap)
	if trunc {
		t.Fatalf("unexpected truncation: %+v", got)
	}
	return got
}

func TestHonestyWarningsParityFileOnBranch(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/parity"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/parity/snapraid.parity"}, Data: []string{"/mnt/disk1"}}
	got := mustWarnings(t, pools, snap)
	if len(got) != 1 || got[0].Code != WarnParityInPool || got[0].Branch != "/mnt/parity" || got[0].Other != "/mnt/parity/snapraid.parity" {
		t.Fatalf("got %+v", got)
	}
}

func TestHonestyWarningsParityEqualsBranch(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/parity1"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/parity1"}}
	got := mustWarnings(t, pools, snap)
	if len(got) != 1 || got[0].Code != WarnParityInPool {
		t.Fatalf("got %+v", got)
	}
}

func TestHonestyWarningsParityOnFusedMount(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/media/snapraid.parity"}}
	got := mustWarnings(t, pools, snap)
	if len(got) != 1 || got[0].Code != WarnParityInPool || got[0].Branch != "/mnt/media" {
		t.Fatalf("got %+v", got)
	}
}

func TestHonestyWarningsParityEqualsMountpoint(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/media"}}
	got := mustWarnings(t, pools, snap)
	if len(got) != 1 || got[0].Code != WarnParityInPool || got[0].Other != "/mnt/media" {
		t.Fatalf("got %+v", got)
	}
}

func TestHonestyWarningsDataBranchIsNotAWarning(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/disk2"}}}
	snap := SnapRAID{Present: true, Data: []string{"/mnt/disk1", "/mnt/disk2"}, Parity: []string{"/mnt/parity1"}}
	got := mustWarnings(t, pools, snap)
	if len(got) != 0 {
		t.Fatalf("data disks as branches must not warn: %+v", got)
	}
}

func TestHonestyWarningsFalsePrefix(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/parity"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/parity2/snapraid.parity"}}
	got := mustWarnings(t, pools, snap)
	if len(got) != 0 {
		t.Fatalf("/mnt/parity must not match /mnt/parity2: %+v", got)
	}
}

func TestHonestyWarningsDuplicateBranch(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/disk1", "/mnt/disk2"}}}
	got := mustWarnings(t, pools, SnapRAID{})
	if len(got) != 1 || got[0].Code != WarnDuplicateBranch || got[0].Branch != "/mnt/disk1" || got[0].Other != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestHonestyWarningsEmptyWhenNoSnapRAID(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1"}}}
	got := mustWarnings(t, pools, SnapRAID{Present: false, Parity: []string{"/mnt/disk1/x"}})
	if len(got) != 0 {
		t.Fatalf("absent snapraid must not use listed parity: %+v", got)
	}
}

func TestHonestyWarningsBranchModeSuffix(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1=RW", "/mnt/disk2=NC"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/disk1/snapraid.parity"}}
	got := mustWarnings(t, pools, snap)
	if len(got) != 1 || got[0].Code != WarnParityInPool || got[0].Branch != "/mnt/disk1" {
		t.Fatalf("=RW branch must still cover parity: %+v", got)
	}
}

func TestHonestyWarningsTrailingSlashParity(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/parity1/"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/parity1"}}
	got := mustWarnings(t, pools, snap)
	if len(got) != 1 || got[0].Code != WarnParityInPool {
		t.Fatalf("trailing slash must still cover: %+v", got)
	}
}

func TestHonestyWarningsModeSuffixIsDuplicate(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/disk1=RW"}}}
	got := mustWarnings(t, pools, SnapRAID{})
	if len(got) != 1 || got[0].Code != WarnDuplicateBranch {
		t.Fatalf("=RW spelling of the same branch is a duplicate: %+v", got)
	}
}

func TestHonestyWarningsParityOnTwoBranches(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/a", "/mnt/b"}}}
	snap := SnapRAID{Present: true, Parity: []string{"/mnt/a/p", "/mnt/b/p"}}
	got := mustWarnings(t, pools, snap)
	if len(got) != 2 {
		t.Fatalf("one warning per covered parity: %+v", got)
	}
}

func TestHonestyWarningsCapAndTruncation(t *testing.T) {
	branches := make([]string, 0, 20000)
	for i := 0; i < 10000; i++ {
		p := fmt.Sprintf("/mnt/d%d", i)
		branches = append(branches, p, p)
	}
	got, trunc := HonestyWarnings(context.Background(), []Pool{{Mountpoint: "/mnt/media", Branches: branches}}, SnapRAID{})
	if !trunc || len(got) != maxWarningsPerPool {
		t.Fatalf("20k-branch duplicates must cap per pool: len=%d trunc=%v", len(got), trunc)
	}
	for _, w := range got {
		if w.Code != WarnDuplicateBranch {
			t.Fatalf("got %+v", w)
		}
	}
}

func TestHonestyWarningsTotalCapAcrossPools(t *testing.T) {
	pools := make([]Pool, 0, 12)
	for i := 0; i < 12; i++ {
		var branches []string
		for j := 0; j < maxWarningsPerPool+1; j++ {
			p := fmt.Sprintf("/mnt/p%d/d%d", i, j)
			branches = append(branches, p, p)
		}
		pools = append(pools, Pool{Mountpoint: fmt.Sprintf("/mnt/pool%d", i), Branches: branches})
	}
	got, trunc := HonestyWarnings(context.Background(), pools, SnapRAID{})
	if !trunc || len(got) != maxWarningsTotal {
		t.Fatalf("total cap: len=%d trunc=%v", len(got), trunc)
	}
}

func TestHonestyWarningsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, trunc := HonestyWarnings(ctx, []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/disk1"}}}, SnapRAID{})
	if !trunc {
		t.Fatalf("cancelled ctx must truncate: %+v", got)
	}
}

func TestHonestyWarningsClipsFields(t *testing.T) {
	long := "/" + strings.Repeat("p", warningFieldMax+8)
	got := mustWarnings(t, []Pool{{Mountpoint: long, Branches: []string{long, long}}}, SnapRAID{})
	if len(got) != 1 || len(got[0].Pool) != warningFieldMax || len(got[0].Branch) != warningFieldMax {
		t.Fatalf("fields must clip to %d: %+v", warningFieldMax, got)
	}
}

func TestHonestyWarningsContentOnDataBranchIsSilent(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/disk2"}}}
	snap := SnapRAID{
		Present: true,
		Data:    []string{"/mnt/disk1", "/mnt/disk2"},
		Parity:  []string{"/mnt/parity1"},
		Content: []string{"/mnt/disk1/snapraid.content", "/mnt/disk2/snapraid.content"},
	}
	got := mustWarnings(t, pools, snap)
	if len(got) != 0 {
		t.Fatalf("content on data branches must not warn: %+v", got)
	}
}

func TestHonestyWarningsContentOnPoolAndParity(t *testing.T) {
	pools := []Pool{{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1"}}}
	snap := SnapRAID{
		Present: true,
		Parity:  []string{"/mnt/parity1"},
		Content: []string{"/mnt/media/content", "/mnt/parity1/content"},
	}
	got := mustWarnings(t, pools, snap)
	codes := map[string]bool{}
	for _, w := range got {
		codes[w.Code] = true
	}
	if !codes[WarnContentOnPool] || !codes[WarnContentOnParity] || codes[WarnContentNotRedundant] {
		t.Fatalf("got %+v", got)
	}
}

func TestHonestyWarningsSingleContentFile(t *testing.T) {
	snap := SnapRAID{Present: true, ConfigPath: "/etc/snapraid.conf", Content: []string{"/mnt/disk1/snapraid.content"}}
	got := mustWarnings(t, nil, snap)
	if len(got) != 1 || got[0].Code != WarnContentNotRedundant || got[0].Branch != "/mnt/disk1/snapraid.content" {
		t.Fatalf("got %+v", got)
	}
}

func TestHonestyWarningsNoContentFiles(t *testing.T) {
	snap := SnapRAID{Present: true, ConfigPath: "/etc/snapraid.conf", Content: []string{}}
	got := mustWarnings(t, nil, snap)
	if len(got) != 1 || got[0].Code != WarnContentNotRedundant || got[0].Branch != "/etc/snapraid.conf" {
		t.Fatalf("got %+v", got)
	}
}

func TestPathCovers(t *testing.T) {
	if !pathCovers("/mnt/parity", "/mnt/parity") || !pathCovers("/mnt/parity/", "/mnt/parity/snapraid.parity") {
		t.Fatal("expected covers")
	}
	if pathCovers("/mnt/parity", "/mnt/parity2") || pathCovers("", "/mnt/x") || pathCovers("/mnt/disk1", "/mnt/disk10") {
		t.Fatal("false prefix")
	}
	if pathCovers("/", "/mnt/media") || pathCovers("/", "/foo") {
		t.Fatal("root must not cover every path")
	}
	if !pathCovers("/", "/") {
		t.Fatal("root covers root")
	}
}

func TestBranchKey(t *testing.T) {
	if branchKey("/mnt/disk1=RW") != "/mnt/disk1" || branchKey("/mnt/disk1/=NC") != "/mnt/disk1" {
		t.Fatal("mode suffix")
	}
	if branchKey("/mnt/disk1/") != "/mnt/disk1" {
		t.Fatal("slash")
	}
}
