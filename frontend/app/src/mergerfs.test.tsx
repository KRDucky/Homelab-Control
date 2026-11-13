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
  fusermount3Installed?: boolean;
  kernelFusePresent?: boolean;
  smartctlInstalled?: boolean;
}): MergerFSInventory {
  const fusermount3Installed = opts.fusermount3Installed ?? true;
  const kernelFusePresent = opts.kernelFusePresent ?? true;
  const smartctlInstalled = opts.smartctlInstalled ?? true;
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
        item(
          'fusermount3',
          'fuse3',
          fusermount3Installed,
          fusermount3Installed ? '/usr/bin/fusermount3' : '',
        ),
      ],
      optional: [
        item(
          'snapraid',
          'snapraid',
          opts.snapraidInstalled,
          opts.snapraidInstalled ? '/usr/bin/snapraid' : '',
        ),
        item('smartctl', 'smartmontools', smartctlInstalled, smartctlInstalled ? '/usr/sbin/smartctl' : ''),
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
    kernelFuse: kernelFusePresent
      ? { present: true, reason: '' }
      : {
          present: false,
          reason:
            'The kernel FUSE filesystem is not currently listed in /proc/filesystems (the module may load on first mount).',
        },
    warnings: [],
    warningsTruncated: false,
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
    expect(screen.getByText(/Capacity is measured per live branch of a root-owned mount/)).toBeTruthy();
  });

  it('shows a per-branch capacity meter when usage is measured', async () => {
    const data = inventory({
      mergerfsInstalled: true,
      snapraidInstalled: false,
      mergerfsPresent: true,
      snapraidPresent: false,
    });
    data.mergerfs.pools = [
      {
        ...poolFixture,
        usage: [
          {
            path: '/mnt/disk1',
            measured: true,
            totalBytes: 100,
            usedBytes: 40,
            availBytes: 50,
          },
        ],
      },
    ];
    vi.mocked(getMergerFS).mockResolvedValue(data);
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByRole('meter', { name: '/mnt/disk1' })).toBeTruthy();
    expect(screen.getByText('50 B free of 100 B')).toBeTruthy();
    expect(screen.queryByText('/mnt/disk1, /mnt/disk2')).toBeNull();
  });

  it('shows a fixed unread reason when a branch cannot be measured', async () => {
    const data = inventory({
      mergerfsInstalled: true,
      snapraidInstalled: false,
      mergerfsPresent: true,
      snapraidPresent: false,
    });
    data.mergerfs.pools = [
      {
        ...poolFixture,
        usage: [{ path: '/mnt/disk1', measured: false, reason: 'path could not be measured' }],
        usageTruncated: true,
      },
    ];
    vi.mocked(getMergerFS).mockResolvedValue(data);
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText('path could not be measured')).toBeTruthy();
    expect(screen.getByText('(truncated)')).toBeTruthy();
    expect(screen.queryByRole('meter')).toBeNull();
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
    expect(screen.queryByText(/smartctl is optional/)).toBeNull();
  });

  it('shows the optional-missing smartmontools offer without opening disks', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: false,
        smartctlInstalled: false,
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
    expect(await screen.findByText(/smartctl is optional/)).toBeTruthy();
    expect(screen.getByText(/does not run it or open disks/)).toBeTruthy();
    expect(screen.getByText(/smartctl is probed, not run/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Add smartmontools' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add smartmontools' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=smartmontools',
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

  it('shows the fuse3 install offer when fusermount3 is missing', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: false,
        fusermount3Installed: false,
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
    fireEvent.click(screen.getByRole('button', { name: 'Install fuse3' }));
    const dialog = await screen.findByRole('dialog', { name: 'Install fuse3' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=fuse3',
    );
  });

  it('reports missing kernel FUSE without mutating controls', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: false,
        kernelFusePresent: false,
      }),
    );
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/not currently listed/)).toBeTruthy();
    expect(screen.getByText('/mnt/media')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Install mergerfs' })).toBeNull();
  });

  it('renders when kernelFuse is omitted', async () => {
    const data = inventory({
      mergerfsInstalled: true,
      snapraidInstalled: true,
      mergerfsPresent: true,
      snapraidPresent: false,
    });
    delete (data as { kernelFuse?: unknown }).kernelFuse;
    vi.mocked(getMergerFS).mockResolvedValue(data);
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/not currently listed|does not load kernel modules/)).toBeTruthy();
    expect(screen.getByText('/mnt/media')).toBeTruthy();
  });

  it('renders honesty warnings without mutating controls', async () => {
    const data = inventory({
      mergerfsInstalled: true,
      snapraidInstalled: true,
      mergerfsPresent: true,
      snapraidPresent: true,
    });
    data.warnings = [
      {
        code: 'parity-in-pool',
        pool: '/mnt/media',
        branch: '/mnt/parity',
        other: '/mnt/parity/snapraid.parity',
        detail: 'SnapRAID parity sits on a mergerfs branch; pooling is not redundancy',
      },
    ];
    vi.mocked(getMergerFS).mockResolvedValue(data);
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByRole('heading', { name: 'Honesty warnings' })).toBeTruthy();
    // detail is a text node next to <code> branch/other; match the li via substring.
    expect(
      screen.getByText(/SnapRAID parity sits on a mergerfs branch; pooling is not redundancy/),
    ).toBeTruthy();
    expect(screen.getByText('/mnt/parity/snapraid.parity')).toBeTruthy();
  });

  it('reports truncated warnings without mutating controls', async () => {
    const data = inventory({
      mergerfsInstalled: true,
      snapraidInstalled: true,
      mergerfsPresent: true,
      snapraidPresent: false,
    });
    data.warningsTruncated = true;
    vi.mocked(getMergerFS).mockResolvedValue(data);
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText('Further warnings omitted.')).toBeTruthy();
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
    expect(screen.getByText(/No regular mergerfs fstab lines/)).toBeTruthy();
    expect(screen.getByText(/drop-ins/)).toBeTruthy();
    expect(screen.getByText(/Symlinked, masked, or generated units are not read/)).toBeTruthy();
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
        branchesTruncated: true,
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
    expect(screen.getByText('a mergerfs mount is present at this mountpoint')).toBeTruthy();
    expect(screen.getByText('not currently mounted')).toBeTruthy();
    expect(screen.getByText(/\(truncated\)/)).toBeTruthy();
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
