// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { Chassis, DriveLayout, LayoutCell } from './api';
import { SLOT_MATCH_TEXT, bayLabel, locationBadges, slotText } from './bayModel';
import { DriveBays } from './pages/DriveBays';
import { Storage } from './pages/Storage';
import type { StorageHardware, StorageInventory, StorageNode } from './storageModel';

// M2-5 Stage B. Synthetic fixtures only (labelled examples, not host data).

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
const assignment = (identity: string) => ({
  identity,
  provenance: 'manual' as const,
  confidence: 'unverified' as const,
  assigned_by: 'admin',
  assigned_at: '2026-11-20T00:00:00Z',
});
const cell = (col: number, label: string, identity?: string): LayoutCell => ({
  row: 0,
  col,
  kind: 'hdd-3.5',
  label,
  ...(identity ? { assignment: assignment(identity) } : {}),
});
const chassis = (cells: LayoutCell[]): Chassis => ({
  id: 3,
  name: 'Example Rack A',
  kind: 'server',
  rows: 1,
  cols: cells.length,
  version: 2,
  created_at: '2026-11-20T00:00:00Z',
  updated_at: '2026-11-20T00:00:00Z',
  cells,
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('location badges', () => {
  it('shows chassis and bay only for a present drive', () => {
    const badges = locationBadges([chassis([cell(0, 'Bay 3', 'wwn:naa.5000000000000f01')])], [disk({})]);
    expect(badges.get('n-a')).toEqual({ chassis: 'Example Rack A', bay: 'Bay 3' });
  });
  it('uses the grid position when the bay has no label', () => {
    expect(bayLabel(cell(2, '  '))).toBe('Row 1, Column 3');
  });
  it('follows identity, not the kernel name, after renumbering', () => {
    const renumbered = disk({ id: 'n-q', name: 'sdq', device_number: '65:0' });
    const badges = locationBadges([chassis([cell(0, 'Bay 3', 'wwn:naa.5000000000000f01')])], [renumbered]);
    expect(badges.get('n-q')?.bay).toBe('Bay 3');
    expect(badges.has('n-a')).toBe(false);
  });
  it('gives no bay to missing, mismatched, unknown or unassigned drives', () => {
    const layout = [chassis([cell(0, 'Bay 1', 'wwn:naa.5000000000000f01'), cell(1, 'Bay 2')])];
    // missing: the assigned identity is not observed; the other drive is unassigned.
    const other = disk({
      id: 'n-b',
      name: 'sdb',
      persistent_id: 'wwn:naa.5000000000000f02',
      identifiers: [],
    });
    expect(locationBadges(layout, [other]).size).toBe(0);
    // mismatch: the identity is duplicated (ambiguous) in this observation.
    const dupA = disk({ identity_status: 'ambiguous', persistent_id: undefined as never });
    const dupB = disk({
      id: 'n-c',
      name: 'sdc',
      identity_status: 'ambiguous',
      persistent_id: undefined as never,
    });
    expect(locationBadges(layout, [dupA, dupB]).size).toBe(0);
    // mismatch: the identity now appears on a partition.
    expect(locationBadges(layout, [disk({ kind: 'partition' })]).size).toBe(0);
    // unknown: no current observation.
    expect(locationBadges(layout, undefined).size).toBe(0);
  });
  it('describes enclosure evidence as a slot, never as a bay', () => {
    expect(slotText({ enclosure_id: 'ses:500a0b8000000001', element: 'Slot 00', slot: 0 })).toBe(
      'Enclosure ses:500a0b8000000001, slot 0',
    );
    expect(slotText({})).toBeUndefined();
    for (const text of [...Object.values(SLOT_MATCH_TEXT), slotText({ enclosure_id: 'ses:1', slot: 1 })])
      expect(text).not.toMatch(/\bbay\b/i);
  });
});

const hardware: StorageHardware = {
  status: 'observed',
  source: 'sysfs scsi_host, nvme, enclosure, sas_device',
  notes: ['Fixture: enclosure slots are evidence only.'],
  controllers: [
    {
      id: 'pci:0000:01:00.0',
      kind: 'sas',
      driver: 'mpt3sas',
      pci_address: '0000:01:00.0',
      sas_address: '500605b0000000aa',
      model: 'Example-HBA-8i',
      hosts: ['host0'],
    },
  ],
  enclosures: [
    {
      id: 'ses:500a0b8000000001',
      identity_status: 'observed',
      name: '0:0:1:0',
      vendor: 'EXAMPLE',
      model: 'Example JBOD',
      controller_id: 'pci:0000:01:00.0',
      components: 3,
      slots: [
        {
          element: 'Slot 00',
          slot: 0,
          status: 'OK',
          occupied: true,
          match: 'matched',
          node_id: 'n-a',
          persistent_id: 'wwn:naa.5000000000000f01',
        },
        { element: 'Slot 01', slot: 1, status: 'OK', occupied: true, match: 'unidentified' },
        { element: 'Slot 02', slot: 2, status: 'not installed', occupied: false, match: 'empty' },
      ],
    },
  ],
  links: [
    {
      node: 'n-a',
      controller_id: 'pci:0000:01:00.0',
      port: 'phy 4',
      enclosure_id: 'ses:500a0b8000000001',
      element: 'Slot 00',
      slot: 0,
    },
  ],
};

const observation = (nodes: StorageNode[], hw?: StorageHardware): StorageInventory => ({
  collected_at: '2026-11-20T10:00:00Z',
  read_only: true,
  snapshot_persisted: false,
  graph: { status: 'partial', notes: [], nodes, edges: [], claims: [], ...(hw ? { hardware: hw } : {}) },
});

function stubFetch(inventory: StorageInventory, layout: DriveLayout) {
  const fetchMock = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(
    async (url) =>
      new Response(JSON.stringify(url.includes('/storage/layout') ? layout : inventory), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('Storage page (Stage B)', () => {
  const second = disk({
    id: 'n-b',
    name: 'sdb',
    device_number: '8:16',
    persistent_id: 'wwn:naa.5000000000000f02',
    identifiers: [{ kind: 'wwn', value: 'naa.5000000000000f02', source: 'fixture', ambiguous: false }],
  });
  const layout: DriveLayout = {
    chassis: [chassis([cell(0, 'Bay 3', 'wwn:naa.5000000000000f01'), cell(1, 'Bay 4')])],
  };

  it('badges the present drive in the inventory and leaves the unassigned drive without a bay', async () => {
    stubFetch(observation([disk({}), second], hardware), layout);
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    const table = await screen.findByRole('table', { name: 'Observed storage resources' });
    await waitFor(() => expect(within(table).getByText('Example Rack A · Bay 3')).toBeTruthy());
    const rowA = within(table)
      .getByRole('button', { name: /Inspect .*f01/ })
      .closest('tr') as HTMLElement;
    const rowB = within(table)
      .getByRole('button', { name: /Inspect .*f02/ })
      .closest('tr') as HTMLElement;
    expect(within(rowA).getByText('Manual, unverified')).toBeTruthy();
    expect(within(rowB).queryByText(/Example Rack A/)).toBeNull();
    expect(within(rowB).getByText('None')).toBeTruthy();
  });

  it('shows read-only controller and enclosure evidence with no LED or write action', async () => {
    const fetchMock = stubFetch(observation([disk({}), second], hardware), layout);
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    const controllers = await screen.findByRole('table', { name: 'Observed storage controllers' });
    expect(within(controllers).getByText('pci:0000:01:00.0')).toBeTruthy();
    expect(within(controllers).getByText('Example-HBA-8i')).toBeTruthy();
    expect(within(controllers).getByText('500605b0000000aa')).toBeTruthy();
    const enclosure = screen.getByRole('table', {
      name: /Enclosure ses:500a0b8000000001 \(EXAMPLE Example JBOD\)/,
    });
    const slot0 = within(enclosure).getByRole('rowheader', { name: 'Slot 0' }).closest('tr') as HTMLElement;
    await waitFor(() => expect(within(slot0).getByText('Example Rack A · Bay 3')).toBeTruthy());
    expect(within(enclosure).getByText(SLOT_MATCH_TEXT.unidentified)).toBeTruthy();
    expect(within(enclosure).getByText(SLOT_MATCH_TEXT.empty)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /locate|identify|blink|led/i })).toBeNull();
    for (const [, init] of fetchMock.mock.calls) expect(init?.method ?? 'GET').toBe('GET');
  });

  it('shows bay, controller and enclosure slot in the selected drive details', async () => {
    stubFetch(observation([disk({})], hardware), layout);
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    const table = await screen.findByRole('table', { name: 'Observed storage resources' });
    await waitFor(() => expect(within(table).getByText('Example Rack A · Bay 3')).toBeTruthy());
    fireEvent.click(within(table).getByRole('button', { name: /Inspect / }));
    const details = screen
      .getByRole('heading', { name: 'Selected resource' })
      .closest('section') as HTMLElement;
    expect(within(details).getByText('Example Rack A · Bay 3')).toBeTruthy();
    expect(within(details).getByText('Example-HBA-8i, PCI 0000:01:00.0, phy 4')).toBeTruthy();
    expect(within(details).getByText('Enclosure ses:500a0b8000000001, slot 0')).toBeTruthy();
  });

  it('says hardware is unknown when the observation carries none', async () => {
    stubFetch(observation([disk({})]), { chassis: [] });
    render(<Storage csrf="example-csrf" onUnauthorized={() => undefined} />);
    await screen.findByRole('table', { name: 'Observed storage resources' });
    expect(screen.queryByRole('table', { name: 'Observed storage controllers' })).toBeNull();
    expect(screen.getByText('This observation carries no controller or enclosure data.')).toBeTruthy();
  });
});

describe('Drives & bays panel (Stage B)', () => {
  it('shows enclosure slot evidence for a present bay and reports its layout', async () => {
    const layout: DriveLayout = { chassis: [chassis([cell(0, 'Bay 3', 'wwn:naa.5000000000000f01')])] };
    stubFetch(observation([disk({})]), layout);
    const onLayout = vi.fn();
    render(
      <DriveBays
        csrf="example-csrf"
        nodes={[disk({})]}
        canManage={false}
        preference="wwn"
        onUnauthorized={() => undefined}
        hardware={hardware}
        onLayout={onLayout}
      />,
    );
    fireEvent.click(await screen.findByRole('button', { name: /Bay 3: Present/ }));
    expect(screen.getByText('Enclosure ses:500a0b8000000001, slot 0')).toBeTruthy();
    await waitFor(() => expect(onLayout).toHaveBeenLastCalledWith(layout));
  });
});
