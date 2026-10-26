// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { App } from './App';
import {
  applyPreferences,
  DEFAULT_PREFERENCES,
  parsePreferences,
  STORAGE_KEY,
  type Preferences,
} from './preferences';
import { TableRegion } from './ui';

// Test fixtures only — not real host data.
const session = { username: 'admin', scopes: ['system.read'], csrf_token: 't', version: '0.1.0' };
const caps = {
  broker: { status: 'available' },
  capabilities: [
    { id: 'overview', label: 'Overview', path: '/', scope: 'system.read', status: 'available' },
    {
      id: 'hardware',
      label: 'Hardware',
      path: '/hardware',
      scope: 'hardware.read',
      status: 'degraded',
      reason: 'Limited here.',
    },
    {
      id: 'audit',
      label: 'Audit',
      path: '/audit',
      scope: 'audit.read',
      status: 'forbidden',
      reason: 'No audit.read.',
    },
  ],
};

function mock() {
  const routes: Record<string, { status: number; body?: unknown }> = {
    'GET /api/v1/auth/session': { status: 200, body: session },
    'GET /api/v1/capabilities': { status: 200, body: caps },
    'GET /api/v1/host/overview': { status: 503, body: {} },
    'GET /api/v1/hardware': { status: 503, body: {} },
  };
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const r = routes[`${init?.method ?? 'GET'} ${url.split('?')[0]}`] ?? { status: 404, body: {} };
      return new Response(JSON.stringify(r.body ?? {}), { status: r.status });
    }),
  );
}

/** jsdom has no matchMedia; emulate a viewport width for the shell's breakpoints. */
function viewport(width: number) {
  vi.stubGlobal('matchMedia', (q: string) => {
    const max = /max-width:\s*([\d.]+)px/.exec(q);
    const min = /min-width:\s*([\d.]+)px/.exec(q);
    const matches = (max ? width <= Number(max[1]) : true) && (min ? width >= Number(min[1]) : true);
    return { matches, media: q, addEventListener: () => undefined, removeEventListener: () => undefined };
  });
}

const html = document.documentElement;
const stored = () => JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}') as Record<string, unknown>;

beforeEach(() => {
  window.history.pushState(null, '', '/');
  localStorage.clear();
  applyPreferences(DEFAULT_PREFERENCES);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('preferences', () => {
  it('validates stored data and falls back to defaults', () => {
    expect(parsePreferences(null)).toEqual(DEFAULT_PREFERENCES);
    expect(parsePreferences('not json')).toEqual(DEFAULT_PREFERENCES);
    expect(parsePreferences('[]')).toEqual(DEFAULT_PREFERENCES);
    expect(parsePreferences('{"schemaVersion":2,"theme":"oled"}')).toEqual(DEFAULT_PREFERENCES);
    const p = parsePreferences(
      JSON.stringify({
        schemaVersion: 1,
        theme: 'oled',
        density: 'tiny',
        font: 'comic-sans',
        textScale: '125',
        transparency: 'reduced',
        motion: 'none',
        sidebar: 'collapsed',
        layout: 'right',
      }),
    );
    expect(p).toEqual({
      ...DEFAULT_PREFERENCES,
      theme: 'oled',
      textScale: '125',
      transparency: 'reduced',
      motion: 'none',
      sidebar: 'collapsed',
    });
    expect(Object.keys(p)).not.toContain('layout');
  });

  it('ignores prototype keys', () => {
    const p = parsePreferences('{"schemaVersion":1,"__proto__":{"theme":"oled"},"constructor":"x"}');
    expect(p).toEqual(DEFAULT_PREFERENCES);
    expect(({} as Record<string, unknown>).theme).toBeUndefined();
  });

  it('applies preferences as data attributes; System theme removes the theme attribute', () => {
    const p: Preferences = {
      ...DEFAULT_PREFERENCES,
      theme: 'graphite',
      density: 'compact',
      font: 'system',
      textScale: '112',
    };
    applyPreferences(p);
    expect(html.getAttribute('data-hc-theme')).toBe('graphite');
    expect(html.getAttribute('data-hc-density')).toBe('compact');
    expect(html.getAttribute('data-hc-font')).toBe('system');
    expect(html.getAttribute('data-hc-text-scale')).toBe('112');
    expect(html.hasAttribute('style')).toBe(false);
    applyPreferences({ ...p, theme: 'system' });
    expect(html.hasAttribute('data-hc-theme')).toBe(false);
  });
});

describe('appearance menu', () => {
  it('opens as a dialog, applies and saves choices, and closes on Escape', async () => {
    mock();
    render(<App />);
    const button = await screen.findByRole('button', { name: 'Appearance' });
    expect(button.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(button);
    const dialog = screen.getByRole('dialog', { name: 'Appearance' });
    expect(button.getAttribute('aria-expanded')).toBe('true');
    // Focus moves to the selected theme.
    const theme = within(dialog).getByRole('group', { name: 'Theme' });
    expect(document.activeElement).toBe(within(theme).getByRole('radio', { name: /^System/ }));

    fireEvent.click(within(dialog).getByRole('radio', { name: /^Daybreak/ }));
    expect(html.getAttribute('data-hc-theme')).toBe('daybreak');
    const density = within(dialog).getByRole('group', { name: 'Density' });
    fireEvent.click(within(density).getByRole('radio', { name: 'Compact' }));
    fireEvent.click(within(dialog).getByRole('radio', { name: '125%' }));
    expect(html.getAttribute('data-hc-density')).toBe('compact');
    expect(html.getAttribute('data-hc-text-scale')).toBe('125');
    expect(stored()).toMatchObject({
      schemaVersion: 1,
      theme: 'daybreak',
      density: 'compact',
      textScale: '125',
    });

    // Switching theme leaves every other preference alone.
    fireEvent.click(within(dialog).getByRole('radio', { name: /^OLED/ }));
    expect(html.getAttribute('data-hc-theme')).toBe('oled');
    expect(html.getAttribute('data-hc-density')).toBe('compact');
    expect(html.getAttribute('data-hc-text-scale')).toBe('125');

    fireEvent.keyDown(dialog, { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'Appearance' })).toBeNull();
    expect(document.activeElement).toBe(button);
  });

  it('resets to defaults', async () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ schemaVersion: 1, theme: 'oled', transparency: 'reduced' }),
    );
    mock();
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Appearance' }));
    expect(html.getAttribute('data-hc-theme')).toBe('oled');
    expect(html.getAttribute('data-hc-transparency')).toBe('reduced');
    fireEvent.click(screen.getByRole('button', { name: 'Reset to defaults' }));
    expect(html.hasAttribute('data-hc-theme')).toBe(false);
    expect(html.getAttribute('data-hc-transparency')).toBe('full');
    expect(stored()).toMatchObject({ theme: 'system', transparency: 'full' });
  });

  it('follows changes made in another tab', async () => {
    mock();
    render(<App />);
    await screen.findByRole('button', { name: 'Appearance' });
    const value = JSON.stringify({ schemaVersion: 1, theme: 'graphite', density: 'spacious' });
    act(() => {
      window.dispatchEvent(new StorageEvent('storage', { key: STORAGE_KEY, newValue: value }));
    });
    expect(html.getAttribute('data-hc-theme')).toBe('graphite');
    expect(html.getAttribute('data-hc-density')).toBe('spacious');
  });
});

describe('shell navigation', () => {
  it('keeps one Main navigation with capability states, and remembers a collapsed sidebar', async () => {
    viewport(1440);
    mock();
    render(<App />);
    const nav = await screen.findByRole('navigation', { name: 'Main' });
    await within(nav).findByRole('link', { name: /Overview/ });
    expect(within(nav).getByRole('link', { name: /Hardware/ }).textContent).toContain('Limited');
    expect(within(nav).queryByRole('link', { name: /Audit/ })).toBeNull();
    expect(within(nav).getByText(/No permission: No audit.read./)).toBeTruthy();
    expect(document.querySelector('.shell')?.getAttribute('data-sidebar')).toBe('expanded');

    fireEvent.click(within(nav).getByRole('button', { name: 'Collapse sidebar' }));
    expect(document.querySelector('.shell')?.getAttribute('data-sidebar')).toBe('rail');
    expect(stored()).toMatchObject({ sidebar: 'collapsed' });
    // In the rail, the unavailable module is focusable so its reason can be read.
    expect(within(nav).getByText('Audit').closest('.nav-item')?.getAttribute('tabindex')).toBe('0');
    fireEvent.click(within(nav).getByRole('button', { name: 'Expand sidebar' }));
    expect(document.querySelector('.shell')?.getAttribute('data-sidebar')).toBe('expanded');
  });

  it('uses the icon rail on tablets', async () => {
    viewport(900);
    mock();
    render(<App />);
    await screen.findByRole('navigation', { name: 'Main' });
    expect(document.querySelector('.shell')?.getAttribute('data-sidebar')).toBe('rail');
  });

  it('opens a navigation drawer on phones; Escape closes it and returns focus', async () => {
    viewport(390);
    mock();
    render(<App />);
    const open = await screen.findByRole('button', { name: 'Open navigation' });
    expect(document.querySelector('.shell')?.getAttribute('data-sidebar')).toBe('drawer');
    expect(open.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(open);
    expect(open.getAttribute('aria-expanded')).toBe('true');
    expect(document.querySelector('.workspace')?.hasAttribute('inert')).toBe(true);
    const nav = screen.getByRole('navigation', { name: 'Main' });
    await within(nav).findByRole('link', { name: /Overview/ });
    expect(nav.contains(document.activeElement)).toBe(true);
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(document.querySelector('.shell')?.hasAttribute('data-drawer')).toBe(false);
    expect(document.querySelector('.workspace')?.hasAttribute('inert')).toBe(false);
    expect(document.activeElement).toBe(open);

    // Choosing a page closes the drawer.
    fireEvent.click(open);
    fireEvent.click(
      within(screen.getByRole('navigation', { name: 'Main' })).getByRole('link', { name: /Hardware/ }),
    );
    expect(window.location.pathname).toBe('/hardware');
    expect(document.querySelector('.shell')?.hasAttribute('data-drawer')).toBe(false);
  });
});

describe('scrollable tables', () => {
  it('become labelled, focusable regions only when they scroll', () => {
    render(
      <TableRegion label="Units">
        <table>
          <tbody>
            <tr>
              <td>x</td>
            </tr>
          </tbody>
        </table>
      </TableRegion>,
    );
    const wrap = document.querySelector('.table-wrap') as HTMLElement;
    expect(wrap.hasAttribute('tabindex')).toBe(false);
    expect(wrap.getAttribute('role')).toBeNull();
    cleanup();

    vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockReturnValue(800);
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(300);
    render(
      <TableRegion label="Units">
        <table />
      </TableRegion>,
    );
    const region = screen.getByRole('region', { name: 'Units' });
    expect(region.getAttribute('tabindex')).toBe('0');
  });
});

describe('document title', () => {
  it('names the page being shown', async () => {
    viewport(1440);
    mock();
    render(<App />);
    const nav = await screen.findByRole('navigation', { name: 'Main' });
    await within(nav).findByRole('link', { name: /Overview/ });
    expect(document.title).toBe('Overview · Homelab Control');
    fireEvent.click(within(nav).getByRole('link', { name: /Hardware/ }));
    expect(document.title).toBe('Hardware · Homelab Control');
    fireEvent.click(screen.getByRole('link', { name: 'Sessions' }));
    expect(document.title).toBe('Sessions · Homelab Control');
    fireEvent.click(screen.getByRole('button', { name: /^Sign out$/ }));
    await screen.findByLabelText('Username');
    expect(document.title).toBe('Sign in · Homelab Control');
  });
});
