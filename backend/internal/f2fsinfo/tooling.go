// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package f2fsinfo

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
var f2fsBinaryPaths = []string{"/usr/sbin/dump.f2fs", "/usr/bin/dump.f2fs"}

const capabilityReasonMax = 256

// ProbeTooling reports dump.f2fs binary presence with family-adapted package
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
		requiredPkg = "f2fs-tools"
	}
	tool := storagecap.Probe(family, []storagecap.Spec{
		{ID: "dump.f2fs", Package: requiredPkg, Paths: f2fsBinaryPaths, Required: true},
	}, stat)
	if tool.Optional == nil {
		tool.Optional = []ToolItem{}
	}
	return tool
}

// ProbeCapability is the cheap capabilities GET probe: binary Stat plus one
// /proc/filesystems read. It never reads mountinfo. Missing the dump.f2fs
// binary or an unlisted kernel f2fs is degraded, not unavailable.
// dump.f2fs is never executed.
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
	k := ProbeKernelF2fs(osRoot)
	if len(missing) > 0 {
		reason = "Required tooling is not installed: " + strings.Join(missing, ", ") + "."
		if !k.Present {
			extra := reasonF2fsShort
			if k.Reason == reasonF2fsUnread {
				extra = reasonF2fsUnread
			}
			reason = clipCapabilityReason(reason + " " + extra)
		}
		return false, reason
	}
	if !k.Present {
		if k.Reason != "" {
			return false, k.Reason
		}
		return false, reasonF2fsMissing
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
