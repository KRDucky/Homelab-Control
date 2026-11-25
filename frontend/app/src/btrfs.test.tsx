// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { Btrfs } from './pages/Btrfs';
import { getBtrfs, type BtrfsInventory, type BtrfsToolItem } from './api';

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return { ...mod, getBtrfs: vi.fn() };
});

const csrf = 'token';
const onUnauthorized = vi.fn();

function item(id: string, pkg: string, installed: boolean, binary = ''): BtrfsToolItem {
  return { id, package: pkg, installed, binary };
}

function inventory(opts: {
  btrfsInstalled: boolean;
  kernelPresent?: boolean;
  family?: string;
  pkg?: string;
  mounts?: boolean;
}): BtrfsInventory {
  const kernelPresent = opts.kernelPresent ?? true;
  const pkg = opts.pkg ?? 'btrfs-progs';
  return {
    tooling: {
      family: opts.family ?? 'debian',
      required: [item('btrfs', pkg, opts.btrfsInstalled, opts.btrfsInstalled ? '/usr/bin/btrfs' : '')],
      optional: [],
    },
    btrfs:
      opts.mounts === false
        ? {
            present: opts.btrfsInstalled,
            reason: opts.btrfsInstalled ? '' : 'btrfs binary was not found',
            mounts: [],
            mountsTruncated: false,
          }
        : {
            present: true,
            reason: '',
            mounts: [{ mountpoint: '/mnt/data', device: '/dev/sda1', subvol: '/', subvolId: '5' }],
            mountsTruncated: false,
          },
    kernelBtrfs: kernelPresent
      ? { present: true, reason: '' }
      : { present: false, reason: 'Kernel Btrfs is not currently listed.' },
    status: { available: false, reason: 'Filesystem status is unavailable: helper stopped' },
  };
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

describe('Btrfs read-only page', () => {
  it('renders an observed mount and the reason when the filesystem report is unavailable', async () => {
    vi.mocked(getBtrfs).mockResolvedValue(inventory({ btrfsInstalled: true }));
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('/mnt/data')).toBeTruthy();
    expect(screen.getByText('/dev/sda1')).toBeTruthy();
    expect(screen.getByText('/')).toBeTruthy();
    expect(screen.getByText('5')).toBeTruthy();
    expect(screen.getByText('Filesystem status is unavailable: helper stopped')).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Btrfs tools' })).toBeNull();
    expect(screen.queryByRole('button', { name: /scrub|balance|format/i })).toBeNull();
  });

  it('shows the install offer for btrfs-progs', async () => {
    vi.mocked(getBtrfs).mockResolvedValue(inventory({ btrfsInstalled: false, mounts: false }));
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByRole('heading', { name: 'Btrfs tools' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install btrfs-progs' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install btrfs-progs' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=btrfs-progs',
    );
  });

  it('shows too many mounts to list when the inventory was truncated', async () => {
    const data = inventory({ btrfsInstalled: true });
    data.btrfs.mountsTruncated = true;
    vi.mocked(getBtrfs).mockResolvedValue(data);
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText(/The mount list is incomplete/)).toBeTruthy();
    expect(screen.getByText('/mnt/data')).toBeTruthy();
  });

  it('shows each filesystem from btrfs-progs: profiles, scrub, device errors and subvolumes', async () => {
    const data = inventory({ btrfsInstalled: true });
    data.status = {
      available: true,
      report: {
        filesystems: [
          {
            uuid: 'f857c26f-273a-46ba-9ae9-65996efe0a50',
            mountpoint: '/mnt/pool2',
            mounts: ['/mnt/pool2', '/srv/home'],
            overall: { device_size: 2147483648, used: 294912, free_estimated: 924450816, missing: 0 },
            profiles: [
              { type: 'Data', profile: 'RAID1', size: 107347968, used: 0, devices: [] },
              { type: 'Metadata', profile: 'RAID1', size: 107347968, used: 131072, devices: [] },
              { type: 'Unallocated', devices: [{ path: '/dev/vdb', bytes: 1 }] },
            ],
            devices: [
              {
                path: '/dev/vdb',
                write_io_errs: 0,
                read_io_errs: 0,
                flush_io_errs: 0,
                corruption_errs: 0,
                generation_errs: 0,
              },
              {
                path: '/dev/vdc',
                write_io_errs: 0,
                read_io_errs: 3,
                flush_io_errs: 0,
                corruption_errs: 2,
                generation_errs: 0,
              },
            ],
            scrub: {
              status: 'finished',
              started: 'Wed Nov 25 11:02:35 2026',
              error_summary: 'no errors found',
            },
            subvolumes: [{ id: 256, top_level: 5, path: '@home' }],
            errors: ['subvolume list: exit status 1'],
          },
        ],
        skipped: [
          {
            mountpoint: '/home',
            reason: 'mounted only under /home, /root or /run/user, which the read-only helper cannot open',
          },
        ],
      },
    };
    vi.mocked(getBtrfs).mockResolvedValue(data);
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    const fs = await screen.findByRole('article', { name: 'Filesystem /mnt/pool2' });
    expect(within(fs).getByText('5 device errors recorded')).toBeTruthy();
    expect(within(fs).getAllByText(/^RAID1 ·/, { selector: 'dd' })).toHaveLength(2);
    expect(within(fs).queryByText('Unallocated')).toBeNull();
    expect(within(fs).getByText('finished, Wed Nov 25 11:02:35 2026')).toBeTruthy();
    expect(within(fs).getByText('/mnt/pool2, /srv/home')).toBeTruthy();
    expect(within(fs).getByText('Not reported: subvolume list: exit status 1')).toBeTruthy();
    const errs = within(fs).getByRole('table', { name: 'Device errors of /mnt/pool2' });
    const vdc = within(errs).getByText('/dev/vdc').closest('tr');
    if (!vdc) throw new Error('vdc row missing');
    expect(within(vdc).getByText('3')).toBeTruthy();
    expect(within(fs).getByRole('table', { name: 'Subvolumes of /mnt/pool2' })).toBeTruthy();
    expect(screen.getByText(/which the read-only helper cannot open/)).toBeTruthy();
    // /mnt/data (on /dev/sda1) is in the page's mount list but no reported filesystem covers it.
    expect(screen.getByText(/Not inspected: \/mnt\/data\./)).toBeTruthy();
    // The filesystem report replaces the mountinfo list.
    expect(screen.queryByRole('heading', { name: 'Btrfs mounts' })).toBeNull();
  });

  it('does not flag a subvolume mount of a reported filesystem', async () => {
    const data = inventory({ btrfsInstalled: true });
    data.btrfs.mounts.push({
      mountpoint: '/home/data',
      device: '/dev/vdd',
      subvol: '/@data',
      subvolId: '257',
    });
    data.status = {
      available: true,
      report: {
        filesystems: [
          {
            mountpoint: '/mnt/data',
            mounts: ['/mnt/data'],
            overall: {},
            profiles: [],
            devices: [{ path: '/dev/vdd' }],
            scrub: {},
            subvolumes: [],
          },
        ],
      },
    };
    vi.mocked(getBtrfs).mockResolvedValue(data);
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    await screen.findByRole('article', { name: 'Filesystem /mnt/data' });
    expect(screen.queryByText(/Not inspected/)).toBeNull();
  });

  it('marks a filesystem with missing devices', async () => {
    const data = inventory({ btrfsInstalled: true });
    data.status = {
      available: true,
      report: {
        filesystems: [
          {
            mountpoint: '/srv/a',
            mounts: ['/srv/a'],
            overall: { missing: 1073741824 },
            profiles: [],
            devices: [],
            scrub: { never_run: true },
            subvolumes: [],
          },
        ],
      },
    };
    vi.mocked(getBtrfs).mockResolvedValue(data);
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    const fs = await screen.findByRole('article', { name: 'Filesystem /srv/a' });
    expect(within(fs).getByText('Devices missing').className).toContain('status-crit');
    expect(within(fs).getByText('Never run')).toBeTruthy();
  });

  it('says no filesystem is mounted when btrfs reports none', async () => {
    const data = inventory({ btrfsInstalled: true, mounts: false });
    data.status = { available: true, report: { filesystems: [] } };
    vi.mocked(getBtrfs).mockResolvedValue(data);
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('No Btrfs filesystem is mounted.')).toBeTruthy();
  });

  it('does not invent a package name for an unknown family', async () => {
    vi.mocked(getBtrfs).mockResolvedValue(
      inventory({ btrfsInstalled: false, mounts: false, family: 'unknown', pkg: '' }),
    );
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText(/cannot determine a package name for btrfs/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Install/ })).toBeNull();
  });
});
