// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagecap

// IdentityKind is how a disk is named. Persist/write uses the strongest
// tier. Kernel names (/dev/sdX) are display-only. by-id is discovery, not
// assignment.
type IdentityKind string

const (
	IdentityWWN      IdentityKind = "wwn"
	IdentityNVMeUUID IdentityKind = "nvme-uuid"
	IdentitySerial   IdentityKind = "serial"
	IdentityByID     IdentityKind = "by-id"
	IdentityKernel   IdentityKind = "kernel"
)

// Strongest reports whether k is a persist/write identity.
func (k IdentityKind) Strongest() bool {
	return k == IdentityWWN || k == IdentityNVMeUUID || k == IdentitySerial
}

// ExtraConfirm is a SHARED-02 extra-confirmation kind. Presence of a kind
// does not run the operation.
type ExtraConfirm string

const (
	ConfirmFormat              ExtraConfirm = "format"
	ConfirmMetadataWipe        ExtraConfirm = "metadata-wipe"
	ConfirmForcedImport        ExtraConfirm = "forced-import"
	ConfirmDegradedMount       ExtraConfirm = "degraded-mount"
	ConfirmSnapRAIDMissingSync ExtraConfirm = "snapraid-missing-disk-sync"
	ConfirmRepairNoBackup      ExtraConfirm = "repair-no-backup"
	ConfirmLVMWriteback        ExtraConfirm = "lvm-writeback"
	ConfirmParityReshape       ExtraConfirm = "parity-reshape"
	ConfirmPoolRemove          ExtraConfirm = "pool-remove"
	ConfirmFSShrink            ExtraConfirm = "fs-shrink"
	ConfirmXFSRepairL          ExtraConfirm = "xfs-repair-L"
	ConfirmForce               ExtraConfirm = "force"
)

// DefaultReadOnly documents the P0 default: discovery never formats.
const DefaultReadOnly = true
