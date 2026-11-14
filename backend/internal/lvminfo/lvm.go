// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package lvminfo is the unprivileged, read-only LVM2 inventory (M7 first
// checkpoint). It Stats well-known lvm and thin_check paths, reads bounded
// /proc/devices for device-mapper, and lists sysfs dm devices whose uuid
// starts with LVM-. It never executes lvm, pvs, vgs, or lvs, never creates
// or removes a PV, VG, or LV, never loads a kernel module, and never opens
// a device node.
package lvminfo

import (
	"context"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Device is one observed device-mapper node with an LVM uuid prefix.
type Device struct {
	Name   string `json:"name"`
	Mapper string `json:"mapper"`
}

// Inventory is the lvm half of the response.
type Inventory struct {
	Present          bool     `json:"present"`
	Reason           string   `json:"reason"`
	Devices          []Device `json:"devices"`
	DevicesTruncated bool     `json:"devicesTruncated"`
}

// Snapshot is the full read-only LVM inventory.
type Snapshot struct {
	Tooling  Tooling   `json:"tooling"`
	Lvm      Inventory `json:"lvm"`
	KernelDM KernelDM  `json:"kernelDm"`
}

const (
	deviceMax     = 256
	dirReadMax    = 8192
	uuidReadMax   = 256
	mapperNameMax = 128
)

// Collect reads observed LVM device-mapper names and probes tooling plus
// kernel device-mapper. lvm is never executed. Missing or unreadable inputs
// become present=false with a reason. Only context cancellation returns an
// error.
func Collect(ctx context.Context, osRoot, family string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	snap.Tooling = ProbeTooling(family)
	snap.KernelDM = ProbeKernelDM(osRoot)
	devices, truncated, seen, thin, err := collectDevices(osRoot)
	if !thin {
		// thin_check is never run. Name its package only after a thin pool
		// component was observed, so the page does not offer it otherwise.
		for i := range snap.Tooling.Optional {
			if snap.Tooling.Optional[i].ID == "thin_check" {
				snap.Tooling.Optional[i].Package = ""
			}
		}
	}
	if devices == nil {
		devices = []Device{}
	}
	base := Inventory{Devices: devices, DevicesTruncated: truncated}
	if err != nil {
		base.Present = false
		base.Reason = "device inventory could not be read"
		base.Devices = []Device{}
		base.DevicesTruncated = false
		snap.Lvm = base
	} else if snap.Tooling.RequiredInstalled("lvm") || seen {
		base.Present = true
		snap.Lvm = base
	} else {
		base.Present = false
		base.Reason = "lvm binary was not found"
		snap.Lvm = base
	}
	return snap, nil
}

func collectDevices(osRoot string) (devices []Device, truncated, seen, thin bool, err error) {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return nil, false, false, false, err
	}
	defer root.Close()
	dir, err := root.Open("sys/block")
	if err != nil {
		return nil, false, false, false, err
	}
	defer dir.Close()
	names, err := dir.Readdirnames(dirReadMax + 1)
	if err != nil && err != io.EOF {
		return nil, false, false, false, err
	}
	truncated = len(names) > dirReadMax
	if truncated {
		names = names[:dirReadMax]
	}
	var dm []string
	for _, name := range names {
		if dmName(name) {
			dm = append(dm, name)
		}
	}
	// Numeric order before the keep cap, so the returned slice is the
	// lowest dm-N values rather than directory order.
	slices.SortFunc(dm, compareDM)
	devices = []Device{}
	for _, name := range dm {
		uuidPath := "sys/block/" + name + "/dm/uuid"
		info, stErr := root.Lstat(uuidPath)
		if stErr != nil || !info.Mode().IsRegular() {
			continue
		}
		uuid, uerr := readSmall(root, uuidPath)
		if uerr != nil || !strings.HasPrefix(strings.TrimRight(uuid, "\n"), "LVM-") {
			continue
		}
		seen = true
		mapper, _ := readSmall(root, "sys/block/"+name+"/dm/name")
		mapper = strings.TrimRight(mapper, "\n")
		if thinComponent(mapper) {
			thin = true
		}
		if internalComponent(mapper) {
			continue
		}
		if !validMapper(mapper) {
			mapper = ""
		}
		if len(devices) >= deviceMax {
			truncated = true
			continue
		}
		devices = append(devices, Device{Name: name, Mapper: mapper})
	}
	return devices, truncated, seen, thin, nil
}

func compareDM(a, b string) int {
	na, aok := dmIndex(a)
	nb, bok := dmIndex(b)
	switch {
	case aok && bok && na != nb:
		if na < nb {
			return -1
		}
		return 1
	case aok != bok:
		if aok {
			return -1
		}
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func dmIndex(name string) (uint64, bool) {
	rest, ok := strings.CutPrefix(name, "dm-")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(rest, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func dmName(name string) bool {
	rest, ok := strings.CutPrefix(name, "dm-")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func thinComponent(mapper string) bool {
	return strings.HasSuffix(mapper, "_tmeta") || strings.HasSuffix(mapper, "_tdata")
}

func internalComponent(mapper string) bool {
	if thinComponent(mapper) {
		return true
	}
	return strings.HasSuffix(mapper, "_cmeta") || strings.HasSuffix(mapper, "_cdata") || strings.HasSuffix(mapper, "_corig")
}

func validMapper(name string) bool {
	if name == "" || len(name) > mapperNameMax || !utf8.ValidString(name) {
		return false
	}
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func readSmall(root *os.Root, path string) (string, error) {
	f, err := root.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, uuidReadMax+1))
	if err != nil || len(b) > uuidReadMax {
		return "", io.ErrUnexpectedEOF
	}
	return string(b), nil
}
