// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var blockName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.!:-]{0,254}$`)

// Collect reads fixed kernel metadata beneath an administrator-selected OS root.
// Production uses "/"; request data must never select this root. os.Root keeps
// sysfs traversal beneath the selected tree. No /dev node is opened, no
// mountpoint is inspected, and no programs are executed.
func Collect(ctx context.Context, osRoot string) (Graph, error) {
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return Graph{}, err
	}
	defer root.Close()
	notes := []string{}
	read := func(path string) string {
		f, e := root.Open(path)
		if e != nil {
			return ""
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() {
			return ""
		}
		b, e := io.ReadAll(io.LimitReader(f, 4097))
		if e != nil || len(b) > 4096 {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	// readRaw returns a bounded binary attribute such as a SCSI VPD page.
	readRaw := func(path string) []byte {
		f, e := root.Open(path)
		if e != nil {
			return nil
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() {
			return nil
		}
		b, e := io.ReadAll(io.LimitReader(f, 4097))
		if e != nil || len(b) > 4096 {
			return nil
		}
		return b
	}
	dir, err := root.Open("sys/class/block")
	if err != nil {
		return Graph{}, fmt.Errorf("block metadata unavailable: %w", err)
	}
	entries, err := dir.ReadDir(4097)
	dir.Close()
	if err != nil && err != io.EOF {
		return Graph{}, err
	}
	if len(entries) > 4096 {
		return Graph{}, fmt.Errorf("block inventory exceeds entry limit")
	}
	blocks := []Block{}
	totalMembers := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return Graph{}, err
		}
		name := entry.Name()
		if !blockName.MatchString(name) {
			notes = append(notes, "An invalid kernel block name was omitted.")
			continue
		}
		base := "sys/class/block/" + name + "/"
		if read(base+"hidden") == "1" {
			// Hidden NVMe multipath per-path disks (nvmeXcYnZ) have no /dev node
			// and repeat the head namespace's identifiers.
			notes = append(notes, "Hidden per-path block devices (NVMe native multipath) were not listed separately.")
			continue
		}
		dev := read(base + "dev")
		if !validDeviceNumber(dev) {
			notes = append(notes, "A device disappeared or its metadata is unreadable.")
			continue
		}
		b := Block{Name: name, DeviceNumber: dev, Kind: "block", Source: "/sys/class/block"}
		if read(base+"partition") != "" {
			// Partitions never take whole-device identifiers (review A5).
			b.Kind = "partition"
			b.Parent = read(base + "../dev")
			b.DiskSeq = diskSeq(read(base + "../diskseq"))
		} else {
			b.DiskSeq = diskSeq(read(base + "diskseq"))
			collectIdentity(&b, base, read, readRaw)
		}
		if dmUUID := read(base + "dm/uuid"); dmUUID != "" {
			b.Kind = "device-mapper"
			b.NativeKind, b.NativeUUID = "dm-uuid", dmUUID
			if strings.HasPrefix(dmUUID, "LVM-") {
				b.Kind = "lvm"
			}
			if strings.HasPrefix(dmUUID, "CRYPT-") {
				b.Kind = "crypt"
			}
		}
		if level := read(base + "md/level"); level != "" {
			b.Kind = "mdraid"
			b.NativeKind, b.NativeUUID = "md-uuid", read(base+"md/uuid")
			md, mdNotes := collectMD(root, base, level, read)
			b.MD = md
			notes = append(notes, mdNotes...)
		}
		sectors, e := strconv.ParseUint(read(base+"size"), 10, 64)
		if e == nil && sectors <= math.MaxUint64/512 {
			b.SizeBytes = sectors * 512
		} else {
			notes = append(notes, "A block device has an unreadable or overflowing sector count.")
		}
		if slaves, e := root.Open(base + "slaves"); e == nil {
			lower, e := slaves.ReadDir(1025)
			totalMembers += len(lower)
			if totalMembers > 16384 {
				slaves.Close()
				return Graph{}, fmt.Errorf("stacked device relationships exceed total budget")
			}
			slaves.Close()
			if len(lower) > 1024 || e != nil && e != io.EOF {
				return Graph{}, fmt.Errorf("stacked device metadata exceeds bounds or is unreadable")
			}
			for _, member := range lower {
				if !blockName.MatchString(member.Name()) {
					return Graph{}, fmt.Errorf("invalid stacked device entry")
				}
				if number := read(filepath.Join(base, "slaves", member.Name(), "dev")); validDeviceNumber(number) {
					b.Lower = append(b.Lower, number)
				} else {
					notes = append(notes, "A stacked device member could not be inspected.")
				}
			}
		}
		blocks = append(blocks, b)
	}
	mounts := []Mount{}
	f, e := root.Open("proc/self/mountinfo")
	if e != nil {
		notes = append(notes, "Mount metadata unavailable; no mountpoints were probed.")
	} else {
		mounts, e = ParseMountInfo(f)
		f.Close()
		if e != nil {
			return Graph{}, e
		}
	}
	g := Build(blocks, mounts)
	g.BootID = read("proc/sys/kernel/random/boot_id")
	g.Status = "partial"
	g.Notes = append(g.Notes, notes...)
	g.Notes = append(g.Notes, "Serial/ID comes from sysfs only (NVMe controller serial, SCSI/SATA VPD page 0x80, virtio-blk serial); other device types may show an alternative identifier until a reviewed probe adds udev or by-id data.")
	g.Notes = append(g.Notes, "On-disk signatures have not been probed; absence of a claim does not mean a device is unused.", "ZFS, Btrfs multi-device and offline storage membership require additional observations; this graph is not authorization to write.")
	return g, nil
}

// diskSeq accepts the decimal sysfs diskseq (Linux 5.15+); anything else is
// treated as unavailable.
func diskSeq(s string) string {
	if n, e := strconv.ParseUint(s, 10, 64); e == nil && n > 0 {
		return strconv.FormatUint(n, 10)
	}
	return ""
}

// collectIdentity reads whole-device identifiers from world-readable sysfs.
// NVMe namespaces expose wwid at the block level and the controller serial in
// device/serial. SCSI and libata disks expose wwid and VPD page 0x80 (unit
// serial number) under device/. virtio-blk exposes serial at the block level.
func collectIdentity(b *Block, base string, read func(string) string, readRaw func(string) []byte) {
	if w := read(base + "device/wwid"); w != "" {
		b.WWID, b.WWIDSource = w, "sysfs device/wwid"
	} else if w := read(base + "wwid"); w != "" {
		b.WWID, b.WWIDSource = w, "sysfs wwid"
	}
	if read(base+"nsid") != "" || read(base+"device/cntlid") != "" {
		// NVMe: device/ is the controller. Its serial is shared by namespaces.
		if s := printable(read(base + "device/serial")); s != "" {
			b.ControllerSerial = s
			b.ControllerKey = read(base+"device/subsysnqn") + "\x00" + read(base+"device/cntlid")
			if b.ControllerKey == "\x00" {
				b.ControllerKey = ""
			}
		}
		return
	}
	if s := vpdUnitSerial(readRaw(base + "device/vpd_pg80")); s != "" {
		b.Serial, b.SerialSource = s, "sysfs device/vpd_pg80"
	} else if s := printable(read(base + "serial")); s != "" {
		b.Serial, b.SerialSource = s, "sysfs serial"
	} else if s := printable(read(base + "device/serial")); s != "" {
		b.Serial, b.SerialSource = s, "sysfs device/serial"
	}
}

// vpdUnitSerial decodes SCSI VPD page 0x80: byte 1 is the page code, bytes
// 2-3 the big-endian page length, then the ASCII product serial number.
func vpdUnitSerial(page []byte) string {
	if len(page) < 4 || page[1] != 0x80 {
		return ""
	}
	n := int(page[2])<<8 | int(page[3])
	if n == 0 || 4+n > len(page) {
		return ""
	}
	return printable(string(page[4 : 4+n]))
}

// printable trims and accepts only visible ASCII, so binary or control bytes
// in a sysfs attribute are never relayed as an identifier.
func printable(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 256 {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return ""
		}
	}
	return s
}

// collectMD reads md runtime attributes and members. It reads attributes only;
// it never writes sync_action or any other md control file.
func collectMD(root *os.Root, base, level string, read func(string) string) (*MDState, []string) {
	md := &MDState{Level: mdToken(level), Members: []MDMember{}, Source: "/sys/class/block/*/md"}
	notes := []string{}
	md.ArrayState = mdToken(read(base + "md/array_state"))
	md.RaidDisks = mdUint(read(base + "md/raid_disks"))
	md.Degraded = mdUint(read(base + "md/degraded"))
	md.SyncAction = mdToken(read(base + "md/sync_action"))
	md.SyncCompleted = mdToken(read(base + "md/sync_completed"))
	md.MismatchCount = mdUint(read(base + "md/mismatch_cnt"))
	if md.Level == "container" {
		notes = append(notes, "An md container (IMSM/DDF metadata) was observed; its member arrays report their own state.")
	}
	dir, err := root.Open(base + "md")
	if err != nil {
		notes = append(notes, "md member list could not be read.")
		return md, notes
	}
	entries, err := dir.ReadDir(1025)
	dir.Close()
	if (err != nil && err != io.EOF) || len(entries) > 1024 {
		notes = append(notes, "md member list exceeds bounds or is unreadable.")
		return md, notes
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "dev-") || !blockName.MatchString(name) {
			continue
		}
		p := base + "md/" + name + "/"
		m := MDMember{Slot: mdToken(read(p + "slot")), State: mdToken(read(p + "state"))}
		if number := read(p + "block/dev"); validDeviceNumber(number) {
			m.DeviceNumber = number
		} else {
			notes = append(notes, "An md member's device could not be identified.")
		}
		md.Members = append(md.Members, m)
	}
	sort.Slice(md.Members, func(i, j int) bool { return md.Members[i].DeviceNumber < md.Members[j].DeviceNumber })
	return md, notes
}
