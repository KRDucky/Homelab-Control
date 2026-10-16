import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { App } from './App';
import { formatBytes, formatUptime, level, ratio } from './format';
import { Meter } from './Meter';
import type { Overview } from './api';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

// Test fixture only — not real host data.
const fixture: Overview = {
  collected_at: '2026-10-15T08:00:00Z',
  system: {
    status: 'ok',
    sources: ['/proc/sys/kernel/hostname'],
    hostname: 'fixture-host',
    kernel: '6.8.0-fixture',
    os: { pretty_name: 'Fixture OS 1.0' },
    distro_family: 'debian',
    uptime_seconds: 90061,
    load_average: [0.1, 0.2, 0.3],
  },
  cpu: {
    status: 'ok',
    sources: [],
    logical_cpus: 4,
    model: 'Fixture CPU',
    usage_percent: 12.5,
    sample_millis: 250,
  },
  memory: {
    status: 'ok',
    sources: [],
    total_bytes: 1000,
    available_bytes: 400,
    used_bytes: 600,
    swap_total_bytes: 0,
    swap_free_bytes: 0,
  },
  filesystems: {
    status: 'ok',
    sources: [],
    view: 'host',
    items: [
      {
        kernel_source: 'nas:/x',
        mount_point: '/mnt/nas',
        fs_type: 'nfs4',
        read_only: false,
        probed: false,
        skip_reason: 'network filesystem not probed',
      },
    ],
  },
  network: {
    status: 'unavailable',
    sources: ['/sys/class/net'],
    errors: ['permission denied'],
    interfaces: [],
  },
  legacy_webmin: { status: 'ok', sources: [], installed: false, config_readable: false },
};

// Test fixture only — not real host data.
const allCaps = {
  broker: { status: 'available', version: 't' },
  capabilities: [
    { id: 'overview', label: 'Overview', path: '/', scope: 'system.read', status: 'available' },
    { id: 'services', label: 'Services', path: '/services', scope: 'services.read', status: 'available' },
    { id: 'hardware', label: 'Hardware', path: '/hardware', scope: 'hardware.read', status: 'available' },
    { id: 'legacy', label: 'Legacy Webmin', path: '/legacy', scope: 'system.read', status: 'available' },
  ],
};

function mockFetch(routes: Record<string, { status: number; body?: unknown }>) {
  const calls: { url: string; init?: RequestInit }[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, ...(init ? { init } : {}) });
      const key = `${init?.method ?? 'GET'} ${url}`;
      const r = routes[key] ?? { status: 404, body: { error: 'not_found', message: 'nope' } };
      return new Response(r.status === 204 ? null : JSON.stringify(r.body ?? {}), { status: r.status });
    }),
  );
  return calls;
}

describe('format helpers', () => {
  it('formats bytes and uptime', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(1536)).toBe('1.5 KiB');
    expect(formatBytes(undefined)).toBe('—');
    expect(formatUptime(90061)).toBe('1d 1h');
    expect(formatUptime(undefined)).toBe('—');
  });
  it('computes ratios and levels without inventing data', () => {
    expect(ratio(1, 0)).toBeUndefined();
    expect(ratio(undefined, 10)).toBeUndefined();
    expect(level(0.5)).toBe('ok');
    expect(level(0.8)).toBe('warn');
    expect(level(0.95)).toBe('crit');
  });
});

describe('Meter', () => {
  it('reports unknown instead of zero when data is missing', () => {
    render(<Meter label="Swap" used={undefined} total={undefined} detail="No swap configured" />);
    expect(screen.getByRole('meter', { name: 'Swap' }).getAttribute('aria-valuetext')).toBe('Unknown');
  });
  it('uses no inline styles (strict CSP)', () => {
    const { container } = render(<Meter label="RAM" used={50} total={100} detail="x" />);
    expect(container.querySelectorAll('[style]').length).toBe(0);
    expect(container.querySelectorAll('.seg-on').length).toBe(12);
  });
});

describe('App', () => {
  it('shows the login form when there is no session', async () => {
    mockFetch({
      'GET /api/v1/auth/session': { status: 401, body: { error: 'unauthenticated', message: 'x' } },
    });
    render(<App />);
    expect(await screen.findByLabelText('Username')).toBeTruthy();
  });

  it('logs in, renders the overview and labels unavailable sections', async () => {
    mockFetch({
      'GET /api/v1/auth/session': { status: 401, body: { error: 'unauthenticated', message: 'x' } },
      'POST /api/v1/auth/login': {
        status: 200,
        body: { username: 'admin', scopes: ['system.read'], csrf_token: 't', version: '0.1.0' },
      },
      'GET /api/v1/host/overview': { status: 200, body: fixture },
      'GET /api/v1/capabilities': { status: 200, body: allCaps },
    });
    render(<App />);
    fireEvent.change(await screen.findByLabelText('Username'), { target: { value: 'admin' } });
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'a-long-password' } });
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('heading', { name: 'fixture-host' })).toBeTruthy();
    expect(screen.getByText('Unavailable')).toBeTruthy();
    expect(screen.getByText('network filesystem not probed')).toBeTruthy();
  });

  it('explains bad credentials without saying which field was wrong', async () => {
    mockFetch({
      'GET /api/v1/auth/session': { status: 401, body: {} },
      'POST /api/v1/auth/login': { status: 401, body: { error: 'invalid_credentials', message: 'x' } },
    });
    render(<App />);
    fireEvent.change(await screen.findByLabelText('Username'), { target: { value: 'admin' } });
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'wrong-password' } });
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect((await screen.findByRole('alert')).textContent).toMatch(/don’t match an account/);
  });

  it('sends the CSRF token on sign-out', async () => {
    const calls = mockFetch({
      'GET /api/v1/auth/session': {
        status: 200,
        body: { username: 'admin', scopes: ['system.read'], csrf_token: 'csrf-123', version: '0.1.0' },
      },
      'GET /api/v1/host/overview': { status: 200, body: fixture },
      'GET /api/v1/capabilities': { status: 200, body: allCaps },
      'POST /api/v1/auth/logout': { status: 204 },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    await waitFor(() => expect(screen.getByLabelText('Username')).toBeTruthy());
    const out = calls.find((c) => c.url === '/api/v1/auth/logout');
    expect(new Headers(out?.init?.headers).get('X-CSRF-Token')).toBe('csrf-123');
  });
});
