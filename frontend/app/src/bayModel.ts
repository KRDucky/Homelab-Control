// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import type { CellKind, Chassis, LayoutCell } from './api';
import type { StorageNode } from './storageModel';

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
