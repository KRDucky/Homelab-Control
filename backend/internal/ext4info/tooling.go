// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ext4info

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
var ext4BinaryPaths = []string{"/usr/sbin/dumpe2fs", "/usr/bin/dumpe2fs"}

const capabilityReasonMax = 256

// ProbeTooling reports dumpe2fs binary presence with family-adapted package
// names. It never spawns a process and never calls the package manager.
// An unknown family names no package. No disk is opened and no filesystem
// is created or repaired.
func ProbeTooling(family string) Tooling {
	return probeTooling(family, storagecap.RealStat)
}

func probeTooling(family string, stat storagecap.StatFunc) Tooling {
	requiredPkg := ""
	switch family {
	case "debian":
		requiredPkg = "e2fsprogs"
	case "fedora":
		requiredPkg = "e2fsprogs"
	case "suse":
		requiredPkg = "e2fsprogs"
	case "arch":
		requiredPkg = "e2fsprogs"
	}
	tool := storagecap.Probe(family, []storagecap.Spec{
		{ID: "dumpe2fs", Package: requiredPkg, Paths: ext4BinaryPaths, Required: true},
	}, stat)
	if tool.Optional == nil {
		tool.Optional = []ToolItem{}
	}
	return tool
}

// ProbeCapability is the cheap capabilities GET probe: binary Stat plus one
// /proc/filesystems read. It never reads mountinfo. Missing the dumpe2fs
// binary or an unlisted kernel ext4 is degraded, not unavailable.
// dumpe2fs is never executed.
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
	k := ProbeKernelExt4(osRoot)
	if len(missing) > 0 {
		reason = "Required tooling is not installed: " + strings.Join(missing, ", ") + "."
		if !k.Present {
			extra := reasonExt4Short
			if k.Reason == reasonExt4Unread {
				extra = reasonExt4Unread
			}
			reason = clipCapabilityReason(reason + " " + extra)
		}
		return false, reason
	}
	if !k.Present {
		if k.Reason != "" {
			return false, k.Reason
		}
		return false, reasonExt4Missing
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
