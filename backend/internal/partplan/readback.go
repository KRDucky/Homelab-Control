// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package partplan

import (
	"encoding/json"
	"fmt"
	"strings"
)

// sfdiskDump is the part of `sfdisk --json` output (util-linux) the read-back uses.
type sfdiskDump struct {
	PartitionTable *struct {
		Label      string `json:"label"`
		Partitions []struct {
			Start uint64 `json:"start"`
			Size  uint64 `json:"size"`
			Type  string `json:"type"`
		} `json:"partitions"`
	} `json:"partitiontable"`
}

// CheckSfdiskJSON compares `sfdisk --json` output for the written disk with
// the planned rows. It returns nil only when the table is GPT and holds
// exactly the planned entries, in start order, with the same start, size and
// type GUID (sectors, as sfdisk reports them).
func CheckSfdiskJSON(out []byte, want []Row) error {
	var d sfdiskDump
	if err := json.Unmarshal(out, &d); err != nil || d.PartitionTable == nil {
		return fmt.Errorf("sfdisk --json output could not be read")
	}
	if d.PartitionTable.Label != "gpt" {
		return fmt.Errorf("the disk has a %q label, not gpt", d.PartitionTable.Label)
	}
	got := d.PartitionTable.Partitions
	if len(got) != len(want) {
		return fmt.Errorf("the disk has %d partitions; the plan has %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Start != w.Start || g.Size != w.Size || !strings.EqualFold(g.Type, w.Type) {
			return fmt.Errorf("partition %d is start=%d size=%d type=%s; the plan has start=%d size=%d type=%s",
				i+1, g.Start, g.Size, g.Type, w.Start, w.Size, w.Type)
		}
	}
	return nil
}
