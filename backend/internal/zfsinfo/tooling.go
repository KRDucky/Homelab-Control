// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package zfsinfo

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
var (
	zfsBinaryPaths      = []string{"/usr/sbin/zfs", "/usr/bin/zfs"}
	zpoolBinaryPaths    = []string{"/usr/sbin/zpool", "/usr/bin/zpool"}
	smartctlBinaryPaths = []string{"/usr/sbin/smartctl", "/usr/bin/smartctl"}
	nvmeBinaryPaths     = []string{"/usr/sbin/nvme", "/usr/bin/nvme"}
)

const capabilityReasonMax = 256

// ProbeTooling reports zfs and zpool binary presence with family-adapted
// package names. It never spawns a process and never calls the package
// manager. Both binaries are required. Debian/Ubuntu name zfsutils-linux;
// Fedora, SUSE, and Arch get an empty package (OpenZFS is not in those
// default archives; this probe never adds a third-party repository).
// smartctl and nvme are optional inspect tools. They are never executed
// and no disk is opened. Missing them does not degrade capability.
func ProbeTooling(family string) Tooling {
	return probeTooling(family, storagecap.RealStat)
}

func probeTooling(family string, stat storagecap.StatFunc) Tooling {
	pkg := ""
	if family == "debian" {
		pkg = "zfsutils-linux"
	}
	opt := func(name string) string {
		if storagecap.KnownFamily(family) {
			return name
		}
		return ""
	}
	tool := storagecap.Probe(family, []storagecap.Spec{
		{ID: "zfs", Package: pkg, Paths: zfsBinaryPaths, Required: true},
		{ID: "zpool", Package: pkg, Paths: zpoolBinaryPaths, Required: true},
		{ID: "smartctl", Package: opt("smartmontools"), Paths: smartctlBinaryPaths, Required: false},
		{ID: "nvme", Package: opt("nvme-cli"), Paths: nvmeBinaryPaths, Required: false},
	}, stat)
	if tool.Optional == nil {
		tool.Optional = []ToolItem{}
	}
	return tool
}

// ProbeCapability is the cheap capabilities GET probe: binary Stat plus one
// /proc/filesystems read. It never reads mountinfo. Missing required binaries
// or kernel ZFS is degraded (page still mounts). Missing smartctl or nvme
// is not degraded. zpool, zfs, smartctl, and nvme are never executed.
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
	k := ProbeKernelZfs(osRoot)
	if len(missing) > 0 {
		reason = "Required tooling is not installed: " + strings.Join(missing, ", ") + "."
		if !k.Present {
			extra := reasonZfsMissingShort
			if k.Reason == reasonZfsUnread {
				extra = reasonZfsUnread
			}
			reason = clipCapabilityReason(reason + " " + extra)
		}
		return false, reason
	}
	if !k.Present {
		if k.Reason != "" {
			return false, k.Reason
		}
		return false, reasonZfsMissing
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
