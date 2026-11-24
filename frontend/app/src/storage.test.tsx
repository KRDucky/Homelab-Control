// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { Storage } from './pages/Storage';
import { DRIVE_LABEL_KEY, driveLabel, type StorageInventory, type StorageNode } from './storageModel';

// Synthetic fixture only. Production Storage never supplies placeholder data.
const observation: StorageInventory = {
  collected_at: '2026-11-05T20:00:00Z',
  read_only: true,
  snapshot_persisted: false,
  graph: {
    status: 'partial',
    notes: ['Fixture: signatures not probed.'],
    nodes: [
      {
        id: 'disk-A',
        kind: 'disk',
        name: 'sda',
        device_number: '8:0',
        persistent_id: 'wwn:WWN-A',
        identity_status: 'observed',
        size_bytes: 1048576,
        evidence: [{ source: 'fixture sysfs', confidence: 'observed' }],
        identifiers: [
          { kind: 'serial', value: 'SERIAL-A', source: 'fixture', ambiguous: false },
          { kind: 'wwn', value: 'WWN-A', source: 'fixture', ambiguous: false },
        ],
      },
      {
        id: 'mount-A',
        kind: 'mount',
        name: '/fixture-data',
        mountpoint: '/fixture-data',
        filesystem_type: 'ext4',
        identity_status: 'snapshot-local',
        evidence: [{ source: 'fixture mountinfo', confidence: 'observed' }],
      },
    ],
    edges: [
      {
        from: 'disk-A',
        to: 'mount-A',
        kind: 'mounted-as',
        evidence: { source: 'fixture mountinfo', confidence: 'observed' },
      },
    ],
    claims: [],
  },
};
beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (url: string) =>
        new Response(JSON.stringify(url.includes('/storage/layout') ? { chassis: [] } : observation), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    ),
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('Storage read-only interface', () => {
  it('defaults to serial, switches to WWN and preserves corroborating details', async () => {
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    const inspect = await screen.findByRole('button', { name: 'Inspect SERIAL-A' });
    expect(screen.getAllByText('Not established').length).toBeGreaterThan(0);
    fireEvent.click(inspect);
    expect(screen.getByRole('heading', { name: 'Selected resource' })).toBeTruthy();
    expect(screen.getByText('None — no manual bay assignment is present for this drive')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Identify drives by'), { target: { value: 'wwn' } });
    expect(screen.getByRole('button', { name: 'Inspect WWN-A' })).toBeTruthy();
    expect(localStorage.getItem(DRIVE_LABEL_KEY)).toBe('wwn');
    const table = screen.getByRole('table', { name: 'Relationships of selected resource' });
    expect(within(table).getByText('mounted-as')).toBeTruthy();
    expect(within(table).getByText('/fixture-data')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /format|wipe|create raid/i })).toBeNull();
  });
  it('sends the session CSRF token on the audited storage read', async () => {
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    await screen.findByRole('button', { name: 'Inspect SERIAL-A' });
    const [path, init] = vi.mocked(fetch).mock.calls[0] as [string, RequestInit];
    expect(path).toBe('/api/v1/storage');
    expect(new Headers(init.headers).get('X-CSRF-Token')).toBe('example-csrf');
  });
  it('filters by alternative identifiers without fabricated empty rows', async () => {
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    await screen.findByRole('button', { name: 'Inspect SERIAL-A' });
    fireEvent.change(screen.getByLabelText('Find a resource'), { target: { value: 'WWN-A' } });
    expect(screen.getByRole('button', { name: 'Inspect SERIAL-A' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Inspect /fixture-data' })).toBeNull();
    fireEvent.change(screen.getByLabelText('Find a resource'), { target: { value: 'nonexistent' } });
    expect(screen.getByText('No matching records.')).toBeTruthy();
  });
  it('shows authorization errors without displaying identifiers', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response(JSON.stringify({ error: 'forbidden', message: 'Missing storage.read permission.' }), {
            status: 403,
            headers: { 'Content-Type': 'application/json' },
          }),
      ),
    );
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('does not have permission'));
    expect(screen.queryByText('SERIAL-A')).toBeNull();
  });
  it('refuses duplicate preferred identifiers as drive labels', () => {
    const first = observation.graph.nodes[0];
    if (!first) throw new Error('Missing labelled disk fixture');
    const node = {
      ...first,
      identifiers: (first.identifiers ?? []).map((id) => ({
        ...id,
        ambiguous: id.kind === 'serial',
      })),
    };
    const label = driveLabel(node, 'serial-id');
    expect(label.value).toBe('WWN-A');
    expect(label.warning).toContain('duplicated');
  });
  it('does not carry a selection to a reused device number in a fresh observation', async () => {
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Inspect SERIAL-A' }));
    expect(screen.getByRole('heading', { name: 'Selected resource' })).toBeTruthy();
    const newer: StorageInventory = { ...observation, collected_at: '2026-11-05T20:01:00Z' };
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response(JSON.stringify(newer), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
      ),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Refresh inventory' }));
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Selected resource' })).toBeNull());
  });
});

describe('partition dry run', () => {
  it('draws an observed layout and posts a dry run that does not execute', async () => {
    const disk = observation.graph.nodes[0];
    const mount = observation.graph.nodes[1];
    if (!disk || !mount) throw new Error('Missing storage fixture');
    const laidOut: StorageInventory = {
      ...observation,
      graph: {
        ...observation.graph,
        nodes: [
          { ...disk, size_bytes: 4 * 1048576 },
          mount,
          {
            id: 'part-1',
            kind: 'partition',
            name: 'sda1',
            identity_status: 'unavailable',
            start_bytes: 1048576,
            size_bytes: 1048576,
            filesystem_type: 'ext4',
            evidence: [{ source: 'fixture sysfs', confidence: 'observed' }],
          },
        ],
        edges: [
          ...observation.graph.edges,
          {
            from: 'disk-A',
            to: 'part-1',
            kind: 'contains',
            evidence: { source: 'fixture sysfs', confidence: 'observed' },
          },
        ],
      },
    };
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const path = String(url);
        if (path.includes('/storage/plan/dry-run')) {
          const body = JSON.parse(String(init?.body)) as {
            disk_bytes: number;
            parts: { start_bytes: number; size_bytes: number; filesystem: string }[];
          };
          expect(body).toEqual({
            disk_bytes: 4 * 1048576,
            parts: [{ start_bytes: 1048576, size_bytes: 1048576, filesystem: 'ext4' }],
          });
          expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('example-csrf');
          return new Response(
            JSON.stringify({
              ok: true,
              errors: [],
              executes: false,
              steps: [
                {
                  action: 'format',
                  detail:
                    'slice 1 would be formatted as ext4. The only live format path is ext4.format on a whole by-id disk, not this slice. This dry run does not partition or format.',
                  destructive: true,
                  executable: false,
                },
              ],
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          );
        }
        const payload = path.includes('/storage/layout') ? { chassis: [] } : laidOut;
        return new Response(JSON.stringify(payload), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    );
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Inspect SERIAL-A' }));
    expect(
      screen.getByRole('img', { name: 'Partition layout: sda1 from 1048576 bytes, 1048576 bytes' }),
    ).toBeTruthy();
    expect(screen.getByText('Dry run only. Nothing is partitioned or formatted.')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /apply/i })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Validate observed layout' }));
    expect(await screen.findByText(/ext4.format on a whole by-id disk, not this slice/)).toBeTruthy();
  });

  it('lists a partition whose start was not observed and does not validate it', async () => {
    const partial: StorageInventory = {
      ...observation,
      graph: {
        ...observation.graph,
        nodes: [
          ...observation.graph.nodes,
          {
            id: 'part-1',
            kind: 'partition',
            name: 'sda1',
            identity_status: 'unavailable',
            size_bytes: 1048576,
            evidence: [{ source: 'fixture sysfs', confidence: 'observed' }],
          },
        ],
        edges: [
          ...observation.graph.edges,
          {
            from: 'disk-A',
            to: 'part-1',
            kind: 'contains',
            evidence: { source: 'fixture sysfs', confidence: 'observed' },
          },
        ],
      },
    };
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        const payload = String(url).includes('/storage/layout') ? { chassis: [] } : partial;
        return new Response(JSON.stringify(payload), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    );
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Inspect SERIAL-A' }));
    expect(screen.getByText(/start not observed/)).toBeTruthy();
    expect(screen.queryByRole('img', { name: /Partition layout/ })).toBeNull();
    const validate = screen.getByRole('button', { name: 'Validate observed layout' }) as HTMLButtonElement;
    expect(validate.disabled).toBe(true);
  });

  it('sends an unobserved filesystem as unknown', async () => {
    const disk = observation.graph.nodes[0];
    if (!disk) throw new Error('Missing storage fixture');
    const laidOut: StorageInventory = {
      ...observation,
      graph: {
        ...observation.graph,
        nodes: [
          { ...disk, size_bytes: 4 * 1048576 },
          {
            id: 'part-1',
            kind: 'partition',
            name: 'sda1',
            identity_status: 'unavailable',
            start_bytes: 1048576,
            size_bytes: 1048576,
            filesystem_type: 'ntfs',
            evidence: [{ source: 'fixture sysfs', confidence: 'observed' }],
          },
          {
            id: 'part-2',
            kind: 'partition',
            name: 'sda2',
            identity_status: 'unavailable',
            start_bytes: 2 * 1048576,
            size_bytes: 1048576,
            evidence: [{ source: 'fixture sysfs', confidence: 'observed' }],
          },
        ],
        edges: [
          {
            from: 'disk-A',
            to: 'part-1',
            kind: 'contains',
            evidence: { source: 'fixture sysfs', confidence: 'observed' },
          },
          {
            from: 'disk-A',
            to: 'part-2',
            kind: 'contains',
            evidence: { source: 'fixture sysfs', confidence: 'observed' },
          },
        ],
      },
    };
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const path = String(url);
        if (path.includes('/storage/plan/dry-run')) {
          const body = JSON.parse(String(init?.body)) as {
            parts: { filesystem: string }[];
          };
          expect(body.parts.map((part) => part.filesystem)).toEqual(['unknown', 'unknown']);
          return new Response(
            JSON.stringify({
              ok: true,
              errors: [],
              executes: false,
              steps: [
                {
                  action: 'unknown',
                  detail: 'slice 1 filesystem was not observed. This dry run does not partition or format.',
                  destructive: false,
                  executable: false,
                },
              ],
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          );
        }
        const payload = path.includes('/storage/layout') ? { chassis: [] } : laidOut;
        return new Response(JSON.stringify(payload), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    );
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Inspect SERIAL-A' }));
    fireEvent.click(screen.getByRole('button', { name: 'Validate observed layout' }));
    expect(await screen.findByText(/not observed/)).toBeTruthy();
    expect(screen.queryByText(/stays unformatted/)).toBeNull();
  });
});

describe('drive label policy', () => {
  const base: StorageNode = {
    id: 'block:example',
    kind: 'block',
    name: 'sdx',
    device_number: '8:0',
    identity_status: 'observed',
    evidence: [{ source: 'example fixture', confidence: 'observed' }],
  };
  const id = (kind: string, value: string, ambiguous = false) => ({
    kind,
    value,
    source: 'example',
    ambiguous,
  });
  it('keeps the persistent identity independent of the display preference', () => {
    const node = {
      ...base,
      persistent_id: 'wwn:naa.5000000000000001',
      identifiers: [id('wwn', 'naa.5000000000000001'), id('serial', 'SER-1')],
    };
    expect(driveLabel(node, 'serial-id')).toMatchObject({ value: 'SER-1', warning: '' });
    expect(driveLabel(node, 'wwn')).toMatchObject({ value: 'naa.5000000000000001', warning: '' });
    expect(node.persistent_id).toBe('wwn:naa.5000000000000001');
  });
  it('never presents a vendor-derived wwid as a WWN', () => {
    const node = { ...base, identifiers: [id('t10-vendor-id', 't10.ATA EXAMPLE')] };
    const label = driveLabel(node, 'wwn');
    expect(label.kind).toBe('t10-vendor-id');
    expect(label.warning).toContain('unavailable');
  });
  it('uses an NVMe controller serial as the Serial/ID label without a duplicate warning', () => {
    const node = {
      ...base,
      identifiers: [
        id('wwn', 'eui.0000000000000001'),
        { ...id('controller-serial', 'CTRL-1'), scope: 'controller' as const },
      ],
    };
    expect(driveLabel(node, 'serial-id')).toMatchObject({
      value: 'CTRL-1',
      kind: 'controller-serial',
      warning: '',
    });
  });
  it('falls back to the volatile name with a warning when nothing unique exists', () => {
    const label = driveLabel({ ...base, identifiers: [id('serial', 'CLONE', true)] }, 'serial-id');
    expect(label.kind).toBe('volatile name');
    expect(label.warning).toContain('Do not use');
  });
});

describe('LVM report rendering', () => {
  it('states metadata-only limits and shows PV allocation without inventing health', async () => {
    const withLVM: StorageInventory = {
      ...observation,
      graph: {
        ...observation.graph,
        lvm: {
          mode: 'metadata-only',
          source: 'Example LVM fixture',
          volumes: [
            {
              uuid: 'EX-RAID',
              name: 'data',
              vg_uuid: 'EX-VG',
              vg_name: 'vg',
              health: 'unknown',
              kernel_join: 'no-kernel-node',
              segments: [{ type: 'raid1' }],
            },
          ],
          physical_volumes: [
            {
              uuid: 'EX-PV',
              name: '/dev/example1',
              vg_name: 'vg',
              device_number: '8:1',
              missing: false,
              missing_state: 'present',
              ambiguous: false,
              device_link: 'provisional',
              device_link_reason: 'no on-disk signature observed',
              segments: [{ lv_uuid: 'EX-RAID' }],
            },
            {
              uuid: 'EX-PV-2',
              name: '/dev/example2',
              vg_name: 'vg',
              device_number: '8:17',
              missing: false,
              missing_state: 'not-reported',
              ambiguous: false,
              device_link: 'withheld',
              device_link_reason: 'candidate block device changed between observations',
              segments: [],
            },
          ],
        },
      },
    };
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response(JSON.stringify(withLVM), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
      ),
    );
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    expect(await screen.findByText(/metadata-only mode/)).toBeTruthy();
    const lvs = screen.getByRole('table', { name: 'Observed LVM logical volumes and segments' });
    expect(within(lvs).getByText('unknown')).toBeTruthy();
    expect(within(lvs).getByText('None observed (inactive, internal or layered)')).toBeTruthy();
    const pvs = screen.getByRole('table', { name: 'Observed LVM physical volumes' });
    expect(within(pvs).getByText('data')).toBeTruthy();
    expect(within(pvs).getByText('8:1')).toBeTruthy();
    expect(within(pvs).getByText('Provisional, not verified — no on-disk signature observed')).toBeTruthy();
    expect(
      within(pvs).getByText(
        'Not shown — observations disagree — candidate block device changed between observations',
      ),
    ).toBeTruthy();
    expect(within(pvs).getByText('Not reported')).toBeTruthy();
    expect(within(pvs).queryByText(/^Verified/)).toBeNull();
    expect(screen.queryByText(/healthy/i)).toBeNull();
  });
});

describe('storage probe action', () => {
  function probeAwareFetch(over: { probe?: Response } = {}) {
    return vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/v1/storage/probe' && init?.method === 'POST') {
        return (
          over.probe ??
          new Response(JSON.stringify(observation), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }
      if (url.includes('/storage/layout')) {
        return new Response(JSON.stringify({ chassis: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return new Response(JSON.stringify(observation), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
  }

  it('hides the probe action without the storage.probe scope', async () => {
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    await screen.findByRole('button', { name: 'Refresh inventory' });
    expect(screen.queryByRole('button', { name: 'Probe storage' })).toBeNull();
  });

  it('does not invoke the probe from the polling read', async () => {
    const fetchMock = probeAwareFetch();
    vi.stubGlobal('fetch', fetchMock);
    render(<Storage csrf="example-csrf" scopes={['storage.probe']} onUnauthorized={() => undefined} />);
    await screen.findByRole('button', { name: 'Probe storage' });
    expect(
      fetchMock.mock.calls.some(([url, init]) => url === '/api/v1/storage/probe' && init?.method === 'POST'),
    ).toBe(false);
  });

  it('sends exactly one CSRF POST with an empty body on explicit click', async () => {
    const fetchMock = probeAwareFetch();
    vi.stubGlobal('fetch', fetchMock);
    render(<Storage csrf="example-csrf" scopes={['storage.probe']} onUnauthorized={() => undefined} />);
    await screen.findByRole('button', { name: 'Probe storage' });
    fireEvent.click(screen.getByRole('button', { name: 'Probe storage' }));
    await screen.findByText(/Probe complete/);
    const probes = fetchMock.mock.calls.filter(
      ([url, init]) => url === '/api/v1/storage/probe' && init?.method === 'POST',
    );
    expect(probes).toHaveLength(1);
    const init = probes.at(0)?.[1];
    expect((init?.headers as Record<string, string>)['X-CSRF-Token']).toBe('example-csrf');
    expect(init?.body).toBeUndefined();
  });

  it('surfaces a probe error without crashing', async () => {
    const fetchMock = probeAwareFetch({
      probe: new Response(
        JSON.stringify({ error: 'storage_probe_unavailable', message: 'Probe helper failed.' }),
        {
          status: 503,
          headers: { 'Content-Type': 'application/json' },
        },
      ),
    });
    vi.stubGlobal('fetch', fetchMock);
    render(<Storage csrf="example-csrf" scopes={['storage.probe']} onUnauthorized={() => undefined} />);
    await screen.findByRole('button', { name: 'Probe storage' });
    fireEvent.click(screen.getByRole('button', { name: 'Probe storage' }));
    await screen.findByText('Probe helper failed.');
  });
});
