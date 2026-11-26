// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { Ext4View } from './pages/Ext4';
import { XFSView } from './pages/XFS';
import { F2fsView } from './pages/F2fs';
import type { Ext4Inventory, F2fsInventory, XFSInventory } from './api';

afterEach(cleanup);

const tooling = { family: 'debian', required: [], optional: [] };
const kernel = { present: true, reason: '' };

function ext4(status: Ext4Inventory['status']): Ext4Inventory {
  return {
    tooling,
    ext4: {
      present: true,
      reason: '',
      mounts: [{ mountpoint: '/srv/data', device: '/dev/vdb' }],
      mountsTruncated: false,
    },
    kernelExt4: kernel,
    ...(status ? { status } : {}),
  };
}

describe('filesystem reports from the standard tools', () => {
  it('ext4: shows the superblock and restates a recorded error', () => {
    render(
      <Ext4View
        data={ext4({
          available: true,
          report: {
            filesystems: [
              {
                device: '252:16',
                mounts: ['/srv/data', '/srv/data-bind'],
                state: 'not clean with errors',
                error_count: 3,
                first_error_time: 'Fri Nov 20 12:00:00 2026',
                first_error_function: 'ext4_lookup',
                block_size: 4096,
                block_count: 1000,
                free_blocks: 250,
                mount_count: 21,
                max_mount_count: 20,
                features: ['has_journal', 'metadata_csum'],
              },
              {
                device: '252:1',
                mounts: ['/'],
                state: 'clean',
                features: [],
                max_mount_count: -1,
                mount_count: 4,
              },
            ],
            skipped: [
              {
                mountpoint: '/srv/img',
                reason: 'on a loop device, which the read-only helper does not read',
              },
            ],
          },
        })}
        error={null}
        packagesEnabled={false}
      />,
    );
    const bad = screen.getByRole('article', { name: 'Filesystem /srv/data' });
    expect(within(bad).getByText('3 errors recorded')).toBeTruthy();
    expect(within(bad).getByText('Fri Nov 20 12:00:00 2026, ext4_lookup')).toBeTruthy();
    expect(within(bad).getByText('21 of 20 before a forced check')).toBeTruthy();
    expect(within(bad).getByText('/srv/data-bind')).toBeTruthy();
    expect(within(bad).getByText('75%')).toBeTruthy();
    const root = screen.getByRole('article', { name: 'Filesystem /' });
    expect(within(root).getByText('Clean, no errors recorded')).toBeTruthy();
    expect(within(root).getByText('4 (no forced check by count)')).toBeTruthy();
    expect(screen.getByText(/on a loop device/)).toBeTruthy();
  });

  it('ext4: an unavailable report gives its reason and keeps the mount list', () => {
    render(
      <Ext4View
        data={ext4({ available: false, reason: 'Superblock status is unavailable: helper stopped' })}
        error={null}
        packagesEnabled={false}
      />,
    );
    expect(screen.getByText('Superblock status is unavailable: helper stopped')).toBeTruthy();
    expect(screen.getByText('/dev/vdb')).toBeTruthy();
  });

  it('xfs: shows geometry and a per-filesystem failure', () => {
    const data: XFSInventory = {
      tooling,
      xfs: { present: true, reason: '', mounts: [], mountsTruncated: false },
      kernelXfs: kernel,
      status: {
        available: true,
        report: {
          filesystems: [
            {
              mountpoint: '/srv/x',
              mounts: ['/srv/x'],
              block_size: 4096,
              blocks: 262144,
              ag_count: 4,
              ag_size_blocks: 65536,
              log: 'internal log',
              log_blocks: 2560,
              realtime: 'none',
              features: ['crc', 'reflink'],
            },
            { mountpoint: '/srv/y', mounts: ['/srv/y'], features: [], error: 'xfs_info: exit status 1' },
          ],
        },
      },
    };
    render(<XFSView data={data} error={null} packagesEnabled={false} />);
    const x = screen.getByRole('article', { name: 'Filesystem /srv/x' });
    expect(within(x).getByText('1.0 GiB')).toBeTruthy();
    expect(within(x).getByText('4 × 65536 blocks (256 MiB)')).toBeTruthy();
    expect(within(x).getByText('crc reflink')).toBeTruthy();
    expect(within(x).getByText('internal log, 2560 blocks (10.0 MiB)')).toBeTruthy();
    expect(
      within(screen.getByRole('article', { name: 'Filesystem /srv/y' })).getByText('xfs_info: exit status 1'),
    ).toBeTruthy();
  });

  it('f2fs: shows the kernel counters', () => {
    const data: F2fsInventory = {
      tooling,
      f2fs: { present: true, reason: '', mounts: [], mountsTruncated: false },
      kernelF2fs: kernel,
      status: {
        filesystems: [
          {
            name: 'vdb',
            mounts: ['/srv/flash'],
            features: ['extra_attr', 'sb_checksum'],
            freeSegments: 480,
            lifetimeWriteKiB: 2048,
          },
        ],
      },
    };
    render(<F2fsView data={data} error={null} packagesEnabled={false} />);
    const fs = screen.getByRole('article', { name: 'Filesystem /srv/flash' });
    expect(within(fs).getByText('480')).toBeTruthy();
    expect(within(fs).getByText('2.0 MiB')).toBeTruthy();
    expect(within(fs).getByText('extra_attr, sb_checksum')).toBeTruthy();
  });
});

// VM finding (2026-11-26): ProtectHome removes /home sub-mounts from the
// helper's namespace, so they are neither reported nor skipped. The page must
// name them instead of implying every mount was inspected.
describe('mounts hidden from the helper', () => {
  it('xfs: names a mount under /home that the helper could not see', () => {
    const data: XFSInventory = {
      tooling,
      xfs: {
        present: true,
        reason: '',
        mounts: [
          { mountpoint: '/srv/x', device: '/dev/vdb' },
          { mountpoint: '/home/media', device: '/dev/vdc' },
        ],
        mountsTruncated: false,
      },
      kernelXfs: kernel,
      status: {
        available: true,
        report: { filesystems: [{ mountpoint: '/srv/x', mounts: ['/srv/x'], features: [] }] },
      },
    };
    render(<XFSView data={data} error={null} packagesEnabled={false} />);
    expect(screen.getByText(/Not inspected: \/home\/media\./)).toBeTruthy();
  });

  it('ext4: no note when every mount is covered', () => {
    render(
      <Ext4View
        data={ext4({
          available: true,
          report: { filesystems: [{ device: '252:16', mounts: ['/srv/data'], features: [] }] },
        })}
        error={null}
        packagesEnabled={false}
      />,
    );
    expect(screen.queryByText(/Not inspected/)).toBeNull();
  });
});
