// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagetools

import (
	"fmt"
	"regexp"
	"strings"
)

// XFS: for a mounted filesystem xfs_info(8) is a shell script that runs
// `xfs_spaceman -p xfs_info -c info <mountpoint>`; the helper runs that
// command itself (no shell) and this parser reads the geometry report, the
// same text on every xfsprogs since 4.20. Free space is on the host overview
// (statfs), as for every local filesystem.

// Bounds on XFS output.
const (
	MaxXFSBytes       = 64 << 10 // per command
	MaxXFSFilesystems = 32
)

// XFSInfoArgs is the fixed template; mountpoint comes from the kernel's
// mount table.
func XFSInfoArgs(mountpoint string) []string {
	return []string{"-p", "xfs_info", "-c", "info", mountpoint}
}

// XFSReport lists the mounted XFS filesystems xfs_spaceman described.
type XFSReport struct {
	Filesystems []XFSFilesystem `json:"filesystems"`
	Skipped     []SkippedMount  `json:"skipped,omitempty"`
	Truncated   bool            `json:"truncated,omitempty"`
}

// XFSFilesystem is one filesystem's geometry. Features lists
// the on-disk features xfs_info reports as enabled (crc, reflink, ...).
type XFSFilesystem struct {
	Mountpoint string   `json:"mountpoint"`
	Mounts     []string `json:"mounts"`
	BlockSize  *uint64  `json:"block_size,omitempty"`
	Blocks     *uint64  `json:"blocks,omitempty"`
	AGCount    *uint64  `json:"ag_count,omitempty"`
	AGSize     *uint64  `json:"ag_size_blocks,omitempty"`
	InodeSize  *uint64  `json:"inode_size,omitempty"`
	SectorSize *uint64  `json:"sector_size,omitempty"`
	Sunit      *uint64  `json:"sunit_blocks,omitempty"`
	Swidth     *uint64  `json:"swidth_blocks,omitempty"`
	Features   []string `json:"features"`
	// Log is "internal log" or the external log device; Realtime is "none"
	// or the realtime device.
	Log       string  `json:"log,omitempty"`
	LogBlocks *uint64 `json:"log_blocks,omitempty"`
	Realtime  string  `json:"realtime,omitempty"`
	Error     string  `json:"error,omitempty"`
}

var (
	xfsSection = regexp.MustCompile(`^([a-z-]+)\s*=(.*)$`)
	xfsPair    = regexp.MustCompile(`([a-z0-9-]+)=(\S+?),?(?:\s|$)`)
)

// xfsFeatures are the on-disk feature flags xfs_info prints as name=0/1.
var xfsFeatures = map[string]bool{"crc": true, "finobt": true, "sparse": true, "rmapbt": true, "reflink": true,
	"bigtime": true, "inobtcount": true, "nrext64": true, "exchange": true, "metadir": true, "parent": true,
	"ftype": true, "projid32bit": true, "lazy-count": true}

// ParseXFSInfo reads one geometry report into fs.
func ParseXFSInfo(fs *XFSFilesystem, b []byte) error {
	if len(b) > MaxXFSBytes {
		return fmt.Errorf("xfs_info output exceeds %d bytes", MaxXFSBytes)
	}
	fs.Features = []string{}
	section, sawData := "", false
	for _, line := range lines(b) {
		m := xfsSection.FindStringSubmatch(line)
		rest := line
		if m != nil {
			section, rest = m[1], m[2]
			head := strings.TrimSpace(rest)
			if i := strings.Index(head, "  "); i >= 0 {
				head = head[:i]
			}
			if strings.Contains(head, "=") {
				head = "" // no head: the line starts with its pairs
			}
			// The meta-data head is the mountpoint, which may contain "=":
			// scan only from its first pair, isize.
			if section == "meta-data" {
				if i := strings.LastIndex(rest, " isize="); i >= 0 {
					rest = rest[i:]
				}
			}
			switch section {
			case "log":
				fs.Log = clipText(head)
			case "realtime":
				fs.Realtime = clipText(head)
			}
		} else if t := strings.TrimSpace(line); strings.HasPrefix(t, "=") {
			rest = t[1:]
		} else {
			continue
		}
		for _, p := range xfsPair.FindAllStringSubmatch(rest, -1) {
			k, v := p[1], p[2]
			switch {
			case xfsFeatures[k]:
				if v == "1" {
					fs.Features = append(fs.Features, k)
				}
			case section == "meta-data" && k == "isize":
				fs.InodeSize = num(v)
			case section == "meta-data" && k == "agcount":
				fs.AGCount = num(v)
			case section == "meta-data" && k == "agsize":
				fs.AGSize = num(v)
			case section == "meta-data" && k == "sectsz":
				fs.SectorSize = num(v)
			case section == "data" && k == "bsize":
				fs.BlockSize, sawData = num(v), true
			case section == "data" && k == "blocks":
				fs.Blocks = num(v)
			case section == "data" && k == "sunit":
				fs.Sunit = num(v)
			case section == "data" && k == "swidth":
				fs.Swidth = num(v)
			case section == "log" && k == "blocks":
				fs.LogBlocks = num(v)
			}
		}
	}
	if !sawData || fs.Blocks == nil {
		return fmt.Errorf("unexpected xfs_info output")
	}
	return nil
}
