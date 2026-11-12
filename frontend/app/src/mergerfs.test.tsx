// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { MergerFS } from './pages/MergerFS';
import { ApiError, getMergerFS, type MergerFSInventory } from './api';

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

function inventory(mergerfsPresent: boolean, snapraidPresent: boolean): MergerFSInventory {
  return {
    mergerfs: mergerfsPresent
      ? { present: true, reason: '', pools: [poolFixture] }
      : { present: false, reason: 'no fuse.mergerfs mounts found', pools: [] },
    snapraid: snapraidPresent
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
});
afterEach(cleanup);

describe('MergerFS read-only page', () => {
  it('renders pools and hides SnapRAID when only mergerfs is present', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(inventory(true, false));
    render(<MergerFS csrf={csrf} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText('/mnt/media')).toBeTruthy();
    expect(screen.getByText('/mnt/disk1, /mnt/disk2')).toBeTruthy();
    expect(screen.queryByText(/SnapRAID is optional/)).toBeNull();
  });

  it('shows the not-available card when mergerfs is missing', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(inventory(false, false));
    render(<MergerFS csrf={csrf} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/MergerFS is not available/)).toBeTruthy();
    expect(screen.getByText(/verify/)).toBeTruthy();
    expect(screen.queryByText('/mnt/media')).toBeNull();
  });

  it('shows the SnapRAID section only when a config is present', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(inventory(false, true));
    render(<MergerFS csrf={csrf} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/SnapRAID is optional/)).toBeTruthy();
    expect(screen.getByText('/etc/snapraid.conf')).toBeTruthy();
    expect(screen.queryByText('/mnt/media')).toBeNull();
  });

  it('renders both pools and SnapRAID together', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(inventory(true, true));
    render(<MergerFS csrf={csrf} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText('/mnt/media')).toBeTruthy();
    expect(screen.getByText(/SnapRAID is optional/)).toBeTruthy();
  });

  it('has no mutating controls in the DOM', async () => {
    vi.mocked(getMergerFS).mockResolvedValue(inventory(true, true));
    render(<MergerFS csrf={csrf} onUnauthorized={onUnauthorized} />);
    await screen.findByText('/mnt/media');
    for (const name of ['Create', 'Add branch', 'Sync', 'Scrub', 'Fix', 'Install']) {
      expect(screen.queryByRole('button', { name })).toBeNull();
    }
  });

  it('signals an expired or revoked session through onUnauthorized', async () => {
    vi.mocked(getMergerFS).mockRejectedValue(new ApiError(401, 'unauthenticated', 'login required'));
    render(<MergerFS csrf={csrf} onUnauthorized={onUnauthorized} />);
    await waitFor(() => expect(onUnauthorized).toHaveBeenCalled());
  });
});
