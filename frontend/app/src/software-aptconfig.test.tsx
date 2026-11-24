// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { AptMirrorsView, AptSettings } from './pages/SoftwareAptConfig';
import {
  ApiError,
  applyAptConfig,
  applyAptMirror,
  getAptConfig,
  getAptMirrors,
  planAptConfig,
  planAptMirror,
  probeAptMirrors,
  type AptMirrors,
  type AptPlan,
} from './api';

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return {
    ...mod,
    getAptConfig: vi.fn(),
    planAptConfig: vi.fn(),
    applyAptConfig: vi.fn(),
    getAptMirrors: vi.fn(),
    probeAptMirrors: vi.fn(),
    planAptMirror: vi.fn(),
    applyAptMirror: vi.fn(),
  };
});

const csrf = 'c';
const onUnauthorized = vi.fn();

// Fixture data for tests only (labelled; never shown in production views).
const plan: AptPlan = {
  action: 'conf.write',
  risk: 'reversible',
  step_up_required: false,
  confirmation_required: true,
  token: 'tok',
  expires_at: '2026-11-24T12:00:00Z',
  file: '/etc/apt/apt.conf.d/60homelab-control',
  before: '',
  after: 'APT::Install-Recommends "false";\n',
  effect: 'Write /etc/apt/apt.conf.d/60homelab-control with the reviewed settings.',
  consequences: ['The previous file is kept as a backup.'],
};

const mirrors: AptMirrors = {
  distro: { id: 'ubuntu', codename: 'noble' },
  default_country: 'DE',
  files: [{ path: '/etc/apt/sources.list.d/ubuntu.sources', format: 'deb822', digest: 'd' }],
  entries: [
    {
      file: '/etc/apt/sources.list.d/ubuntu.sources',
      format: 'deb822',
      line: 2,
      enabled: true,
      types: ['deb'],
      uri: 'http://de.archive.ubuntu.com/ubuntu/',
      suites: ['noble', 'noble-updates'],
      components: ['main'],
      role: 'archive',
      switchable: true,
    },
    {
      file: '/etc/apt/sources.list.d/ubuntu.sources',
      format: 'deb822',
      line: 8,
      enabled: true,
      types: ['deb'],
      uri: 'http://security.ubuntu.com/ubuntu/',
      suites: ['noble-security'],
      components: ['main'],
      role: 'security',
      switchable: false,
      reason: 'Security updates stay on the distribution security server.',
    },
  ],
};

beforeEach(() => {
  vi.clearAllMocks();
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
  };
  vi.mocked(getAptConfig).mockResolvedValue({
    effective: {
      install_recommends: true,
      install_suggests: false,
      skip_translations: false,
      force_ipv4: false,
      pdiffs: true,
      keep_downloaded: false,
      retries: 0,
      timeout_seconds: 120,
      queue_mode: 'host',
      http_proxy_set: false,
    },
    managed: {},
    managed_present: false,
    managed_changed_by_hand: false,
    path: '/etc/apt/apt.conf.d/60homelab-control',
  });
  vi.mocked(planAptConfig).mockResolvedValue(plan);
  vi.mocked(applyAptConfig).mockResolvedValue({
    operation_id: 'op-123456789012',
    action: 'conf.write',
    outcome: 'succeeded',
    message: 'Written.',
    backup: '60homelab-control.x.bak',
    overridden_by_other_files: ['install_recommends'],
  });
  vi.mocked(getAptMirrors).mockResolvedValue(mirrors);
});
afterEach(cleanup);

describe('APT settings tab', () => {
  it('shows current values, reviews the change, then applies it with the plan token', async () => {
    render(<AptSettings csrf={csrf} onUnauthorized={onUnauthorized} />);
    const rec = (await screen.findByLabelText(/Install recommended packages/)) as HTMLInputElement;
    expect(rec.checked).toBe(true);
    fireEvent.click(rec);
    fireEvent.click(screen.getByRole('button', { name: 'Review settings' }));
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByText(/nothing has been changed yet/)).toBeTruthy();
    expect(vi.mocked(planAptConfig)).toHaveBeenCalledWith(
      { tunables: expect.objectContaining({ install_recommends: false, retries: 0, timeout_seconds: 120 }) },
      csrf,
    );
    expect(vi.mocked(applyAptConfig)).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Apply change' }));
    expect(await within(dialog).findByText(/Written\./)).toBeTruthy();
    expect(vi.mocked(applyAptConfig)).toHaveBeenCalledWith(expect.anything(), 'tok', csrf);
    expect(
      within(dialog).getByText(/Overridden by a later APT configuration file: install_recommends/),
    ).toBeTruthy();
  });

  it('refuses a proxy with credentials before any request', async () => {
    render(<AptSettings csrf={csrf} onUnauthorized={onUnauthorized} />);
    fireEvent.change(await screen.findByLabelText(/HTTP proxy/), {
      target: { value: 'http://user:pw@cache:3142' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Review settings' }));
    expect(await screen.findByText(/without a user name or password/)).toBeTruthy();
    expect(vi.mocked(planAptConfig)).not.toHaveBeenCalled();
  });

  it('explains a missing scope', async () => {
    vi.mocked(getAptConfig).mockRejectedValue(new ApiError(403, 'forbidden', 'forbidden'));
    render(<AptSettings csrf={csrf} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/packages\.configure/)).toBeTruthy();
  });
});

describe('Mirrors tab', () => {
  it('ranks mirrors and switches only the archive entry', async () => {
    vi.mocked(probeAptMirrors).mockResolvedValue({
      country: 'DE',
      codename: 'noble',
      results: [
        {
          uri: 'http://mirror.example.de/ubuntu/',
          host: 'mirror.example.de',
          source: 'official',
          status: 'ok',
          rank: 1,
          total_ms: 80,
        },
        {
          uri: 'http://de.archive.ubuntu.com/ubuntu/',
          host: 'de.archive.ubuntu.com',
          source: 'current',
          status: 'ok',
          rank: 2,
          current: true,
        },
        {
          uri: 'http://old.example.de/ubuntu/',
          host: 'old.example.de',
          source: 'official',
          status: 'stale',
          reason: 'older',
        },
      ],
    });
    vi.mocked(planAptMirror).mockResolvedValue({
      ...plan,
      action: 'mirror.set',
      file: '/etc/apt/sources.list.d/ubuntu.sources',
      lines: [
        {
          line: 2,
          before: 'URIs: http://de.archive.ubuntu.com/ubuntu/',
          after: 'URIs: http://mirror.example.de/ubuntu/',
        },
      ],
    });
    vi.mocked(applyAptMirror).mockResolvedValue({
      operation_id: 'op-2',
      action: 'mirror.set',
      outcome: 'succeeded',
      message: 'Written.',
    });
    render(<AptMirrorsView csrf={csrf} onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/Security updates stay on the distribution security server/)).toBeTruthy();
    expect((screen.getByLabelText('Country') as HTMLInputElement).value).toBe('DE');
    fireEvent.click(screen.getByRole('button', { name: 'Find fastest mirror' }));
    const use = await screen.findAllByRole('button', { name: /Use mirror\.example\.de/ });
    // The current and the stale mirror offer no switch.
    expect(use).toHaveLength(1);
    expect(screen.queryByRole('button', { name: /Use old\.example\.de/ })).toBeNull();
    const [first] = use;
    if (!first) throw new Error('no switch button');
    fireEvent.click(first);
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByText('URIs: http://mirror.example.de/ubuntu/')).toBeTruthy();
    expect(vi.mocked(planAptMirror)).toHaveBeenCalledWith(
      {
        file: '/etc/apt/sources.list.d/ubuntu.sources',
        from: 'http://de.archive.ubuntu.com/ubuntu/',
        to: 'http://mirror.example.de/ubuntu/',
      },
      csrf,
    );
    fireEvent.click(within(dialog).getByRole('button', { name: 'Apply change' }));
    expect(await within(dialog).findByText(/Written\./)).toBeTruthy();
    expect(vi.mocked(applyAptMirror)).toHaveBeenCalledWith(expect.anything(), 'tok', csrf);
  });
});
