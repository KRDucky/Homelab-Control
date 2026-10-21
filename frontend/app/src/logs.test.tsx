// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { App } from './App';

// Test fixtures only — not real host data.
const session = {
  username: 'admin',
  scopes: ['services.read', 'services.logs'],
  csrf_token: 't',
  version: '0.1.0',
};

function caps(logs: { status: string; reason?: string }) {
  return {
    broker: { status: 'available' },
    capabilities: [
      { id: 'overview', label: 'Overview', path: '/', scope: 'system.read', status: 'available' },
      { id: 'services', label: 'Services', path: '/services', scope: 'services.read', status: 'available' },
      { id: 'hardware', label: 'Hardware', path: '/hardware', scope: 'hardware.read', status: 'available' },
      { id: 'legacy', label: 'Legacy Webmin', path: '/legacy', scope: 'system.read', status: 'available' },
      { id: 'service_logs', label: 'Service logs', path: '/services', scope: 'services.logs', ...logs },
    ],
  };
}

const units = {
  truncated: false,
  source: 'systemctl',
  counts: {},
  units: [
    {
      name: 'ssh.service',
      type: 'service',
      load: 'loaded',
      active: 'active',
      sub: 'running',
      description: 'SSH',
    },
    {
      name: 'ghost.service',
      type: 'service',
      load: 'not-found',
      active: 'inactive',
      sub: 'dead',
      description: 'x',
    },
  ],
};

const sshLogs = {
  unit: 'ssh.service',
  requested: 100,
  truncated: true,
  skipped: 1,
  source: 'journalctl --unit --output=json',
  entries: [
    {
      time: '2026-10-21T08:00:00Z',
      priority: 6,
      pid: 1,
      identifier: 'systemd',
      message: 'Started ssh.service.',
    },
    {
      time: '2026-10-21T08:01:00Z',
      priority: 3,
      pid: 812,
      identifier: 'sshd',
      message: '<img src=x onerror=alert(1)>',
    },
    { priority: 4, message: 'x'.repeat(10), truncated: true },
  ],
};

type Route = { status: number; body?: unknown };
let calls: string[] = [];

function mock(routes: Record<string, Route | ((url: string) => Route)>) {
  calls = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const key = `${init?.method ?? 'GET'} ${url.split('?')[0]}`;
      calls.push(`${init?.method ?? 'GET'} ${url}`);
      const r0 = routes[key];
      const r = (typeof r0 === 'function' ? r0(url) : r0) ?? {
        status: 404,
        body: { error: 'not_found', message: 'nope' },
      };
      return new Response(JSON.stringify(r.body ?? {}), { status: r.status });
    }),
  );
}

function base(logs: { status: string; reason?: string }) {
  return {
    'GET /api/v1/auth/session': { status: 200, body: session },
    'GET /api/v1/capabilities': { status: 200, body: caps(logs) },
    'GET /api/v1/services': { status: 200, body: units },
  };
}

beforeEach(() => window.history.pushState(null, '', '/services'));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('service logs', () => {
  it('lists one navigation entry per page even though logs share /services', async () => {
    mock(base({ status: 'available' }));
    render(<App />);
    const nav = await screen.findByRole('navigation', { name: 'Main' });
    await within(nav).findByRole('link', { name: 'Services' });
    expect(within(nav).queryByText('Service logs')).toBeNull();
  });

  it('shows the latest entries of a unit as text, with levels and caps explained', async () => {
    mock({
      ...base({ status: 'available' }),
      'GET /api/v1/services/units/ssh.service/logs': { status: 200, body: sshLogs },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Show logs for ssh.service' }));
    const panel = await screen.findByRole('region', { name: 'Logs: ssh.service' });
    await within(panel).findByText('Started ssh.service.');
    expect(calls).toContain('GET /api/v1/services/units/ssh.service/logs?lines=100');
    // Journal content is rendered as text, never as markup.
    expect(within(panel).getByText('<img src=x onerror=alert(1)>')).toBeTruthy();
    expect(panel.querySelector('img')).toBeNull();
    expect(within(panel).getByText('err')).toBeTruthy();
    expect(within(panel).getByText('sshd[812]')).toBeTruthy();
    expect(within(panel).getByText('cut at 4 KiB')).toBeTruthy();
    expect(within(panel).getByText(/left out to keep this response under 1 MiB/)).toBeTruthy();
    expect(within(panel).getByText(/1 journal record was unreadable/)).toBeTruthy();
    // No log action for units systemd does not have.
    expect(screen.queryByRole('button', { name: 'Show logs for ghost.service' })).toBeNull();
  });

  it('requests a different number of entries and can be closed', async () => {
    mock({
      ...base({ status: 'available' }),
      'GET /api/v1/services/units/ssh.service/logs': { status: 200, body: sshLogs },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Show logs for ssh.service' }));
    const panel = await screen.findByRole('region', { name: 'Logs: ssh.service' });
    await within(panel).findByText('Started ssh.service.');
    fireEvent.change(within(panel).getByLabelText('Entries'), { target: { value: '500' } });
    await vi.waitFor(() => expect(calls).toContain('GET /api/v1/services/units/ssh.service/logs?lines=500'));
    fireEvent.click(within(panel).getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('region', { name: 'Logs: ssh.service' })).toBeNull();
  });

  it('explains a unit that disappeared and a missing permission', async () => {
    mock({
      ...base({ status: 'available' }),
      'GET /api/v1/services/units/ssh.service/logs': {
        status: 404,
        body: { error: 'not_found', message: 'no such unit' },
      },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Show logs for ssh.service' }));
    expect((await screen.findByRole('alert')).textContent).toContain('systemd no longer lists this unit');
  });

  it('hides the action and says why when the account lacks services.logs', async () => {
    mock(base({ status: 'forbidden', reason: 'Your account does not have the services.logs permission.' }));
    render(<App />);
    await screen.findByText('ssh.service');
    expect(screen.queryByRole('button', { name: /Show logs/ })).toBeNull();
    expect(screen.getByRole('note').textContent).toContain('does not have the services.logs permission');
    expect(calls.some((c) => c.includes('/logs'))).toBe(false);
  });
});
