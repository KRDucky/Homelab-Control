// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

export interface StorageEvidence {
  source: string;
  confidence: string;
}
export interface StorageIdentifier {
  kind: string;
  value: string;
  source: string;
  ambiguous: boolean;
  /** "controller" for an NVMe controller serial shared by its namespaces. */
  scope?: 'device' | 'controller';
}
/** Raw Linux md sysfs observations; never summarized as healthy. */
export interface MDState {
  level: string;
  array_state?: string;
  raid_disks?: number;
  degraded?: number;
  sync_action?: string;
  sync_completed?: string;
  mismatch_count?: number;
  members: { device_number?: string; slot?: string; state?: string }[];
  source: string;
}
export interface StorageNode {
  id: string;
  kind: string;
  name: string;
  persistent_id?: string;
  identifiers?: StorageIdentifier[];
  identity_status: 'observed' | 'ambiguous' | 'unavailable' | 'snapshot-local';
  device_number?: string;
  disk_seq?: string;
  size_bytes?: number;
  filesystem_type?: string;
  signature_uuid?: string;
  mountpoint?: string;
  mount_root?: string;
  mount_view?: 'service' | 'host';
  md?: MDState;
  evidence: StorageEvidence[];
  notes?: string[];
}
export interface StorageEdge {
  from: string;
  to: string;
  kind: string;
  evidence: StorageEvidence;
}
export interface LVMVolume {
  uuid: string;
  name: string;
  vg_uuid: string;
  vg_name: string;
  parent?: string;
  size_bytes?: number;
  attributes?: string;
  health: string;
  sync_action?: string;
  sync_percent?: number;
  cache_total_blocks?: number;
  cache_used_blocks?: number;
  cache_dirty_blocks?: number;
  origin_uuid?: string;
  pool_uuid?: string;
  data_uuid?: string;
  metadata_uuid?: string;
  kernel_join?: 'joined' | 'no-kernel-node' | 'ambiguous' | 'not-checked';
  segments: {
    type: string;
    start_bytes?: number;
    size_bytes?: number;
    cache_mode?: string;
    cache_policy?: string;
    devices?: string;
    metadata_devices?: string;
  }[];
}
export interface PhysicalVolume {
  uuid: string;
  name?: string;
  vg_uuid?: string;
  vg_name?: string;
  size_bytes?: number;
  device_size_bytes?: number;
  device_number?: string;
  attributes?: string;
  missing: boolean;
  missing_state: 'present' | 'missing' | 'missing-from-attr' | 'not-reported';
  ambiguous: boolean;
  device_link?: PVDeviceLink;
  device_link_reason?: string;
  segments: { start_extent?: number; size_extents?: number; lv_uuid?: string }[];
}
export type PVDeviceLink = 'verified' | 'provisional' | 'withheld' | 'no-device';

// How strongly a PV is tied to a disk. Only "verified" means corroborated by an
// on-disk signature and a later unchanged observation.
export function pvDeviceLinkText(pv: PhysicalVolume): string {
  const reason = pv.device_link_reason ? ` — ${pv.device_link_reason}` : '';
  switch (pv.device_link) {
    case 'verified':
      return 'Verified (signature and revalidation)';
    case 'provisional':
      return `Provisional, not verified${reason}`;
    case 'withheld':
      return `Not shown — observations disagree${reason}`;
    case 'no-device':
      return 'No device reported';
    default:
      return 'Not checked';
  }
}
export type LVMReportMode = 'metadata-only' | 'live';
export interface StorageInventory {
  collected_at: string;
  read_only: true;
  snapshot_persisted: boolean;
  snapshot_id?: string;
  graph: {
    boot_id?: string;
    lvm?: { mode: LVMReportMode; source: string; volumes: LVMVolume[]; physical_volumes: PhysicalVolume[] };
    nodes: StorageNode[];
    edges: StorageEdge[];
    claims: { node: string; technology: string; evidence: StorageEvidence }[];
    status: 'ok' | 'partial' | 'unavailable';
    notes: string[];
  };
}
export type DriveLabelPreference = 'serial-id' | 'wwn';
export const DRIVE_LABEL_KEY = 'hc.storage.drive-label.v1';

// Identifier kinds in display order. Only "wwn" (NAA/EUI designators) is a
// world-wide name; vendor-derived wwid forms are shown only as fallbacks.
const LABEL_ORDER: Record<DriveLabelPreference, { primary: string[]; order: string[] }> = {
  'serial-id': {
    primary: ['serial', 'controller-serial', 'by-id'],
    order: [
      'serial',
      'controller-serial',
      'by-id',
      'wwn',
      'nvme-uuid',
      't10-vendor-id',
      'nvme-vendor-id',
      'wwid-unclassified',
      'partuuid',
      'dm-uuid',
      'md-uuid',
    ],
  },
  wwn: {
    primary: ['wwn'],
    order: [
      'wwn',
      'nvme-uuid',
      'serial',
      'controller-serial',
      'by-id',
      't10-vendor-id',
      'nvme-vendor-id',
      'wwid-unclassified',
      'partuuid',
      'dm-uuid',
      'md-uuid',
    ],
  },
};

/** Display choice only: persistent associations never depend on this label. */
export function driveLabel(node: StorageNode, preference: DriveLabelPreference) {
  if (!node.device_number) return { value: node.name, kind: node.kind, warning: '' };
  const { primary, order } = LABEL_ORDER[preference];
  const identifiers = node.identifiers ?? [];
  const duplicate = identifiers.some((id) => primary.includes(id.kind) && id.ambiguous);
  for (const kind of order) {
    const id = identifiers.find((item) => item.kind === kind && !item.ambiguous && item.value);
    if (id) {
      return {
        value: id.value,
        kind,
        warning: duplicate
          ? 'Preferred identifier is duplicated; verify the alternative identifiers.'
          : !primary.includes(kind)
            ? 'Preferred identifier unavailable; showing an alternative.'
            : '',
      };
    }
  }
  return {
    value: node.name,
    kind: 'volatile name',
    warning: 'No unique persistent identifier. Do not use this name for drive mapping.',
  };
}

export function savedDriveLabel(): DriveLabelPreference {
  try {
    return localStorage.getItem(DRIVE_LABEL_KEY) === 'wwn' ? 'wwn' : 'serial-id';
  } catch {
    return 'serial-id';
  }
}
