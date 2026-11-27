// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package partplan validates a proposed partition layout. DryRun never
// partitions, formats, or mounts, and it never calls the broker. Every
// report has Executes false and every step Executable false.
package partplan

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
)

const (
	mib          = 1 << 20
	maxDiskBytes = 256 << 40 // 256 TiB
	maxSlices    = 128
)

// Request is a proposal. It is not a device identity and not an authorization.
type Request struct {
	DiskBytes uint64  `json:"disk_bytes"`
	Parts     []Slice `json:"parts"`
}

// Slice is one proposed region. Filesystem is an allowlisted name, "none",
// or empty (treated as none).
type Slice struct {
	StartBytes uint64 `json:"start_bytes"`
	SizeBytes  uint64 `json:"size_bytes"`
	FSType     string `json:"filesystem"`
}

// Step describes one check outcome. Executable is always false.
type Step struct {
	Action      string `json:"action"`
	Detail      string `json:"detail"`
	Destructive bool   `json:"destructive"`
	Executable  bool   `json:"executable"`
}

// Report is the dry-run result. OK false is a rejected layout. Executes is
// always false: nothing in this package is applied.
type Report struct {
	OK       bool     `json:"ok"`
	Errors   []string `json:"errors"`
	Steps    []Step   `json:"steps"`
	Executes bool     `json:"executes"`
}

type placed struct {
	n     int
	slice Slice
	end   uint64
	ok    bool
	fsOK  bool
}

// DryRun checks overlap, 1 MiB alignment, bounds, and the filesystem
// allowlist. Gaps are recorded and are not errors. A rejected layout still
// returns steps, and none of them can run.
func DryRun(req Request) Report {
	rep := Report{Errors: []string{}, Steps: []Step{}}
	diskOK := req.DiskBytes > 0 && req.DiskBytes <= maxDiskBytes
	if !diskOK {
		rep.Errors = append(rep.Errors, "disk_bytes must be from 1 byte through 256 TiB")
	}
	n := len(req.Parts)
	if n < 1 || n > maxSlices {
		rep.Errors = append(rep.Errors, "parts must contain 1 to 128 slices")
		return finish(rep)
	}
	items := make([]placed, 0, n)
	for i, p := range req.Parts {
		it := placed{n: i + 1, slice: p}
		if p.SizeBytes == 0 {
			rep.Errors = append(rep.Errors, fmt.Sprintf("slice %d size_bytes must be greater than 0", it.n))
		}
		if p.StartBytes > math.MaxUint64-p.SizeBytes {
			rep.Errors = append(rep.Errors, fmt.Sprintf("slice %d start_bytes + size_bytes overflows", it.n))
		} else if p.SizeBytes > 0 {
			it.end = p.StartBytes + p.SizeBytes
			it.ok = true
			if diskOK && it.end > req.DiskBytes {
				rep.Errors = append(rep.Errors, fmt.Sprintf("slice %d extends past the disk", it.n))
			}
		}
		if p.StartBytes%mib != 0 || p.SizeBytes%mib != 0 {
			rep.Errors = append(rep.Errors, fmt.Sprintf("slice %d start_bytes and size_bytes must be aligned to 1 MiB", it.n))
		}
		fs, ok := canonicalFS(p.FSType)
		it.fsOK = ok
		if ok {
			it.slice.FSType = fs
		} else {
			rep.Errors = append(rep.Errors, fmt.Sprintf("slice %d filesystem is not allowed", it.n))
		}
		items = append(items, it)
	}
	slices.SortStableFunc(items, func(a, b placed) int { return cmp.Compare(a.Start(), b.Start()) })
	var cursor uint64
	var have bool
	for _, it := range items {
		if it.ok && it.Start() < cursor {
			rep.Errors = append(rep.Errors, fmt.Sprintf("slice %d overlaps another slice", it.n))
		}
		if it.ok && it.Start() > cursor {
			rep.Steps = append(rep.Steps, Step{
				Action: "gap",
				Detail: fmt.Sprintf("unallocated %d bytes before slice %d; a gap is allowed", it.Start()-cursor, it.n),
			})
		}
		if it.ok {
			cursor = it.end
			have = true
		}
		rep.Steps = append(rep.Steps, describe(it))
	}
	if diskOK && have && cursor < req.DiskBytes {
		rep.Steps = append(rep.Steps, Step{
			Action: "gap",
			Detail: fmt.Sprintf("unallocated %d bytes after the last slice; a gap is allowed", req.DiskBytes-cursor),
		})
	}
	return finish(rep)
}

func (it placed) Start() uint64 { return it.slice.StartBytes }

func canonicalFS(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none":
		return "none", true
	case "unknown":
		return "unknown", true
	case "ext4", "xfs", "f2fs", "btrfs", "exfat", "swap":
		return strings.ToLower(strings.TrimSpace(s)), true
	default:
		return "", false
	}
}

func describe(it placed) Step {
	if !it.fsOK {
		return Step{Action: "rejected", Detail: fmt.Sprintf("slice %d was not planned because its filesystem is not allowed", it.n)}
	}
	if it.slice.FSType == "unknown" {
		return Step{Action: "unknown", Detail: fmt.Sprintf("slice %d filesystem was not observed. This dry run does not partition or format.", it.n)}
	}
	if it.slice.FSType == "none" {
		return Step{Action: "leave", Detail: fmt.Sprintf("slice %d stays unformatted. This dry run does not partition.", it.n)}
	}
	detail := fmt.Sprintf("slice %d would be formatted as %s. No planner format runs. This dry run does not partition or format.", it.n, it.slice.FSType)
	if it.slice.FSType == "ext4" {
		detail = "slice " + fmt.Sprint(it.n) + " would be formatted as ext4. The only live format path is ext4.format on a whole by-id disk, not this slice. This dry run does not partition or format."
	}
	return Step{Action: "format", Detail: detail, Destructive: true}
}

func finish(rep Report) Report {
	for i := range rep.Steps {
		rep.Steps[i].Executable = false
	}
	rep.OK = len(rep.Errors) == 0
	rep.Executes = false
	return rep
}
