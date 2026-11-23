// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { F2fs } from './pages/F2fs';
import { getF2fs, type F2fsInventory, type F2fsToolItem } from './api';

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return { ...mod, getF2fs: vi.fn() };
});

const csrf = 'token';
const onUnauthorized = vi.fn();

function item(id: string, pkg: string, installed: boolean, binary = ''): F2fsToolItem {
  return { id, package: pkg, installed, binary };
}

function inventory(opts: {
  f2fsInstalled: boolean;
  kernelPresent?: boolean;
  family?: string;
  pkg?: string;
  mounts?: boolean;
}): F2fsInventory {
  const kernelPresent = opts.kernelPresent ?? true;
  const pkg = opts.pkg ?? 'f2fs-tools';
  return {
    tooling: {
      family: opts.family ?? 'debian',
      required: [item('dump.f2fs', pkg, opts.f2fsInstalled, opts.f2fsInstalled ? '/usr/sbin/dump.f2fs' : '')],
      optional: [],
    },
    f2fs:
      opts.mounts === false
        ? {
            present: opts.f2fsInstalled,
            reason: opts.f2fsInstalled ? '' : 'dump.f2fs binary was not found',
            mounts: [],
            mountsTruncated: false,
          }
        : {
            present: true,
            reason: '',
            mounts: [{ mountpoint: '/mnt/data', device: '/dev/sda1' }],
            mountsTruncated: false,
          },
    kernelF2fs: kernelPresent
      ? { present: true, reason: '' }
      : { present: false, reason: 'Kernel f2fs is not currently listed.' },
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

describe('f2fs read-only page', () => {
  it('renders an observed mount and says the binary is probed, not run', async () => {
    vi.mocked(getF2fs).mockResolvedValue(inventory({ f2fsInstalled: true }));
    render(
      <F2fs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('/mnt/data')).toBeTruthy();
    expect(screen.getByText('/dev/sda1')).toBeTruthy();
    expect(screen.getByText(/The dump.f2fs binary is probed, not run/)).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'f2fs tools' })).toBeNull();
  });

  it('shows the install offer for f2fs-tools', async () => {
    vi.mocked(getF2fs).mockResolvedValue(inventory({ f2fsInstalled: false, mounts: false }));
    render(
      <F2fs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByRole('heading', { name: 'f2fs tools' })).toBeTruthy();
    expect(screen.getByText(/separate Software Center planning dialog/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install f2fs-tools' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install f2fs-tools' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=f2fs-tools',
    );
  });

  it('identifies the displayed count and incomplete inventory when mounts were omitted', async () => {
    const data = inventory({ f2fsInstalled: true });
    data.f2fs.mountsTruncated = true;
    vi.mocked(getF2fs).mockResolvedValue(data);
    render(
      <F2fs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText(/Showing 1 mounts. Inventory incomplete/)).toBeTruthy();
    expect(screen.getByText('/mnt/data')).toBeTruthy();
  });

  it('does not invent a package name for an unknown family', async () => {
    vi.mocked(getF2fs).mockResolvedValue(
      inventory({ f2fsInstalled: false, mounts: false, family: 'unknown', pkg: '' }),
    );
    render(
      <F2fs csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText(/cannot determine a package name for dump\.f2fs/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Install/ })).toBeNull();
  });
});
