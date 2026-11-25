// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagetools

import (
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Real OpenZFS 2.4.1 output (Ubuntu 26.04 guest): a degraded raidz1 pool with
// log and cache devices and one offline disk, and a healthy mirror.
func TestParseZFSReal(t *testing.T) {
	rep, err := ParseZFS(fixture(t, "zpool-list-2.4.txt"), fixture(t, "zpool-status-2.4.txt"), fixture(t, "zfs-list-2.4.txt"))
	if err != nil || rep.Truncated || len(rep.Pools) != 2 || len(rep.Datasets) != 3 {
		t.Fatalf("%+v %v", rep, err)
	}
	lab := rep.Pools[0]
	if lab.Name != "lab" || lab.Health != "DEGRADED" || lab.State != "DEGRADED" || *lab.Size != 1476395008 || *lab.FragPct != 1 {
		t.Fatalf("lab: %+v", lab)
	}
	if !strings.HasPrefix(lab.Status, "One or more devices has been taken offline") || !strings.Contains(lab.Status, "degraded state.") {
		t.Fatalf("multi-line status: %q", lab.Status)
	}
	if !strings.Contains(lab.Action, "'zpool replace'") || !strings.HasPrefix(lab.Scan, "scrub repaired 0B") || lab.Errors != "No known data errors" {
		t.Fatalf("report lines: %+v", lab)
	}
	type row struct {
		name  string
		depth int
		group bool
		state string
	}
	want := []row{{"lab", 0, false, "DEGRADED"}, {"raidz1-0", 1, false, "DEGRADED"}, {"/var/tmp/hcz1", 2, false, "ONLINE"},
		{"/var/tmp/hcz2", 2, false, "OFFLINE"}, {"/var/tmp/hcz3", 2, false, "ONLINE"}, {"logs", 0, true, ""},
		{"/var/tmp/hcl1", 1, false, "ONLINE"}, {"cache", 0, true, ""}, {"/var/tmp/hcc1", 1, false, "ONLINE"}}
	if len(lab.Vdevs) != len(want) {
		t.Fatalf("vdevs: %+v", lab.Vdevs)
	}
	for i, w := range want {
		v := lab.Vdevs[i]
		if v.Name != w.name || v.Depth != w.depth || v.Group != w.group || v.State != w.state {
			t.Fatalf("vdev %d: %+v want %+v", i, v, w)
		}
		if !w.group && (v.Read == nil || *v.Read != 0 || v.Cksum == nil) {
			t.Fatalf("vdev %d counters: %+v", i, v)
		}
	}
	tank := rep.Pools[1]
	if tank.Health != "ONLINE" || len(tank.Vdevs) != 4 || tank.Vdevs[1].Name != "mirror-0" || tank.Vdevs[2].Depth != 2 || tank.Scan != "" {
		t.Fatalf("tank: %+v", tank)
	}
	d := rep.Datasets[1]
	if d.Name != "tank/documents" || d.Compression != "zstd" || d.Mountpoint != "/tank/documents" || d.Type != "filesystem" || *d.Used != 98304 {
		t.Fatalf("dataset: %+v", d)
	}
}

func TestParseZFSCountersAndNotes(t *testing.T) {
	status := "  pool: p\n state: ONLINE\n  scan: resilver in progress since Wed\n\t1.2G scanned at 100M/s\nconfig:\n\n" +
		"\tNAME        STATE     READ WRITE CKSUM\n\tp           ONLINE       0     0     0\n" +
		"\t  sda       ONLINE       3     0    12  (resilvering)\n\tspares\n\t  sdb       AVAIL\n\nerrors: 2 data errors, use '-v' for a list\n"
	rep, err := ParseZFS([]byte("p\t100\t10\t90\t0\t10\tONLINE\n"), []byte(status), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := rep.Pools[0]
	if p.Scan != "resilver in progress since Wed 1.2G scanned at 100M/s" || !strings.HasPrefix(p.Errors, "2 data errors") {
		t.Fatalf("%+v", p)
	}
	sda, spare := p.Vdevs[1], p.Vdevs[3]
	if *sda.Read != 3 || *sda.Cksum != 12 || sda.Note != "(resilvering)" || spare.State != "AVAIL" || spare.Read != nil || !p.Vdevs[2].Group {
		t.Fatalf("%+v", p.Vdevs)
	}
}

func TestParseZFSNoPoolsAndRejects(t *testing.T) {
	rep, err := ParseZFS(nil, []byte("no pools available\n"), nil)
	if err != nil || len(rep.Pools) != 0 || len(rep.Datasets) != 0 {
		t.Fatalf("%+v %v", rep, err)
	}
	if _, err := ParseZFS([]byte("tank only-two-fields\n"), nil, nil); err == nil {
		t.Error("malformed zpool list accepted")
	}
	if _, err := ParseZFS(nil, nil, []byte("a\tb\n")); err == nil {
		t.Error("malformed zfs list accepted")
	}
	if _, err := ParseZFS([]byte(strings.Repeat("x", MaxZFSBytes+1)), nil, nil); err == nil {
		t.Error("oversized output accepted")
	}
}

func TestZFSTemplatesAreReadOnly(t *testing.T) {
	for _, args := range [][]string{ZpoolListArgs(), ZpoolStatusArgs(), ZfsListArgs()} {
		if args[0] != "list" && args[0] != "status" {
			t.Fatalf("not a read-only subcommand: %v", args)
		}
	}
}
