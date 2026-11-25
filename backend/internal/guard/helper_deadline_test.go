// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package guard

import (
	"testing"

	"homelab-control/backend/internal/broker/ops"
	"homelab-control/backend/internal/storagetools"
)

// TestStorageHelperOpsOutlastTheHelper: a broker op that waits on the storage
// helper must allow longer than the helper's own budget for that scope, or a
// slow report that the helper would still deliver is cancelled by the broker
// (zfs.status had 20 s against the helper's 3 × 10 s command sequence).
func TestStorageHelperOpsOutlastTheHelper(t *testing.T) {
	reg := ops.Registry()
	for name, scope := range map[string]string{"storage.probe": "", "zfs.status": storagetools.ScopeZFS, "btrfs.status": storagetools.ScopeBtrfs, "mdadm.status": storagetools.ScopeMd, "lvm.status": storagetools.ScopeLVM} {
		op, ok := reg[name]
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if budget := storagetools.HelperBudget(scope); op.Timeout <= budget {
			t.Errorf("%s: broker timeout %v does not exceed the helper budget %v", name, op.Timeout, budget)
		}
	}
}
