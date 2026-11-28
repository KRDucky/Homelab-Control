// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package partplan

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

const (
	// GPTLinux is the Linux filesystem GUID. It is not a format.
	GPTLinux = "0FC63DAF-8483-4772-8E79-3D69D8477DE4"
	// GPTSwap is the Linux swap GUID. It is not a swap format.
	GPTSwap = "0657FD6D-A4AB-43C4-84E5-0933C84B4F4F"
)

// Row is one planned GPT entry in logical sectors.
type Row struct {
	Start uint64
	Size  uint64
	Type  string
}

// GPTScript builds one partition script for a layout DryRun already accepts.
// It does not partition. sectorSize is the disk's logical block size and
// must be 512 or 4096. The first slice starts at or after 1 MiB, and every
// slice ends at least 1 MiB before the disk so the GPT backup header fits.
// unknown is refused: it is an unobserved type, not a proposal. none creates
// a Linux filesystem partition and does not format it.
func GPTScript(req Request, sectorSize uint64) (string, error) {
	rows, err := GPTRows(req, sectorSize)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("label: gpt\nunit: sectors\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "start=%d, size=%d, type=%s\n", r.Start, r.Size, r.Type)
	}
	script := b.String()
	if len(script) > 16<<10 {
		return "", fmt.Errorf("partition script is too long")
	}
	return script, nil
}

// GPTRows returns the entries GPTScript writes, in start order. The same rows
// are what a read-back of the written table must show (CheckSfdiskJSON).
func GPTRows(req Request, sectorSize uint64) ([]Row, error) {
	if sectorSize != 512 && sectorSize != 4096 {
		return nil, fmt.Errorf("logical block size must be 512 or 4096 bytes")
	}
	rep := DryRun(req)
	if !rep.OK {
		if len(rep.Errors) == 0 {
			return nil, fmt.Errorf("layout was rejected")
		}
		return nil, fmt.Errorf("%s", rep.Errors[0])
	}
	if req.DiskBytes%sectorSize != 0 {
		return nil, fmt.Errorf("disk_bytes is not a multiple of the logical block size")
	}
	if req.DiskBytes < 2*mib {
		return nil, fmt.Errorf("disk_bytes leaves no room for a GPT header and backup")
	}
	type item struct {
		n  int
		sl Slice
	}
	items := make([]item, len(req.Parts))
	for i, sl := range req.Parts {
		fs, ok := canonicalFS(sl.FSType)
		if !ok {
			return nil, fmt.Errorf("slice %d filesystem is not allowed", i+1)
		}
		sl.FSType = fs
		items[i] = item{n: i + 1, sl: sl}
	}
	slices.SortStableFunc(items, func(a, b item) int { return cmp.Compare(a.sl.StartBytes, b.sl.StartBytes) })
	rows := make([]Row, 0, len(items))
	for _, it := range items {
		n, sl := it.n, it.sl
		if sl.FSType == "unknown" {
			return nil, fmt.Errorf("slice %d filesystem was not observed", n)
		}
		if sl.StartBytes%sectorSize != 0 || sl.SizeBytes%sectorSize != 0 {
			return nil, fmt.Errorf("slice %d is not a multiple of the logical block size", n)
		}
		if sl.StartBytes < mib {
			return nil, fmt.Errorf("slice %d must start at or after 1 MiB", n)
		}
		if sl.StartBytes+sl.SizeBytes > req.DiskBytes-mib {
			return nil, fmt.Errorf("slice %d leaves no room for the GPT backup header", n)
		}
		guid := GPTLinux
		if sl.FSType == "swap" {
			guid = GPTSwap
		}
		rows = append(rows, Row{Start: sl.StartBytes / sectorSize, Size: sl.SizeBytes / sectorSize, Type: guid})
	}
	return rows, nil
}
