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
  report?: boolean;
  thinPool?: boolean;
}): LVMInventory {
  const kernelPresent = opts.kernelPresent ?? true;
  const pkg = opts.pkg ?? 'lvm2';
  return {
    tooling: {
      family: opts.family ?? 'debian',
      required: [item('lvm', pkg, opts.lvmInstalled, opts.lvmInstalled ? '/usr/sbin/lvm' : '')],
      optional: [item('thin_check', 'thin-provisioning-tools', false)],
    },
    kernelDm: kernelPresent
      ? { present: true, reason: '' }
      : {
          present: false,
          reason:
            'The kernel device-mapper target is not currently listed in /proc/devices (this page does not load modules).',
        },
    status:
      opts.report === false
        ? { available: false, reason: 'LVM status is unavailable: helper stopped' }
        : {
            available: true,
            report: {
              volume_groups: [
                {
                  uuid: 'vg-u',
                  name: 'datavg',
                  size_bytes: 4290772992,
                  free_bytes: 2680160256,
                  attributes: 'wz-pn-',
                  pv_count: 2,
                  lv_count: 2,
                  missing_pv_count: 1,
                },
              ],
              volumes: [
                {
                  uuid: 'lv-1',
                  name: 'media',
                  vg_name: 'datavg',
                  size_bytes: 1073741824,
                  health: 'partial',
                  segments: [{ type: 'raid1' }],
                  sync_percent: 42,
                },
                ...(opts.thinPool
                  ? [
                      {
                        uuid: 'lv-2',
                        name: 'pool',
                        vg_name: 'datavg',
                        health: 'unknown',
                        segments: [{ type: 'thin-pool' }],
                      },
                    ]
                  : []),
              ],
              physical_volumes: [
                { uuid: 'pv-1', name: '/dev/sda', vg_name: 'datavg', size_bytes: 2145386496, missing: false },
                { uuid: 'pv-2', vg_name: 'datavg', missing: true },
              ],
            },
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
  it('shows volume groups, logical volumes and physical volumes as LVM reports them', async () => {
    vi.mocked(getLVM).mockResolvedValue(inventory({ lvmInstalled: true }));
    render(
      <LVM csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    const vgs = await screen.findByRole('table', { name: 'Volume groups' });
    const vg = within(vgs).getByText('datavg').closest('tr');
    if (!vg) throw new Error('vg row missing');
    expect(within(vg).getByText('1').className).toContain('status-crit');
    const lvs = screen.getByRole('table', { name: 'Logical volumes' });
    const media = within(lvs).getByText('media').closest('tr');
    if (!media) throw new Error('lv row missing');
    expect(within(media).getByText('raid1')).toBeTruthy();
    expect(within(media).getByText('partial')).toBeTruthy();
    expect(within(media).getByText('42%')).toBeTruthy();
    const pvs = screen.getByRole('table', { name: 'Physical volumes' });
    expect(within(pvs).getByText('/dev/sda')).toBeTruthy();
    expect(within(pvs).getByText('Missing').className).toContain('status-crit');
    // No thin pool reported, so no thin_check offer.
    expect(screen.queryByRole('heading', { name: 'Thin pool check' })).toBeNull();
    expect(screen.queryByRole('button', { name: /create|remove|extend|resize/i })).toBeNull();
  });

  it('states why LVM data is missing', async () => {
    vi.mocked(getLVM).mockResolvedValue(inventory({ lvmInstalled: true, report: false }));
    render(
      <LVM csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText('LVM status is unavailable: helper stopped')).toBeTruthy();
    expect(screen.queryByRole('table', { name: 'Logical volumes' })).toBeNull();
  });

  it('shows the install offer for lvm2', async () => {
    vi.mocked(getLVM).mockResolvedValue(inventory({ lvmInstalled: false, report: false }));
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

  it('offers thin_check only when LVM reports a thin pool', async () => {
    const data = inventory({ lvmInstalled: true, thinPool: true });
    vi.mocked(getLVM).mockResolvedValue(data);
    render(
      <LVM csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByRole('heading', { name: 'Thin pool check' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install thin-provisioning-tools' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install thin-provisioning-tools' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=thin-provisioning-tools',
    );
  });

  it('does not invent a package name for an unknown family', async () => {
    vi.mocked(getLVM).mockResolvedValue(
      inventory({ lvmInstalled: false, report: false, family: 'unknown', pkg: '' }),
    );
    render(
      <LVM csrf={csrf} scopes={['packages.read']} packagesEnabled={true} onUnauthorized={onUnauthorized} />,
    );
    expect(await screen.findByText(/cannot determine the package name/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Install/ })).toBeNull();
  });
});
