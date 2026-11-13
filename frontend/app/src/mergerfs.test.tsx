// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MergerFS } from './pages/MergerFS';
import { ApiError, getMergerFS, type MergerFSInventory, type MergerFSToolItem } from './api';

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return { ...mod, getMergerFS: vi.fn() };
});

const csrf = 'token';
const onUnauthorized = vi.fn();

const poolFixture = {
  mountpoint: '/mnt/media',
  branches: ['/mnt/disk1', '/mnt/disk2'],
  options: ['category.create=epmfs'],
};

function item(id: string, pkg: string, installed: boolean, binary = ''): MergerFSToolItem {
  return { id, package: pkg, installed, binary };
}

function inventory(opts: {
  mergerfsInstalled: boolean;
  snapraidInstalled: boolean;
  mergerfsPresent: boolean;
  snapraidPresent: boolean;
}): MergerFSInventory {
  return {
    tooling: {
      family: 'debian',
      required: [
        item(
          'mergerfs',
          'mergerfs',
          opts.mergerfsInstalled,
          opts.mergerfsInstalled ? '/usr/bin/mergerfs' : '',
        ),
      ],
      optional: [
        item(
          'snapraid',
          'snapraid',
          opts.snapraidInstalled,
          opts.snapraidInstalled ? '/usr/bin/snapraid' : '',
        ),
      ],
    },
    mergerfs: opts.mergerfsPresent
      ? { present: true, reason: '', pools: [poolFixture], configured: [] }
      : { present: false, reason: 'mergerfs is not installed', pools: [], configured: [] },
    snapraid: opts.snapraidPresent
      ? {
          present: true,
          reason: '',
          configPath: '/etc/snapraid.conf',
          data: ['/mnt/disk1'],
          parity: ['/mnt/parity1'],
          content: ['/var/snapraid/content'],
        }
      : {
          present: false,
          reason: 'optional: no snapraid.conf',
          configPath: '',
          data: [],
          parity: [],
          content: [],
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

describe('MergerFS read-only page', () => {
  it('renders pools when mergerfs is present and no snapraid section', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: false,
      }),
    );
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText('/mnt/media')).toBeTruthy();
    expect(screen.getByText('/mnt/disk1, /mnt/disk2')).toBeTruthy();
    expect(screen.queryByText(/SnapRAID is optional/)).toBeNull();
  });

  it('shows the required-missing offer with an Install CTA', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: false,
        snapraidInstalled: true,
        mergerfsPresent: false,
        snapraidPresent: false,
      }),
    );
    render(
      <MergerFS
        csrf={csrf}
        scopes={['packages.read']}
        packagesEnabled={true}
        onUnauthorized={onUnauthorized}
      />,
    );
    expect(await screen.findByText(/MergerFS is not available/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install mergerfs' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install mergerfs' });
    expect(within(dialog).getByText(/does not install packages/)).toBeTruthy();
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=mergerfs',
    );
    expect(screen.queryByText('/mnt/media')).toBeNull();
  });

  it('says Software Center is unavailable when packages is disabled', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: false,
        snapraidInstalled: true,
        mergerfsPresent: false,
        snapraidPresent: false,
      }),
    );
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    const offer = await screen.findByText(/Software Center module must be enabled/);
    expect(offer.textContent).toContain('mergerfs');
    expect(screen.queryByRole('button', { name: 'Install mergerfs' })).toBeNull();
  });

  it('shows observed pools together with the install offer when the binary is missing', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: false,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: false,
      }),
    );
    render(
      <MergerFS
        csrf={csrf}
        scopes={['packages.read']}
        packagesEnabled={true}
        onUnauthorized={onUnauthorized}
      />,
    );
    expect(await screen.findByText('/mnt/media')).toBeTruthy();
    expect(screen.getByText('observed')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install mergerfs' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install mergerfs' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=mergerfs',
    );
  });

  it('shows the optional-missing SnapRAID offer with an Add CTA', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: false,
        mergerfsPresent: true,
        snapraidPresent: false,
      }),
    );
    render(
      <MergerFS
        csrf={csrf}
        scopes={['packages.read']}
        packagesEnabled={true}
        onUnauthorized={onUnauthorized}
      />,
    );
    expect(await screen.findByText(/SnapRAID is optional/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Add snapraid' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add snapraid' });
    expect(within(dialog).getByText(/does not install packages/)).toBeTruthy();
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=snapraid',
    );
  });

  it('renders the SnapRAID config block only when a config is present', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: false,
        snapraidPresent: true,
      }),
    );
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/SnapRAID is optional/)).toBeTruthy();
    expect(screen.getByText('/etc/snapraid.conf')).toBeTruthy();
  });

  it('renders both pools and SnapRAID together', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: true,
      }),
    );
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText('/mnt/media')).toBeTruthy();
    expect(screen.getByText(/SnapRAID is optional/)).toBeTruthy();
  });

  it('has no mutating controls in the DOM', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: true,
      }),
    );
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    await screen.findByText('/mnt/media');
    for (const name of [
      'Create',
      'Add branch',
      'Sync',
      'Scrub',
      'Fix',
      'Install package',
      'Mount',
      'Unmount',
    ]) {
      expect(screen.queryByRole('button', { name })).toBeNull();
    }
    expect(screen.getByRole('heading', { name: 'Configured mounts' })).toBeTruthy();
    expect(screen.getByText(/No mergerfs entries in fstab or systemd mount units/)).toBeTruthy();
  });

  it('lists fstab and systemd configured mounts without mount controls', async () => {
    const data = inventory({
      mergerfsInstalled: true,
      snapraidInstalled: true,
      mergerfsPresent: true,
      snapraidPresent: false,
    });
    data.mergerfs.configured = [
      {
        origin: 'fstab',
        path: '/etc/fstab',
        mountpoint: '/mnt/media',
        branches: ['/mnt/disk1', '/mnt/disk2'],
        options: ['allow_other'],
        mounted: true,
      },
      {
        origin: 'systemd',
        path: '/etc/systemd/system/mnt-idle.mount',
        unit: 'mnt-idle.mount',
        mountpoint: '/mnt/idle',
        branches: ['/mnt/a', '/mnt/b'],
        options: [],
        mounted: false,
      },
    ];
    vi.mocked(getMergerFS).mockResolvedValue(data);
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByRole('heading', { name: 'Configured mounts' })).toBeTruthy();
    expect(screen.getByText('currently mounted')).toBeTruthy();
    expect(screen.getByText('not currently mounted')).toBeTruthy();
    expect(screen.getByText('mnt-idle.mount')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Mount' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Unmount' })).toBeNull();
  });

  it('says when configured import was truncated', async () => {
    const data = inventory({
      mergerfsInstalled: true,
      snapraidInstalled: true,
      mergerfsPresent: true,
      snapraidPresent: false,
    });
    data.mergerfs.configuredTruncated = true;
    vi.mocked(getMergerFS).mockResolvedValue(data);
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/import limit was reached/)).toBeTruthy();
  });

  it('signals an expired or revoked session through onUnauthorized', async () => {
    vi.mocked(getMergerFS).mockRejectedValue(new ApiError(401, 'unauthenticated', 'login required'));
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    await waitFor(() => expect(onUnauthorized).toHaveBeenCalled());
  });
});
