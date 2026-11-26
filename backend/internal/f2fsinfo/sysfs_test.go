// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package f2fsinfo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Two mounts of one f2fs, one f2fs whose kernel publishes fewer counters, and
// unrelated mounts: counters come from /sys/fs/f2fs/<disk>, found by device
// number through /sys/dev/block/<maj:min>/uevent.
func TestCollectReadsSysfsCountersPerDevice(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"proc/filesystems": "nodev\tproc\n\tf2fs\n",
		"proc/self/mountinfo": "24 1 252:1 / / rw - ext4 /dev/vda1 rw\n" +
			"30 24 252:16 / /srv/flash rw - f2fs /dev/vdb rw\n" +
			"31 24 252:16 / /srv/flash-bind rw - f2fs /dev/vdb rw\n" +
			"32 24 253:2 / /srv/card rw - f2fs /dev/mapper/card rw\n",
		"sys/dev/block/252:16/uevent":             "MAJOR=252\nMINOR=16\nDEVNAME=vdb\nDEVTYPE=disk\n",
		"sys/dev/block/253:2/uevent":              "MAJOR=253\nMINOR=2\nDEVNAME=dm-2\n",
		"sys/fs/f2fs/vdb/features":                "encryption, extra_attr, inode_checksum, sb_checksum\n",
		"sys/fs/f2fs/vdb/encoding":                "(none)\n",
		"sys/fs/f2fs/vdb/free_segments":           "480\n",
		"sys/fs/f2fs/vdb/dirty_segments":          "3\n",
		"sys/fs/f2fs/vdb/ovp_segments":            "26\n",
		"sys/fs/f2fs/vdb/current_reserved_blocks": "0\n",
		"sys/fs/f2fs/vdb/lifetime_write_kbytes":   "11520\n",
		"sys/fs/f2fs/dm-2/free_segments":          "17\n",
	})
	snap, err := Collect(context.Background(), root, "debian")
	if err != nil {
		t.Fatal(err)
	}
	fs := snap.Status.Filesystems
	if len(fs) != 2 || snap.Status.Truncated {
		t.Fatalf("%+v", snap.Status)
	}
	a, b := fs[0], fs[1]
	if a.Name != "vdb" || len(a.Mounts) != 2 || a.Mounts[1] != "/srv/flash-bind" || len(a.Features) != 4 || a.Features[1] != "extra_attr" ||
		*a.FreeSegments != 480 || *a.DirtySegments != 3 || *a.OvpSegments != 26 || *a.ReservedBlocks != 0 || *a.LifetimeWriteKB != 11520 || a.Encoding != "(none)" {
		t.Fatalf("vdb: %+v", a)
	}
	if b.Name != "dm-2" || *b.FreeSegments != 17 || b.DirtySegments != nil || len(b.Features) != 0 || b.Features == nil {
		t.Fatalf("dm-2: %+v", b)
	}
}

// A device whose uevent names no safe disk is not read.
func TestCollectSkipsUnsafeDiskName(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"proc/filesystems":            "\tf2fs\n",
		"proc/self/mountinfo":         "30 1 252:16 / /srv/flash rw - f2fs /dev/vdb rw\n",
		"sys/dev/block/252:16/uevent": "DEVNAME=../../etc\n",
	})
	snap, _ := Collect(context.Background(), root, "debian")
	if len(snap.Status.Filesystems) != 0 {
		t.Fatalf("%+v", snap.Status)
	}
}
