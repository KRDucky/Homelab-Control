// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package partplan

import (
	"os"
	"strings"
	"testing"
)

// testdata/sfdisk-json-gpt-2.json is real `sfdisk --json` output (util-linux
// 2.42.2) for the script of twoSlices on an 8 MiB image; device paths replaced.
func twoSlices() Request {
	return Request{DiskBytes: 8 << 20, Parts: []Slice{
		{StartBytes: 3 << 20, SizeBytes: 1 << 20, FSType: "swap"},
		{StartBytes: 1 << 20, SizeBytes: 2 << 20, FSType: "ext4"},
	}}
}

func TestCheckSfdiskJSONMatchesTheWrittenPlan(t *testing.T) {
	out, err := os.ReadFile("testdata/sfdisk-json-gpt-2.json")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := GPTRows(twoSlices(), 512)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckSfdiskJSON(out, rows); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSfdiskJSONRefusesADifferentTable(t *testing.T) {
	out, err := os.ReadFile("testdata/sfdisk-json-gpt-2.json")
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := GPTRows(twoSlices(), 512)
	cases := map[string]struct {
		out  string
		rows []Row
		want string
	}{
		"size":     {string(out), []Row{rows[0], {Start: rows[1].Start, Size: rows[1].Size + 8, Type: rows[1].Type}}, "partition 2"},
		"type":     {string(out), []Row{{Start: rows[0].Start, Size: rows[0].Size, Type: GPTSwap}, rows[1]}, "partition 1"},
		"count":    {string(out), rows[:1], "has 2 partitions; the plan has 1"},
		"label":    {strings.Replace(string(out), `"gpt"`, `"dos"`, 1), rows, "not gpt"},
		"no table": {`{}`, rows, "could not be read"},
		"not json": {"Disk /dev/sdb", rows, "could not be read"},
	}
	for name, c := range cases {
		err := CheckSfdiskJSON([]byte(c.out), c.rows)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want %q", name, err, c.want)
		}
	}
}
