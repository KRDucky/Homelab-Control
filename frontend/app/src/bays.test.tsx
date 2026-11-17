// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { Chassis, DriveLayout, LayoutCell } from './api';
import { assignable, bayState, redactedLayout, redactIdentity, unassignedDrives } from './bayModel';
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
      version: 1,
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
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({
      persistent_id: 'wwn:naa.5000000000000f01',
      version: 1,
    });
    expect((put?.[1]?.headers as Record<string, string>)['X-CSRF-Token']).toBe('example-csrf');
    await waitFor(() => expect(screen.getByRole('button', { name: /Bay 2: Present/ })).toBeTruthy());
    expect(screen.getByText(/Manual, unverified — stated by admin/)).toBeTruthy();
  });

  it('unassigns an occupied bay and never offers to overwrite it', async () => {
    const bay: LayoutCell = {
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
    const nas: Chassis = {
      id: 7,
      name: 'Example NAS',
      kind: 'server',
      rows: 1,
      cols: 1,
      version: 1,
      created_at: '2026-11-06T00:00:00Z',
      updated_at: '2026-11-06T00:00:00Z',
      cells: [bay],
    };
    const before: DriveLayout = { chassis: [nas] };
    const after: DriveLayout = {
      chassis: [{ ...nas, cells: [{ row: 0, col: 0, kind: 'hdd-3.5', label: 'Bay 1' }] }],
    };
    const fetchMock = vi.fn(
      async (_url: string, init?: RequestInit) =>
        new Response(JSON.stringify(init?.method === 'DELETE' ? after : before), {
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
    const occupied = await screen.findByRole('button', { name: /Bay 1: Present/ });
    fireEvent.click(occupied);
    // An occupied bay exposes unassign, never a second assign (no overwrite).
    expect(screen.getByRole('button', { name: 'Unassign drive' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Assign drive' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Unassign drive' }));
    await screen.findByText('Assignment removed.');
    const del = fetchMock.mock.calls.find(([, init]) => init?.method === 'DELETE');
    expect(del?.[0]).toBe('/api/v1/storage/layout/chassis/7/bays/0/0/assignment?version=1');
    expect((del?.[1]?.headers as Record<string, string>)['X-CSRF-Token']).toBe('example-csrf');
  });

  it('selects a bay with arrow keys so assignment is keyboard-reachable', async () => {
    const nas: Chassis = {
      id: 7,
      name: 'Example NAS',
      kind: 'server',
      rows: 1,
      cols: 2,
      version: 1,
      created_at: '2026-11-06T00:00:00Z',
      updated_at: '2026-11-06T00:00:00Z',
      cells: [
        { row: 0, col: 0, kind: 'hdd-3.5', label: 'Bay 1' },
        { row: 0, col: 1, kind: 'hdd-3.5', label: 'Bay 2' },
      ],
    };
    const layout: DriveLayout = { chassis: [nas] };
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify(layout), {
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
    first.focus();
    fireEvent.keyDown(first, { key: 'ArrowRight' });
    // Arrow keys both focus and select the bay, so the assign control is reachable.
    expect(await screen.findByLabelText('Drive to assign')).toBeTruthy();
    expect(document.activeElement?.getAttribute('data-bay')).toBe('0-1');
  });

  it('re-syncs to the server after a conflicted layout change', async () => {
    const assignedCell: LayoutCell = {
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
    const nas: Chassis = {
      id: 7,
      name: 'Example NAS',
      kind: 'server',
      rows: 1,
      cols: 1,
      version: 1,
      created_at: '2026-11-06T00:00:00Z',
      updated_at: '2026-11-06T00:00:00Z',
      cells: [assignedCell],
    };
    const v1: DriveLayout = { chassis: [nas] };
    const v2: DriveLayout = { chassis: [{ ...nas, name: 'Example NAS (renamed elsewhere)' }] };
    let gets = 0;
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') {
        return new Response(
          JSON.stringify({ error: 'layout_conflict', message: 'Refused: this would drop an assignment.' }),
          {
            status: 409,
            headers: { 'Content-Type': 'application/json' },
          },
        );
      }
      gets += 1;
      return new Response(JSON.stringify(gets === 1 ? v1 : v2), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
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
    await screen.findByRole('button', { name: /Bay 1: Present/ });
    fireEvent.click(screen.getByRole('button', { name: 'Delete chassis Example NAS' }));
    await screen.findByText(/Refused:/);
    // After the conflict the page drops the stale snapshot and re-syncs.
    await screen.findByRole('heading', { name: /renamed elsewhere/ });
  });

  it('assigns a drive by drag and drop onto a vacant bay', async () => {
    const bay: LayoutCell = { row: 0, col: 0, kind: 'hdd-3.5', label: 'Bay 1' };
    const nas: Chassis = {
      id: 7,
      name: 'Example NAS',
      kind: 'server',
      rows: 1,
      cols: 1,
      version: 1,
      created_at: '2026-11-06T00:00:00Z',
      updated_at: '2026-11-06T00:00:00Z',
      cells: [bay],
    };
    const before: DriveLayout = { chassis: [nas] };
    const after: DriveLayout = {
      chassis: [
        {
          ...nas,
          cells: [
            {
              ...bay,
              assignment: {
                identity: 'wwn:naa.5000000000000f01',
                provenance: 'manual',
                confidence: 'unverified',
                assigned_by: 'admin',
                assigned_at: '2026-11-06T00:00:00Z',
              },
            },
          ],
        },
      ],
    };
    const fetchMock = vi.fn(
      async (_url: string, init?: RequestInit) =>
        new Response(JSON.stringify(init?.method === 'PUT' ? after : before), {
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
    const bayBtn = await screen.findByRole('button', { name: /Bay 1: No drive assigned/ });
    const drive = await screen.findByText('naa.5000000000000f01');
    const dataTransfer = {
      effectAllowed: '',
      setData: vi.fn(),
      getData: vi.fn(() => 'wwn:naa.5000000000000f01'),
    };
    fireEvent.dragStart(drive, { dataTransfer });
    fireEvent.dragOver(bayBtn, { dataTransfer });
    fireEvent.drop(bayBtn, { dataTransfer });
    await screen.findByText('Drive assigned (manual, unverified).');
    const put = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT');
    expect(put?.[0]).toBe('/api/v1/storage/layout/chassis/7/bays/0/0/assignment');
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({
      persistent_id: 'wwn:naa.5000000000000f01',
      version: 1,
    });
  });
});

describe('assignment guard', () => {
  it('accepts only whole disks with an observed assignable tier', () => {
    expect(assignable(disk({}))).toBe(true);
    expect(assignable(disk({ persistent_id: 'serial:S123' }))).toBe(true);
    expect(assignable(disk({ persistent_id: 'nvme-uuid:abc' }))).toBe(true);
    expect(assignable(disk({ kind: 'part' }))).toBe(false);
    expect(assignable(disk({ identity_status: 'ambiguous' }))).toBe(false);
    expect(assignable(disk({ persistent_id: 'by-id:foo' }))).toBe(false);
    expect(assignable(disk({ persistent_id: '' }))).toBe(false);
    expect(assignable(disk({ persistent_id: 'wwn:' }))).toBe(false);
  });

  it('splits unassigned disks into eligible and ineligible', () => {
    const good = disk({});
    const weak = disk({ id: 'n-weak', name: 'sdb', persistent_id: 'by-id:foo' });
    const part = disk({ id: 'n-part', name: 'sda1', kind: 'part', persistent_id: 'wwn:naa.x' });
    const { eligible, ineligible } = unassignedDrives([good, weak, part], []);
    expect(eligible.map((n) => n.id)).toEqual(['n-a']);
    expect(ineligible.map((n) => n.id)).toEqual(['n-weak']);
  });
});

describe('bay reconciliation states', () => {
  it('reports non-bay, vacant and unobserved cells', () => {
    expect(bayState({ row: 0, col: 0, kind: 'empty', label: '' }, [])).toEqual({ state: 'empty' });
    expect(bayState({ row: 0, col: 0, kind: 'disabled', label: '' }, [])).toEqual({ state: 'disabled' });
    expect(bayState({ row: 0, col: 0, kind: 'reserved', label: '' }, [])).toEqual({ state: 'reserved' });
    expect(bayState({ row: 0, col: 0, kind: 'hdd-3.5', label: '' }, [])).toEqual({ state: 'vacant' });
    expect(bayState(assigned, undefined)).toEqual({ state: 'unknown' });
  });

  it('treats a partition or a duplicated identity as a mismatch', () => {
    const part = disk({ kind: 'part' });
    expect(bayState(assigned, [part]).state).toBe('mismatch');
    const dup = disk({
      identifiers: [{ kind: 'wwn', value: 'naa.5000000000000f01', source: 'fixture', ambiguous: true }],
    });
    expect(bayState(assigned, [dup]).state).toBe('mismatch');
  });
});

describe('export redaction', () => {
  it('redacts identities to their tier only', () => {
    expect(redactIdentity('wwn:naa.5000000000000f01')).toBe('wwn:[redacted]');
    expect(redactIdentity('serial:S123')).toBe('serial:[redacted]');
    expect(redactIdentity('nvme-uuid:abc')).toBe('nvme-uuid:[redacted]');
    expect(redactIdentity('plain')).toBe('[redacted]');
  });

  it('redacts assignment identities in the exported layout', () => {
    const chassis: Chassis = {
      id: 7,
      name: 'Example NAS',
      kind: 'server',
      rows: 1,
      cols: 1,
      version: 1,
      created_at: '2026-11-06T00:00:00Z',
      updated_at: '2026-11-06T00:00:00Z',
      cells: [assigned],
    };
    const out = redactedLayout({ chassis: [chassis] });
    const [c] = out.chassis;
    expect(c?.name).toBe('Example NAS');
    expect(c?.cells[0]?.kind).toBe('hdd-3.5');
    expect(c?.cells[0]?.assignment?.identity).toBe('wwn:[redacted]');
    expect(c?.cells[0]?.assignment?.provenance).toBe('manual');
  });

  it('offers a redacted layout export', async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ chassis: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    );
    vi.stubGlobal('fetch', fetchMock);
    render(
      <DriveBays
        csrf="example-csrf"
        nodes={[]}
        canManage
        preference="wwn"
        onUnauthorized={() => undefined}
      />,
    );
    await screen.findByText('No chassis has been described yet.');
    expect(screen.getByRole('button', { name: 'Export layout (identifiers redacted)' })).toBeTruthy();
  });
});

describe('layout import', () => {
  function fileInput(container: HTMLElement) {
    return container.querySelector('input[type="file"]') as HTMLInputElement;
  }

  it('imports a chassis array with CSRF and discards identities server-side', async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      void init; // the read and the import both answer empty; assertions check the request shape
      return new Response(JSON.stringify({ chassis: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    vi.stubGlobal('fetch', fetchMock);
    const { container } = render(
      <DriveBays
        csrf="example-csrf"
        nodes={[]}
        canManage
        preference="wwn"
        onUnauthorized={() => undefined}
      />,
    );
    await screen.findByText('No chassis has been described yet.');
    const payload = { chassis: [{ name: 'Rack', kind: 'server', rows: 2, cols: 3, cells: [] }] };
    fireEvent.change(fileInput(container), {
      target: { files: [new File([JSON.stringify(payload)], 'layout.json', { type: 'application/json' })] },
    });
    await screen.findByText(/Layout imported/);
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST');
    expect(post?.[0]).toBe('/api/v1/storage/layout/import');
    expect(JSON.parse(String(post?.[1]?.body))).toEqual(payload);
    expect((post?.[1]?.headers as Record<string, string>)['X-CSRF-Token']).toBe('example-csrf');
  });

  it('refuses an oversized import file before reading it', async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ chassis: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const { container } = render(
      <DriveBays
        csrf="example-csrf"
        nodes={[]}
        canManage
        preference="wwn"
        onUnauthorized={() => undefined}
      />,
    );
    await screen.findByText('No chassis has been described yet.');
    const big = new File([new Uint8Array(8 * 1024 * 1024 + 1)], 'big.json', { type: 'application/json' });
    fireEvent.change(fileInput(container), { target: { files: [big] } });
    await screen.findByText(/exceeds the 8 MiB limit/);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('refuses a non-JSON import file', async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ chassis: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const { container } = render(
      <DriveBays
        csrf="example-csrf"
        nodes={[]}
        canManage
        preference="wwn"
        onUnauthorized={() => undefined}
      />,
    );
    await screen.findByText('No chassis has been described yet.');
    fireEvent.change(fileInput(container), {
      target: { files: [new File(['not json'], 'bad.json', { type: 'application/json' })] },
    });
    await screen.findByText(/not valid JSON/);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
