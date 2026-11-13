// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import "homelab-control/backend/internal/storagecap"

// ToolItem and Tooling keep the MergerFS JSON wire format; probing lives
// in storagecap so later storage modules share it.
type (
	ToolItem = storagecap.ToolItem
	Tooling  = storagecap.Tooling
)

// Well-known, fixed paths only: never search $PATH, never exec the binary.
var (
	mergerfsBinaryPaths = []string{"/usr/bin/mergerfs", "/usr/sbin/mergerfs"}
	snapraidBinaryPaths = []string{"/usr/bin/snapraid", "/usr/sbin/snapraid"}
)

// ProbeTooling reports binary presence with family-adapted package names. It
// never spawns a process and never calls the package manager.
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
		{ID: "snapraid", Package: pkg("snapraid"), Paths: snapraidBinaryPaths, Required: false},
	}, stat)
}
