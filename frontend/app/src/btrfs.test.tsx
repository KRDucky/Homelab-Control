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
  smartPackage?: string;
}): BtrfsInventory {
  const kernelPresent = opts.kernelPresent ?? true;
  const pkg = opts.pkg ?? 'btrfs-progs';
  return {
    tooling: {
      family: opts.family ?? 'debian',
      required: [item('btrfs', pkg, opts.btrfsInstalled, opts.btrfsInstalled ? '/usr/bin/btrfs' : '')],
      optional: [item('smartctl', opts.smartPackage ?? '', false)],
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
  it('renders an observed mount and says RAID5/6 is not offered', async () => {
    vi.mocked(getBtrfs).mockResolvedValue(inventory({ btrfsInstalled: true }));
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('/mnt/data')).toBeTruthy();
    expect(screen.getByText('/dev/sda1')).toBeTruthy();
    expect(screen.getByText('/')).toBeTruthy();
    expect(screen.getByText('5')).toBeTruthy();
    expect(screen.getByText(/The btrfs binary is probed, not run/)).toBeTruthy();
    expect(screen.getByText(/RAID5\/6 is not offered/)).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Btrfs tools' })).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Disk health' })).toBeNull();
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
    expect(await screen.findByText('too many mounts to list')).toBeTruthy();
    expect(screen.getByText('/mnt/data')).toBeTruthy();
  });

  it('offers smartctl only after a mount is observed and the API names a package', async () => {
    const data = inventory({ btrfsInstalled: true, smartPackage: 'smartmontools' });
    vi.mocked(getBtrfs).mockResolvedValue(data);
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByRole('heading', { name: 'Disk health' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install smartmontools' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install smartmontools' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=smartmontools',
    );
  });

  it('does not offer smartctl when no mount was observed', async () => {
    vi.mocked(getBtrfs).mockResolvedValue(
      inventory({ btrfsInstalled: true, mounts: false, smartPackage: 'smartmontools' }),
    );
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText(/No Btrfs mounts found/)).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Disk health' })).toBeNull();
  });

  it('does not invent a package name for an unknown family', async () => {
    vi.mocked(getBtrfs).mockResolvedValue(
      inventory({ btrfsInstalled: false, mounts: false, family: 'unknown', pkg: '' }),
    );
    render(
      <Btrfs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText(/cannot determine the package name/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Install/ })).toBeNull();
  });
});
