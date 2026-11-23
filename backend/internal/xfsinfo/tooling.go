// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package xfsinfo

import (
	"strings"
	"unicode/utf8"

	"homelab-control/backend/internal/storagecap"
)

type (
	ToolItem = storagecap.ToolItem
	Tooling  = storagecap.Tooling
)

// Well-known, fixed paths only: never search $PATH, never exec the binary.
var xfsBinaryPaths = []string{"/usr/sbin/xfs_info", "/usr/bin/xfs_info"}

const capabilityReasonMax = 256

// ProbeTooling reports xfs_info binary presence with family-adapted package
// names. It never spawns a process and never calls the package manager.
// An unknown family names no package. No disk is opened and no filesystem
// is created or repaired.
func ProbeTooling(family string) Tooling {
	return probeTooling(family, storagecap.RealStat)
}

func probeTooling(family string, stat storagecap.StatFunc) Tooling {
	requiredPkg := ""
	switch family {
	case "debian", "fedora", "suse", "arch":
		requiredPkg = "xfsprogs"
	}
	tool := storagecap.Probe(family, []storagecap.Spec{
		{ID: "xfs_info", Package: requiredPkg, Paths: xfsBinaryPaths, Required: true},
	}, stat)
	if tool.Optional == nil {
		tool.Optional = []ToolItem{}
	}
	return tool
}

// ProbeCapability is the cheap capabilities GET probe: binary Stat plus one
// /proc/filesystems read. It never reads mountinfo. A missing xfs_info binary
// or an unlisted kernel XFS returns available=false with a reason, which the
// capabilities handler reports as degraded (not unavailable) so the page
// still mounts. xfs_info is never executed.
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
	k := ProbeKernelXfs(osRoot)
	if len(missing) > 0 {
		reason = "Required tooling is not installed: " + strings.Join(missing, ", ") + "."
		if !k.Present {
			extra := reasonXfsShort
			if k.Reason == reasonXfsUnread {
				extra = reasonXfsUnread
			}
			reason = clipCapabilityReason(reason + " " + extra)
		}
		return false, reason
	}
	if !k.Present {
		if k.Reason != "" {
			return false, k.Reason
		}
		return false, reasonXfsMissing
	}
	return true, ""
}

func clipCapabilityReason(s string) string {
	if len(s) <= capabilityReasonMax {
		return s
	}
	s = s[:capabilityReasonMax]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
