// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { Software } from './pages/Software';
import type { PackageList, SourceList, UpdateList } from './api';

const installedFixture: PackageList = {
  packages: [{ name: 'nginx', version: '1.2.3', arch: 'amd64', installed: true }],
  truncated: false,
  source: 'fixture',
};
const updatesFixture: UpdateList = {
  updates: [{ name: 'nginx', arch: 'amd64', current: '1.2.3', new: '1.2.4' }],
  truncated: false,
  source: 'fixture',
  as_of: '2026-11-07T00:00:00Z',
};
const sourcesFixture: SourceList = {
  sources: [
    { name: 'aur', origin: 'https://aur.archlinux.org', enabled: true, signed: false, third_party: true },
  ],
  truncated: false,
  source: 'fixture',
};
const searchFixture: PackageList = {
  packages: [{ name: 'nginx', summary: 'web server', installed: false }],
  truncated: false,
  source: 'fixture',
};

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return {
    ...mod,
    getPackageSearch: vi.fn(async () => searchFixture),
    getPackagesInstalled: vi.fn(async () => installedFixture),
    getPackagesUpdates: vi.fn(async () => updatesFixture),
    getPackagesSources: vi.fn(async () => sourcesFixture),
  };
});

beforeEach(() => {
  vi.clearAllMocks();
});
afterEach(() => cleanup());

const csrf = 'token';
const onUnauthorized = vi.fn();

describe('Software Center UI model', () => {
  it('renders the four tabs and the read-only preview label', async () => {
    render(<Software csrf={csrf} onUnauthorized={onUnauthorized} />);
    for (const label of ['Search', 'Installed', 'Updates', 'Sources']) {
      expect(screen.getByRole('tab', { name: label })).toBeTruthy();
    }
    expect(await screen.findByText(/Read-only preview/)).toBeTruthy();
  });

  it('has no mutating controls in the DOM', async () => {
    render(<Software csrf={csrf} onUnauthorized={onUnauthorized} />);
    await screen.findByText(/Read-only preview/);
    for (const name of ['Install', 'Remove', 'Upgrade', 'Add', 'Update']) {
      expect(screen.queryByRole('button', { name })).toBeNull();
    }
    expect(screen.queryByText(/Add repository/i)).toBeNull();
  });

  it('shows installed packages and flags third-party/unsigned sources', async () => {
    render(<Software csrf={csrf} onUnauthorized={onUnauthorized} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Installed' }));
    expect(await screen.findByText('nginx')).toBeTruthy();
    expect(screen.getByText('installed')).toBeTruthy();

    fireEvent.click(screen.getByRole('tab', { name: 'Sources' }));
    expect(await screen.findByText('third-party script source')).toBeTruthy();
    expect(screen.getByText('unsigned')).toBeTruthy();
  });

  it('labels pending updates with the local metadata refresh time', async () => {
    render(<Software csrf={csrf} onUnauthorized={onUnauthorized} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Updates' }));
    expect(await screen.findByText(/As of last metadata refresh/)).toBeTruthy();
    expect(screen.getByText('1.2.4')).toBeTruthy();
  });
});
