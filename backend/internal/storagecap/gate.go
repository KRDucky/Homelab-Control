// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagecap

// Gate is a SHARED-01 discovery state. Write actions stay closed unless
// the gate is Available.
type Gate string

const (
	GateAvailable    Gate = "available"
	GateMissing      Gate = "missing"
	GateDegraded     Gate = "degraded"
	GateUnsupported  Gate = "unsupported"
	GateRestricted   Gate = "restricted"
	GateBroken       Gate = "broken"
	GateReadOnly     Gate = "readonly"
	GateNeedsReboot  Gate = "needs-reboot"
)

// FromTooling maps a probe to a gate. Missing required binaries are
// Degraded (page still mounts; write actions stay closed). Unknown family
// is Unsupported. Optional tools never degrade.
func FromTooling(t Tooling) Gate {
	if !KnownFamily(t.Family) {
		return GateUnsupported
	}
	if len(t.Required) == 0 {
		return GateAvailable
	}
	for _, it := range t.Required {
		if !it.Installed {
			return GateDegraded
		}
	}
	return GateAvailable
}

// WritesAllowed is true only when required tooling is present on a known
// family. P0 still does not perform storage writes.
func WritesAllowed(t Tooling) bool {
	return FromTooling(t) == GateAvailable
}
