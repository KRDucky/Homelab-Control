// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
    'Confined-root listing, read, download, write, upload, create, rename and delete, subject to files.read/files.write/files.destroy.',
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

let calls: { key: string; url: string; init: RequestInit | undefined }[] = [];

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
      const path = url.split('?')[0] ?? url;
      const key = `${init?.method ?? 'GET'} ${path}`;
      calls.push({ key, url, init });
      const r = all[key] ?? { status: 404, body: { error: 'not_found', message: 'nope' } };
      return new Response(JSON.stringify(r.body), { status: r.status });
    }),
  );
}

beforeEach(() => {
  window.history.pushState(null, '', '/files');
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
  };
});
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
    const firstEntries = calls.find((c) => c.key === 'GET /api/v1/files/entries');
    expect(firstEntries).toBeTruthy();
    expect((firstEntries?.init?.headers as Record<string, string>)['X-CSRF-Token']).toBe('csrf-fixture');
    expect(new URL(firstEntries?.url ?? '', 'http://hc.test').searchParams.get('path')).toBe('');
    expect(calls.some((c) => (c.init?.method ?? 'GET') !== 'GET')).toBe(false);
  });

  it('shows the omitted-name count as an accessible status', async () => {
    mock(['system.read', 'files.read'], {
      'GET /api/v1/files/roots': { status: 200, body: roots },
      'GET /api/v1/files/entries': { status: 200, body: { ...rootEntries, omitted: 2 } },
    });
    render(<App />);
    const note = await screen.findByText(/2 names omitted/);
    expect(note.getAttribute('role')).toBe('status');
    expect(note.textContent).toContain('unrepresentable or unreadable');
  });

  it('clears a stale omitted count when navigation fails', async () => {
    routedFetch({ '': { ...rootEntries, omitted: 2 } });
    render(<App />);
    expect(await screen.findByText(/2 names omitted/)).toBeTruthy();
    fireEvent.click(screen.getAllByRole('button', { name: 'sub' })[0] as HTMLElement);
    await screen.findByText(/Unknown file root or path/);
    expect(screen.queryByText(/omitted/)).toBeNull();
  });

  it('shows no omitted note when omitted is zero', async () => {
    mock(['system.read', 'files.read'], {
      'GET /api/v1/files/roots': { status: 200, body: roots },
      'GET /api/v1/files/entries': { status: 200, body: { ...rootEntries, omitted: 0 } },
    });
    render(<App />);
    await screen.findByText('readme.txt');
    expect(screen.queryByText(/omitted/)).toBeNull();
  });

  it('opens a directory by GET and still never posts writes or downloads', async () => {
    calls = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const path = url.split('?')[0] ?? url;
        const key = `${init?.method ?? 'GET'} ${path}`;
        calls.push({ key, url, init });
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

  // Navigation and metadata UX (read-only). Fixture data only.
  const nested = {
    root: 'lab',
    path: 'sub/deep',
    entries: [
      {
        name: 'leaf.txt',
        dir: false,
        size: 2048,
        owner: '0:0',
        mode: 'urwxr-xr-x',
        mtime: '2026-11-07T12:00:00Z',
      },
      {
        name: 'link',
        dir: false,
        size: 7,
        owner: '1000:1000',
        mode: 'Lrwxrwxrwx',
        mtime: '2026-11-07T12:00:00Z',
      },
      { name: 'dev0', dir: false, size: 0, owner: '0:6', mode: 'Drw-rw----', mtime: '2026-11-07T12:00:00Z' },
    ],
  };

  function routedFetch(byPath: Record<string, unknown>, delays: Record<string, Promise<void>> = {}) {
    calls = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const path = url.split('?')[0] ?? url;
        calls.push({ key: `${init?.method ?? 'GET'} ${path}`, url, init });
        if (path.endsWith('/auth/session'))
          return new Response(JSON.stringify(session(['system.read', 'files.read'])), { status: 200 });
        if (path.endsWith('/capabilities')) return new Response(JSON.stringify(caps), { status: 200 });
        if (path.endsWith('/files/roots')) return new Response(JSON.stringify(roots), { status: 200 });
        if (path.endsWith('/files/entries')) {
          const rel = new URL(url, 'http://hc.test').searchParams.get('path') ?? '';
          const wait = delays[rel];
          if (wait) await wait;
          const body = byPath[rel];
          if (body === undefined)
            return new Response(
              JSON.stringify({ error: 'not_found', message: 'Unknown file root or path.' }),
              { status: 404 },
            );
          return new Response(JSON.stringify(body), { status: 200 });
        }
        return new Response(JSON.stringify({ error: 'not_found' }), { status: 404 });
      }),
    );
  }

  const entryPaths = () =>
    calls
      .filter((c) => c.key === 'GET /api/v1/files/entries')
      .map((c) => new URL(c.url, 'http://hc.test').searchParams.get('path'));

  it('deep-links from the URL, goes up one level and keeps the location in history', async () => {
    window.history.pushState(null, '', '/files?root=lab&path=sub%2Fdeep');
    routedFetch({ '': rootEntries, sub: subEntries, 'sub/deep': nested });
    render(<App />);
    expect(await screen.findByText('leaf.txt')).toBeTruthy();
    expect(entryPaths()[0]).toBe('sub/deep');
    const up = screen.getByRole('button', { name: 'Up one level' });
    fireEvent.click(up);
    expect(await screen.findByText('inner.txt')).toBeTruthy();
    expect(window.location.search).toBe('?root=lab&path=sub');
    fireEvent.click(screen.getByRole('button', { name: 'Up one level' }));
    expect(await screen.findByText('readme.txt')).toBeTruthy();
    expect(window.location.search).toBe('?root=lab');
    expect((screen.getByRole('button', { name: 'Up one level' }) as HTMLButtonElement).disabled).toBe(true);
    // Back restores the previous folder from the URL.
    await act(async () => {
      window.history.back();
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(await screen.findByText('inner.txt')).toBeTruthy();
    expect(calls.some((c) => (c.init?.method ?? 'GET') !== 'GET')).toBe(false);
  });

  it('ignores an unknown root in the URL and opens the first allow-listed root', async () => {
    window.history.pushState(null, '', '/files?root=other&path=x');
    routedFetch({ '': rootEntries });
    render(<App />);
    expect(await screen.findByText('readme.txt')).toBeTruthy();
    const first = calls.find((c) => c.key === 'GET /api/v1/files/entries');
    const q = new URL(first?.url ?? '', 'http://hc.test').searchParams;
    expect(q.get('root')).toBe('lab');
    expect(q.get('path')).toBe('');
  });

  it('Alt+ArrowUp opens the parent folder', async () => {
    window.history.pushState(null, '', '/files?root=lab&path=sub');
    routedFetch({ '': rootEntries, sub: subEntries });
    render(<App />);
    const name = await screen.findByText('inner.txt');
    fireEvent.keyDown(name, { key: 'ArrowUp', altKey: true });
    expect(await screen.findByText('readme.txt')).toBeTruthy();
  });

  it('filters names in the current folder without another request', async () => {
    routedFetch({ '': rootEntries });
    render(<App />);
    await screen.findByText('readme.txt');
    const before = calls.length;
    fireEvent.change(screen.getByLabelText('Filter by name'), { target: { value: 'READ' } });
    expect(screen.getByText('1 of 2 items')).toBeTruthy();
    const table = screen.getByRole('table', { name: 'Contents of Lab files' });
    expect(within(table).queryByRole('button', { name: 'sub' })).toBeNull();
    fireEvent.change(screen.getByLabelText('Filter by name'), { target: { value: 'nothing' } });
    expect(screen.getByText('No names match the filter.')).toBeTruthy();
    expect(calls.length).toBe(before);
  });

  it('shows metadata details with type, exact size, octal mode and special bits', async () => {
    window.history.pushState(null, '', '/files?root=lab&path=sub%2Fdeep');
    routedFetch({ 'sub/deep': nested });
    render(<App />);
    await screen.findByText('leaf.txt');
    expect(screen.getByText('Symbolic link (not followed)')).toBeTruthy();
    expect(screen.getByText('Block device')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Details for leaf.txt' }));
    const details = screen.getByRole('region', { name: 'Details' });
    expect(within(details).getByText(/2,048 bytes/)).toBeTruthy();
    expect(within(details).getByText('4755')).toBeTruthy();
    expect(within(details).getByText(/setuid/)).toBeTruthy();
    expect(within(details).getByText('0:0')).toBeTruthy();
    expect(within(details).getByText('2026-11-07T12:00:00Z')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Details for link' }));
    expect(
      within(screen.getByRole('region', { name: 'Details' })).getByText(/never followed or opened/),
    ).toBeTruthy();
    // A symlink is never a navigable folder.
    expect(screen.queryByRole('button', { name: 'link' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Close details' }));
    expect(screen.queryByRole('region', { name: 'Details' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Download|Permissions|New folder/i })).toBeNull();
  });

  it('keeps the newest folder when an older listing answers late', async () => {
    let releaseSub: () => void = () => {};
    const slowSub = new Promise<void>((r) => (releaseSub = r));
    routedFetch({ '': rootEntries, sub: subEntries }, { sub: slowSub });
    render(<App />);
    await screen.findByText('readme.txt');
    // Open sub (slow), then go back to the root (fast) before sub answers.
    const table = screen.getByRole('table', { name: 'Contents of Lab files' });
    fireEvent.click(within(table).getByRole('button', { name: 'sub' }));
    fireEvent.click(screen.getByRole('button', { name: 'Lab files' }));
    expect(await screen.findByText('readme.txt')).toBeTruthy();
    await act(async () => {
      releaseSub();
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(screen.getByText('readme.txt')).toBeTruthy();
    expect(screen.queryByText('inner.txt')).toBeNull();
    expect(window.location.search).toBe('?root=lab');
  });

  it('shows the server refusal for a path it will not list', async () => {
    window.history.pushState(null, '', '/files?root=lab&path=gone');
    routedFetch({});
    render(<App />);
    expect((await screen.findByRole('alert')).textContent).toContain('Unknown file root or path.');
  });
});

describe('files write slice', () => {
  const entry = {
    name: 'readme.txt',
    dir: false,
    size: 12,
    owner: '1000:1000',
    mode: '-rw-r--r--',
    mtime: '2026-11-07T12:00:00Z',
  };

  function mockRW(scopes: string[], opts: { deleteStepUp?: boolean } = {}) {
    calls = [];
    let applied = 0;
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const path = (url.split('?')[0] ?? url) as string;
        const key = `${init?.method ?? 'GET'} ${path}`;
        calls.push({ key, url, init });
        const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
        if (path.endsWith('/auth/session')) return json(session(scopes));
        if (path.endsWith('/capabilities')) return json(caps);
        if (path.endsWith('/files/roots')) return json(roots);
        if (path.endsWith('/files/entries') && (init?.method ?? 'GET') === 'GET') return json(rootEntries);
        if (path.endsWith('/files/content') && (init?.method ?? 'GET') === 'GET') {
          return new Response('hello', {
            status: 200,
            headers: { 'X-Content-Size': '5', 'X-Content-Truncated': 'false', 'X-File-Encoding': 'utf8' },
          });
        }
        if (path.endsWith('/files/entries') && init?.method === 'POST') {
          return json({ root: 'lab', path: 'new.txt', entry: { ...entry, name: 'new.txt' } }, 201);
        }
        if (path.endsWith('/files/entries') && init?.method === 'PUT') {
          return json({ root: 'lab', path: 'renamed.txt', entry: { ...entry, name: 'renamed.txt' } });
        }
        if (path.endsWith('/files/delete/plan')) {
          return json({ token: 'tok-1', root: 'lab', path: 'readme.txt', kind: 'file', size: 12 });
        }
        if (path.endsWith('/files/delete/apply')) {
          applied += 1;
          if (opts.deleteStepUp && applied === 1) {
            return json({ error: 'step_up_required', message: 'Re-enter your password' }, 403);
          }
          return json({ root: 'lab', path: 'readme.txt', status: 'deleted' });
        }
        if (path.endsWith('/auth/step-up')) return json({ step_up_until: '2026-11-07T13:00:00Z' });
        return json({ error: 'not_found' }, 404);
      }),
    );
  }

  function openDetails() {
    const btn = screen.getByRole('button', { name: 'Details for readme.txt' });
    fireEvent.click(btn);
    return screen.getByRole('region', { name: 'Details' });
  }

  it('gates write controls on files.write and delete on files.destroy', async () => {
    mockRW(['system.read', 'files.read']);
    render(<App />);
    await screen.findByText('readme.txt');
    expect(screen.queryByRole('button', { name: 'New file' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'New folder' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Upload' })).toBeNull();
    const details = openDetails();
    expect(within(details).getByRole('button', { name: 'Open' })).toBeTruthy();
    expect(within(details).getByRole('button', { name: 'Download' })).toBeTruthy();
    expect(within(details).queryByRole('button', { name: 'Rename' })).toBeNull();
    expect(within(details).queryByRole('button', { name: 'Delete' })).toBeNull();
  });

  it('shows write and delete controls with the scopes', async () => {
    mockRW(['system.read', 'files.read', 'files.write', 'files.destroy']);
    render(<App />);
    await screen.findByText('readme.txt');
    expect(screen.getByRole('button', { name: 'New file' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'New folder' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Upload' })).toBeTruthy();
    const details = openDetails();
    expect(within(details).getByRole('button', { name: 'Rename' })).toBeTruthy();
    expect(within(details).getByRole('button', { name: 'Delete' })).toBeTruthy();
  });

  it('reads content with CSRF when a file is opened', async () => {
    mockRW(['system.read', 'files.read']);
    render(<App />);
    await screen.findByText('readme.txt');
    fireEvent.click(within(openDetails()).getByRole('button', { name: 'Open' }));
    const textarea = await screen.findByLabelText('File content');
    expect((textarea as HTMLTextAreaElement).value).toBe('hello');
    const content = calls.find((c) => c.key === 'GET /api/v1/files/content');
    expect(content).toBeTruthy();
    expect((content?.init?.headers as Record<string, string>)['X-CSRF-Token']).toBe('csrf-fixture');
  });

  it('creates a file via POST with CSRF', async () => {
    mockRW(['system.read', 'files.read', 'files.write']);
    render(<App />);
    await screen.findByText('readme.txt');
    fireEvent.click(screen.getByRole('button', { name: 'New file' }));
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'new.txt' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await screen.findByText('readme.txt');
    const create = calls.find((c) => c.key === 'POST /api/v1/files/entries');
    expect(create).toBeTruthy();
    expect((create?.init?.headers as Record<string, string>)['X-CSRF-Token']).toBe('csrf-fixture');
    expect(JSON.parse(String(create?.init?.body))).toEqual({ name: 'new.txt', type: 'file' });
  });

  it('renames via PUT with CSRF', async () => {
    mockRW(['system.read', 'files.read', 'files.write']);
    render(<App />);
    await screen.findByText('readme.txt');
    fireEvent.click(within(openDetails()).getByRole('button', { name: 'Rename' }));
    const renameDialog = screen.getByRole('dialog', { name: 'Rename' });
    fireEvent.change(within(renameDialog).getByLabelText('New name'), { target: { value: 'renamed.txt' } });
    fireEvent.click(within(renameDialog).getByRole('button', { name: 'Rename' }));
    await screen.findByText('readme.txt');
    const rename = calls.find((c) => c.key === 'PUT /api/v1/files/entries');
    expect(rename).toBeTruthy();
    expect(JSON.parse(String(rename?.init?.body))).toEqual({ to: 'renamed.txt' });
  });

  it('deletes via plan then confirm', async () => {
    mockRW(['system.read', 'files.read', 'files.destroy']);
    render(<App />);
    await screen.findByText('readme.txt');
    fireEvent.click(within(openDetails()).getByRole('button', { name: 'Delete' }));
    const dialog = await screen.findByRole('dialog', { name: 'Delete file' });
    expect(within(dialog).getByText(/readme.txt/)).toBeTruthy();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Delete file' })).toBeNull());
    expect(calls.some((c) => c.key === 'POST /api/v1/files/delete/plan')).toBe(true);
    expect(calls.some((c) => c.key === 'POST /api/v1/files/delete/apply')).toBe(true);
  });

  it('asks for a password when delete needs step-up', async () => {
    mockRW(['system.read', 'files.read', 'files.destroy'], { deleteStepUp: true });
    render(<App />);
    await screen.findByText('readme.txt');
    fireEvent.click(within(openDetails()).getByRole('button', { name: 'Delete' }));
    const dialog = await screen.findByRole('dialog', { name: 'Delete file' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }));
    expect(await within(dialog).findByLabelText(/re-enter to confirm/)).toBeTruthy();
  });
});
