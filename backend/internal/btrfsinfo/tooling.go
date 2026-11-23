// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package btrfsinfo

import (
	"strings"
	"unicode/utf8"

	"homelab-control/backend/internal/storagecap"
)

// ToolItem and Tooling keep the storage JSON wire format; probing lives
// in storagecap so later storage modules share it.
type (
	ToolItem = storagecap.ToolItem
	Tooling  = storagecap.Tooling
)

// Well-known, fixed paths only: never search $PATH, never exec the binary.
var btrfsBinaryPaths = []string{"/usr/bin/btrfs", "/usr/sbin/btrfs"}

const capabilityReasonMax = 256

// ProbeTooling reports btrfs binary presence with family-adapted package
// names. It never spawns a process and never calls the package manager.
// Debian, Fedora, and Arch name btrfs-progs. SUSE names btrfsprogs. An
// unknown family names no package. smartctl is optional, never executed,
// and missing it does not degrade capability. No disk is opened.
func ProbeTooling(family string) Tooling {
	return probeTooling(family, storagecap.RealStat)
}

func probeTooling(family string, stat storagecap.StatFunc) Tooling {
	pkg := ""
	if storagecap.KnownFamily(family) {
		pkg = "btrfs-progs"
		if family == "suse" {
			pkg = "btrfsprogs"
		}
	}
	opt := ""
	if storagecap.KnownFamily(family) {
		opt = "smartmontools"
	}
	tool := storagecap.Probe(family, []storagecap.Spec{
		{ID: "btrfs", Package: pkg, Paths: btrfsBinaryPaths, Required: true},
		{ID: "smartctl", Package: opt, Paths: storagecap.SmartctlPaths(), Required: false},
	}, stat)
	if tool.Optional == nil {
		tool.Optional = []ToolItem{}
	}
	return tool
}

// ProbeCapability is the cheap capabilities GET probe: binary Stat plus one
// /proc/filesystems read. It never reads mountinfo. Missing the btrfs binary
// or kernel Btrfs is degraded (the page still mounts). Missing smartctl is
// not degraded. btrfs and smartctl are never executed.
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
	k := ProbeKernelBtrfs(osRoot)
	if len(missing) > 0 {
		reason = "Required tooling is not installed: " + strings.Join(missing, ", ") + "."
		if !k.Present {
			extra := reasonBtrfsMissingShort
			if k.Reason == reasonBtrfsUnread {
				extra = reasonBtrfsUnread
			}
			reason = clipCapabilityReason(reason + " " + extra)
		}
		return false, reason
	}
	if !k.Present {
		if k.Reason != "" {
			return false, k.Reason
		}
		return false, reasonBtrfsMissing
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
