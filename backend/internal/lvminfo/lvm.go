// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package lvminfo is the unprivileged part of the LVM page: it Stats the lvm
// and thin_check binaries and reads bounded /proc/devices for
// device-mapper. Volume groups, logical volumes and physical volumes come
// from pvs, lvs and vgs, run by the read-only storage helper (broker op
// lvm.status); this package never reconstructs them.
package lvminfo

import "context"

// Snapshot is the unprivileged LVM inventory.
type Snapshot struct {
	Tooling  Tooling  `json:"tooling"`
	KernelDM KernelDM `json:"kernelDm"`
}

// Collect probes tooling and kernel device-mapper. Only context
// cancellation returns an error.
func Collect(ctx context.Context, osRoot, family string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Tooling: ProbeTooling(family), KernelDM: ProbeKernelDM(osRoot)}, nil
}
