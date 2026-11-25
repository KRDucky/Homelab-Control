// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagetools

import (
	"fmt"
	"regexp"
	"strings"
)

// Linux software RAID: the helper runs `mdadm --detail /dev/<kernel name>`
// for each array in /proc/mdstat and this parser decodes the report. Its
// "Key : value" block and device table have been stable for many mdadm
// releases; `--detail --export` lacks the state and failed/spare counts.

// Bounds on md output.
const (
	MaxMdBytes   = 128 << 10 // per array
	MaxMdArrays  = 64
	MaxMdMembers = 256 // per array
)

// MdDetailArgs is the fixed template; name is a kernel array name the helper
// read from /proc/mdstat (md0, md127), never a caller's path.
func MdDetailArgs(name string) []string { return []string{"--detail", "/dev/" + name} }

// MdReport lists the arrays mdadm described.
type MdReport struct {
	Arrays    []MdArray `json:"arrays"`
	Truncated bool      `json:"truncated,omitempty"`
}

// MdArray is one `mdadm --detail` report. State is mdadm's own wording, e.g.
// "clean, degraded, recovering". Counts are nil when mdadm did not print them.
type MdArray struct {
	Device      string  `json:"device"` // /dev/md127
	Name        string  `json:"name,omitempty"`
	UUID        string  `json:"uuid,omitempty"`
	Level       string  `json:"level,omitempty"`
	State       string  `json:"state,omitempty"`
	Metadata    string  `json:"metadata,omitempty"`
	ArraySizeKB *uint64 `json:"array_size_kib,omitempty"`
	RaidDevices *uint64 `json:"raid_devices,omitempty"`
	Active      *uint64 `json:"active_devices,omitempty"`
	Working     *uint64 `json:"working_devices,omitempty"`
	Failed      *uint64 `json:"failed_devices,omitempty"`
	Spare       *uint64 `json:"spare_devices,omitempty"`
	Layout      string  `json:"layout,omitempty"`
	ChunkSize   string  `json:"chunk_size,omitempty"`
	// Progress is a running rebuild, resync, reshape or check, as mdadm words
	// it ("Rebuild Status : 1% complete").
	Progress string     `json:"progress,omitempty"`
	Members  []MdMember `json:"members"`
	// Error is set when mdadm could not describe this array.
	Error string `json:"error,omitempty"`
}

// MdMember is one row of the device table. Slot is the RaidDevice column;
// nil for a spare or a removed slot ("-").
type MdMember struct {
	Slot   *uint64 `json:"slot,omitempty"`
	State  string  `json:"state"`
	Device string  `json:"device,omitempty"`
}

var mdKV = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z ]*[A-Za-z])\s+:\s*(.*)$`)

// ParseMdDetail reads one `mdadm --detail` report.
func ParseMdDetail(device string, b []byte) (MdArray, error) {
	if len(b) > MaxMdBytes {
		return MdArray{}, fmt.Errorf("mdadm report exceeds %d bytes", MaxMdBytes)
	}
	a := MdArray{Device: device, Members: []MdMember{}}
	inTable, sawLevel := false, false
	for _, line := range lines(b) {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "Number") && strings.Contains(t, "RaidDevice") {
			inTable = true
			continue
		}
		if inTable {
			f := strings.Fields(t)
			if len(f) < 5 {
				continue
			}
			if len(a.Members) >= MaxMdMembers {
				continue
			}
			m := MdMember{Slot: num(f[3])}
			rest := f[4:]
			if last := rest[len(rest)-1]; strings.HasPrefix(last, "/dev/") {
				m.Device = clipText(last)
				rest = rest[:len(rest)-1]
			}
			m.State = clipText(strings.Join(rest, " "))
			a.Members = append(a.Members, m)
			continue
		}
		kv := mdKV.FindStringSubmatch(line)
		if kv == nil {
			continue
		}
		k, v := kv[1], clipText(kv[2])
		switch k {
		case "Version":
			a.Metadata = v
		case "Raid Level":
			a.Level, sawLevel = v, true
		case "State":
			a.State = v
		case "Array Size":
			a.ArraySizeKB = num(firstField(v))
		case "Raid Devices":
			a.RaidDevices = num(v)
		case "Active Devices":
			a.Active = num(v)
		case "Working Devices":
			a.Working = num(v)
		case "Failed Devices":
			a.Failed = num(v)
		case "Spare Devices":
			a.Spare = num(v)
		case "Layout":
			a.Layout = v
		case "Chunk Size":
			a.ChunkSize = v
		case "Name":
			a.Name = firstField(v) // "host:name  (local to host host)"
		case "UUID":
			a.UUID = v
		case "Rebuild Status", "Resync Status", "Reshape Status", "Check Status":
			a.Progress = strings.TrimSuffix(k, " Status") + " " + v
		}
	}
	if !sawLevel {
		return MdArray{}, fmt.Errorf("unexpected mdadm report")
	}
	return a, nil
}

func firstField(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

// ValidMdName accepts a kernel md array name as /proc/mdstat prints it:
// md0, md127, or md_<name> (mdadm names=yes), the same rule as the mdstat
// parser in mdadminfo.
func ValidMdName(s string) bool { return mdName.MatchString(s) }

var mdName = regexp.MustCompile(`^md[A-Za-z0-9_]{1,30}$`)
