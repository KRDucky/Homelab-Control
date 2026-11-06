// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { App } from './App';

// Test fixtures only — not real host data.
function session(scopes: string[]) {
  return { username: 'admin', scopes, csrf_token: 'csrf-fixture', version: '0.1.0' };
}

const caps = {
  broker: { status: 'available' },
  modules: [],
  capabilities: [
    { id: 'overview', label: 'Overview', path: '/', scope: 'system.read', status: 'available' },
    {
      id: 'host_settings',
      label: 'Host settings',
      path: '/host-settings',
      scope: 'system.read',
      status: 'degraded',
      reason: 'Read-only.',
    },
  ],
};

const settings = {
  collected_at: '2026-11-06T08:00:00Z',
  read_only: true,
  settings: {
    kernel_hostname: { value: 'fixture-kernel', status: 'observed', source: '/proc/sys/kernel/hostname' },
    static_hostname: { value: 'fixture-static', status: 'observed', source: '/etc/hostname' },
    pretty_hostname: { status: 'not-set', source: '/etc/machine-info' },
    timezone: { status: 'unavailable', source: '/etc/localtime', note: 'Not a link.' },
  },
};

const preview = {
  preview_only: true,
  executed: false,
  host_changed: false,
  apply_available: false,
  notice:
    'Preview only. Nothing was changed on this host: Homelab Control cannot apply hostname or time zone changes yet.',
  valid: false,
  collected_at: '2026-11-06T08:00:01Z',
  current: settings.settings,
  changes: [
    { field: 'static_hostname', current: 'fixture-static', requested: 'nas-01', outcome: 'would-change' },
    {
      field: 'timezone',
      current: '',
      requested: 'Mars/Base',
      outcome: 'invalid',
      reason: 'Not in the database.',
    },
  ],
  warnings: [],
};

let calls: { key: string; init: RequestInit | undefined }[] = [];

function mock(scopes: string[], routes: Record<string, { status: number; body: unknown }> = {}) {
  calls = [];
  const all: Record<string, { status: number; body: unknown }> = {
    'GET /api/v1/auth/session': { status: 200, body: session(scopes) },
    'GET /api/v1/capabilities': { status: 200, body: caps },
    'GET /api/v1/host/settings': { status: 200, body: settings },
    ...routes,
  };
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const key = `${init?.method ?? 'GET'} ${url.split('?')[0]}`;
      calls.push({ key, init });
      const r = all[key] ?? { status: 404, body: { error: 'not_found', message: 'nope' } };
      return new Response(JSON.stringify(r.body), { status: r.status });
    }),
  );
}

beforeEach(() => window.history.pushState(null, '', '/host-settings'));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('host settings', () => {
  it('shows observed values and says when a value is not set or unavailable', async () => {
    mock(['system.read']);
    render(<App />);
    expect(await screen.findByText('fixture-static')).toBeTruthy();
    expect(screen.getByText('fixture-kernel')).toBeTruthy();
    expect(screen.getByText('Not set')).toBeTruthy();
    expect(screen.getByText('Unavailable')).toBeTruthy();
    expect(screen.getByText(/Preview only\. Nothing is changed on this host/)).toBeTruthy();
  });

  it('hides the preview form without host.settings.preview', async () => {
    mock(['system.read']);
    render(<App />);
    await screen.findByText('fixture-static');
    expect(screen.queryByRole('button', { name: /Preview/ })).toBeNull();
    expect(screen.getByText(/does not have the host.settings.preview permission/)).toBeTruthy();
  });

  it('sends a CSRF-protected preview and labels the result as not applied; no apply call exists', async () => {
    mock(['system.read', 'host.settings.preview'], {
      'POST /api/v1/host/settings/preview': { status: 200, body: preview },
    });
    render(<App />);
    fireEvent.change(await screen.findByLabelText('Static host name'), { target: { value: 'nas-01' } });
    fireEvent.change(screen.getByLabelText('Time zone'), { target: { value: 'Mars/Base' } });
    fireEvent.click(screen.getByRole('button', { name: 'Preview (does not apply)' }));
    expect(await screen.findByText(/Nothing was changed on this host/)).toBeTruthy();
    expect(screen.getByText('Preview result — not applied')).toBeTruthy();
    expect(screen.getByText('Would change')).toBeTruthy();
    expect(screen.getByText('Not accepted')).toBeTruthy();
    const post = calls.find((c) => c.key === 'POST /api/v1/host/settings/preview');
    expect((post?.init?.headers as Record<string, string>)['X-CSRF-Token']).toBe('csrf-fixture');
    expect(JSON.parse(String(post?.init?.body))).toEqual({
      static_hostname: 'nas-01',
      timezone: 'Mars/Base',
    });
    expect(calls.some((c) => /apply|plan/.test(c.key))).toBe(false);
    expect(screen.queryByRole('button', { name: /^Apply/ })).toBeNull();
  });

  it('shows the server refusal of a preview', async () => {
    mock(['system.read', 'host.settings.preview'], {
      'POST /api/v1/host/settings/preview': {
        status: 403,
        body: { error: 'csrf_rejected', message: 'missing or invalid CSRF token' },
      },
    });
    render(<App />);
    fireEvent.change(await screen.findByLabelText('Static host name'), { target: { value: 'nas-01' } });
    fireEvent.click(screen.getByRole('button', { name: 'Preview (does not apply)' }));
    expect((await screen.findByRole('alert')).textContent).toContain('missing or invalid CSRF token');
  });
});
