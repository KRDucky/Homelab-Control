// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"strings"

	"homelab-control/backend/internal/storagecap"
)

// ToolItem and Tooling keep the MergerFS JSON wire format; probing lives
// in storagecap so later storage modules share it.
type (
	ToolItem = storagecap.ToolItem
	Tooling  = storagecap.Tooling
)

// Well-known, fixed paths only: never search $PATH, never exec the binary.
var (
	mergerfsBinaryPaths    = []string{"/usr/bin/mergerfs", "/usr/sbin/mergerfs"}
	fusermount3BinaryPaths = []string{"/usr/bin/fusermount3", "/usr/sbin/fusermount3"}
	snapraidBinaryPaths    = []string{"/usr/bin/snapraid", "/usr/sbin/snapraid"}
)

// ProbeTooling reports binary presence with family-adapted package names. It
// never spawns a process and never calls the package manager. fusermount3 is
// required (FUSE3 mount helper, package fuse3); snapraid stays optional.
func ProbeTooling(family string) Tooling {
	return probeTooling(family, storagecap.RealStat)
}

func probeTooling(family string, stat storagecap.StatFunc) Tooling {
	pkg := func(name string) string {
		if storagecap.KnownFamily(family) {
			return name
		}
		return ""
	}
	return storagecap.Probe(family, []storagecap.Spec{
		{ID: "mergerfs", Package: pkg("mergerfs"), Paths: mergerfsBinaryPaths, Required: true},
		{ID: "fusermount3", Package: pkg("fuse3"), Paths: fusermount3BinaryPaths, Required: true},
		{ID: "snapraid", Package: pkg("snapraid"), Paths: snapraidBinaryPaths, Required: false},
	}, stat)
}

// ProbeCapability is the cheap capabilities GET probe: binary Stat plus one
// /proc/filesystems read. It never reads mountinfo or snapraid.conf. Missing
// required binaries or kernel FUSE is degraded (page still mounts). Missing
// snapraid never degrades.
func ProbeCapability(family string) (available bool, reason string) {
	return probeCapability(family, storagecap.RealStat, "/")
}

func probeCapability(family string, stat storagecap.StatFunc, osRoot string) (available bool, reason string) {
	tool := probeTooling(family, stat)
	var missing []string
	for _, it := range tool.Required {
		if !it.Installed {
			missing = append(missing, it.ID)
		}
	}
	fuse := ProbeKernelFuse(osRoot)
	if len(missing) > 0 {
		reason = "Required tooling is not installed: " + strings.Join(missing, ", ") + "."
		if !fuse.Present {
			extra := fuse.Reason
			if extra == "" {
				extra = reasonFuseMissing
			}
			reason += " " + extra
		}
		return false, reason
	}
	if !fuse.Present {
		if fuse.Reason != "" {
			return false, fuse.Reason
		}
		return false, reasonFuseMissing
	}
	return true, ""
}
