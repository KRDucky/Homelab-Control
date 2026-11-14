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
      optional: [item('smartctl', 'smartmontools', true), item('nvme', 'nvme-cli', true)],
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
    expect(screen.getByText(/zfs and zpool are probed, not run/)).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'OpenZFS tools' })).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Disk inspect tools' })).toBeNull();
  });

  it('offers smartctl and nvme only when missing and a package is named', async () => {
    const data = inventory({ zfsInstalled: true });
    data.tooling.optional = [item('smartctl', 'smartmontools', false), item('nvme', '', false)];
    vi.mocked(getZFS).mockResolvedValue(data);
    render(
      <ZFS csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByRole('heading', { name: 'Disk inspect tools' })).toBeTruthy();
    expect(screen.getByText(/does not run them or open disks/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Install smartmontools' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /nvme/ })).toBeNull();
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
