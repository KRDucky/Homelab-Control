// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { ZFS } from './pages/ZFS';
import { getZFS, type ZFSInventory, type ZFSToolItem } from './api';

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return { ...mod, getZFS: vi.fn() };
});

const csrf = 'token';
const onUnauthorized = vi.fn();

function item(id: string, pkg: string, installed: boolean, binary = ''): ZFSToolItem {
  return { id, package: pkg, installed, binary };
}

function inventory(opts: {
  zfsInstalled: boolean;
  zpoolInstalled?: boolean;
  kernelPresent?: boolean;
  family?: string;
  pkg?: string;
  mounts?: boolean;
}): ZFSInventory {
  const zpoolInstalled = opts.zpoolInstalled ?? opts.zfsInstalled;
  const kernelPresent = opts.kernelPresent ?? true;
  const pkg = opts.pkg ?? 'zfsutils-linux';
  return {
    tooling: {
      family: opts.family ?? 'debian',
      required: [
        item('zfs', pkg, opts.zfsInstalled, opts.zfsInstalled ? '/usr/sbin/zfs' : ''),
        item('zpool', pkg, zpoolInstalled, zpoolInstalled ? '/usr/sbin/zpool' : ''),
      ],
      optional: [],
    },
    zfs:
      opts.mounts === false
        ? {
            present: opts.zfsInstalled,
            reason: opts.zfsInstalled ? '' : 'zfs and zpool binaries were not found',
            mounts: [],
            mountsTruncated: false,
          }
        : {
            present: true,
            reason: '',
            mounts: [{ mountpoint: '/tank/media', dataset: 'tank/media' }],
            mountsTruncated: false,
          },
    kernelZfs: kernelPresent
      ? { present: true, reason: '' }
      : {
          present: false,
          reason:
            'The kernel ZFS filesystem is not currently listed in /proc/filesystems (this page does not load modules).',
        },
    status: { available: false, reason: 'Pool status is unavailable: helper stopped' },
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

describe('ZFS read-only page', () => {
  it('renders observed mounts', async () => {
    vi.mocked(getZFS).mockResolvedValue(inventory({ zfsInstalled: true }));
    render(
      <ZFS csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('/tank/media')).toBeTruthy();
    expect(screen.getByText('tank/media')).toBeTruthy();
    expect(screen.getByText('Pool status is unavailable: helper stopped')).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'OpenZFS tools' })).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Disk inspect tools' })).toBeNull();
  });

  it('shows pools, the vdev tree with error counters, and datasets from zpool and zfs', async () => {
    const data = inventory({ zfsInstalled: true });
    data.status = {
      available: true,
      report: {
        pools: [
          {
            name: 'lab',
            health: 'DEGRADED',
            size: 1476395008,
            alloc: 262144,
            free: 1476132864,
            frag_pct: 1,
            status: 'One or more devices has been taken offline by the administrator.',
            scan: 'scrub repaired 0B in 00:00:01 with 0 errors',
            errors: 'No known data errors',
            vdevs: [
              {
                name: 'lab',
                depth: 0,
                state: 'DEGRADED',
                read_errors: 0,
                write_errors: 0,
                checksum_errors: 0,
              },
              {
                name: 'raidz1-0',
                depth: 1,
                state: 'DEGRADED',
                read_errors: 0,
                write_errors: 0,
                checksum_errors: 0,
              },
              {
                name: 'vdb',
                depth: 2,
                state: 'OFFLINE',
                read_errors: 3,
                write_errors: 0,
                checksum_errors: 12,
              },
              { name: 'logs', depth: 0, group: true },
            ],
          },
        ],
        datasets: [
          {
            name: 'lab/media',
            type: 'filesystem',
            used: 98304,
            mountpoint: '/lab/media',
            compression: 'zstd',
          },
        ],
      },
    };
    vi.mocked(getZFS).mockResolvedValue(data);
    render(
      <ZFS csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    const pool = await screen.findByRole('article', { name: 'Pool lab' });
    expect(within(pool).getByText('DEGRADED', { selector: '.status' })).toBeTruthy();
    expect(within(pool).getByText(/taken offline by the administrator/)).toBeTruthy();
    expect(within(pool).getByText(/scrub repaired 0B/)).toBeTruthy();
    const devices = within(pool).getByRole('table', { name: 'Devices of lab' });
    const vdb = within(devices).getByText('vdb').closest('tr');
    if (!vdb) throw new Error('vdb row missing');
    expect(within(vdb).getByText('OFFLINE')).toBeTruthy();
    expect(within(vdb).getByText('12')).toBeTruthy();
    expect(within(devices).getByText('logs').tagName).toBe('STRONG');
    const datasets = screen.getByRole('table', { name: 'Datasets and volumes' });
    expect(within(datasets).getByText('lab/media')).toBeTruthy();
    expect(within(datasets).getByText('zstd')).toBeTruthy();
    // The pool report replaces the mountinfo list.
    expect(screen.queryByRole('heading', { name: 'Mounted datasets' })).toBeNull();
  });

  it('says no pools are imported when zpool lists none', async () => {
    const data = inventory({ zfsInstalled: true, mounts: false });
    data.status = { available: true, report: { pools: [], datasets: [] } };
    vi.mocked(getZFS).mockResolvedValue(data);
    render(
      <ZFS csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('No pools are imported.')).toBeTruthy();
  });

  it('shows the Debian install offer without adding a third-party repo', async () => {
    vi.mocked(getZFS).mockResolvedValue(inventory({ zfsInstalled: false, mounts: false }));
    render(
      <ZFS csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByRole('heading', { name: 'OpenZFS tools' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install zfsutils-linux' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install zfsutils-linux' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=zfsutils-linux',
    );
  });

  it('shows too many mounts to list when the inventory was truncated', async () => {
    const data = inventory({ zfsInstalled: true });
    data.zfs.mountsTruncated = true;
    vi.mocked(getZFS).mockResolvedValue(data);
    render(
      <ZFS csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('too many mounts to list')).toBeTruthy();
    expect(screen.getByText('/tank/media')).toBeTruthy();
  });

  it('does not offer a third-party repository on Fedora', async () => {
    vi.mocked(getZFS).mockResolvedValue(
      inventory({ zfsInstalled: false, mounts: false, family: 'fedora', pkg: '' }),
    );
    render(
      <ZFS csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText(/will not add one/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Install/ })).toBeNull();
  });
});
