// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ContainerIdentity, parseCustom } from './ContainerIdentity';
import { getIdentityProfiles, previewIdentity, type IdentityPreview } from './api';
import type { ContainerRuntime } from './plugin';

vi.mock('./api', async (original) => ({
  ...(await original<typeof import('./api')>()),
  getIdentityProfiles: vi.fn(),
  previewIdentity: vi.fn(),
}));

const runtime: ContainerRuntime = {
  supportedPlatforms: ['docker'],
  deploymentMethods: ['compose'],
  identity: {
    configurable: true,
    method: 'env',
    uidVariable: 'PUID',
    gidVariable: 'PGID',
    umask: { method: 'env', variable: 'UMASK' },
    supplementalGroups: true,
  },
  privileged: false,
  capabilities: [],
  volumes: [],
};

const preview = (over: Partial<IdentityPreview> = {}, withoutCompose = false): IdentityPreview => {
  const p: IdentityPreview = {
    schemaVersion: 1,
    mode: 'custom',
    platform: { id: 'docker-local', kind: 'docker', mode: 'rootless', mappingKnown: true },
    resolved: {
      uid: { value: 3000, source: 'override', host: 302999 },
      gid: { value: 3000, source: 'override', host: 302999 },
      umask: {
        value: '002',
        source: 'override',
        effect: { files: '0664', directories: '0775', explanation: 'Keeps group write.' },
      },
      supplementalGroups: { value: [44], source: 'override' },
    },
    mechanism: { identity: 'env', umask: 'env', supplementalGroups: true, declared: true },
    compose: { environment: { PUID: '3000', PGID: '3000', UMASK: '002' }, group_add: ['44'] },
    volumes: [],
    issues: [{ severity: 'info', code: 'declared_only', message: 'Declared only.' }],
    verification: 'not-reported',
    applied: false,
    deployable: false,
    ...over,
  };
  if (withoutCompose) delete p.compose;
  return p;
};

beforeEach(() => {
  vi.mocked(getIdentityProfiles).mockResolvedValue({
    schemaVersion: 1,
    profiles: [
      {
        id: 'media-application',
        name: 'Media application',
        description: '',
        umask: '002',
        groupRoles: ['media'],
        readOnlyVolumes: false,
      },
    ],
    privilegedGroupNames: ['docker'],
    modes: ['image', 'inherit', 'custom'],
  });
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it('validates custom input without guessing', () => {
  expect(parseCustom({ uid: '3000', gid: '', umask: '0022', groups: '44, 109 44' }).settings).toEqual({
    uid: 3000,
    umask: '0022',
    supplementalGroups: [44, 109, 44],
  });
  for (const bad of [
    { uid: '-1', gid: '', umask: '', groups: '' },
    { uid: '1.5', gid: '', umask: '', groups: '' },
    { uid: '4294967295', gid: '', umask: '', groups: '' },
    { uid: '', gid: '', umask: '0999', groups: '' },
    { uid: '', gid: '', umask: '', groups: 'video' },
  ])
    expect(parseCustom(bad).errors.length, JSON.stringify(bad)).toBeGreaterThan(0);
});

it('previews custom identity with sources, UMASK effect and no apply control', async () => {
  vi.mocked(previewIdentity).mockResolvedValue({ platform: {} as never, preview: preview(), runtime });
  render(
    <ContainerIdentity csrf="c" platformId="docker-local" runtime={runtime} onUnauthorized={() => {}} />,
  );
  fireEvent.click(screen.getByLabelText('Custom settings'));
  fireEvent.change(screen.getByLabelText('PUID'), { target: { value: '3000' } });
  fireEvent.change(screen.getByLabelText('PGID'), { target: { value: '3000' } });
  fireEvent.change(screen.getByLabelText('UMASK'), { target: { value: '002' } });
  fireEvent.change(screen.getByLabelText('Supplemental group IDs'), { target: { value: '44' } });
  fireEvent.click(screen.getByRole('button', { name: 'Preview identity' }));
  await screen.findByText('Effective configuration');
  expect(vi.mocked(previewIdentity).mock.calls[0]?.[0]).toMatchObject({
    platform: 'docker-local',
    mode: 'custom',
    override: { uid: 3000, gid: 3000, umask: '002', supplementalGroups: [44] },
  });
  expect(screen.getAllByText('This container').length).toBe(4);
  expect(screen.getByText(/New files up to 0664, directories up to 0775/)).toBeTruthy();
  expect(screen.getByText(/not reported by this platform/)).toBeTruthy();
  expect(screen.queryByRole('button', { name: /apply|deploy/i })).toBeNull();
});

it('blocks invalid custom input client-side and asks for privileged-group acknowledgement', async () => {
  render(
    <ContainerIdentity csrf="c" platformId="docker-local" runtime={runtime} onUnauthorized={() => {}} />,
  );
  fireEvent.click(screen.getByLabelText('Custom settings'));
  fireEvent.change(screen.getByLabelText('UMASK'), { target: { value: '99' } });
  fireEvent.click(screen.getByRole('button', { name: 'Preview identity' }));
  expect((await screen.findByRole('alert')).textContent).toMatch(/UMASK/);
  expect(previewIdentity).not.toHaveBeenCalled();
  vi.mocked(previewIdentity).mockResolvedValue({
    platform: {} as never,
    preview: preview(
      {
        issues: [
          {
            severity: 'blocker',
            code: 'privileged_group',
            message: 'GID 970 (docker) may grant elevated authority.',
          },
        ],
      },
      true,
    ),
    runtime,
  });
  fireEvent.change(screen.getByLabelText('UMASK'), { target: { value: '' } });
  fireEvent.change(screen.getByLabelText('Supplemental group IDs'), { target: { value: '970' } });
  fireEvent.click(screen.getByRole('button', { name: 'Preview identity' }));
  await screen.findByText(/Blocked:/);
  expect(screen.getByLabelText(/I understand GID 970/)).toBeTruthy();
});

it('shows fixed identity images as non-configurable', () => {
  const fixed: ContainerRuntime = {
    ...runtime,
    identity: {
      configurable: false,
      method: 'fixed',
      fixedUid: 65532,
      fixedGid: 65532,
      umask: { method: 'none' },
      supplementalGroups: false,
    },
  };
  render(<ContainerIdentity csrf="c" platformId="docker-local" runtime={fixed} onUnauthorized={() => {}} />);
  expect(screen.getByText(/fixed identity \(65532:65532\)/)).toBeTruthy();
  fireEvent.click(screen.getByLabelText('Custom settings'));
  expect((screen.getByLabelText('PUID') as HTMLInputElement).disabled).toBe(true);
  expect((screen.getByLabelText('UMASK') as HTMLInputElement).disabled).toBe(true);
});
