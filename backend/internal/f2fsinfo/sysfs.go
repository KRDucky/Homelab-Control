// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package f2fsinfo

import (
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"homelab-control/backend/internal/mountinfo"
)

// The kernel publishes each mounted f2fs's live counters under
// /sys/fs/f2fs/<disk>/ (Documentation/ABI/testing/sysfs-fs-f2fs), readable
// without privilege. f2fs-tools has no read-only report for a mounted
// filesystem (fsck.f2fs and dump.f2fs read the device), so these files are
// the f2fs status view. Nothing here writes a sysfs attribute.

// Filesystem is one mounted f2fs. Name is the kernel disk name (vdb, dm-0);
// a counter is nil when this kernel does not publish it.
type Filesystem struct {
	Name            string   `json:"name"`
	Mounts          []string `json:"mounts"`
	Features        []string `json:"features"`
	Encoding        string   `json:"encoding,omitempty"`
	FreeSegments    *uint64  `json:"freeSegments,omitempty"`
	DirtySegments   *uint64  `json:"dirtySegments,omitempty"`
	OvpSegments     *uint64  `json:"overprovisionSegments,omitempty"`
	ReservedBlocks  *uint64  `json:"reservedBlocks,omitempty"`
	LifetimeWriteKB *uint64  `json:"lifetimeWriteKiB,omitempty"`
}

// Status is the per-filesystem part of the f2fs response.
type Status struct {
	Filesystems []Filesystem `json:"filesystems"`
	Truncated   bool         `json:"truncated,omitempty"`
}

const (
	maxSysfsFilesystems = 32
	maxAttrBytes        = 4096
)

var (
	diskName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	devNumber = regexp.MustCompile(`^[0-9]{1,10}:[0-9]{1,10}$`)
)

// readAttr reads one bounded sysfs attribute; "" when absent or unreadable.
func readAttr(root *os.Root, path string) string {
	f, err := root.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxAttrBytes+1))
	if err != nil || len(b) > maxAttrBytes {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func attrNum(root *os.Root, path string) *uint64 {
	v, err := strconv.ParseUint(readAttr(root, path), 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

// collectStatus groups the f2fs mounts by device number and reads each
// device's /sys/fs/f2fs/<disk>/ directory, found through /sys/dev/block.
func collectStatus(root *os.Root, recs []mountinfo.Record) Status {
	st := Status{Filesystems: []Filesystem{}}
	index := map[string]int{}
	for _, r := range recs {
		if r.FSType != "f2fs" || !devNumber.MatchString(r.DeviceNumber) || mountinfo.HasDotDot(r.Mountpoint) {
			continue
		}
		if i, ok := index[r.DeviceNumber]; ok {
			if len(st.Filesystems[i].Mounts) < 32 {
				st.Filesystems[i].Mounts = append(st.Filesystems[i].Mounts, r.Mountpoint)
			}
			continue
		}
		if len(st.Filesystems) >= maxSysfsFilesystems {
			st.Truncated = true
			continue
		}
		name := readAttr(root, "sys/dev/block/"+r.DeviceNumber+"/uevent")
		disk := ""
		for _, line := range strings.Split(name, "\n") {
			if v, ok := strings.CutPrefix(line, "DEVNAME="); ok {
				disk = v
			}
		}
		if !diskName.MatchString(disk) {
			continue
		}
		base := "sys/fs/f2fs/" + disk + "/"
		fs := Filesystem{Name: disk, Mounts: []string{r.Mountpoint}, Features: []string{},
			Encoding:        readAttr(root, base+"encoding"),
			FreeSegments:    attrNum(root, base+"free_segments"),
			DirtySegments:   attrNum(root, base+"dirty_segments"),
			OvpSegments:     attrNum(root, base+"ovp_segments"),
			ReservedBlocks:  attrNum(root, base+"current_reserved_blocks"),
			LifetimeWriteKB: attrNum(root, base+"lifetime_write_kbytes"),
		}
		// "features" is a comma-separated list ("encryption, extra_attr").
		for _, f := range strings.Split(readAttr(root, base+"features"), ",") {
			if f = strings.TrimSpace(f); f != "" && len(fs.Features) < 64 {
				fs.Features = append(fs.Features, f)
			}
		}
		index[r.DeviceNumber] = len(st.Filesystems)
		st.Filesystems = append(st.Filesystems, fs)
	}
	return st
}
