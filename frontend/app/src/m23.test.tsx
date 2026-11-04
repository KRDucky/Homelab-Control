// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import axe from 'axe-core';
import { App } from './App';

// M2-3 UI: boot-time enable/disable, unit detail, timers/sockets, module
// registry. Test fixtures only — labelled, not real host data.
const EX = 'Example data (test fixture)';

function session(scopes: string[]) {
  return { username: 'admin', scopes, csrf_token: 'csrf-1', version: '0.1.0' };
}

const caps = (enabled = true) => ({
  broker: { status: 'available' },
  modules: [
    {
      id: 'services',
      title: 'Services',
      description: EX,
      path: '/services',
      enabled,
      scopes: [],
      reason: enabled
        ? undefined
        : 'Disabled by the host administrator in /etc/homelab-control/modules.json.',
    },
  ],
  capabilities: enabled
    ? [
        { id: 'services', label: 'Services', path: '/services', scope: 'services.read', status: 'available' },
        {
          id: 'service_logs',
          label: 'Service logs',
          path: '/services',
          scope: 'services.logs',
          status: 'available',
        },
        { id: 'audit', label: 'Audit', path: '/audit', scope: 'audit.read', status: 'available' },
      ]
    : [{ id: 'audit', label: 'Audit', path: '/audit', scope: 'audit.read', status: 'available' }],
});

function units(configure: { available: boolean; reason?: string }) {
  return {
    truncated: false,
    source: EX,
    counts: {},
    operate: { available: false, reason: 'Your account does not have the services.operate permission.' },
    configure,
    protected: { 'ssh.service': { protected: true, category: 'remote', reason: 'Remote login (SSH).' } },
    units: [
      {
        name: 'ssh.service',
        type: 'service',
        load: 'loaded',
        active: 'active',
        sub: 'running',
        description: EX,
        unit_file_state: 'enabled',
      },
      {
        name: 'web.service',
        type: 'service',
        load: 'loaded',
        active: 'active',
        sub: 'running',
        description: EX,
        unit_file_state: 'enabled',
      },
      {
        name: 'batch.service',
        type: 'service',
        load: 'loaded',
        active: 'inactive',
        sub: 'dead',
        description: EX,
        unit_file_state: 'disabled',
      },
      {
        name: 'helper.service',
        type: 'service',
        load: 'loaded',
        active: 'inactive',
        sub: 'dead',
        description: EX,
        unit_file_state: 'static',
      },
      {
        name: 'fresh.service',
        type: 'service',
        load: 'not-loaded',
        active: 'unknown',
        sub: 'unknown',
        description: '',
        unit_file_state: 'disabled',
      },
      {
        name: 'tty@.service',
        type: 'service',
        load: 'not-loaded',
        active: 'unknown',
        sub: 'unknown',
        description: '',
        unit_file_state: 'disabled',
      },
    ],
  };
}

const enablePlan = {
  unit: 'batch.service',
  requested: 'batch.service',
  action: 'enable',
  risk: 'disruptive',
  step_up_required: true,
  token: 'hcp1.payload.' + 'S'.repeat(43),
  expires_at: new Date(Date.now() + 120_000).toISOString(),
  observed: {
    id: 'batch.service',
    load_state: 'loaded',
    active_state: 'inactive',
    sub_state: 'dead',
    unit_file_state: 'disabled',
  },
  boot_state: 'disabled',
  effect: 'Enable batch.service at boot. It is currently disabled.',
  consequences: [
    'This does not start it now: its current state stays inactive. Use Start for that.',
    'The running systemd manager is not reloaded: the change takes effect at the next boot.',
    'There is no automatic rollback. To undo it, review the opposite action.',
  ],
  links: ['/etc/systemd/system/multi-user.target.wants/batch.service'],
  wanted_by: ['multi-user.target'],
};

const enableOutcome = {
  operation_id: 'op0123456789abcdef',
  unit: 'batch.service',
  action: 'enable',
  outcome: 'succeeded',
  message: 'The service is enabled at boot.',
  exit_code: 0,
  boot_state_before: 'disabled',
  boot_state: 'enabled',
  links: [{ path: '/etc/systemd/system/multi-user.target.wants/batch.service', present: true }],
  reported: ["Created symlink '/etc/systemd/system/multi-user.target.wants/batch.service'"],
  started_at: new Date().toISOString(),
  finished_at: new Date().toISOString(),
};

type Route = { status: number; body?: unknown };
let calls: { key: string; body: string | undefined }[] = [];
function mock(routes: Record<string, Route>) {
  calls = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const key = `${init?.method ?? 'GET'} ${url.split('?')[0]}`;
      calls.push({ key, body: init?.body as string | undefined });
      const r = routes[key] ?? { status: 404, body: { error: 'not_found', message: 'unknown API route' } };
      return new Response(JSON.stringify(r.body ?? {}), { status: r.status });
    }),
  );
}

const configureScopes = ['services.read', 'services.configure', 'audit.read'];
function base(
  scopes = configureScopes,
  configure: { available: boolean; reason?: string } = { available: true },
) {
  return {
    'GET /api/v1/auth/session': { status: 200, body: session(scopes) },
    'GET /api/v1/capabilities': { status: 200, body: caps() },
    'GET /api/v1/services': { status: 200, body: units(configure) },
  } as Record<string, Route>;
}

beforeEach(() => window.history.pushState(null, '', '/services'));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('boot-time enable/disable', () => {
  it('offers the boot setting only with services.configure, only for enabled/disabled non-template units', async () => {
    mock(base());
    render(<App />);
    await screen.findByRole('button', { name: 'Enable at boot: batch.service' });
    expect(screen.getByRole('button', { name: 'Disable at boot: web.service' })).toBeTruthy();
    // Runtime controls stay hidden without services.operate.
    expect(screen.queryByRole('button', { name: 'Stop web.service' })).toBeNull();
    // An installed unit systemd has not loaded yet can be enabled (found by
    // the real-backend browser run), but gets no runtime control.
    expect(screen.getByRole('button', { name: 'Enable at boot: fresh.service' })).toBeTruthy();
    // Static, template and protected units get no boot control.
    expect(screen.queryByRole('button', { name: /at boot: helper\.service/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /at boot: tty@\.service/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /at boot: ssh\.service/ })).toBeNull();
    // Boot-time vs runtime is explained.
    expect(screen.getByText(/does not start or stop the service now/)).toBeTruthy();
    // WCAG 2.5.3 (label in name): found by the real-backend browser run.
    const res = await axe.run(document.body, { rules: { 'label-content-name-mismatch': { enabled: true } } });
    expect(res.violations.map((v) => v.id)).toEqual([]);
  });

  it('explains why there are no boot controls without the permission', async () => {
    mock(
      base(['services.read', 'services.operate'], {
        available: false,
        reason: 'Your account does not have the services.configure permission.',
      }),
    );
    render(<App />);
    await screen.findByText(/services\.configure permission/);
    expect(screen.queryByRole('button', { name: /at boot/ })).toBeNull();
  });

  it('reviews, asks for the password, applies and shows the independently verified result', async () => {
    const r = base();
    r['POST /api/v1/services/units/batch.service/enablement/plan'] = { status: 200, body: enablePlan };
    r['POST /api/v1/auth/step-up'] = {
      status: 200,
      body: { step_up_until: new Date(Date.now() + 300_000).toISOString() },
    };
    r['POST /api/v1/services/units/batch.service/enablement/apply'] = { status: 200, body: enableOutcome };
    mock(r);
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Enable at boot: batch.service' }));
    const dialog = await screen.findByRole('dialog');
    await within(dialog).findByText('Preview · nothing has been changed yet');
    expect(
      within(dialog).getByText('/etc/systemd/system/multi-user.target.wants/batch.service'),
    ).toBeTruthy();
    expect(within(dialog).getByText(/does not start it now/)).toBeTruthy();
    expect(within(dialog).getByText(/not reloaded/)).toBeTruthy();
    expect(within(dialog).getByText('Disruptive')).toBeTruthy();
    // The confirm button never applies without the password.
    fireEvent.click(within(dialog).getByRole('button', { name: 'Enable batch.service at boot' }));
    expect(await within(dialog).findByRole('alert')).toBeTruthy();
    expect(calls.some((c) => c.key.endsWith('/enablement/apply'))).toBe(false);
    fireEvent.change(within(dialog).getByLabelText(/Re-enter your password/), { target: { value: 'pw' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Enable batch.service at boot' }));
    await within(dialog).findByText(/The service is enabled at boot/);
    expect(within(dialog).getByText('disabled → enabled')).toBeTruthy();
    expect(within(dialog).getByText(/— present/)).toBeTruthy();
    const apply = calls.find((c) => c.key.endsWith('/enablement/apply'));
    expect(JSON.parse(apply?.body ?? '{}')).toEqual({ action: 'enable', token: enablePlan.token });
    // The start/stop endpoints are never used for a boot-time change.
    expect(calls.some((c) => /\/units\/batch\.service\/(plan|apply)$/.test(c.key))).toBe(false);
    const res = await axe.run(dialog);
    expect(res.violations.map((v) => v.id)).toEqual([]);
  });

  it('reports a partial change honestly', async () => {
    const r = base();
    r['POST /api/v1/services/units/batch.service/enablement/plan'] = {
      status: 200,
      body: { ...enablePlan, links: [...enablePlan.links, '/etc/systemd/system/batch-alias.service'] },
    };
    r['POST /api/v1/auth/step-up'] = {
      status: 200,
      body: { step_up_until: new Date(Date.now() + 300_000).toISOString() },
    };
    r['POST /api/v1/services/units/batch.service/enablement/apply'] = {
      status: 200,
      body: {
        ...enableOutcome,
        outcome: 'failed',
        exit_code: 1,
        boot_state: 'enabled',
        message: 'systemctl exited with status 1, and the change is only partly applied.',
        links: [
          { path: enablePlan.links[0], present: true },
          { path: '/etc/systemd/system/batch-alias.service', present: false },
        ],
        recovery: ['No automatic rollback was performed.'],
      },
    };
    mock(r);
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Enable at boot: batch.service' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(await within(dialog).findByLabelText(/Re-enter your password/), {
      target: { value: 'pw' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Enable batch.service at boot' }));
    await within(dialog).findByText(/only partly applied/);
    expect(within(dialog).getByText('Failed')).toBeTruthy();
    expect(within(dialog).getByText(/— absent/)).toBeTruthy();
    expect(within(dialog).getByText(/No automatic rollback/)).toBeTruthy();
  });

  it('shows a policy refusal without offering a retry', async () => {
    const r = base();
    r['POST /api/v1/services/units/web.service/enablement/plan'] = {
      status: 403,
      body: {
        error: 'policy_denied',
        message: 'web.service declares Also=web.socket; this version does not support it',
      },
    };
    mock(r);
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Disable at boot: web.service' }));
    const dialog = await screen.findByRole('dialog');
    await within(dialog).findByText(/declares Also=/);
    expect(within(dialog).queryByRole('button', { name: 'Review again' })).toBeNull();
  });
});

describe('unit detail, timers and sockets', () => {
  it('opens a read-only detail panel that does not invent state', async () => {
    const r = base(['services.read']);
    // As the server reports it for an account without services.logs.
    const c = caps();
    c.capabilities = c.capabilities.map((x) => (x.id === 'service_logs' ? { ...x, status: 'forbidden' } : x));
    r['GET /api/v1/capabilities'] = { status: 200, body: c };
    r['GET /api/v1/services/units/web.service'] = {
      status: 200,
      body: {
        requested: 'web.service',
        id: 'web.service',
        kind: 'unit',
        in_runtime: true,
        load_state: 'loaded',
        active_state: 'failed',
        sub_state: 'failed',
        result: 'exit-code',
        boot_state: 'enabled',
        fragment_path: '/etc/systemd/system/web.service',
        restarts: 2,
        last_exit: { code: 'exited', status: 3, exited_at: 'Wed 2026-11-04 06:00:01 UTC' },
        dependencies: { Requires: ['db.service'], WantedBy: ['multi-user.target'] },
        notes: [],
        source: EX,
      },
    };
    mock(r);
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Show details of web.service' }));
    const panel = await screen.findByRole('region', { name: /Details: web\.service/ });
    await within(panel).findByText('/etc/systemd/system/web.service');
    expect(within(panel).getByText(/2 since the manager started/)).toBeTruthy();
    expect(within(panel).getByText(/with status 3/)).toBeTruthy();
    expect(within(panel).getByText('db.service')).toBeTruthy();
    // Without services.logs the unit file is not offered.
    expect(within(panel).queryByRole('button', { name: 'Show unit file' })).toBeNull();
    expect(within(panel).getByText(/needs the services\.logs permission/)).toBeTruthy();
    const res = await axe.run(panel);
    expect(res.violations.map((v) => v.id)).toEqual([]);
  });

  it('shows the redacted unit file on request with services.logs', async () => {
    const r = base(['services.read', 'services.logs']);
    r['GET /api/v1/services/units/web.service'] = {
      status: 200,
      body: {
        requested: 'sshweb.service',
        id: 'web.service',
        kind: 'alias',
        in_runtime: true,
        dependencies: {},
        notes: ['sshweb.service is an alias of web.service.'],
        source: EX,
      },
    };
    r['GET /api/v1/services/units/web.service/contents'] = {
      status: 200,
      body: {
        unit: 'web.service',
        files: [
          { path: '/etc/systemd/system/web.service', text: '[Service]\nEnvironment=TOKEN=[redacted]' },
          { path: '/etc/systemd/system/web.service.d/secret.conf', text: '', unreadable: true },
        ],
        redactions: 1,
        truncated: false,
        source: EX,
      },
    };
    mock(r);
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Show details of web.service' }));
    const panel = await screen.findByRole('region', { name: /Details: web\.service/ });
    await within(panel).findByText(/is an alias of web\.service/);
    expect(calls.some((c) => c.key.endsWith('/contents'))).toBe(false); // loaded only on request
    fireEvent.click(within(panel).getByRole('button', { name: 'Show unit file' }));
    await within(panel).findByText(/1 line redacted/);
    expect(within(panel).getByText(/Environment=TOKEN=\[redacted\]/)).toBeTruthy();
    expect(within(panel).getByText(/only root can read this file/)).toBeTruthy();
  });

  it('switches to timers and sockets with the keyboard (WAI-ARIA tabs)', async () => {
    const r = base(['services.read']);
    r['GET /api/v1/services/timers'] = {
      status: 200,
      body: {
        timers: [
          {
            unit: 'clean.timer',
            activates: 'clean.service',
            active_state: 'active',
            sub_state: 'waiting',
            schedule: ['OnCalendar=daily'],
            next: '2026-11-05T00:00:00Z',
            persistent: true,
          },
          { unit: 'odd.timer', active_state: 'unknown' },
        ],
        truncated: false,
        source: EX,
      },
    };
    r['GET /api/v1/services/sockets'] = {
      status: 200,
      body: {
        sockets: [
          {
            unit: 'ssh.socket',
            listen: ['0.0.0.0:22', '[::]:22'],
            activates: ['ssh.service'],
            active_state: 'active',
            sub_state: 'listening',
          },
        ],
        truncated: false,
        source: EX,
      },
    };
    mock(r);
    render(<App />);
    const unitsTab = await screen.findByRole('tab', { name: 'Units' });
    expect(unitsTab.getAttribute('aria-selected')).toBe('true');
    unitsTab.focus();
    fireEvent.keyDown(unitsTab, { key: 'ArrowRight' });
    const timersTab = screen.getByRole('tab', { name: 'Timers' });
    expect(timersTab.getAttribute('aria-selected')).toBe('true');
    expect(document.activeElement).toBe(timersTab);
    await screen.findByText('OnCalendar=daily');
    expect(screen.getAllByText('unknown').length).toBeGreaterThan(0); // never filled in
    fireEvent.keyDown(timersTab, { key: 'End' });
    await screen.findByText('[::]:22');
    expect(screen.getByRole('tab', { name: 'Sockets' }).getAttribute('aria-selected')).toBe('true');
    const res = await axe.run(document.body);
    expect(res.violations.map((v) => v.id)).toEqual([]);
  });
});

describe('module registry', () => {
  it('hides a disabled module from navigation and says so on its page', async () => {
    mock({
      'GET /api/v1/auth/session': { status: 200, body: session(configureScopes) },
      'GET /api/v1/capabilities': { status: 200, body: caps(false) },
    });
    render(<App />);
    await screen.findByText(/This module is disabled on this host/);
    const nav = screen.getByRole('navigation', { name: 'Main' });
    expect(within(nav).queryByText('Services')).toBeNull();
    expect(calls.some((c) => c.key === 'GET /api/v1/services')).toBe(false);
  });
});
