// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { App } from './App';

// Test fixtures only — not a real host tree.
function session(scopes: string[]) {
  return { username: 'admin', scopes, csrf_token: 'csrf-fixture', version: '0.1.0' };
}

const filesCap = {
  id: 'files' as const,
  label: 'Files',
  path: '/files',
  scope: 'files.read',
  status: 'degraded',
  reason:
    'Read-only listing inside allow-listed roots; create, rename, permissions, upload and delete are not available.',
};

const caps = {
  broker: { status: 'available' },
  modules: [],
  capabilities: [
    { id: 'overview', label: 'Overview', path: '/', scope: 'system.read', status: 'available' },
    filesCap,
  ],
};

const roots = {
  browse_available: true,
  roots: [{ id: 'lab', label: 'Lab files' }],
};

const rootEntries = {
  root: 'lab',
  path: '.',
  entries: [
    {
      name: 'sub',
      dir: true,
      size: 0,
      owner: '1000:1000',
      mode: 'drwxr-xr-x',
      mtime: '2026-11-07T12:00:00Z',
    },
    {
      name: 'readme.txt',
      dir: false,
      size: 12,
      owner: '1000:1000',
      mode: '-rw-r--r--',
      mtime: '2026-11-07T12:00:00Z',
    },
  ],
};

const subEntries = {
  root: 'lab',
  path: 'sub',
  entries: [
    {
      name: 'inner.txt',
      dir: false,
      size: 5,
      owner: '1000:1000',
      mode: '-rw-r--r--',
      mtime: '2026-11-07T12:00:00Z',
    },
  ],
};

let calls: { key: string; init: RequestInit | undefined }[] = [];

function mock(scopes: string[], routes: Record<string, { status: number; body: unknown }> = {}) {
  calls = [];
  const all: Record<string, { status: number; body: unknown }> = {
    'GET /api/v1/auth/session': { status: 200, body: session(scopes) },
    'GET /api/v1/capabilities': { status: 200, body: caps },
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

beforeEach(() => window.history.pushState(null, '', '/files'));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('files', () => {
  it('is gated off without files.read and does not call the files API', async () => {
    mock(['system.read'], {
      'GET /api/v1/capabilities': {
        status: 200,
        body: {
          ...caps,
          capabilities: caps.capabilities.map((c) =>
            c.id === 'files' ? { ...c, status: 'forbidden', reason: 'Requires files.read.' } : c,
          ),
        },
      },
    });
    render(<App />);
    expect((await screen.findAllByText(/Requires files.read/)).length).toBeGreaterThan(0);
    expect(screen.queryByRole('button', { name: /New folder/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /Permissions/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /Download/i })).toBeNull();
    expect(calls.some((c) => c.key.includes('/api/v1/files/'))).toBe(false);
  });

  it('lists roots and entries with CSRF, hides write actions, and never POSTs', async () => {
    mock(['system.read', 'files.read'], {
      'GET /api/v1/files/roots': { status: 200, body: roots },
      'GET /api/v1/files/entries': { status: 200, body: rootEntries },
    });
    render(<App />);
    expect((await screen.findAllByText('Lab files')).length).toBeGreaterThan(0);
    expect(screen.getByText('readme.txt')).toBeTruthy();
    expect(screen.getAllByText('1000:1000').length).toBeGreaterThan(0);
    expect(screen.getByText('-rw-r--r--')).toBeTruthy();
    expect(screen.queryByText(/\/tmp|\/home|\/etc/)).toBeNull();
    expect(screen.queryByRole('button', { name: /New folder/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /Permissions/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /Download/i })).toBeNull();
    const rootsCall = calls.find((c) => c.key === 'GET /api/v1/files/roots');
    expect((rootsCall?.init?.headers as Record<string, string>)['X-CSRF-Token']).toBe('csrf-fixture');
    const entriesCall = calls.find((c) => c.key === 'GET /api/v1/files/entries');
    expect((entriesCall?.init?.headers as Record<string, string>)['X-CSRF-Token']).toBe('csrf-fixture');
    expect(calls.some((c) => (c.init?.method ?? 'GET') !== 'GET')).toBe(false);
  });

  it('opens a directory by GET and still never posts writes or downloads', async () => {
    calls = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const path = url.split('?')[0] ?? url;
        const key = `${init?.method ?? 'GET'} ${path}`;
        calls.push({ key, init });
        if (path.endsWith('/auth/session')) {
          return new Response(JSON.stringify(session(['system.read', 'files.read'])), { status: 200 });
        }
        if (path.endsWith('/capabilities')) {
          return new Response(JSON.stringify(caps), { status: 200 });
        }
        if (path.endsWith('/files/roots')) {
          return new Response(JSON.stringify(roots), { status: 200 });
        }
        if (path.endsWith('/files/entries')) {
          const q = new URL(url, 'http://hc.test');
          const body = q.searchParams.get('path') === 'sub' ? subEntries : rootEntries;
          return new Response(JSON.stringify(body), { status: 200 });
        }
        return new Response(JSON.stringify({ error: 'not_found' }), { status: 404 });
      }),
    );
    render(<App />);
    const folders = await screen.findAllByRole('button', { name: 'sub' });
    const folder = folders[0];
    if (!folder) throw new Error('fixture directory control missing');
    fireEvent.click(folder);
    expect(await screen.findByText('inner.txt')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /New folder/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /Download/i })).toBeNull();
    expect(calls.some((c) => (c.init?.method ?? 'GET') !== 'GET')).toBe(false);
    expect(calls.filter((c) => c.key === 'GET /api/v1/files/entries').length).toBeGreaterThan(1);
  });

  it('says when no roots are configured', async () => {
    mock(['system.read', 'files.read'], {
      'GET /api/v1/files/roots': { status: 200, body: { browse_available: false, roots: [] } },
    });
    render(<App />);
    expect(await screen.findByText(/No browseable file roots are configured/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /New folder/i })).toBeNull();
    expect(calls.some((c) => c.key === 'GET /api/v1/files/entries')).toBe(false);
  });

  it('does not treat capability available as a write', async () => {
    mock(['system.read', 'files.read'], {
      'GET /api/v1/capabilities': {
        status: 200,
        body: {
          ...caps,
          capabilities: caps.capabilities.map((c) =>
            c.id === 'files' ? { ...c, status: 'available', reason: '' } : c,
          ),
        },
      },
      'GET /api/v1/files/roots': { status: 200, body: roots },
      'GET /api/v1/files/entries': { status: 200, body: rootEntries },
    });
    render(<App />);
    expect(await screen.findByText('readme.txt')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /New folder/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /Permissions/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /Download/i })).toBeNull();
    expect(calls.some((c) => (c.init?.method ?? 'GET') !== 'GET')).toBe(false);
  });
});
