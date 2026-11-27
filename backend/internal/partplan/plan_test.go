// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package partplan

import (
	"math"
	"os"
	"strings"
	"testing"
)

func TestDryRunAcceptsAlignedGapAndRefusesExecution(t *testing.T) {
	rep := DryRun(Request{
		DiskBytes: 4 << 20,
		Parts: []Slice{{
			StartBytes: 1 << 20,
			SizeBytes:  1 << 20,
			FSType:     "ext4",
		}},
	})
	if !rep.OK || rep.Executes {
		t.Fatalf("report %+v", rep)
	}
	var sawExt4, sawGap bool
	for _, step := range rep.Steps {
		if step.Executable {
			t.Fatalf("executable step %+v", step)
		}
		if step.Action == "format" {
			sawExt4 = true
			if !step.Destructive || !strings.Contains(step.Detail, "ext4.format on a whole by-id disk, not this slice") {
				t.Fatalf("ext4 detail %+v", step)
			}
		}
		if step.Action == "gap" {
			sawGap = true
			if step.Destructive {
				t.Fatal("gap marked destructive")
			}
		}
	}
	if !sawExt4 || !sawGap {
		t.Fatalf("steps %+v", rep.Steps)
	}
}

// Slices that share a start keep request order, so the later one is the one
// reported (as in the M6 virtual-disk run: "slice 2 overlaps another slice").
func TestDryRunSameStartReportsTheLaterSlice(t *testing.T) {
	for _, parts := range [][]Slice{
		{{StartBytes: 1 << 20, SizeBytes: 1 << 20}, {StartBytes: 1 << 20, SizeBytes: 2 << 20}},
		{{StartBytes: 1 << 20, SizeBytes: 2 << 20}, {StartBytes: 1 << 20, SizeBytes: 1 << 20}},
	} {
		rep := DryRun(Request{DiskBytes: 8 << 20, Parts: parts})
		if rep.OK || len(rep.Errors) != 1 || rep.Errors[0] != "slice 2 overlaps another slice" {
			t.Fatalf("parts %+v: errors %q", parts, rep.Errors)
		}
	}
}

func TestDryRunRejectsOverlapAlignmentOverflowAndFilesystem(t *testing.T) {
	rep := DryRun(Request{
		DiskBytes: 4 << 20,
		Parts: []Slice{
			{StartBytes: 0, SizeBytes: 2 << 20, FSType: "none"},
			{StartBytes: 1 << 20, SizeBytes: 1 << 20, FSType: "xfs"},
			{StartBytes: 512, SizeBytes: 1 << 20, FSType: "ntfs"},
		},
	})
	if rep.OK || rep.Executes {
		t.Fatal("overlap accepted")
	}
	joined := strings.Join(rep.Errors, "\n")
	for _, want := range []string{"overlaps", "aligned to 1 MiB", "filesystem is not allowed"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	for _, step := range rep.Steps {
		if step.Executable {
			t.Fatal("rejected layout produced an executable step")
		}
	}
}

func TestDryRunRejectsPastEndAndOverflow(t *testing.T) {
	past := DryRun(Request{DiskBytes: 2 << 20, Parts: []Slice{{StartBytes: 1 << 20, SizeBytes: 2 << 20, FSType: "swap"}}})
	if past.OK || !strings.Contains(strings.Join(past.Errors, " "), "extends past") {
		t.Fatalf("%+v", past)
	}
	over := DryRun(Request{DiskBytes: 2 << 20, Parts: []Slice{{StartBytes: math.MaxUint64 - 100, SizeBytes: 1 << 20, FSType: "btrfs"}}})
	if over.OK || !strings.Contains(strings.Join(over.Errors, " "), "overflows") {
		t.Fatalf("%+v", over)
	}
	if over.Executes {
		t.Fatal("overflow executes")
	}
}

func TestDryRunAdjacentSlicesAndEmpty(t *testing.T) {
	rep := DryRun(Request{DiskBytes: 2 << 20, Parts: []Slice{
		{StartBytes: 0, SizeBytes: 1 << 20, FSType: ""},
		{StartBytes: 1 << 20, SizeBytes: 1 << 20, FSType: " NONE "},
	}})
	if !rep.OK || len(rep.Errors) != 0 {
		t.Fatalf("%+v", rep)
	}
	for _, step := range rep.Steps {
		if step.Action == "gap" {
			t.Fatalf("adjacent slices reported a gap: %+v", rep.Steps)
		}
		if step.Destructive || step.Executable || step.Action != "leave" {
			t.Fatalf("unformatted step %+v", step)
		}
	}
	empty := DryRun(Request{DiskBytes: 1 << 20})
	if empty.OK || empty.Executes {
		t.Fatal("empty plan accepted")
	}
	huge := DryRun(Request{DiskBytes: (256 << 40) + 1, Parts: []Slice{{SizeBytes: 1 << 20}}})
	if huge.OK {
		t.Fatal("disk over 256 TiB accepted")
	}
}

func TestDryRunUnknownIsNotUnformatted(t *testing.T) {
	rep := DryRun(Request{DiskBytes: 1 << 20, Parts: []Slice{{StartBytes: 0, SizeBytes: 1 << 20, FSType: "unknown"}}})
	if !rep.OK || rep.Executes {
		t.Fatalf("%+v", rep)
	}
	var saw bool
	for _, step := range rep.Steps {
		if step.Executable || step.Destructive {
			t.Fatalf("unknown step can run: %+v", step)
		}
		if strings.Contains(strings.ToLower(step.Detail), "unformatted") {
			t.Fatalf("unknown filesystem reported as unformatted: %+v", step)
		}
		if step.Action == "unknown" && strings.Contains(step.Detail, "not observed") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("steps %+v", rep.Steps)
	}
}

func TestDryRunSourceStaysClosed(t *testing.T) {
	b, err := os.ReadFile("plan.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{"mkfs", "parted", "wipefs", "os/exec", "internal/runner", "internal/broker"} {
		if strings.Contains(src, banned) {
			t.Fatalf("dry run names %q", banned)
		}
	}
}
