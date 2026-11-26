// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagetools

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseTune2fsCleanFilesystem(t *testing.T) {
	var fs Ext4Filesystem
	if err := ParseTune2fs(&fs, readFixture(t, "tune2fs-1.47.4.txt")); err != nil {
		t.Fatal(err)
	}
	if fs.VolumeName != "data" || fs.UUID != "b678277d-db47-48ef-b3c4-c53e78aea603" || fs.State != "clean" || fs.ErrorsBehavior != "Continue" {
		t.Fatalf("identity/state: %+v", fs)
	}
	if *fs.BlockSize != 1024 || *fs.BlockCount != 65536 || *fs.FreeBlocks != 55991 || *fs.ReservedBlocks != 3276 ||
		*fs.InodeCount != 16384 || *fs.FreeInodes != 16372 || *fs.MountCount != 0 || *fs.MaxMountCount != -1 {
		t.Fatalf("counts: %+v", fs)
	}
	if !slices.Contains(fs.Features, "metadata_csum") || !slices.Contains(fs.Features, "has_journal") || len(fs.Features) != 16 {
		t.Fatalf("features: %v", fs.Features)
	}
	if fs.LastChecked != "Wed Nov 25 23:59:30 2026" || fs.CheckInterval != "0 (<none>)" || fs.LastMountTime != "n/a" || fs.LifetimeWrites != "315 kB" {
		t.Fatalf("times: %+v", fs)
	}
	if fs.ErrorCount != nil || fs.FirstErrorTime != "" || fs.LastErrorFunc != "" {
		t.Fatalf("a clean filesystem has no error record: %+v", fs)
	}
}

func TestParseTune2fsRecordedErrors(t *testing.T) {
	var fs Ext4Filesystem
	if err := ParseTune2fs(&fs, readFixture(t, "tune2fs-1.47.4-errors.txt")); err != nil {
		t.Fatal(err)
	}
	if fs.State != "not clean with errors" || *fs.ErrorCount != 3 || *fs.MountCount != 21 || *fs.MaxMountCount != 20 {
		t.Fatalf("state: %+v", fs)
	}
	if fs.FirstErrorTime != "Fri Nov 20 12:00:00 2026" || fs.FirstErrorFunc != "ext4_lookup" ||
		fs.LastErrorTime != "Tue Nov 24 08:00:00 2026" || fs.LastErrorFunc != "ext4_iget" {
		t.Fatalf("error record: %+v", fs)
	}
}

func TestParseTune2fsRejectsOtherOutput(t *testing.T) {
	for name, in := range map[string]string{
		"empty":     "",
		"usage":     "Usage: tune2fs [-c max_mounts_count]\n",
		"bad magic": "Filesystem magic number:  0x1234\nBlock count: 5\n",
	} {
		var fs Ext4Filesystem
		if err := ParseTune2fs(&fs, []byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	var fs Ext4Filesystem
	if err := ParseTune2fs(&fs, []byte(strings.Repeat("x", MaxTune2fsBytes+1))); err == nil {
		t.Error("oversized output accepted")
	}
}

func TestParseXFSInfo(t *testing.T) {
	var fs XFSFilesystem
	if err := ParseXFSInfo(&fs, readFixture(t, "xfs_info-6.19.0.txt")); err != nil {
		t.Fatal(err)
	}
	if *fs.BlockSize != 4096 || *fs.Blocks != 81920 || *fs.AGCount != 4 || *fs.AGSize != 20480 || *fs.InodeSize != 512 ||
		*fs.SectorSize != 512 || *fs.Sunit != 0 || *fs.Swidth != 0 || *fs.LogBlocks != 16384 {
		t.Fatalf("geometry: %+v", fs)
	}
	if fs.Log != "internal log" || fs.Realtime != "none" {
		t.Fatalf("log/realtime: %q %q", fs.Log, fs.Realtime)
	}
	want := []string{"projid32bit", "crc", "finobt", "sparse", "rmapbt", "reflink", "bigtime", "inobtcount", "nrext64", "exchange", "ftype", "parent", "lazy-count"}
	if !slices.Equal(fs.Features, want) {
		t.Fatalf("features: %v", fs.Features)
	}
}

// A mountpoint that looks like name=value pairs must not set features.
func TestParseXFSInfoIgnoresPairsInThePath(t *testing.T) {
	in := strings.Replace(string(readFixture(t, "xfs_info-6.19.0.txt")), "/srv/x   ", "/a crc=1", 1)
	in = strings.Replace(in, "crc=1        finobt", "crc=0        finobt", 1)
	var fs XFSFilesystem
	if err := ParseXFSInfo(&fs, []byte(in)); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(fs.Features, "crc") || *fs.InodeSize != 512 {
		t.Fatalf("crc taken from the path: %v", fs.Features)
	}
}

func TestParseXFSInfoRejectsOtherOutput(t *testing.T) {
	for _, in := range []string{"", "xfs_spaceman: /srv is not on an XFS filesystem\n", "meta-data=/x isize=512\n"} {
		var fs XFSFilesystem
		if err := ParseXFSInfo(&fs, []byte(in)); err == nil {
			t.Errorf("accepted %q", in)
		}
	}
}
