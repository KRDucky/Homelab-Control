// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import type { CellKind, Chassis, LayoutCell } from './api';
import type { HardwareLink, SlotMatch, StorageHardware, StorageNode } from './storageModel';

// Bay reconciliation (M2-5 Stage A). Stored assignments are joined with the
// current audited observation by persistent identity only: never by kernel
// name, device number or snapshot node ID. A missing drive keeps its stored
// assignment; nothing is transferred to another drive.

export const BAY_KINDS: ReadonlySet<CellKind> = new Set(['hdd-3.5', 'ssd-2.5', 'nvme-m2', 'nvme-u2', 'tray']);
export const CELL_KINDS: CellKind[] = [
  'hdd-3.5',
  'ssd-2.5',
  'nvme-m2',
  'nvme-u2',
  'tray',
  'empty',
  'disabled',
  'reserved',
];
const ASSIGNABLE_PREFIXES = ['wwn:', 'nvme-uuid:', 'serial:'];

export type BayState =
  'present' | 'missing' | 'mismatch' | 'vacant' | 'unknown' | 'empty' | 'disabled' | 'reserved';

export const BAY_STATE_TEXT: Record<BayState, string> = {
  present: 'Present',
  missing: 'Assigned drive missing',
  mismatch: 'Identity mismatch — not safely matched',
  vacant: 'No drive assigned',
  unknown: 'Unknown — no current observation',
  empty: 'Empty position',
  disabled: 'Disabled',
  reserved: 'Reserved',
};

function carries(node: StorageNode, identity: string) {
  return (
    node.persistent_id === identity ||
    (node.identifiers ?? []).some((id) => `${id.kind}:${id.value}` === identity)
  );
}

/** nodes undefined means there is no current observation to compare with. */
export function bayState(
  cell: LayoutCell,
  nodes: StorageNode[] | undefined,
): { state: BayState; node?: StorageNode } {
  if (!BAY_KINDS.has(cell.kind)) return { state: cell.kind as BayState };
  if (!cell.assignment) return { state: 'vacant' };
  if (!nodes) return { state: 'unknown' };
  const identity = cell.assignment.identity;
  const matches = nodes.filter((node) => carries(node, identity));
  if (matches.length === 0) return { state: 'missing' };
  const node = matches[0];
  if (!node) return { state: 'missing' };
  const duplicated = (node.identifiers ?? []).some(
    (id) => `${id.kind}:${id.value}` === identity && id.ambiguous,
  );
  if (
    matches.length === 1 &&
    node.kind === 'block' &&
    node.identity_status === 'observed' &&
    node.persistent_id === identity &&
    !duplicated
  )
    return { state: 'present', node };
  return matches.length === 1 ? { state: 'mismatch', node } : { state: 'mismatch' };
}

export function assignable(node: StorageNode) {
  const id = node.persistent_id ?? '';
  return (
    node.kind === 'block' &&
    node.identity_status === 'observed' &&
    ASSIGNABLE_PREFIXES.some((p) => id.startsWith(p) && id.length > p.length)
  );
}

/** Whole disks not assigned to any bay, split by whether assignment is safe. */
export function unassignedDrives(nodes: StorageNode[], chassis: Chassis[]) {
  const assigned = new Set(
    chassis.flatMap((c) => c.cells.flatMap((cell) => cell.assignment?.identity ?? [])),
  );
  const disks = nodes.filter((node) => node.kind === 'block' && !!node.device_number);
  const free = disks.filter((node) => !(node.persistent_id && assigned.has(node.persistent_id)));
  return { eligible: free.filter(assignable), ineligible: free.filter((node) => !assignable(node)) };
}

/** A layout safe to export: geometry (chassis name/kind/rows/cols, cell kind
 * and label) plus an `assigned` boolean only. Identities, the administrator
 * account name, timestamps and provenance/confidence are never exported. */
export function redactedLayout(layout: { chassis: Chassis[] }) {
  return {
    chassis: layout.chassis.map((c) => ({
      name: c.name,
      kind: c.kind,
      rows: c.rows,
      cols: c.cols,
      cells: c.cells.map((cell) => ({
        row: cell.row,
        col: cell.col,
        kind: cell.kind,
        label: cell.label,
        assigned: cell.assignment !== undefined,
      })),
    })),
  };
}

// M2-5 Stage B: location badge. A chassis/bay is shown for a drive only when
// reconciliation is `present` (exactly one observed whole disk carries the
// stored identity). Missing, mismatch, unknown and unassigned drives get no
// bay. The badge is the administrator's manual, unverified statement.

export interface LocationBadge {
  chassis: string;
  bay: string;
}

/** The bay label, or its grid position when the administrator left it blank. */
export function bayLabel(cell: LayoutCell) {
  return cell.label.trim() || `Row ${cell.row + 1}, Column ${cell.col + 1}`;
}

/** Badges by snapshot-local node ID, for the current observation only. */
export function locationBadges(chassis: Chassis[], nodes: StorageNode[] | undefined) {
  const badges = new Map<string, LocationBadge>();
  const conflicted = new Set<string>();
  if (!nodes) return badges;
  for (const c of chassis) {
    for (const cell of c.cells) {
      const { state, node } = bayState(cell, nodes);
      if (state !== 'present' || !node) continue;
      // UNIQUE(identity) makes two bays per drive impossible; never guess if it happens.
      if (badges.has(node.id)) conflicted.add(node.id);
      badges.set(node.id, { chassis: c.name, bay: bayLabel(cell) });
    }
  }
  for (const id of conflicted) badges.delete(id);
  return badges;
}

export function hardwareLink(
  hardware: StorageHardware | undefined,
  nodeID: string,
): HardwareLink | undefined {
  return hardware?.links.find((link) => link.node === nodeID);
}

/** Enclosure slot evidence in words; never called a bay. */
export function slotText(link: { enclosure_id?: string; element?: string; slot?: number }) {
  if (!link.enclosure_id) return undefined;
  const where = link.slot === undefined ? `element ${link.element ?? '?'}` : `slot ${link.slot}`;
  return `Enclosure ${link.enclosure_id}, ${where}`;
}

export const SLOT_MATCH_TEXT: Record<SlotMatch, string> = {
  matched: 'Drive identified',
  empty: 'No device reported',
  'not-a-disk': 'Device is not a disk',
  changed: 'Device changed during discovery — refresh',
  unidentified: 'Drive has no unambiguous identity',
  ambiguous: 'Ambiguous (seen more than once) — not matched',
  'no-enclosure-id': 'Enclosure reports no identifier — not matched',
};
