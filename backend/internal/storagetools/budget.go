// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagetools

import "time"

// Time budgets of the read-only storage helper, shared by the helper (which
// enforces them) and its callers (which must wait longer). Every deadline on
// the path API → broker client → broker op → helper derives from these, so a
// slow answer inside the helper's budget is never cut off upstream.
const (
	// HelperCommandTimeout bounds one zpool/zfs/btrfs command.
	HelperCommandTimeout = 10 * time.Second
	// ZFSBudget: zpool list, zpool status and zfs list run in sequence.
	ZFSBudget = 3 * HelperCommandTimeout
	// BtrfsBudget bounds the four btrfs-progs commands over all mounted
	// filesystems; a filesystem reached after it reports its commands failed.
	BtrfsBudget = 45 * time.Second
	// MdBudget bounds mdadm --detail over all arrays; an array reached after
	// it reports an error.
	MdBudget = 30 * time.Second
	// ProbeBudget bounds a full storage probe (lsblk, smartctl per disk,
	// pvs/lvs). It stays below the unit's TimeoutStartSec (60 s).
	ProbeBudget = 50 * time.Second
)

// HelperBudget is how long the helper may take for a request scope.
func HelperBudget(scope string) time.Duration {
	switch scope {
	case ScopeZFS:
		return ZFSBudget
	case ScopeBtrfs:
		return BtrfsBudget
	case ScopeMd:
		return MdBudget
	}
	return ProbeBudget
}
