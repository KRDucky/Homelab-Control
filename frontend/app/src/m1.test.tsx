// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { App } from './App';

// Test fixtures only — not real host data.
const session = { username: 'admin', scopes: ['system.read'], csrf_token: 't', version: '0.1.0' };

function caps(overrides: Record<string, { status: string; reason?: string }> = {}, broker = 'available') {
  const base = [
    { id: 'overview', label: 'Overview', path: '/', scope: 'system.read', status: 'available' },
    { id: 'services', label: 'Services', path: '/services', scope: 'services.read', status: 'available' },
    { id: 'hardware', label: 'Hardware', path: '/hardware', scope: 'hardware.read', status: 'available' },
    { id: 'legacy', label: 'Legacy Webmin', path: '/legacy', scope: 'system.read', status: 'available' },
  ];
  return {
    broker:
      broker === 'available' ? { status: 'available' } : { status: 'unavailable', reason: 'Helper is down.' },
    capabilities: base.map((c) => ({ ...c, ...(overrides[c.id] ?? {}) })),
  };
}

const units = {
  systemd_version: 'systemd 255',
  truncated: false,
  source: 'systemctl',
  counts: { active: 2, failed: 1 },
  units: [
    {
      name: 'ssh.service',
      type: 'service',
      load: 'loaded',
      active: 'active',
      sub: 'running',
      description: 'OpenBSD Secure Shell server',
      unit_file_state: 'enabled',
    },
    {
      name: 'smartd.service',
      type: 'service',
      load: 'loaded',
      active: 'failed',
      sub: 'failed',
      description: 'Self Monitoring',
      unit_file_state: 'enabled',
    },
    {
      name: 'apt-daily.timer',
      type: 'timer',
      load: 'loaded',
      active: 'active',
      sub: 'waiting',
      description: 'Daily apt',
    },
    {
      name: 'auditd.service',
      type: 'service',
      load: 'not-found',
      active: 'inactive',
      sub: 'dead',
      description: 'auditd.service',
    },
  ],
};

const hardware = {
  inventory: {
    system: { status: 'ok', sources: [], vendor: 'ASRock', product: 'B550M' },
    pci: { status: 'ok', sources: [], devices: [] },
    usb: {
      status: 'unavailable',
      sources: [],
      notes: ['No USB bus is visible on this system.'],
      devices: [],
    },
    disks: {
      status: 'ok',
      sources: [],
      devices: [
        {
          kernel_name: 'sda',
          kernel_name_note: 'x',
          size_bytes: 8e12,
          model: 'WD80EFAX',
          transport: 'sata',
          rotational: true,
          removable: false,
          read_only: false,
          virtual: false,
          identifiers: [],
          identity_confidence: 'high',
          identity: 'naa.5000cca2',
        },
        {
          kernel_name: 'sdb',
          kernel_name_note: 'x',
          size_bytes: 1e9,
          transport: 'usb',
          removable: true,
          read_only: false,
          virtual: false,
          identifiers: [],
          identity_confidence: 'none',
        },
      ],
    },
    sensors: {
      status: 'unavailable',
      sources: [],
      notes: ['No hardware monitoring chips are exposed.'],
      readings: [],
    },
  },
  identity: {
    status: 'ok',
    data: {
      available: true,
      source: '/sys/class/dmi/id',
      fields: {
        product_serial: { value: 'ABC123', status: 'present' },
        board_serial: { value: 'To Be Filled By O.E.M.', status: 'placeholder' },
      },
    },
  },
};

const legacy = {
  source: 'broker',
  status: {
    installed: true,
    config_dir: '/etc/webmin',
    version: '2.105',
    port: 10000,
    tls: false,
    bind: '',
    listens_on_all_interfaces: true,
    running: true,
    user_count: 1,
    rpc_capable_users: ['admin'],
    checks: [
      { id: 'version', level: 'warn', message: 'Webmin 2.105 is older than the audited baseline 2.670.' },
      { id: 'referer', level: 'ok', message: 'Requests with unknown referers are refused.' },
    ],
  },
};

function mock(routes: Record<string, { status: number; body?: unknown }>) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const r = routes[`${init?.method ?? 'GET'} ${url}`] ?? {
        status: 404,
        body: { error: 'not_found', message: 'nope' },
      };
      return new Response(r.status === 204 ? null : JSON.stringify(r.body ?? {}), { status: r.status });
    }),
  );
}

beforeEach(() => window.history.pushState(null, '', '/'));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('capability-aware navigation', () => {
  it('explains unavailable and forbidden sections instead of linking to them', async () => {
    mock({
      'GET /api/v1/auth/session': { status: 200, body: session },
      'GET /api/v1/capabilities': {
        status: 200,
        body: caps(
          {
            services: { status: 'unavailable', reason: 'Helper is down.' },
            hardware: {
              status: 'forbidden',
              reason: 'Your account does not have the hardware.read permission.',
            },
            legacy: { status: 'degraded', reason: 'Only presence can be checked.' },
          },
          'unavailable',
        ),
      },
      'GET /api/v1/host/overview': { status: 503, body: {} },
    });
    render(<App />);
    const nav = await screen.findByRole('navigation', { name: 'Main' });
    await within(nav).findByText('Services');
    expect(within(nav).queryByRole('link', { name: /Services/ })).toBeNull();
    expect(within(nav).getByText(/Unavailable: Helper is down/)).toBeTruthy();
    expect(within(nav).queryByRole('link', { name: /Hardware/ })).toBeNull();
    expect(within(nav).getByText(/No permission/)).toBeTruthy();
    expect(within(nav).getByRole('link', { name: /Legacy Webmin/ }).textContent).toContain('Limited');
    expect(screen.getByText('Privileged helper not running')).toBeTruthy();
  });

  it('shows an explanation when a deep link targets an unavailable section', async () => {
    window.history.pushState(null, '', '/services');
    mock({
      'GET /api/v1/auth/session': { status: 200, body: session },
      'GET /api/v1/capabilities': {
        status: 200,
        body: caps({ services: { status: 'unsupported', reason: 'No systemd.' } }),
      },
    });
    render(<App />);
    expect((await screen.findByRole('status')).textContent).toContain('Not supported here: No systemd.');
  });
});

describe('Services page', () => {
  it('lists units, highlights failures and filters', async () => {
    window.history.pushState(null, '', '/services');
    mock({
      'GET /api/v1/auth/session': { status: 200, body: session },
      'GET /api/v1/capabilities': { status: 200, body: caps() },
      'GET /api/v1/services': { status: 200, body: units },
    });
    render(<App />);
    expect(await screen.findByRole('heading', { name: 'Failed units' })).toBeTruthy();
    const table = screen.getByRole('table');
    expect(within(table).getByText('ssh.service')).toBeTruthy();
    expect(within(table).queryByText('apt-daily.timer')).toBeNull(); // default type filter: services
    fireEvent.click(screen.getByLabelText('Failed'));
    expect(within(table).queryByText('ssh.service')).toBeNull();
    expect(within(table).getByText('smartd.service')).toBeTruthy();
    fireEvent.click(screen.getByLabelText('All'));
    expect(within(table).queryByText('auditd.service')).toBeNull(); // not installed: hidden by default
    fireEvent.click(screen.getByLabelText(/Show units that aren’t installed/));
    expect(within(table).getAllByText('auditd.service').length).toBeGreaterThan(0);
    fireEvent.change(screen.getByLabelText('Search units'), { target: { value: 'secure shell' } });
    expect(screen.getByText('Showing 1 of 4')).toBeTruthy();
  });

  it('explains when the broker is down', async () => {
    window.history.pushState(null, '', '/services');
    mock({
      'GET /api/v1/auth/session': { status: 200, body: session },
      'GET /api/v1/capabilities': { status: 200, body: caps() },
      'GET /api/v1/services': {
        status: 503,
        body: { error: 'broker_unavailable', message: 'Start the helper.' },
      },
    });
    render(<App />);
    expect((await screen.findByRole('alert')).textContent).toBe('Start the helper.');
  });
});

describe('Hardware page', () => {
  it('labels identity confidence and vendor placeholders honestly', async () => {
    window.history.pushState(null, '', '/hardware');
    mock({
      'GET /api/v1/auth/session': { status: 200, body: session },
      'GET /api/v1/capabilities': { status: 200, body: caps() },
      'GET /api/v1/hardware': { status: 200, body: hardware },
    });
    render(<App />);
    expect(await screen.findByText('High — WWN')).toBeTruthy();
    expect(screen.getByText('None — no stable identifier found')).toBeTruthy();
    expect(screen.getByText(/currently sda \(changes between boots\)/)).toBeTruthy();
    expect(screen.getByText('vendor placeholder, not unique')).toBeTruthy();
    expect(screen.getByText('ABC123')).toBeTruthy();
    expect(screen.getByText('No USB bus is visible on this system.')).toBeTruthy();
  });
});

describe('Legacy page', () => {
  it('shows the security review from the broker', async () => {
    window.history.pushState(null, '', '/legacy');
    mock({
      'GET /api/v1/auth/session': { status: 200, body: { ...session, legacy_url: 'https://host:10000/' } },
      'GET /api/v1/capabilities': { status: 200, body: caps() },
      'GET /api/v1/legacy/webmin': { status: 200, body: legacy },
    });
    render(<App />);
    expect(await screen.findByText(/older than the audited baseline/)).toBeTruthy();
    expect(screen.getByText('None (plain HTTP)')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open the legacy Webmin interface' }).getAttribute('href')).toBe(
      'https://host:10000/',
    );
  });

  it('navigates without a full page load', async () => {
    mock({
      'GET /api/v1/auth/session': { status: 200, body: session },
      'GET /api/v1/capabilities': { status: 200, body: caps() },
      'GET /api/v1/host/overview': { status: 503, body: {} },
      'GET /api/v1/legacy/webmin': { status: 200, body: legacy },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('link', { name: 'Legacy Webmin' }));
    expect(window.location.pathname).toBe('/legacy');
    expect(await screen.findByRole('heading', { name: 'Security review' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Legacy Webmin' }).getAttribute('aria-current')).toBe('page');
  });
});
