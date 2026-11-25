// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mountinfo

import (
	"fmt"
	"strings"
	"testing"
)

// Real lines from an Ubuntu 26.04 guest (optional fields, escapes, fuse).
const sample = `24 1 252:1 / / rw,relatime shared:1 - ext4 /dev/vda1 rw,discard
57 46 0:44 / /mnt/storage rw,relatime shared:90 - fuse.mergerfs mergerfs rw,user_id=0,group_id=0,allow_other
60 24 0:50 / /tank/photos rw,relatime shared:95 - zfs tank/photos rw,xattr,noacl
61 24 8:16 /@data /srv/my\040archive rw,relatime - btrfs /dev/vdd rw,space_cache=v2,subvolid=257,subvol=/@data
`

func TestParseRecords(t *testing.T) {
	recs, info, err := Parse(strings.NewReader(sample))
	if err != nil || info.Truncated || info.Skipped != 0 || len(recs) != 4 {
		t.Fatalf("recs=%d info=%+v err=%v", len(recs), info, err)
	}
	r := recs[3]
	if r.ID != 61 || r.ParentID != 24 || r.DeviceNumber != "8:16" || r.Root != "/@data" || r.Mountpoint != "/srv/my archive" ||
		r.FSType != "btrfs" || r.Source != "/dev/vdd" || !strings.Contains(r.SuperOptions, "subvol=/@data") || r.MountOptions != "rw,relatime" {
		t.Fatalf("%+v", r)
	}
	if recs[1].FSType != "fuse.mergerfs" || recs[1].Source != "mergerfs" {
		t.Fatalf("%+v", recs[1])
	}
}

func TestParseSkipsMalformedAndLongLines(t *testing.T) {
	in := "not-a-line\n" +
		"1 0 0:1 / /x rw - ext4 /dev/sda1\n" + // no super-options
		"1 0 0:1 / /x rw ext4 /dev/sda1 rw\n" + // no separator
		"x 0 0:1 / /x rw - ext4 /dev/sda1 rw\n" + // bad id
		"1 0 0:1 / rel rw - ext4 /dev/sda1 rw\n" + // relative mountpoint
		"1 0 0:1 / /a\\999 rw - ext4 /dev/sda1 rw\n" + // bad escape
		strings.Repeat("y", MaxLine+10) + "\n" +
		sample
	recs, info, err := Parse(strings.NewReader(in))
	if err != nil || len(recs) != 4 || info.Skipped != 7 || !info.Truncated {
		t.Fatalf("recs=%d info=%+v err=%v", len(recs), info, err)
	}
}

func TestParseRecordBound(t *testing.T) {
	var b strings.Builder
	for i := 0; i < MaxRecords+5; i++ {
		fmt.Fprintf(&b, "%d 1 0:1 / /m%d rw - tmpfs tmpfs rw\n", i, i)
	}
	recs, info, err := Parse(strings.NewReader(b.String()))
	if err != nil || len(recs) != MaxRecords || !info.Truncated {
		t.Fatalf("recs=%d info=%+v err=%v", len(recs), info, err)
	}
}

func TestUnescape(t *testing.T) {
	got, err := Unescape(`a\040b\011c\012d\134e`)
	if err != nil || got != "a b\tc\nd\\e" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{`a\04`, `a\`, `a\x41b`, `a\101`} {
		if _, err := Unescape(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestDeviceMounts(t *testing.T) {
	in := "40 1 8:1 / /data rw - ext4 /dev/sda1 rw\n" +
		"41 1 8:2 / /../bad rw - ext4 /dev/sda2 rw\n" + // ".." mountpoint
		"42 1 8:3 / /net rw - ext4 server:/export rw\n" + // not a device
		"43 1 8:4 / /dev rw - ext4 /dev/../etc/x rw\n" + // ".." in device
		"44 1 8:5 / /" + strings.Repeat("m", MountpointMaxBytes) + " rw - ext4 /dev/sda5 rw\n" + // too long
		"45 1 8:6 / /xfs rw - xfs /dev/sda6 rw\n"
	recs, info, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	got, truncated := DeviceMounts(recs, info, "ext4", 10)
	if !truncated || len(got) != 1 || got[0] != (DeviceMount{Mountpoint: "/data", Device: "/dev/sda1"}) {
		t.Fatalf("%+v truncated=%v", got, truncated)
	}
	got, truncated = DeviceMounts(recs, info, "xfs", 10)
	if truncated || len(got) != 1 {
		t.Fatalf("xfs: %+v truncated=%v", got, truncated)
	}
	if got, truncated = DeviceMounts(recs, info, "ext4", 0); len(got) != 0 || !truncated {
		t.Fatalf("max: %+v truncated=%v", got, truncated)
	}
}
