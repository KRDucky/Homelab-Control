// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagetools

import (
	"fmt"
	"strconv"
	"strings"
)

// ext4: the helper runs `tune2fs -l` (e2fsprogs) on the block device of each
// mounted ext4 filesystem and this parser reads its superblock listing. The
// "Key:   value" lines come from e2p's list_super, unchanged for many
// e2fsprogs releases; the error lines appear only when the kernel has
// recorded an error.

// Bounds on ext4 output.
const (
	MaxTune2fsBytes    = 64 << 10 // per filesystem
	MaxExt4Filesystems = 32
)

// Tune2fsArgs is the fixed template; dev is /dev/block/<major>:<minor> from
// the kernel's mount table, never a caller's path.
func Tune2fsArgs(dev string) []string { return []string{"-l", dev} }

// Ext4Report lists the mounted ext4 filesystems tune2fs described.
type Ext4Report struct {
	Filesystems []Ext4Filesystem `json:"filesystems"`
	Skipped     []SkippedMount   `json:"skipped,omitempty"`
	Truncated   bool             `json:"truncated,omitempty"`
}

// Ext4Filesystem is one device's superblock. Device is the kernel's
// major:minor; Mounts are its mountpoints. Counts are nil when tune2fs did not
// print them. Times are tune2fs's own text.
type Ext4Filesystem struct {
	Device         string   `json:"device"`
	Mounts         []string `json:"mounts"`
	VolumeName     string   `json:"volume_name,omitempty"`
	UUID           string   `json:"uuid,omitempty"`
	State          string   `json:"state,omitempty"`
	ErrorsBehavior string   `json:"errors_behavior,omitempty"`
	Features       []string `json:"features"`
	BlockSize      *uint64  `json:"block_size,omitempty"`
	BlockCount     *uint64  `json:"block_count,omitempty"`
	FreeBlocks     *uint64  `json:"free_blocks,omitempty"`
	ReservedBlocks *uint64  `json:"reserved_blocks,omitempty"`
	InodeCount     *uint64  `json:"inode_count,omitempty"`
	FreeInodes     *uint64  `json:"free_inodes,omitempty"`
	MountCount     *uint64  `json:"mount_count,omitempty"`
	// MaxMountCount is -1 when the mount-count check is off.
	MaxMountCount  *int64 `json:"max_mount_count,omitempty"`
	CheckInterval  string `json:"check_interval,omitempty"`
	LastChecked    string `json:"last_checked,omitempty"`
	Created        string `json:"created,omitempty"`
	LastMountTime  string `json:"last_mount_time,omitempty"`
	LastWriteTime  string `json:"last_write_time,omitempty"`
	LifetimeWrites string `json:"lifetime_writes,omitempty"`
	// ErrorCount and the first/last error are set only after the kernel has
	// recorded a filesystem error in the superblock.
	ErrorCount     *uint64 `json:"error_count,omitempty"`
	FirstErrorTime string  `json:"first_error_time,omitempty"`
	FirstErrorFunc string  `json:"first_error_function,omitempty"`
	LastErrorTime  string  `json:"last_error_time,omitempty"`
	LastErrorFunc  string  `json:"last_error_function,omitempty"`
	Error          string  `json:"error,omitempty"`
}

// ParseTune2fs reads one `tune2fs -l` listing into fs.
func ParseTune2fs(fs *Ext4Filesystem, b []byte) error {
	if len(b) > MaxTune2fsBytes {
		return fmt.Errorf("tune2fs output exceeds %d bytes", MaxTune2fsBytes)
	}
	fs.Features = []string{}
	magic := false
	for _, line := range lines(b) {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue // the version banner
		}
		k, v = strings.TrimSpace(k), clipText(v)
		switch k {
		case "Filesystem magic number":
			magic = v == "0xEF53"
		case "Filesystem volume name":
			if v != "<none>" {
				fs.VolumeName = v
			}
		case "Filesystem UUID":
			fs.UUID = v
		case "Filesystem state":
			fs.State = v
		case "Errors behavior":
			fs.ErrorsBehavior = v
		case "Filesystem features":
			if v != "(none)" {
				fs.Features = strings.Fields(v)
			}
		case "Block size":
			fs.BlockSize = num(v)
		case "Block count":
			fs.BlockCount = num(v)
		case "Free blocks":
			fs.FreeBlocks = num(v)
		case "Reserved block count":
			fs.ReservedBlocks = num(v)
		case "Inode count":
			fs.InodeCount = num(v)
		case "Free inodes":
			fs.FreeInodes = num(v)
		case "Mount count":
			fs.MountCount = num(v)
		case "Maximum mount count":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				fs.MaxMountCount = &n
			}
		case "Check interval":
			fs.CheckInterval = v
		case "Last checked":
			fs.LastChecked = v
		case "Filesystem created":
			fs.Created = v
		case "Last mount time":
			fs.LastMountTime = v
		case "Last write time":
			fs.LastWriteTime = v
		case "Lifetime writes":
			fs.LifetimeWrites = v
		case "FS Error count":
			fs.ErrorCount = num(v)
		case "First error time":
			fs.FirstErrorTime = v
		case "First error function":
			fs.FirstErrorFunc = v
		case "Last error time":
			fs.LastErrorTime = v
		case "Last error function":
			fs.LastErrorFunc = v
		}
	}
	if !magic {
		return fmt.Errorf("unexpected tune2fs output")
	}
	return nil
}
