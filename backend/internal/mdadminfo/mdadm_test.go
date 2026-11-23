// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mdadminfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic /proc/mdstat text in the kernel's format (labelled example data,
// not read from a host).
const sampleMdstat = `Personalities : [raid1] [raid6] [raid5] [raid4] [linear]
md0 : active raid1 sdb1[1] sda1[0]
      1046528 blocks super 1.2 [2/2] [UU]

md127 : active (auto-read-only) raid5 sdd[2] sdc[1](F) sde[3](S) sdf[0]
      2093056 blocks super 1.2 level 5, 512k chunk, algorithm 2 [3/2] [U_U]
      bitmap: 0/1 pages [0KB], 65536KB chunk

md1 : inactive sdg[0](S)
      1048576 blocks super external:imsm

unused devices: <none>
`

func TestParseMdstatReturnsPrintedFields(t *testing.T) {
	pers, arrays, truncated, err := ParseMdstat(sampleMdstat)
	if err != nil || truncated {
		t.Fatalf("err=%v truncated=%v", err, truncated)
	}
	if strings.Join(pers, ",") != "raid1,raid6,raid5,raid4,linear" {
		t.Fatalf("personalities %v", pers)
	}
	want := []struct {
		name, level, state string
		count              int
	}{
		{"md0", "raid1", "active", 2},
		{"md127", "raid5", "active (auto-read-only)", 4},
		{"md1", "", "inactive", 1},
	}
	if len(arrays) != len(want) {
		t.Fatalf("arrays %+v", arrays)
	}
	for i, w := range want {
		a := arrays[i]
		if a.Name != w.name || a.Level != w.level || a.State != w.state || a.MemberCount == nil || *a.MemberCount != w.count {
			t.Fatalf("array %d: %+v (count %v), want %+v", i, a, a.MemberCount, w)
		}
	}
}

func TestParseMdstatMissingTrailingFieldsDoNotPanic(t *testing.T) {
	_, arrays, _, err := ParseMdstat("Personalities :\nmd0 : active\nmd1 : active raid1\nmd2 : inactive\n")
	if err != nil || len(arrays) != 3 {
		t.Fatalf("arrays=%+v err=%v", arrays, err)
	}
	for _, a := range arrays {
		if a.MemberCount != nil {
			t.Fatalf("no components listed, count must be null: %+v", a)
		}
	}
	if arrays[1].Level != "raid1" || arrays[0].Level != "" || arrays[2].Level != "" {
		t.Fatalf("levels %+v", arrays)
	}
	for _, bad := range []string{"md0 :", "md0 :\n", "md0", "md0 : active (", "md0 : (read-only)"} {
		if _, _, _, err := ParseMdstat(bad); err == nil {
			t.Fatalf("%q must fail closed", bad)
		}
	}
}

func TestParseMdstatFailsClosedOnBadInput(t *testing.T) {
	for name, in := range map[string]string{
		"unknown state":      "md0 : degraded raid1 sda[0]\n",
		"bad name":           "md/0 : active raid1 sda[0]\n",
		"no separator":       "md0 active raid1 sda[0]\n",
		"bad component":      "md0 : active raid1 sda sdb[1]\n",
		"bad state suffix":   "md0 : active (writable) raid1 sda[0]\n",
		"duplicate array":    "md0 : active raid1 sda[0]\nmd0 : active raid1 sdb[0]\n",
		"bad personality":    "Personalities : raid1\n",
		"control byte":       "md0 : active raid1 sda[0]\x1b\n",
		"non-ascii":          "md0 : active raid1 sdä[0]\n",
		"stray top-level":    "garbage line\n",
		"oversized line":     "md0 : active raid1 " + strings.Repeat("a", mdstatMaxLine) + "\n",
		"oversized file":     strings.Repeat("\n", mdstatMaxBytes+1),
		"uppercase level":    "md0 : active RAID1 sda[0]\n",
		"component overflow": "md0 : active raid1 sda[1234567]\n",
	} {
		if _, _, _, err := ParseMdstat(in); err == nil {
			t.Errorf("%s: must fail closed", name)
		}
	}
}

func TestParseMdstatCapsArrays(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxArrays+5; i++ {
		b.WriteString("md9" + itoa(i) + " : active raid1 sda[0]\n")
	}
	_, arrays, truncated, err := ParseMdstat(b.String())
	if err != nil || !truncated || len(arrays) != maxArrays {
		t.Fatalf("len=%d truncated=%v err=%v", len(arrays), truncated, err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func fixtureRoot(t *testing.T, mdstat *string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if mdstat != nil {
		if err := os.WriteFile(filepath.Join(root, "proc", "mdstat"), []byte(*mdstat), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestProbeMdstatFailsClosed(t *testing.T) {
	missing := ProbeMdstat(fixtureRoot(t, nil))
	if missing.Present || missing.Reason != reasonMissing || missing.Arrays == nil || len(missing.Arrays) != 0 {
		t.Fatalf("missing: %+v", missing)
	}
	bad := "md0 : bogus\n"
	if inv := ProbeMdstat(fixtureRoot(t, &bad)); inv.Present || inv.Reason != reasonUnread || len(inv.Arrays) != 0 {
		t.Fatalf("malformed: %+v", inv)
	}
	// Keep a valid array at the front: exceeding the byte limit must fail even
	// when the content would otherwise produce a legitimate inventory.
	big := sampleMdstat + strings.Repeat("\n", mdstatMaxBytes+1)
	if inv := ProbeMdstat(fixtureRoot(t, &big)); inv.Present || inv.Reason != reasonUnread {
		t.Fatalf("oversized: %+v", inv)
	}
	ok := sampleMdstat
	if inv := ProbeMdstat(fixtureRoot(t, &ok)); !inv.Present || inv.Reason != "" || len(inv.Arrays) != 3 {
		t.Fatalf("sample: %+v", inv)
	}
}

// An mdstat that is a symlink out of the collector root is refused by os.Root.
func TestProbeMdstatRefusesEscapingSymlink(t *testing.T) {
	root := fixtureRoot(t, nil)
	outside := filepath.Join(t.TempDir(), "mdstat")
	if err := os.WriteFile(outside, []byte(sampleMdstat), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "proc", "mdstat")); err != nil {
		t.Fatal(err)
	}
	if inv := ProbeMdstat(root); inv.Present || inv.Reason != reasonUnread || len(inv.Arrays) != 0 {
		t.Fatalf("escaping symlink was read: %+v", inv)
	}
}

func TestProbeToolingPackageByFamily(t *testing.T) {
	none := func(string) bool { return false }
	for _, fam := range []string{"debian", "fedora", "suse", "arch"} {
		tool := probeTooling(fam, none)
		if len(tool.Required) != 1 || tool.Required[0].ID != "mdadm" || tool.Required[0].Package != "mdadm" || tool.Required[0].Installed {
			t.Fatalf("%s: %+v", fam, tool)
		}
	}
	if tool := probeTooling("unknown", none); tool.Required[0].Package != "" || tool.Optional == nil {
		t.Fatalf("unknown family must name no package: %+v", tool)
	}
}

func TestProbeToolingStatsSbinFirst(t *testing.T) {
	var seen []string
	stat := func(p string) bool { seen = append(seen, p); return p == "/usr/bin/mdadm" }
	tool := probeTooling("debian", stat)
	if strings.Join(seen, ",") != "/usr/sbin/mdadm,/usr/bin/mdadm" || !tool.Required[0].Installed || tool.Required[0].Binary != "/usr/bin/mdadm" {
		t.Fatalf("seen=%v tool=%+v", seen, tool)
	}
}

func TestProbeCapability(t *testing.T) {
	ok := sampleMdstat
	root := fixtureRoot(t, &ok)
	yes := func(p string) bool { return p == "/usr/sbin/mdadm" }
	no := func(string) bool { return false }
	if a, r := probeCapability("debian", yes, root); !a || r != "" {
		t.Fatalf("available: %v %q", a, r)
	}
	if a, r := probeCapability("debian", no, root); a || !strings.Contains(r, "mdadm") {
		t.Fatalf("no binary: %v %q", a, r)
	}
	if a, r := probeCapability("debian", yes, fixtureRoot(t, nil)); a || r != reasonMissing {
		t.Fatalf("no mdstat: %v %q", a, r)
	}
}

func TestCollect(t *testing.T) {
	ok := sampleMdstat
	snap, err := Collect(context.Background(), fixtureRoot(t, &ok), "debian")
	if err != nil || !snap.MD.Present || len(snap.MD.Arrays) != 3 || snap.Tooling.Family != "debian" {
		t.Fatalf("snap=%+v err=%v", snap, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Collect(ctx, fixtureRoot(t, &ok), "debian"); err == nil {
		t.Fatal("cancelled context must return an error")
	}
}
