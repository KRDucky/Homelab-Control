// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { LastRun, MergerFS, runPollMs } from './pages/MergerFS';
import {
  ApiError,
  applyMergerFS,
  getMergerFS,
  getSnapraidStatus,
  planMergerFS,
  stepUp,
  type MergerFSInventory,
  type MergerFSToolItem,
  type SnapraidStatus,
} from './api';

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return {
    ...mod,
    getMergerFS: vi.fn(),
    getSnapraidStatus: vi.fn(),
    planMergerFS: vi.fn(),
    applyMergerFS: vi.fn(),
    stepUp: vi.fn(),
  };
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
  dumpe2fsInstalled?: boolean;
  xfsInfoInstalled?: boolean;
  btrfsInstalled?: boolean;
  kernelReleasePresent?: boolean;
}): MergerFSInventory {
  const fusermount3Installed = opts.fusermount3Installed ?? true;
  const kernelFusePresent = opts.kernelFusePresent ?? true;
  const smartctlInstalled = opts.smartctlInstalled ?? true;
  const dumpe2fsInstalled = opts.dumpe2fsInstalled ?? true;
  const xfsInfoInstalled = opts.xfsInfoInstalled ?? true;
  const btrfsInstalled = opts.btrfsInstalled ?? true;
  const kernelReleasePresent = opts.kernelReleasePresent ?? true;
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
        item('dumpe2fs', 'e2fsprogs', dumpe2fsInstalled, dumpe2fsInstalled ? '/usr/sbin/dumpe2fs' : ''),
        item('xfs_info', 'xfsprogs', xfsInfoInstalled, xfsInfoInstalled ? '/usr/sbin/xfs_info' : ''),
        item('btrfs', 'btrfs-progs', btrfsInstalled, btrfsInstalled ? '/usr/bin/btrfs' : ''),
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
    kernelRelease: kernelReleasePresent
      ? { present: true, release: '6.8.0-hc-test', reason: '' }
      : { present: false, release: '', reason: 'Could not read /proc/sys/kernel/osrelease.' },
    fuseAccess: { present: true, userAllowOther: true, reason: '' },
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
    expect(screen.getByText(/user_allow_other is set in \/etc\/fuse.conf/)).toBeTruthy();
    expect(screen.getByText(/does not edit fuse.conf or remount/)).toBeTruthy();
  });

  it('shows when user_allow_other is unset and when a pool lacks allow_other', async () => {
    const data = inventory({
      mergerfsInstalled: true,
      snapraidInstalled: true,
      mergerfsPresent: true,
      snapraidPresent: false,
    });
    data.fuseAccess = { present: true, userAllowOther: false, reason: '' };
    data.warnings = [
      {
        code: 'fuse-allow-other',
        pool: '/mnt/media',
        branch: '/mnt/media',
        other: '',
        detail:
          'this pool is mounted without allow_other; other users may not see its files. This page does not remount',
      },
    ];
    vi.mocked(getMergerFS).mockResolvedValue(data);
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/user_allow_other is not set in \/etc\/fuse.conf/)).toBeTruthy();
    expect(screen.getByText(/only matters when a non-root user mounts with allow_other/)).toBeTruthy();
    expect(screen.getByText(/does not edit fuse.conf or remount/)).toBeTruthy();
    expect(screen.getByText(/mounted without allow_other/)).toBeTruthy();
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
    expect(screen.queryByRole('heading', { name: 'Underlying filesystems' })).toBeNull();
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
    expect(screen.queryByRole('heading', { name: 'Underlying filesystems' })).toBeNull();
  });

  it('shows the underlying filesystem tools offer without formatting disks', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: false,
        dumpe2fsInstalled: false,
        xfsInfoInstalled: false,
        btrfsInstalled: false,
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
    expect(await screen.findByRole('heading', { name: 'Underlying filesystems' })).toBeTruthy();
    expect(screen.getByText(/inspect branch filesystems/)).toBeTruthy();
    expect(screen.getByText(/does not run them or format disks/)).toBeTruthy();
    expect(screen.getByText(/Underlying filesystem tools are probed, not run/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Add e2fsprogs' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add e2fsprogs' });
    expect(within(dialog).getByRole('link', { name: 'Open Software Center' }).getAttribute('href')).toBe(
      '/software?q=e2fsprogs',
    );
    expect(screen.getByRole('button', { name: 'Add xfsprogs' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Add btrfs-progs' })).toBeTruthy();
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

  it('shows kernel release from osrelease without running uname', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: false,
      }),
    );
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText('6.8.0-hc-test')).toBeTruthy();
    expect(screen.getByText(/mergerfs --version is not run/)).toBeTruthy();
  });

  it('reports unread kernel release without mutating controls', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: false,
        kernelReleasePresent: false,
      }),
    );
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/Could not read \/proc\/sys\/kernel\/osrelease/)).toBeTruthy();
    expect(screen.getByText('/mnt/media')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Install mergerfs' })).toBeNull();
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
    expect(screen.getByText(/Writes between syncs are not protected/)).toBeTruthy();
    expect(screen.getByText(/does not read content files/)).toBeTruthy();
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
    expect(screen.getByRole('button', { name: 'Read SnapRAID status' })).toBeTruthy();
    expect(vi.mocked(getSnapraidStatus)).not.toHaveBeenCalled();
    expect(screen.getByRole('heading', { name: 'Configured mounts' })).toBeTruthy();
    expect(screen.getByText(/No regular mergerfs fstab lines/)).toBeTruthy();
    expect(screen.getByText(/drop-ins/)).toBeTruthy();
    expect(screen.getByText(/Symlinked, masked, or generated units are not read/)).toBeTruthy();
  });

  it('reads SnapRAID status only after the button is pressed', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: true,
      }),
    );
    vi.mocked(getSnapraidStatus).mockResolvedValue({
      exitCode: 0,
      syncInProgress: false,
      disks: [
        {
          name: 'd1',
          files: 1,
          fragmentedFiles: 0,
          excessFragments: 0,
          wastedGB: -24.7,
          usedGB: 0,
          freeGB: 2,
          usePercent: 0,
        },
      ],
      summary: ['No sync is in progress.', 'No error detected.'],
      scrubAge: 'The oldest block was scrubbed 3 days ago, the median 1, the newest 0.',
      note: "This report is read from SnapRAID's content file and refers to the latest sync. Later modifications are not counted.",
      source: 'snapraid status',
    });
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    await screen.findByText('/mnt/media');
    expect(vi.mocked(getSnapraidStatus)).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Read SnapRAID status' }));
    expect(await screen.findByText('No sync is in progress.')).toBeTruthy();
    expect(screen.getByText(/Later modifications are not counted/)).toBeTruthy();
    expect(screen.getByRole('rowheader', { name: 'd1' })).toBeTruthy();
    expect(screen.getByRole('cell', { name: '2' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Sync' })).toBeNull();
    expect(vi.mocked(getSnapraidStatus)).toHaveBeenCalledTimes(1);
    expect(vi.mocked(getSnapraidStatus)).toHaveBeenCalledWith(csrf);
  });

  it('says when another SnapRAID command holds the array lock', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: true,
      }),
    );
    vi.mocked(getSnapraidStatus).mockResolvedValue({
      exitCode: 1,
      syncInProgress: false,
      locked: true,
      disks: [],
      summary: [],
      note: 'n',
      source: 'snapraid status',
    });
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    await screen.findByText('/mnt/media');
    fireEvent.click(screen.getByRole('button', { name: 'Read SnapRAID status' }));
    expect(
      await screen.findByText(/Another SnapRAID command \(sync, scrub or fix\) is running/),
    ).toBeTruthy();
  });

  // Review finding on #181: a report the helper cut off must not look complete.
  it('says when the SnapRAID status report is incomplete', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: true,
      }),
    );
    vi.mocked(getSnapraidStatus).mockResolvedValue({
      exitCode: -1,
      syncInProgress: false,
      truncated: true,
      disks: [{ name: 'd1', files: 1, fragmentedFiles: 0, excessFragments: 0, usedGB: 10.5 }],
      summary: [],
      note: 'n',
      source: 'snapraid status',
    });
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    await screen.findByText('/mnt/media');
    fireEvent.click(screen.getByRole('button', { name: 'Read SnapRAID status' }));
    expect(await screen.findByText(/The status report is incomplete/)).toBeTruthy();
    expect(screen.getByRole('cell', { name: '10.5' })).toBeTruthy();
  });

  it('hides SnapRAID status when no config is present', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: false,
        mergerfsPresent: true,
        snapraidPresent: false,
      }),
    );
    render(<MergerFS csrf={csrf} packagesEnabled={false} onUnauthorized={onUnauthorized} />);
    await screen.findByText('/mnt/media');
    expect(screen.queryByRole('button', { name: 'Read SnapRAID status' })).toBeNull();
    expect(vi.mocked(getSnapraidStatus)).not.toHaveBeenCalled();
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

  it('reviews SnapRAID sync and does not apply until confirm', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(
      inventory({
        mergerfsInstalled: true,
        snapraidInstalled: true,
        mergerfsPresent: true,
        snapraidPresent: true,
      }),
    );
    vi.mocked(planMergerFS).mockResolvedValue({
      action: 'sync',
      risk: 'disruptive',
      step_up_required: true,
      token: 'hcp1.token',
      expires_at: '2099-01-01T00:00:00Z',
      effect: 'Run snapraid sync only if diff shows no missing disk and no removed files.',
      consequences: [
        'A missing disk or a removed file blocks this operation. Nothing is synced or scrubbed.',
      ],
      argv: ['/usr/bin/snapraid', 'sync'],
    });
    vi.mocked(applyMergerFS).mockResolvedValue({
      operation_id: 'op-1',
      action: 'sync',
      outcome: 'succeeded',
      message: 'SnapRAID sync finished.',
    });
    vi.mocked(stepUp).mockResolvedValue({ step_up_until: '2099-01-01T00:00:00Z' });
    render(
      <MergerFS
        csrf={csrf}
        scopes={['mergerfs.maintain']}
        packagesEnabled={false}
        onUnauthorized={onUnauthorized}
      />,
    );
    await screen.findByText('/mnt/media');
    expect(screen.queryByRole('button', { name: 'Sync' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Review pool mount' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Review SnapRAID sync' }));
    expect(await screen.findByText('/usr/bin/snapraid sync')).toBeTruthy();
    expect(planMergerFS).toHaveBeenCalledWith('maintain', { action: 'sync' }, csrf);
    expect(applyMergerFS).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'Confirm reviewed action' }));
    await waitFor(() => expect(applyMergerFS).toHaveBeenCalledTimes(1));
    expect(stepUp).toHaveBeenCalledWith('secret', csrf);
    expect(applyMergerFS).toHaveBeenCalledWith('maintain', { action: 'sync', token: 'hcp1.token' }, csrf);
    expect(await screen.findByText('SnapRAID sync finished.')).toBeTruthy();
  });

  // Owner request: a sync, scrub or fix that continues in the background had
  // no visible end on the page, only "locked" and the system journal.
  describe('latest SnapRAID run', () => {
    const base: SnapraidStatus = {
      exitCode: 0,
      syncInProgress: false,
      disks: [],
      summary: [],
      note: 'n',
      source: 's',
    };

    it('shows each state in plain words', () => {
      const { rerender } = render(
        <LastRun
          run={{
            action: 'sync',
            state: 'finished',
            startedAt: '2026-11-26T22:40:00Z',
            runningAt: '2026-11-26T22:40:10Z',
            endedAt: '2026-11-26T22:43:05Z',
            exitCode: 0,
          }}
        />,
      );
      expect(screen.getByRole('heading', { name: 'Latest sync: finished' })).toBeTruthy();
      expect(screen.getByText(/after 2 min 55 s\. SnapRAID exit status 0\./)).toBeTruthy();
      rerender(
        <LastRun
          run={{
            action: 'scrub',
            state: 'refused',
            startedAt: '2026-11-26T22:40:00Z',
            endedAt: '2026-11-26T22:40:02Z',
            blocked: 'missing-disk',
          }}
        />,
      );
      expect(screen.getByText(/Not started .*: a disk is missing\./)).toBeTruthy();
      rerender(
        <LastRun
          run={{
            action: 'sync',
            state: 'failed',
            startedAt: '2026-11-26T22:40:00Z',
            endedAt: '2026-11-26T22:41:00Z',
            exitCode: 1,
          }}
        />,
      );
      expect(screen.getByText(/SnapRAID exited with status 1\./)).toBeTruthy();
      expect(screen.getByText(/homelab-control-mergerfs-apply@\*/)).toBeTruthy();
      rerender(<LastRun run={{ action: 'sync', state: 'checking', startedAt: '2026-11-26T22:40:00Z' }} />);
      expect(
        screen.getByText(/Checking the array \(snapraid diff\) since .*Sync starts only if/),
      ).toBeTruthy();
      rerender(<LastRun run={{ action: 'fix', state: 'interrupted', startedAt: '2026-11-26T22:40:00Z' }} />);
      expect(screen.getByRole('heading', { name: 'Latest recovery (fix): interrupted' })).toBeTruthy();
      expect(screen.getByText(/Stopped before it ended/)).toBeTruthy();
    });

    it('reads status after a confirmed sync and again until the run ends', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true });
      try {
        vi.mocked(getMergerFS).mockResolvedValue(
          inventory({
            mergerfsInstalled: true,
            snapraidInstalled: true,
            mergerfsPresent: true,
            snapraidPresent: true,
          }),
        );
        vi.mocked(planMergerFS).mockResolvedValue({
          action: 'sync',
          risk: 'disruptive',
          step_up_required: false,
          token: 'hcp1.token',
          expires_at: '2099-01-01T00:00:00Z',
          effect: 'e',
          consequences: [],
          argv: ['/usr/bin/snapraid', 'sync'],
        });
        vi.mocked(applyMergerFS).mockResolvedValue({
          operation_id: 'op-1',
          action: 'sync',
          outcome: 'succeeded',
          message: 'Sync started and continues in the background. Follow it under SnapRAID status.',
        });
        vi.mocked(getSnapraidStatus)
          .mockResolvedValueOnce({
            ...base,
            exitCode: 1,
            locked: true,
            lastRun: {
              action: 'sync',
              state: 'running',
              startedAt: '2026-11-26T22:40:00Z',
              runningAt: '2026-11-26T22:40:10Z',
            },
          })
          .mockResolvedValue({
            ...base,
            lastRun: {
              action: 'sync',
              state: 'finished',
              startedAt: '2026-11-26T22:40:00Z',
              runningAt: '2026-11-26T22:40:10Z',
              endedAt: '2026-11-26T22:43:05Z',
              exitCode: 0,
            },
          });
        render(
          <MergerFS
            csrf={csrf}
            scopes={['mergerfs.maintain']}
            packagesEnabled={false}
            onUnauthorized={onUnauthorized}
          />,
        );
        await screen.findByText('/mnt/media');
        fireEvent.click(screen.getByRole('button', { name: 'Review SnapRAID sync' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Confirm reviewed action' }));
        expect(await screen.findByRole('heading', { name: 'Latest sync: running' })).toBeTruthy();
        expect(screen.getByText(/the disk table is read again after it ends/)).toBeTruthy();
        expect(screen.getByRole('link', { name: 'Go to SnapRAID status' }).getAttribute('href')).toBe(
          '#snapraid-heading',
        );
        expect(getSnapraidStatus).toHaveBeenCalledTimes(1);
        await vi.advanceTimersByTimeAsync(runPollMs);
        expect(await screen.findByRole('heading', { name: 'Latest sync: finished' })).toBeTruthy();
        expect(getSnapraidStatus).toHaveBeenCalledTimes(2);
        await vi.advanceTimersByTimeAsync(runPollMs * 3);
        expect(getSnapraidStatus).toHaveBeenCalledTimes(2);
      } finally {
        vi.useRealTimers();
      }
    });
  });
});
