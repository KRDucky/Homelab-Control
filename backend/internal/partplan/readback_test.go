// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package partplan

import (
	"encoding/json"
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

// changeFixtureLabel edits the JSON field rather than relying on whitespace-
// sensitive text replacement; it preserves all other fixture fields.
func changeFixtureLabel(t *testing.T, original []byte, label string) string {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(original, &doc); err != nil {
		t.Fatal(err)
	}
	var table map[string]json.RawMessage
	if err := json.Unmarshal(doc["partitiontable"], &table); err != nil {
		t.Fatal(err)
	}
	encodedLabel, err := json.Marshal(label)
	if err != nil {
		t.Fatal(err)
	}
	table["label"] = encodedLabel
	encodedTable, err := json.Marshal(table)
	if err != nil {
		t.Fatal(err)
	}
	doc["partitiontable"] = encodedTable
	changed, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(changed)
}

func TestSfdiskLabelMismatchFixtureIsActuallyDOS(t *testing.T) {
	original, err := os.ReadFile("testdata/sfdisk-json-gpt-2.json")
	if err != nil {
		t.Fatal(err)
	}
	changed := changeFixtureLabel(t, original, "dos")
	if changed == string(original) {
		t.Fatal("the label mismatch fixture was not changed")
	}
	var got sfdiskDump
	if err := json.Unmarshal([]byte(changed), &got); err != nil {
		t.Fatal(err)
	}
	if got.PartitionTable == nil || got.PartitionTable.Label != "dos" {
		t.Fatalf("changed table label = %v; want dos", got.PartitionTable)
	}
	var source sfdiskDump
	if err := json.Unmarshal(original, &source); err != nil {
		t.Fatal(err)
	}
	if source.PartitionTable == nil || source.PartitionTable.Label != "gpt" {
		t.Fatal("source fixture must remain GPT")
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
		"label":    {changeFixtureLabel(t, out, "dos"), rows, "not gpt"},
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
