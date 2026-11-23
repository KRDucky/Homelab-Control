// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { Ext4 } from './pages/Ext4';
import { getExt4, type Ext4Inventory, type Ext4ToolItem } from './api';

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return { ...mod, getExt4: vi.fn() };
});

const csrf = 'token';
const onUnauthorized = vi.fn();

function item(id: string, pkg: string, installed: boolean, binary = ''): Ext4ToolItem {
  return { id, package: pkg, installed, binary };
}

function inventory(opts: {
  ext4Installed: boolean;
  kernelPresent?: boolean;
  family?: string;
  pkg?: string;
  mounts?: boolean;
}): Ext4Inventory {
  const kernelPresent = opts.kernelPresent ?? true;
  const pkg = opts.pkg ?? 'e2fsprogs';
  return {
    tooling: {
      family: opts.family ?? 'debian',
      required: [item('dumpe2fs', pkg, opts.ext4Installed, opts.ext4Installed ? '/usr/sbin/dumpe2fs' : '')],
      optional: [],
    },
    ext4:
      opts.mounts === false
        ? {
            present: opts.ext4Installed,
            reason: opts.ext4Installed ? '' : 'dumpe2fs binary was not found',
            mounts: [],
            mountsTruncated: false,
          }
        : {
            present: true,
            reason: '',
            mounts: [{ mountpoint: '/mnt/data', device: '/dev/sda1' }],
            mountsTruncated: false,
          },
    kernelExt4: kernelPresent
      ? { present: true, reason: '' }
      : { present: false, reason: 'Kernel ext4 is not currently listed.' },
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

describe('ext4 read-only page', () => {
  it('renders an observed mount and says the binary is probed, not run', async () => {
    vi.mocked(getExt4).mockResolvedValue(inventory({ ext4Installed: true }));
    render(
      <Ext4 csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('/mnt/data')).toBeTruthy();
    expect(screen.getByText('/dev/sda1')).toBeTruthy();
    expect(screen.getByText(/The dumpe2fs binary is probed, not run/)).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'ext4 tools' })).toBeNull();
  });

  it('shows the install offer for e2fsprogs', async () => {
    vi.mocked(getExt4).mockResolvedValue(inventory({ ext4Installed: false, mounts: false }));
    render(
      <Ext4 csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByRole('heading', { name: 'ext4 tools' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install e2fsprogs' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install e2fsprogs' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=e2fsprogs',
    );
  });

  it('shows too many mounts to list when the inventory was truncated', async () => {
    const data = inventory({ ext4Installed: true });
    data.ext4.mountsTruncated = true;
    vi.mocked(getExt4).mockResolvedValue(data);
    render(
      <Ext4 csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('too many mounts to list')).toBeTruthy();
    expect(screen.getByText('/mnt/data')).toBeTruthy();
  });

  it('does not invent a package name for an unknown family', async () => {
    vi.mocked(getExt4).mockResolvedValue(
      inventory({ ext4Installed: false, mounts: false, family: 'unknown', pkg: '' }),
    );
    render(
      <Ext4 csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText(/cannot determine a package name for dumpe2fs/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Install/ })).toBeNull();
  });
});
