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

  it('does not treat capability available as host.settings.apply', async () => {
    // CapAvailable means this server has the broker op and binaries, not that
    // the signed-in account may plan or apply.
    mock(['system.read', 'host.settings.preview'], {
      'GET /api/v1/capabilities': {
        status: 200,
        body: {
          ...caps,
          capabilities: caps.capabilities.map((c) =>
            c.id === 'host_settings' ? { ...c, status: 'available', reason: '' } : c,
          ),
        },
      },
      'POST /api/v1/host/settings/preview': {
        status: 200,
        body: { ...preview, apply_available: true, valid: true, warnings: [] },
      },
    });
    render(<App />);
    fireEvent.change(await screen.findByLabelText('Static host name'), { target: { value: 'nas-01' } });
    fireEvent.click(screen.getByRole('button', { name: 'Preview (does not apply)' }));
    expect(await screen.findByText('Preview result — not applied')).toBeTruthy();
    expect(screen.getAllByText(/requires the host.settings.apply permission/).length).toBeGreaterThan(0);
    expect(screen.queryByText(/needs a separate confirmation/)).toBeNull();
    expect(screen.queryByRole('button', { name: /^Apply/ })).toBeNull();
    expect(calls.some((c) => /\/host\/settings\/(apply|plan)$/.test(c.key))).toBe(false);
  });

  it('does not offer Apply when apply_available is false, and preview still never posts apply or plan', async () => {
    mock(['system.read', 'host.settings.preview', 'host.settings.apply'], {
      'POST /api/v1/host/settings/preview': { status: 200, body: { ...preview, valid: true } },
    });
    render(<App />);
    fireEvent.change(await screen.findByLabelText('Static host name'), { target: { value: 'nas-01' } });
    fireEvent.click(screen.getByRole('button', { name: 'Preview (does not apply)' }));
    expect((await screen.findAllByText(/Apply is not available from this server right now/)).length).toBeGreaterThan(0);
    expect(screen.queryByRole('button', { name: /^Apply/ })).toBeNull();
    expect(calls.some((c) => /\/host\/settings\/(apply|plan)$/.test(c.key))).toBe(false);
  });

  it('does not offer Apply when the preview is not valid, even with apply scope and apply_available', async () => {
    mock(['system.read', 'host.settings.preview', 'host.settings.apply'], {
      'POST /api/v1/host/settings/preview': {
        status: 200,
        body: { ...preview, apply_available: true, valid: false },
      },
    });
    render(<App />);
    fireEvent.change(await screen.findByLabelText('Static host name'), { target: { value: 'nas-01' } });
    fireEvent.click(screen.getByRole('button', { name: 'Preview (does not apply)' }));
    expect(await screen.findByText('Preview result — not applied')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^Apply/ })).toBeNull();
    expect(calls.some((c) => /\/host\/settings\/(apply|plan)$/.test(c.key))).toBe(false);
  });

  it('plans then applies behind host.settings.apply, shows the TLS warning, and skips step-up', async () => {
    const plan = {
      action: 'set',
      risk: 'reversible',
      step_up_required: false,
      confirmation_required: true,
      token: 'hcp1.plan-token-fixture',
      expires_at: new Date(Date.now() + 120_000).toISOString(),
      observed: {
        static_hostname: 'fixture-static',
        static_status: 'observed',
        pretty_status: 'not-set',
        timezone_status: 'observed',
        timezone: 'Europe/Berlin',
        notes: [
          'This result reports hostnamectl and timedatectl. The kernel host name seen by hc-api may stay at the value from when that service started (ProtectHostname=yes); it is not proof that the kernel was renamed.',
        ],
      },
      effect: 'This would set the static host name.',
      consequences: ['Confirmation is required. This class is reversible: it does not require a password re-entry.'],
      warnings: [
        'Changing the static host name can invalidate TLS certificates that name this host, mDNS/Avahi names, and entries in /etc/hosts. Homelab Control does not update those.',
      ],
      changes: [
        { field: 'static_hostname', current: 'fixture-static', requested: 'nas-01', outcome: 'would-change' },
      ],
      fields: ['static_hostname'],
    };
    mock(['system.read', 'host.settings.preview', 'host.settings.apply'], {
      'POST /api/v1/host/settings/preview': {
        status: 200,
        body: {
          ...preview,
          apply_available: true,
          valid: true,
          notice: 'Preview only. Nothing was changed on this host.',
          warnings: ['localhost is accepted with a warning.'],
          changes: [
            { field: 'static_hostname', current: 'fixture-static', requested: 'nas-01', outcome: 'would-change' },
          ],
        },
      },
      'POST /api/v1/host/settings/plan': { status: 200, body: plan },
      'POST /api/v1/host/settings/apply': {
        status: 200,
        body: {
          operation_id: 'op-hs-fixture-1',
          action: 'set',
          outcome: 'succeeded',
          message: 'hostnamectl/timedatectl report the requested values.',
          fields: ['static_hostname'],
          observed: plan.observed,
          started_at: '2026-11-06T09:00:00Z',
          finished_at: '2026-11-06T09:00:02Z',
        },
      },
    });
    render(<App />);
    fireEvent.change(await screen.findByLabelText('Static host name'), { target: { value: 'nas-01' } });
    fireEvent.click(screen.getByRole('button', { name: 'Preview (does not apply)' }));
    expect(await screen.findByRole('button', { name: 'Apply these settings…' })).toBeTruthy();
    const previewWarning = screen.getByText('localhost is accepted with a warning.');
    expect(previewWarning.className).toContain('banner');
    expect(previewWarning.getAttribute('role')).toBe('note');
    expect(calls.some((c) => /\/host\/settings\/(apply|plan)$/.test(c.key))).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Apply these settings…' }));
    const dialog = await screen.findByRole('dialog', { name: 'Apply host settings' });
    expect(await screen.findByText('Preview · nothing has been changed yet')).toBeTruthy();
    expect(screen.getByText('Reversible')).toBeTruthy();
    expect(screen.getByText(/TLS certificates/)).toBeTruthy();
    expect(screen.getByText(/mDNS/)).toBeTruthy();
    expect(screen.getByText(/\/etc\/hosts/)).toBeTruthy();
    expect(screen.getByText(/ProtectHostname=yes/)).toBeTruthy();
    expect(screen.queryByLabelText(/password/i)).toBeNull();
    const planned = calls.find((c) => c.key === 'POST /api/v1/host/settings/plan');
    expect((planned?.init?.headers as Record<string, string>)['X-CSRF-Token']).toBe('csrf-fixture');
    expect(JSON.parse(String(planned?.init?.body))).toEqual({ static_hostname: 'nas-01' });
    expect(calls.some((c) => c.key === 'POST /api/v1/host/settings/apply')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Apply host settings' }));
    expect(await screen.findByText('Done')).toBeTruthy();
    const applied = calls.find((c) => c.key === 'POST /api/v1/host/settings/apply');
    expect((applied?.init?.headers as Record<string, string>)['X-CSRF-Token']).toBe('csrf-fixture');
    expect(JSON.parse(String(applied?.init?.body))).toEqual({
      static_hostname: 'nas-01',
      token: 'hcp1.plan-token-fixture',
    });
    expect(dialog.querySelector('input[type="password"]')).toBeNull();
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
