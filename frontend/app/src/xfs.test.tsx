// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { XFS } from './pages/XFS';
import { getXFS, type XFSInventory, type XFSToolItem } from './api';

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return { ...mod, getXFS: vi.fn() };
});

const csrf = 'token';
const onUnauthorized = vi.fn();

function item(id: string, pkg: string, installed: boolean, binary = ''): XFSToolItem {
  return { id, package: pkg, installed, binary };
}

function inventory(opts: {
  xfsInstalled: boolean;
  kernelPresent?: boolean;
  family?: string;
  pkg?: string;
  mounts?: boolean;
}): XFSInventory {
  const kernelPresent = opts.kernelPresent ?? true;
  const pkg = opts.pkg ?? 'xfsprogs';
  return {
    tooling: {
      family: opts.family ?? 'debian',
      required: [item('xfs_info', pkg, opts.xfsInstalled, opts.xfsInstalled ? '/usr/sbin/xfs_info' : '')],
      optional: [],
    },
    xfs:
      opts.mounts === false
        ? {
            present: opts.xfsInstalled,
            reason: opts.xfsInstalled ? '' : 'xfs_info binary was not found',
            mounts: [],
            mountsTruncated: false,
          }
        : {
            present: true,
            reason: '',
            mounts: [{ mountpoint: '/mnt/data', device: '/dev/sda1' }],
            mountsTruncated: false,
          },
    kernelXfs: kernelPresent
      ? { present: true, reason: '' }
      : { present: false, reason: 'Kernel XFS is not currently listed.' },
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

describe('XFS read-only page', () => {
  it('renders an observed mount and says the binary is probed, not run', async () => {
    vi.mocked(getXFS).mockResolvedValue(inventory({ xfsInstalled: true }));
    render(
      <XFS csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('/mnt/data')).toBeTruthy();
    expect(screen.getByText('/dev/sda1')).toBeTruthy();
    expect(screen.getByText(/The xfs_info binary is probed, not run/)).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'XFS tools' })).toBeNull();
  });

  it('shows the install offer for xfsprogs', async () => {
    vi.mocked(getXFS).mockResolvedValue(inventory({ xfsInstalled: false, mounts: false }));
    render(
      <XFS csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByRole('heading', { name: 'XFS tools' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install xfsprogs' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install xfsprogs' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=xfsprogs',
    );
  });

  it('shows too many mounts to list when the inventory was truncated', async () => {
    const data = inventory({ xfsInstalled: true });
    data.xfs.mountsTruncated = true;
    vi.mocked(getXFS).mockResolvedValue(data);
    render(
      <XFS csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('too many mounts to list')).toBeTruthy();
    expect(screen.getByText('/mnt/data')).toBeTruthy();
  });

  it('does not invent a package name for an unknown family', async () => {
    vi.mocked(getXFS).mockResolvedValue(
      inventory({ xfsInstalled: false, mounts: false, family: 'unknown', pkg: '' }),
    );
    render(
      <XFS csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText(/cannot determine the package name/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Install/ })).toBeNull();
  });
});
