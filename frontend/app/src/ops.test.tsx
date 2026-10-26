// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import axe from 'axe-core';
import { App } from './App';

// Test fixtures only — not real host data.
const operator = {
  username: 'admin',
  scopes: ['services.read', 'services.logs', 'services.operate', 'audit.read'],
  csrf_token: 'csrf-1',
  version: '0.1.0',
};

const caps = {
  broker: { status: 'available' },
  capabilities: [
    { id: 'services', label: 'Services', path: '/services', scope: 'services.read', status: 'available' },
    {
      id: 'service_logs',
      label: 'Service logs',
      path: '/services',
      scope: 'services.logs',
      status: 'available',
    },
    { id: 'audit', label: 'Audit', path: '/audit', scope: 'audit.read', status: 'available' },
  ],
};

function unitList(operate: { available: boolean; reason?: string }) {
  return {
    truncated: false,
    source: 'systemctl',
    counts: {},
    operate,
    protected: {
      'ssh.service': {
        protected: true,
        category: 'remote',
        reason: 'Remote login (SSH): stopping or restarting it can lock you out of the host.',
      },
    },
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
        name: 'demo.service',
        type: 'service',
        load: 'loaded',
        active: 'active',
        sub: 'running',
        description: 'Demo',
      },
      {
        name: 'idle.service',
        type: 'service',
        load: 'loaded',
        active: 'inactive',
        sub: 'dead',
        description: 'Idle',
      },
      {
        name: 'dbus.socket',
        type: 'socket',
        load: 'loaded',
        active: 'active',
        sub: 'running',
        description: 'x',
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
}

const observed = (active: string, sub: string) => ({
  id: 'demo.service',
  load_state: 'loaded',
  active_state: active,
  sub_state: sub,
});

function plan(action: string, risk: 'reversible' | 'disruptive', expiresInMs = 120_000) {
  return {
    unit: 'demo.service',
    requested: 'demo.service',
    action,
    risk,
    step_up_required: risk !== 'reversible',
    token: 'hcp1.payload.' + 'S'.repeat(43),
    expires_at: new Date(Date.now() + expiresInMs).toISOString(),
    observed: observed('active', 'running'),
    effect: `${action} demo.service, which is currently active (running).`,
    consequences: ['The service stops now.', 'This is a runtime action. There is no automatic rollback.'],
    also_affected: ['app.service'],
  };
}

type Route = { status: number; body?: unknown };
let calls: { key: string; body: string | undefined; headers: HeadersInit | undefined }[] = [];

function mock(routes: Record<string, Route | (() => Route)>) {
  calls = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const key = `${init?.method ?? 'GET'} ${url.split('?')[0]}`;
      calls.push({ key, body: init?.body as string | undefined, headers: init?.headers });
      const r0 = routes[key];
      const r = (typeof r0 === 'function' ? r0() : r0) ?? {
        status: 404,
        body: { error: 'not_found', message: 'nope' },
      };
      return new Response(JSON.stringify(r.body ?? {}), { status: r.status });
    }),
  );
}

function base(operate: { available: boolean; reason?: string } = { available: true }) {
  return {
    'GET /api/v1/auth/session': { status: 200, body: operator },
    'GET /api/v1/capabilities': { status: 200, body: caps },
    'GET /api/v1/services': { status: 200, body: unitList(operate) },
  };
}

const posted = (key: string) => calls.filter((c) => c.key === key);

beforeEach(() => window.history.pushState(null, '', '/services'));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('service operations', () => {
  it('offers actions only for operable, unprotected .service units and explains protection', async () => {
    mock(base());
    render(<App />);
    await screen.findByRole('button', { name: 'Stop demo.service' });
    expect(screen.getByRole('button', { name: 'Restart demo.service' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Start idle.service' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Stop idle.service' })).toBeNull();
    // Protected: a label with its reason instead of controls.
    expect(screen.queryByRole('button', { name: /ssh\.service/ })?.textContent).not.toMatch(/Stop|Restart/);
    expect(screen.queryByRole('button', { name: 'Stop ssh.service' })).toBeNull();
    expect(screen.getByText('Protected')).toBeTruthy();
    expect(screen.getByText(/Why some units can’t be started/)).toBeTruthy();
    // Sockets and missing units get no controls.
    expect(screen.queryByRole('button', { name: /dbus\.socket/ })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Start ghost.service' })).toBeNull();
    // The shell is no longer labelled read-only for this account.
    expect(screen.queryByText('Read-only', { selector: '.readonly-badge' })).toBeNull();
  });

  it('shows no controls and says why when operations are unavailable', async () => {
    mock(base({ available: false, reason: 'Your account does not have the services.operate permission.' }));
    render(<App />);
    await screen.findByText(/does not have the services.operate permission/);
    expect(screen.queryByRole('button', { name: /^(Start|Stop|Restart) / })).toBeNull();
    expect(screen.queryByRole('columnheader', { name: 'Actions' })).toBeNull();
  });

  it('previews, asks for the password, executes, and shows the observed result', async () => {
    mock({
      ...base(),
      'POST /api/v1/services/units/demo.service/plan': { status: 200, body: plan('stop', 'disruptive') },
      'POST /api/v1/auth/step-up': {
        status: 200,
        body: { step_up_until: new Date(Date.now() + 300_000).toISOString() },
      },
      'POST /api/v1/services/units/demo.service/apply': {
        status: 200,
        body: {
          operation_id: 'abcdef1234567890',
          unit: 'demo.service',
          action: 'stop',
          outcome: 'succeeded',
          message: 'The service is stopped. Observed state: inactive (dead).',
          exit_code: 0,
          observed: observed('inactive', 'dead'),
          started_at: '2026-10-23T01:00:00Z',
          finished_at: '2026-10-23T01:00:02Z',
        },
      },
    });
    render(<App />);
    const trigger = await screen.findByRole('button', { name: 'Stop demo.service' });
    fireEvent.click(trigger);
    const dialog = await screen.findByRole('dialog', { name: 'Stop demo.service' });
    await within(dialog).findByText('Preview · nothing has been changed yet');
    expect(within(dialog).getByText('Disruptive')).toBeTruthy();
    expect(within(dialog).getByText(/no automatic rollback/)).toBeTruthy();
    expect(within(dialog).getByText('app.service')).toBeTruthy();
    // Focus starts on Close (HCIS), never on the action button.
    expect(document.activeElement?.textContent).toBe('Close');
    // Confirming without the password does not execute anything.
    fireEvent.click(within(dialog).getByRole('button', { name: 'Stop demo.service' }));
    expect(await within(dialog).findByRole('alert')).toBeTruthy();
    expect(posted('POST /api/v1/services/units/demo.service/apply')).toHaveLength(0);
    fireEvent.change(within(dialog).getByLabelText(/Re-enter your password/), { target: { value: 'pw-1' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Stop demo.service' }));
    await within(dialog).findByText('Done');
    expect(within(dialog).getByText('inactive (dead)')).toBeTruthy();
    expect(within(dialog).getByRole('link', { name: 'view in Audit' })).toBeTruthy();
    // Requests carried the CSRF token, the password went only to step-up,
    // and apply sent the plan token for the canonical unit.
    const [su] = posted('POST /api/v1/auth/step-up');
    const [ap] = posted('POST /api/v1/services/units/demo.service/apply');
    expect(JSON.parse(su?.body ?? '{}')).toEqual({ password: 'pw-1' });
    expect(JSON.parse(ap?.body ?? '{}')).toEqual({ action: 'stop', token: plan('stop', 'disruptive').token });
    expect((ap?.headers as Record<string, string>)['X-CSRF-Token']).toBe('csrf-1');
    expect(ap?.body).not.toContain('pw-1');
    // Closing returns focus to the row button and refreshes the list.
    const before = calls.filter((c) => c.key === 'GET /api/v1/services').length;
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect((document.activeElement as HTMLElement).dataset.unit).toBe('demo.service');
    await vi.waitFor(() =>
      expect(calls.filter((c) => c.key === 'GET /api/v1/services').length).toBeGreaterThan(before),
    );
  });

  it('starts a reversible operation without a password', async () => {
    mock({
      ...base(),
      'POST /api/v1/services/units/idle.service/plan': {
        status: 200,
        body: { ...plan('start', 'reversible'), unit: 'idle.service', requested: 'idle.service' },
      },
      'POST /api/v1/services/units/idle.service/apply': {
        status: 200,
        body: {
          operation_id: 'op2',
          unit: 'idle.service',
          action: 'start',
          outcome: 'failed',
          message:
            'systemd accepted the request, but the service failed. Observed state: failed (failed), result exit-code.',
          observed: { ...observed('failed', 'failed'), id: 'idle.service', result: 'exit-code' },
          recovery: [
            'No automatic rollback was performed. Before this operation the unit was inactive (dead).',
          ],
          started_at: '2026-10-23T01:00:00Z',
          finished_at: '2026-10-23T01:00:02Z',
        },
      },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Start idle.service' }));
    const dialog = await screen.findByRole('dialog');
    await within(dialog).findByText('Reversible');
    expect(within(dialog).queryByLabelText(/Re-enter your password/)).toBeNull();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Start idle.service' }));
    // A failed start is shown as failed, with the observed state — never as success.
    await within(dialog).findByText('Failed');
    expect(within(dialog).getByText('failed (failed)')).toBeTruthy();
    expect(within(dialog).getByText('exit-code')).toBeTruthy();
    expect(within(dialog).getByText(/No automatic rollback was performed/)).toBeTruthy();
    expect(within(dialog).queryByText('Done')).toBeNull();
    expect(posted('POST /api/v1/auth/step-up')).toHaveLength(0);
  });

  it('lists the inactive dependencies a start pulls in', async () => {
    mock({
      ...base(),
      'POST /api/v1/services/units/idle.service/plan': {
        status: 200,
        body: {
          ...plan('start', 'reversible'),
          unit: 'idle.service',
          requested: 'idle.service',
          also_affected: [],
          also_started: ['dep-a.service', 'dep-b.socket'],
        },
      },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Start idle.service' }));
    const dialog = await screen.findByRole('dialog');
    await within(dialog).findByText(/Inactive units systemd starts first/);
    expect(within(dialog).getByText('dep-a.service')).toBeTruthy();
    expect(within(dialog).getByText('dep-b.socket')).toBeTruthy();
    expect(within(dialog).queryByText(/stops or restarts together/)).toBeNull();
  });

  it('shows the policy reason when the broker refuses', async () => {
    mock({
      ...base(),
      'POST /api/v1/services/units/demo.service/plan': {
        status: 403,
        body: {
          error: 'policy_denied',
          message: 'stopping demo.service would also stop sshd.service, which is protected',
        },
      },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Stop demo.service' }));
    const dialog = await screen.findByRole('dialog');
    await within(dialog).findByText(/would also stop sshd.service/);
    expect(within(dialog).getByText(/Not allowed for this unit/)).toBeTruthy();
    expect(within(dialog).queryByRole('button', { name: 'Review again' })).toBeNull();
  });

  it('reports a changed unit as nothing changed and offers a new review', async () => {
    let n = 0;
    mock({
      ...base(),
      'POST /api/v1/services/units/demo.service/plan': () => {
        n++;
        return { status: 200, body: plan('restart', 'disruptive') };
      },
      'GET /api/v1/auth/session': {
        status: 200,
        body: { ...operator, step_up_until: new Date(Date.now() + 200_000).toISOString() },
      },
      'POST /api/v1/services/units/demo.service/apply': {
        status: 409,
        body: {
          error: 'conflict',
          message: 'the unit changed since the operation was reviewed; nothing was done.',
        },
      },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Restart demo.service' }));
    const dialog = await screen.findByRole('dialog');
    await within(dialog).findByText('Your password was confirmed in the last five minutes.');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Restart demo.service' }));
    await within(dialog).findByText(/The unit changed since you reviewed it/);
    expect(within(dialog).getByText(/Nothing was changed/)).toBeTruthy();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Review again' }));
    await within(dialog).findByText('Preview · nothing has been changed yet');
    expect(n).toBe(2);
  });

  it('never claims nothing changed when the outcome is unknown', async () => {
    mock({
      ...base(),
      'GET /api/v1/auth/session': {
        status: 200,
        body: { ...operator, step_up_until: new Date(Date.now() + 200_000).toISOString() },
      },
      'POST /api/v1/services/units/demo.service/plan': { status: 200, body: plan('restart', 'disruptive') },
      'POST /api/v1/services/units/demo.service/apply': {
        status: 504,
        body: { error: 'timeout', message: 'Broker operation timed out' },
      },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Restart demo.service' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.click(await within(dialog).findByRole('button', { name: 'Restart demo.service' }));
    await within(dialog).findByText(/may or may not have run/);
    expect(within(dialog).queryByText(/Nothing was changed/)).toBeNull();
  });

  it('reports a lost result as unknown, once, with what to check', async () => {
    mock({
      ...base(),
      'GET /api/v1/auth/session': {
        status: 200,
        body: { ...operator, step_up_until: new Date(Date.now() + 200_000).toISOString() },
      },
      'POST /api/v1/services/units/demo.service/plan': { status: 200, body: plan('restart', 'disruptive') },
      'POST /api/v1/services/units/demo.service/apply': {
        status: 502,
        body: {
          error: 'outcome_unknown',
          message:
            'The privileged helper did not return a result, so the operation may or may not have run. Check the unit state on this page and the operation journal on the Audit page.',
        },
      },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Restart demo.service' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.click(await within(dialog).findByRole('button', { name: 'Restart demo.service' }));
    await within(dialog).findByText(/The result is unknown/);
    expect(within(dialog).getAllByText(/may or may not have run/)).toHaveLength(1);
    expect(within(dialog).queryByText(/Nothing was changed/)).toBeNull();
    expect(within(dialog).queryByText('Done')).toBeNull();
  });

  it('disables confirmation once the review expires', async () => {
    mock({
      ...base(),
      'POST /api/v1/services/units/demo.service/plan': {
        status: 200,
        body: plan('stop', 'disruptive', 1500),
      },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Stop demo.service' }));
    const dialog = await screen.findByRole('dialog');
    await within(dialog).findByText('Preview · nothing has been changed yet');
    await act(async () => {
      await new Promise((r) => setTimeout(r, 2100));
    });
    await within(dialog).findByText(/This review has expired/);
    expect(within(dialog).queryByRole('button', { name: 'Stop demo.service' })).toBeNull();
    expect(within(dialog).getByRole('button', { name: 'Review again' })).toBeTruthy();
  });

  it('keeps keyboard focus inside the dialog and closes with Escape', async () => {
    mock({
      ...base(),
      'POST /api/v1/services/units/demo.service/plan': { status: 200, body: plan('stop', 'disruptive') },
    });
    render(<App />);
    const trigger = await screen.findByRole('button', { name: 'Stop demo.service' });
    fireEvent.click(trigger);
    const dialog = await screen.findByRole('dialog');
    await within(dialog).findByLabelText(/Re-enter your password/);
    const buttons = within(dialog).getAllByRole('button');
    const last = buttons[buttons.length - 1] as HTMLElement;
    last.focus();
    fireEvent.keyDown(last, { key: 'Tab' });
    // Tab from the last control wraps to the first one (the password field).
    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(within(dialog).getByLabelText(/Re-enter your password/));
    fireEvent.keyDown(document.activeElement as Element, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);
    // The page behind the dialog is not reachable (jsdom has no #root inert here,
    // so check the trap itself kept focus inside).
    expect(dialog.contains(document.activeElement)).toBe(true);
    fireEvent.keyDown(dialog, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect((document.activeElement as HTMLElement).dataset.unit).toBe(
      trigger.getAttribute('aria-label')?.split(' ')[1],
    );
  });

  it('has no axe violations in the preview dialog', async () => {
    mock({
      ...base(),
      'POST /api/v1/services/units/demo.service/plan': { status: 200, body: plan('stop', 'disruptive') },
    });
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Stop demo.service' }));
    const dialog = await screen.findByRole('dialog');
    await within(dialog).findByLabelText(/Re-enter your password/);
    const res = await axe.run(dialog, { rules: { 'color-contrast': { enabled: false } } });
    expect(res.violations.map((v) => `${v.id}: ${v.help}`)).toEqual([]);
  });
});
