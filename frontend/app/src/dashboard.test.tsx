// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { Dashboard, validateDashboardLayout } from './Dashboard';
import type { Overview, Capability } from './api';

const data: Overview = {
  collected_at: '2026-10-15T08:00:00Z',
  system: {
    status: 'ok',
    sources: [],
    hostname: 'fixture-host',
    os: { pretty_name: 'Fixture OS' },
    distro_family: 'debian',
    load_average: [0.1, 0.2, 0.3],
  },
  cpu: { status: 'ok', sources: [], logical_cpus: 4, usage_percent: 20, sample_millis: 250 },
  memory: {
    status: 'ok',
    sources: [],
    total_bytes: 1000,
    used_bytes: 600,
    available_bytes: 400,
    swap_total_bytes: 0,
    swap_free_bytes: 0,
  },
  filesystems: { status: 'ok', sources: [], view: 'host', items: [] },
  network: { status: 'unavailable', sources: [], interfaces: [] },
  legacy_webmin: { status: 'ok', sources: [], installed: false, config_readable: false },
};
const pages: Capability[] = [
  { id: 'overview', label: 'Overview', path: '/', scope: 'system.read', status: 'available' },
  { id: 'services', label: 'Services', path: '/services', scope: 'services.read', status: 'available' },
  { id: 'audit', label: 'Audit', path: '/audit', scope: 'audit.read', status: 'forbidden' },
];

beforeEach(() => window.localStorage.clear());
afterEach(() => cleanup());

describe('dashboard layout schema', () => {
  it('validates ids, duplicates, and schema versions', () => {
    expect(validateDashboardLayout({ version: 1, widgets: ['memory', 'cpu'] }).widgets).toEqual([
      'memory',
      'cpu',
    ]);
    expect(validateDashboardLayout({ version: 1, widgets: ['cpu', 'cpu'] }).widgets).toHaveLength(6);
    expect(validateDashboardLayout({ version: 1, widgets: ['unknown'] }).widgets).toHaveLength(6);
    expect(validateDashboardLayout({ version: 2, widgets: ['memory'] }).widgets).toHaveLength(6);
  });
});

describe('dashboard widgets', () => {
  it('uses reported readings and excludes forbidden module links', () => {
    render(<Dashboard data={data} pages={pages} />);
    expect(screen.getByText('fixture-host')).toBeTruthy();
    expect(screen.getAllByText('20%')).toHaveLength(2);
    expect(screen.getByText('No usage data available')).toBeTruthy();
    const modules = screen.getByRole('region', { name: 'Customizable dashboard' });
    expect(within(modules).getByRole('link', { name: 'Services' })).toBeTruthy();
    expect(within(modules).queryByRole('link', { name: 'Audit' })).toBeNull();
  });

  it('persists widget choice and ordering without saving host telemetry', () => {
    const { unmount } = render(<Dashboard data={data} pages={pages} />);
    fireEvent.click(screen.getByRole('button', { name: 'Customize dashboard' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Network' }));
    fireEvent.click(screen.getByRole('button', { name: 'Move Memory up' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save layout' }));
    const stored = window.localStorage.getItem('hc.dashboard.v1') ?? '';
    expect(JSON.parse(stored).widgets).toEqual(['host', 'memory', 'cpu', 'storage', 'modules']);
    expect(stored).not.toContain('fixture-host');
    unmount();
    render(<Dashboard data={data} pages={pages} />);
    expect(screen.queryByRole('heading', { name: 'Network' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Customize dashboard' }));
    fireEvent.click(screen.getByRole('button', { name: 'Reset to default' }));
    expect(screen.getByRole('heading', { name: 'Network' })).toBeTruthy();
  });
});
