// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { LVM } from './pages/LVM';
import { getLVM, type LVMInventory, type LVMToolItem } from './api';

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return { ...mod, getLVM: vi.fn() };
});

const csrf = 'token';
const onUnauthorized = vi.fn();

function item(id: string, pkg: string, installed: boolean, binary = ''): LVMToolItem {
  return { id, package: pkg, installed, binary };
}

function inventory(opts: {
  lvmInstalled: boolean;
  kernelPresent?: boolean;
  family?: string;
  pkg?: string;
  devices?: boolean;
}): LVMInventory {
  const kernelPresent = opts.kernelPresent ?? true;
  const pkg = opts.pkg ?? 'lvm2';
  return {
    tooling: {
      family: opts.family ?? 'debian',
      required: [item('lvm', pkg, opts.lvmInstalled, opts.lvmInstalled ? '/usr/sbin/lvm' : '')],
      optional: [item('thin_check', 'thin-provisioning-tools', false)],
    },
    lvm:
      opts.devices === false
        ? {
            present: opts.lvmInstalled,
            reason: opts.lvmInstalled ? '' : 'lvm binary was not found',
            devices: [],
            devicesTruncated: false,
          }
        : {
            present: true,
            reason: '',
            devices: [{ name: 'dm-0', mapper: 'vg-lv' }],
            devicesTruncated: false,
          },
    kernelDm: kernelPresent
      ? { present: true, reason: '' }
      : {
          present: false,
          reason:
            'The kernel device-mapper target is not currently listed in /proc/devices (this page does not load modules).',
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

describe('LVM read-only page', () => {
  it('renders observed devices', async () => {
    vi.mocked(getLVM).mockResolvedValue(inventory({ lvmInstalled: true }));
    render(
      <LVM csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('dm-0')).toBeTruthy();
    expect(screen.getByText('vg-lv')).toBeTruthy();
    expect(screen.getByText(/The lvm binary is probed, not run/)).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'LVM tools' })).toBeNull();
  });

  it('shows the install offer for lvm2', async () => {
    vi.mocked(getLVM).mockResolvedValue(inventory({ lvmInstalled: false, devices: false }));
    render(
      <LVM csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByRole('heading', { name: 'LVM tools' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install lvm2' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install lvm2' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=lvm2',
    );
  });

  it('shows too many devices to list when the inventory was truncated', async () => {
    const data = inventory({ lvmInstalled: true });
    data.lvm.devicesTruncated = true;
    vi.mocked(getLVM).mockResolvedValue(data);
    render(
      <LVM csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('too many devices to list')).toBeTruthy();
    expect(screen.getByText('dm-0')).toBeTruthy();
  });

  it('does not invent a package name for an unknown family', async () => {
    vi.mocked(getLVM).mockResolvedValue(
      inventory({ lvmInstalled: false, devices: false, family: 'unknown', pkg: '' }),
    );
    render(
      <LVM csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText(/cannot determine the package name/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Install/ })).toBeNull();
  });
});
