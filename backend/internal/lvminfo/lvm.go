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
	blockScanMax  = 1024
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
	devices, truncated, err := collectDevices(osRoot)
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
	} else if snap.Tooling.RequiredInstalled("lvm") || len(devices) > 0 {
		base.Present = true
		snap.Lvm = base
	} else {
		base.Present = false
		base.Reason = "lvm binary was not found"
		snap.Lvm = base
	}
	return snap, nil
}

func collectDevices(osRoot string) ([]Device, bool, error) {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	dir, err := root.Open("sys/block")
	if err != nil {
		return nil, false, err
	}
	defer dir.Close()
	names, err := dir.Readdirnames(blockScanMax + 1)
	if err != nil && err != io.EOF {
		return nil, false, err
	}
	truncated := len(names) > blockScanMax
	if truncated {
		names = names[:blockScanMax]
	}
	devices := []Device{}
	for _, name := range names {
		if !dmName(name) {
			continue
		}
		uuid, uerr := readSmall(root, "sys/block/"+name+"/dm/uuid")
		if uerr != nil || !strings.HasPrefix(uuid, "LVM-") {
			continue
		}
		mapper, _ := readSmall(root, "sys/block/"+name+"/dm/name")
		mapper = strings.TrimRight(mapper, "\n")
		if !validMapper(mapper) {
			mapper = ""
		}
		if len(devices) >= deviceMax {
			truncated = true
			continue
		}
		devices = append(devices, Device{Name: name, Mapper: mapper})
	}
	return devices, truncated, nil
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
