// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { Mdadm } from './pages/Mdadm';
import { getMdadm, type MdadmInventory } from './api';

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return { ...mod, getMdadm: vi.fn() };
});

const csrf = 'token';
const onUnauthorized = vi.fn();

// Synthetic inventory (labelled example data, not a host observation).
function inventory(opts: {
  installed: boolean;
  pkg?: string;
  family?: string;
  present?: boolean;
}): MdadmInventory {
  const present = opts.present ?? true;
  return {
    tooling: {
      family: opts.family ?? 'debian',
      required: [
        {
          id: 'mdadm',
          package: opts.pkg ?? 'mdadm',
          installed: opts.installed,
          binary: opts.installed ? '/usr/sbin/mdadm' : '',
        },
      ],
      optional: [],
    },
    md: present
      ? {
          present: true,
          reason: '',
          personalities: ['raid1', 'raid5'],
          arrays: [
            { name: 'md0', level: 'raid1', state: 'active', memberCount: 2 },
            { name: 'md1', level: '', state: 'inactive', memberCount: null },
          ],
          arraysTruncated: false,
        }
      : {
          present: false,
          reason: '/proc/mdstat could not be read safely; no arrays are shown.',
          personalities: [],
          arrays: [],
          arraysTruncated: false,
        },
  };
}

function renderPage() {
  render(
    <Mdadm csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
  };
});
afterEach(cleanup);

describe('Software RAID read-only page', () => {
  it('lists arrays as printed when the mdadm report is unavailable, with the reason', async () => {
    vi.mocked(getMdadm).mockResolvedValue({
      ...inventory({ installed: true }),
      status: { available: false, reason: 'Array detail is unavailable: helper stopped' },
    });
    renderPage();
    const table = await screen.findByRole('table', { name: 'Arrays as printed in /proc/mdstat' });
    const md0 = within(table).getByRole('rowheader', { name: 'md0' }).closest('tr') as HTMLElement;
    expect(within(md0).getByText('raid1')).toBeTruthy();
    expect(within(md0).getByText('active')).toBeTruthy();
    expect(within(md0).getByText('2')).toBeTruthy();
    const md1 = within(table).getByRole('rowheader', { name: 'md1' }).closest('tr') as HTMLElement;
    expect(within(md1).getAllByText('Not printed')).toHaveLength(2);
    expect(within(md1).getByText('inactive')).toBeTruthy();
    expect(screen.getByText('raid1, raid5')).toBeTruthy();
    expect(screen.getByText('Array detail is unavailable: helper stopped')).toBeTruthy();
    expect(screen.getByText(/does not create, assemble, reshape or fail an array/)).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'mdadm tools' })).toBeNull();
    expect(screen.queryByRole('button', { name: /create|assemble|grow|add|remove|stop/i })).toBeNull();
  });

  it('shows each array as mdadm --detail describes it: state, counts, rebuild and members', async () => {
    vi.mocked(getMdadm).mockResolvedValue({
      ...inventory({ installed: true }),
      status: {
        available: true,
        report: {
          arrays: [
            {
              device: '/dev/md126',
              name: 'homelab-demo:lab5',
              uuid: '4ff124e4:f30e54b6:fa1eb145:3ae20412',
              level: 'raid5',
              state: 'clean, degraded, recovering',
              array_size_kib: 405504,
              raid_devices: 3,
              active_devices: 2,
              failed_devices: 0,
              spare_devices: 1,
              layout: 'left-symmetric',
              chunk_size: '512K',
              progress: 'Rebuild 1% complete',
              members: [
                { slot: 0, state: 'active sync', device: '/dev/loop2' },
                { slot: 1, state: 'spare rebuilding', device: '/dev/loop3' },
                { slot: 2, state: 'active sync', device: '/dev/loop4' },
              ],
            },
            {
              device: '/dev/md127',
              state: 'active',
              level: 'raid1',
              members: [],
              error: 'mdadm --detail: exit status 1',
            },
          ],
        },
      },
    });
    renderPage();
    const arr = await screen.findByRole('article', { name: 'Array /dev/md126' });
    expect(within(arr).getByText('clean, degraded, recovering').className).toContain('status-crit');
    expect(within(arr).getByText('3 in array · 2 active · 0 failed · 1 spare')).toBeTruthy();
    expect(within(arr).getByText('Rebuild 1% complete')).toBeTruthy();
    expect(within(arr).getByText('left-symmetric, 512K chunk')).toBeTruthy();
    const members = within(arr).getByRole('table', { name: 'Members of /dev/md126' });
    const loop3 = within(members).getByText('/dev/loop3').closest('tr');
    if (!loop3) throw new Error('row missing');
    expect(within(loop3).getByText('spare rebuilding')).toBeTruthy();
    const failed = screen.getByRole('article', { name: 'Array /dev/md127' });
    expect(within(failed).getByText('Not described: mdadm --detail: exit status 1')).toBeTruthy();
    // The mdadm report replaces the raw /proc/mdstat table.
    expect(screen.queryByRole('table', { name: 'Arrays as printed in /proc/mdstat' })).toBeNull();
    expect(screen.queryByRole('button', { name: /create|assemble|grow|add|remove|stop|fail/i })).toBeNull();
  });

  it('offers a Software Center search only when mdadm is missing and the package is known', async () => {
    vi.mocked(getMdadm).mockResolvedValue(inventory({ installed: false }));
    renderPage();
    expect(await screen.findByRole('heading', { name: 'mdadm tools' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install mdadm' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install mdadm' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=mdadm',
    );
  });

  it('does not invent a package name for an unknown family', async () => {
    vi.mocked(getMdadm).mockResolvedValue(inventory({ installed: false, pkg: '', family: 'unknown' }));
    renderPage();
    expect(await screen.findByText(/cannot determine the package name/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Install/ })).toBeNull();
  });

  it('shows the fail-closed reason and no arrays when /proc/mdstat is unusable', async () => {
    vi.mocked(getMdadm).mockResolvedValue(inventory({ installed: true, present: false }));
    renderPage();
    expect(
      await screen.findByText('/proc/mdstat could not be read safely; no arrays are shown.'),
    ).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('identifies the displayed count when the array inventory is truncated', async () => {
    const data = inventory({ installed: true });
    data.md.arraysTruncated = true;
    vi.mocked(getMdadm).mockResolvedValue(data);
    renderPage();
    expect(await screen.findByText(/Showing 2 arrays from a truncated inventory/)).toBeTruthy();
    expect(screen.getByRole('rowheader', { name: 'md0' })).toBeTruthy();
  });
});
