// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { App } from './App';

// Test fixtures only — not real host data.
const session = {
  username: 'admin',
  scopes: ['system.read', 'audit.read'],
  csrf_token: 'csrf-1',
  version: '0.1.0',
};

function caps(audit: { status: string; reason?: string }) {
  return {
    broker: { status: 'available' },
    capabilities: [
      { id: 'overview', label: 'Overview', path: '/', scope: 'system.read', status: 'available' },
      { id: 'audit', label: 'Audit', path: '/audit', scope: 'audit.read', ...audit },
    ],
  };
}

const sessions = {
  persistent: true,
  idle_timeout_seconds: 1800,
  max_lifetime_seconds: 43200,
  sessions: [
    {
      id: 'a'.repeat(64),
      created: '2026-10-21T08:00:00Z',
      last_seen: '2026-10-21T09:00:00Z',
      remote_ip: '192.0.2.10',
      user_agent: 'Firefox',
      current: true,
    },
    {
      id: 'b'.repeat(64),
      created: '2026-10-20T08:00:00Z',
      last_seen: '2026-10-20T09:00:00Z',
      remote_ip: '192.0.2.20',
      user_agent: 'Phone',
      current: false,
    },
  ],
};

function ev(id: number, event: string, outcome = 'success', detail = '') {
  return {
    id,
    seq: id,
    time: '2026-10-21T08:00:00Z',
    event,
    user: 'admin',
    remote: '192.0.2.10',
    outcome,
    detail,
    prev: '0',
    hash: '1',
  };
}

type Route = { status: number; body?: unknown };
let calls: { url: string; init?: RequestInit }[] = [];

function mock(routes: Record<string, Route | ((url: string) => Route)>) {
  calls = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, ...(init ? { init } : {}) });
      const key = `${init?.method ?? 'GET'} ${url.split('?')[0]}`;
      const r0 = routes[key];
      const r = (typeof r0 === 'function' ? r0(url) : r0) ?? {
        status: 404,
        body: { error: 'not_found', message: 'nope' },
      };
      return new Response(r.status === 204 ? null : JSON.stringify(r.body ?? {}), { status: r.status });
    }),
  );
}

const base = {
  'GET /api/v1/auth/session': { status: 200, body: session },
  'GET /api/v1/capabilities': { status: 200, body: caps({ status: 'available' }) },
  'GET /api/v1/host/overview': { status: 503, body: {} },
};

beforeEach(() => window.history.pushState(null, '', '/'));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('sessions', () => {
  it('lists own sessions and ends another one with the CSRF token', async () => {
    let list = sessions;
    mock({
      ...base,
      'GET /api/v1/auth/sessions': () => ({ status: 200, body: list }),
      [`POST /api/v1/auth/sessions/${'b'.repeat(64)}/revoke`]: () => {
        list = { ...sessions, sessions: sessions.sessions.slice(0, 1) };
        return { status: 204 };
      },
    });
    window.history.pushState(null, '', '/account/sessions');
    render(<App />);
    expect(await screen.findByRole('heading', { name: 'Sessions', level: 1 })).toBeTruthy();
    await screen.findByText('Phone');
    expect(screen.getByText('this browser')).toBeTruthy();
    expect(screen.getByText(/survive a restart/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'End session from 192.0.2.20' }));
    expect((await screen.findByRole('status')).textContent).toContain('Session ended');
    const post = calls.find((c) => c.init?.method === 'POST');
    expect(new Headers(post?.init?.headers).get('X-CSRF-Token')).toBe('csrf-1');
    await vi.waitFor(() => expect(screen.queryByText('Phone')).toBeNull());
    expect(screen.getByText('No other sessions.')).toBeTruthy();
  });

  it('returns to the sign-in screen after ending the current session', async () => {
    mock({
      ...base,
      'GET /api/v1/auth/sessions': { status: 200, body: sessions },
      [`POST /api/v1/auth/sessions/${'a'.repeat(64)}/revoke`]: { status: 204 },
    });
    window.history.pushState(null, '', '/account/sessions');
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Sign out this browser' }));
    expect(await screen.findByLabelText('Password')).toBeTruthy();
  });

  it('is reachable from the top bar', async () => {
    mock({ ...base, 'GET /api/v1/auth/sessions': { status: 200, body: sessions } });
    render(<App />);
    fireEvent.click(await screen.findByRole('link', { name: 'Sessions' }));
    expect(await screen.findByRole('heading', { name: 'Sessions', level: 1 })).toBeTruthy();
    expect(window.location.pathname).toBe('/account/sessions');
  });
});

describe('audit', () => {
  it('pages through events, filters by type, and renders details as text', async () => {
    mock({
      ...base,
      'GET /api/v1/audit': (url) => {
        const u = new URL(url, 'http://x');
        if (u.searchParams.get('before') === '2')
          return { status: 200, body: { events: [ev(1, 'audit.chain', 'ok', 'new chain')], source: 's' } };
        if (u.searchParams.get('event') === 'auth.')
          return { status: 200, body: { events: [ev(3, 'auth.login', 'failure')], source: 's' } };
        return {
          status: 200,
          body: {
            events: [
              ev(3, 'auth.login', 'failure'),
              ev(2, 'services.logs.read', 'success', '<b>ssh.service</b> lines=100'),
            ],
            next_before: 2,
            source: 'hc-api state database',
          },
        };
      },
    });
    window.history.pushState(null, '', '/audit');
    render(<App />);
    const table = await screen.findByRole('table');
    await within(table).findByText('services.logs.read');
    expect(within(table).getByText('<b>ssh.service</b> lines=100')).toBeTruthy();
    expect(table.querySelector('b')).toBeNull();
    expect(within(table).getByText('failure').closest('tr')?.className).toBe('row-crit');
    fireEvent.click(screen.getByRole('button', { name: 'Show older events' }));
    await within(table).findByText('audit.chain');
    expect(screen.queryByRole('button', { name: 'Show older events' })).toBeNull();
    fireEvent.change(screen.getByLabelText('Show'), { target: { value: 'auth.' } });
    await vi.waitFor(() => expect(within(table).queryByText('audit.chain')).toBeNull());
    expect(calls.some((c) => c.url.includes('event=auth.'))).toBe(true);
  });

  it('is not a link without audit.read', async () => {
    mock({
      ...base,
      'GET /api/v1/capabilities': {
        status: 200,
        body: caps({ status: 'forbidden', reason: 'Your account does not have the audit.read permission.' }),
      },
    });
    render(<App />);
    const nav = await screen.findByRole('navigation', { name: 'Main' });
    await within(nav).findByText('Audit');
    expect(within(nav).queryByRole('link', { name: /Audit/ })).toBeNull();
    expect(within(nav).getByText(/audit.read permission/)).toBeTruthy();
  });
});
