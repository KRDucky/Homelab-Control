// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvminfo

import (
	"strings"
	"unicode/utf8"

	"homelab-control/backend/internal/storagecap"
)

type (
	ToolItem = storagecap.ToolItem
	Tooling  = storagecap.Tooling
)

var (
	lvmBinaryPaths       = []string{"/usr/sbin/lvm", "/usr/bin/lvm"}
	thinCheckBinaryPaths = []string{"/usr/sbin/thin_check", "/usr/bin/thin_check"}
)

const capabilityReasonMax = 256

// ProbeTooling reports the lvm binary (required) and thin_check (optional).
// It never searches $PATH and never executes either binary. lvm2 is in the
// default archive for Debian, Fedora, SUSE, and Arch. The optional thin tool
// package is named only where this checkpoint is sure; SUSE gets an empty
// optional package so the page does not invent one.
func ProbeTooling(family string) Tooling {
	return probeTooling(family, storagecap.RealStat)
}

func probeTooling(family string, stat storagecap.StatFunc) Tooling {
	requiredPkg := ""
	switch family {
	case "debian", "fedora", "suse", "arch":
		requiredPkg = "lvm2"
	}
	optionalPkg := ""
	switch family {
	case "debian", "arch":
		optionalPkg = "thin-provisioning-tools"
	case "fedora":
		optionalPkg = "device-mapper-persistent-data"
	}
	tool := storagecap.Probe(family, []storagecap.Spec{
		{ID: "lvm", Package: requiredPkg, Paths: lvmBinaryPaths, Required: true},
		{ID: "thin_check", Package: optionalPkg, Paths: thinCheckBinaryPaths, Required: false},
	}, stat)
	if tool.Optional == nil {
		tool.Optional = []ToolItem{}
	}
	return tool
}

// ProbeCapability is the cheap capabilities GET probe: binary Stat plus one
// /proc/devices read. It never walks sysfs and never runs lvm. A missing
// binary or an unlisted device-mapper target is degraded, not unavailable.
// Unlisted includes an unloaded dm_mod. The reason stays "not currently
// listed"; this probe does not load the module.
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
	k := ProbeKernelDM(osRoot)
	if len(missing) > 0 {
		reason = "Required tooling is not installed: " + strings.Join(missing, ", ") + "."
		if !k.Present {
			extra := reasonDMMissingShort
			if k.Reason == reasonDMUnread {
				extra = reasonDMUnread
			}
			reason = clipCapabilityReason(reason + " " + extra)
		}
		return false, reason
	}
	if !k.Present {
		if k.Reason != "" {
			return false, k.Reason
		}
		return false, reasonDMMissing
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
