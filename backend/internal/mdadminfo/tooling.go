// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mdadminfo

import (
	"homelab-control/backend/internal/storagecap"
)

// ToolItem and Tooling keep the storage JSON wire format; probing lives in
// storagecap.
type (
	ToolItem = storagecap.ToolItem
	Tooling  = storagecap.Tooling
)

// Well-known, fixed paths only: never search $PATH, never exec the binary.
var mdadmBinaryPaths = []string{"/usr/sbin/mdadm", "/usr/bin/mdadm"}

// ProbeTooling reports mdadm binary presence. Debian, Fedora, SUSE and Arch
// all name the package mdadm; an unknown family names no package. It never
// spawns a process and never calls the package manager.
func ProbeTooling(family string) Tooling {
	return probeTooling(family, storagecap.RealStat)
}

func probeTooling(family string, stat storagecap.StatFunc) Tooling {
	pkg := ""
	if storagecap.KnownFamily(family) {
		pkg = "mdadm"
	}
	tool := storagecap.Probe(family, []storagecap.Spec{
		{ID: "mdadm", Package: pkg, Paths: mdadmBinaryPaths, Required: true},
	}, stat)
	if tool.Optional == nil {
		tool.Optional = []ToolItem{}
	}
	return tool
}

// ProbeCapability is the cheap capabilities GET probe: one binary Stat plus
// one bounded /proc/mdstat read. A missing mdadm binary or an unusable
// /proc/mdstat is degraded (the page still mounts). mdadm is never executed.
func ProbeCapability(family string) (available bool, reason string) {
	return probeCapability(family, storagecap.RealStat, "/")
}

func probeCapability(family string, stat storagecap.StatFunc, osRoot string) (bool, string) {
	if !probeTooling(family, stat).RequiredInstalled("mdadm") {
		return false, "Required tooling is not installed: mdadm."
	}
	if md := ProbeMdstat(osRoot); !md.Present {
		return false, md.Reason
	}
	return true, ""
}
