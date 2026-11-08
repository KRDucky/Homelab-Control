// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { Chassis, DriveLayout, LayoutCell } from './api';
import { bayState, unassignedDrives } from './bayModel';
import { DriveBays } from './pages/DriveBays';
import type { StorageNode } from './storageModel';

// Synthetic fixtures only (labelled examples, not host data).
const disk = (over: Partial<StorageNode>): StorageNode => ({
  id: 'n-a',
  kind: 'block',
  name: 'sda',
  device_number: '8:0',
  persistent_id: 'wwn:naa.5000000000000f01',
  identity_status: 'observed',
  evidence: [{ source: 'fixture', confidence: 'observed' }],
  identifiers: [{ kind: 'wwn', value: 'naa.5000000000000f01', source: 'fixture', ambiguous: false }],
  ...over,
});
const assigned: LayoutCell = {
  row: 0,
  col: 0,
  kind: 'hdd-3.5',
  label: 'Bay 1',
  assignment: {
    identity: 'wwn:naa.5000000000000f01',
    provenance: 'manual',
    confidence: 'unverified',
    assigned_by: 'admin',
    assigned_at: '2026-11-06T00:00:00Z',
  },
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('bay reconciliation', () => {
  it('keeps a renumbered drive in its bay (identity, not kernel name)', () => {
    const renamed = disk({ id: 'n-other', name: 'sdq', device_number: '65:0' });
    expect(bayState(assigned, [renamed])).toEqual({ state: 'present', node: renamed });
  });
  it('never transfers a missing drive’s bay to a replacement', () => {
    const replacement = disk({
      id: 'n-new',
      persistent_id: 'wwn:naa.5000000000000f02',
      identifiers: [{ kind: 'wwn', value: 'naa.5000000000000f02', source: 'fixture', ambiguous: false }],
    });
    const chassis = [{ cells: [assigned] } as Chassis];
    expect(bayState(assigned, [replacement]).state).toBe('missing');
    expect(unassignedDrives([replacement], chassis).eligible).toEqual([replacement]);
  });
  it('treats a duplicated identity as a mismatch and no observation as unknown', () => {
    const a = disk({ persistent_id: '', identity_status: 'ambiguous' });
    const b = disk({ id: 'n-b', name: 'sdb', persistent_id: '', identity_status: 'ambiguous' });
    expect(bayState(assigned, [a, b]).state).toBe('mismatch');
    expect(bayState(assigned, undefined).state).toBe('unknown');
    expect(unassignedDrives([a], []).ineligible).toEqual([a]);
  });
});

describe('Drives & bays panel', () => {
  it('selects a bay with the keyboard and assigns a drive by persistent identity', async () => {
    const bay1: LayoutCell = { row: 0, col: 0, kind: 'hdd-3.5', label: 'Bay 1' };
    const nas: Chassis = {
      id: 7,
      name: 'Example NAS',
      kind: 'server',
      rows: 1,
      cols: 2,
      created_at: '2026-11-06T00:00:00Z',
      updated_at: '2026-11-06T00:00:00Z',
      cells: [bay1, { row: 0, col: 1, kind: 'hdd-3.5', label: 'Bay 2' }],
    };
    const empty: DriveLayout = { chassis: [nas] };
    const after: DriveLayout = {
      chassis: [{ ...nas, cells: [bay1, { ...assigned, col: 1, label: 'Bay 2' }] }],
    };
    const fetchMock = vi.fn(
      async (_url: string, init?: RequestInit) =>
        new Response(JSON.stringify(init?.method === 'PUT' ? after : empty), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    );
    vi.stubGlobal('fetch', fetchMock);
    render(
      <DriveBays
        csrf="example-csrf"
        nodes={[disk({})]}
        canManage
        preference="wwn"
        onUnauthorized={() => undefined}
      />,
    );
    const first = await screen.findByRole('button', { name: /Bay 1: No drive assigned/ });
    // The layout read is audited server-side and requires the session CSRF token.
    const read = fetchMock.mock.calls.find(
      ([path, init]) => path === '/api/v1/storage/layout' && !init?.method,
    );
    expect((read?.[1]?.headers as Record<string, string>)['X-CSRF-Token']).toBe('example-csrf');
    expect(first.tabIndex).toBe(0);
    first.focus();
    fireEvent.keyDown(first, { key: 'ArrowRight' });
    const second = screen.getByRole('button', { name: /Bay 2: No drive assigned/ });
    expect(document.activeElement).toBe(second);
    fireEvent.click(second);
    fireEvent.change(screen.getByLabelText('Drive to assign'), {
      target: { value: 'wwn:naa.5000000000000f01' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Assign drive' }));
    await screen.findByText('Drive assigned (manual, unverified).');
    const put = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT');
    expect(put?.[0]).toBe('/api/v1/storage/layout/chassis/7/bays/0/1/assignment');
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({ persistent_id: 'wwn:naa.5000000000000f01' });
    expect((put?.[1]?.headers as Record<string, string>)['X-CSRF-Token']).toBe('example-csrf');
    await waitFor(() => expect(screen.getByRole('button', { name: /Bay 2: Present/ })).toBeTruthy());
    expect(screen.getByText(/Manual, unverified — stated by admin/)).toBeTruthy();
  });
});
