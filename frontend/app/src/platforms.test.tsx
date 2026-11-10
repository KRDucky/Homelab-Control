// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Traefik } from './pages/Traefik';
import {
  ApiError,
  getTraefikInventory,
  getTraefikDiagnostics,
  getComposeApplications,
  getPlatforms,
  getPlatformInventory,
  previewPlatform,
  type PlatformView,
  type PlatformInventory,
} from './api';
vi.mock('./api', async (original) => ({
  ...(await original<typeof import('./api')>()),
  getTraefikInventory: vi.fn(),
  getTraefikDiagnostics: vi.fn(),
  getComposeApplications: vi.fn(),
  previewCompose: vi.fn(),
  getPlatforms: vi.fn(),
  getPlatformInventory: vi.fn(),
  previewPlatform: vi.fn(),
}));

const caps = (detected: PlatformView['capabilities']['detected']) => ({
  declared: detected,
  detected,
  unsupported: [{ capability: 'routing.apply' as const, reason: 'Not implemented.' }],
});
const view = (id: string, kind: PlatformView['kind'], mode: PlatformView['mode']): PlatformView => ({
  id,
  displayName: `${kind} demo`,
  kind,
  distribution: kind === 'kubernetes' ? 'k3s' : kind,
  mode,
  authentication: 'fixture',
  endpoint: { transport: 'fixture', label: 'Demonstration', fingerprint: 'sha256:00' },
  fixture: true,
  capabilities: caps(
    kind === 'kubernetes' ? ['routing.preview.kubernetes-ingress'] : ['routing.preview.compose-labels'],
  ),
  health: { status: 'unknown', reason: '' },
  policy: { readScope: `${kind}.read`, previewScope: '', apply: 'unavailable' },
});
const k3s = view('k3s-fixture', 'kubernetes', 'cluster');
const ref = (kind: string, name: string) => ({
  platform: 'k3s-fixture',
  kind,
  namespace: 'demo',
  name,
  uid: `${name}-uid`,
});
const k3sInventory: PlatformInventory = {
  platform: 'k3s-fixture',
  fixture: true,
  distribution: 'k3s',
  version: 'v1.31.4+k3s1',
  workloads: [
    {
      ref: ref('Deployment', 'notes'),
      group: 'demo',
      state: 'ready',
      replicas: 2,
      readyReplicas: 2,
      ports: [{ port: 8080, protocol: 'tcp', name: 'http' }],
      networks: ['demo'],
      services: [ref('Service', 'notes')],
      routingLabels: {},
      hiddenRouting: false,
      issues: [],
    },
  ],
  services: [
    {
      ref: ref('Service', 'notes'),
      type: 'ClusterIP',
      ports: [{ port: 80, protocol: 'tcp', name: 'http', target: 'http' }],
      selects: [],
    },
  ],
  networks: [],
  namespaces: ['demo'],
  routing: [{ kind: 'kubernetes-ingress', available: true, classes: ['traefik'], detail: '' }],
  entrypoints: ['web'],
  resolvers: [],
  capabilities: caps(['routing.preview.kubernetes-ingress']),
};
const unauthorized = vi.fn();
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(getTraefikInventory).mockResolvedValue({
    schemaVersion: 1,
    routers: [],
    services: [],
    middlewares: [],
  });
  vi.mocked(getTraefikDiagnostics).mockResolvedValue({ schemaVersion: 1, status: 'available', checks: [] });
  vi.mocked(getComposeApplications).mockRejectedValue(new ApiError(503, 'discovery_unavailable', 'off'));
  vi.mocked(getPlatformInventory).mockResolvedValue({ platform: k3s, inventory: k3sInventory });
  vi.mocked(previewPlatform).mockResolvedValue({
    schemaVersion: 1,
    platform: k3s,
    target: 'routing.preview.kubernetes-ingress',
    format: 'kubernetes-manifests',
    workload: ref('Deployment', 'notes'),
    service: ref('Service', 'notes'),
    manifests: [{ apiVersion: 'networking.k8s.io/v1', kind: 'Ingress' }],
    warnings: [{ code: 'fixture', message: 'Demonstration cluster data.' }],
    blockers: [],
    applyAvailable: false,
    deployable: false,
    validation: [{ check: 'tls-certificate-issuance', status: 'not-verified-by-preview' }],
  });
});
afterEach(cleanup);

async function openAdd() {
  render(<Traefik csrf="csrf" onUnauthorized={unauthorized} />);
  await screen.findByText('available');
  fireEvent.click(screen.getByRole('button', { name: 'Proxy Hosts' }));
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add Proxy Host' }).hasAttribute('disabled')).toBe(false),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Add Proxy Host' }));
}

it('auto-selects the only platform and previews Kubernetes resources, never Compose labels', async () => {
  vi.mocked(getPlatforms).mockResolvedValue({ platforms: [k3s], autoSelected: 'k3s-fixture' });
  await openAdd();
  expect(screen.getByText(/selected automatically/)).toBeTruthy();
  expect(screen.queryByLabelText('Container platform')).toBeNull();
  expect(screen.queryByLabelText('Docker Compose application')).toBeNull();
  await screen.findByLabelText('Workload');
  expect(screen.getByText(/Demonstration platform/)).toBeTruthy();
  expect(screen.getAllByText(/^\d\. (Select|Configure) /).map((e) => e.textContent)).toEqual([
    '1. Select platform',
    '2. Select application',
    '3. Configure hostname',
  ]);
  fireEvent.change(screen.getByLabelText('Workload'), {
    target: { value: 'k3s-fixture/Deployment/demo/notes/notes-uid' },
  });
  fireEvent.change(screen.getByLabelText('Kubernetes Service'), {
    target: { value: 'k3s-fixture/Service/demo/notes/notes-uid' },
  });
  fireEvent.change(screen.getByLabelText('Service port'), { target: { value: '80' } });
  fireEvent.change(screen.getByLabelText('Public hostname'), { target: { value: 'notes.example.test' } });
  fireEvent.click(screen.getByRole('button', { name: 'Preview configuration' }));
  await screen.findByText(/Preview only — not deployable/);
  const body = vi.mocked(previewPlatform).mock.calls[0]?.[0];
  expect(body).toMatchObject({
    platform: 'k3s-fixture',
    target: 'routing.preview.kubernetes-ingress',
    service: 'k3s-fixture/Service/demo/notes/notes-uid',
  });
  expect(body && 'network' in body).toBe(false);
  expect(screen.getByText(/"kind": "Ingress"/)).toBeTruthy();
  expect(screen.queryByRole('button', { name: /apply|deploy/i })).toBeNull();
});

it('asks the user to choose when several platforms are registered', async () => {
  vi.mocked(getPlatforms).mockResolvedValue({
    platforms: [
      view('docker-fixture', 'docker', 'rootless'),
      view('podman-fixture', 'podman', 'rootless'),
      k3s,
    ],
    autoSelected: null,
  });
  await openAdd();
  const select = screen.getByLabelText('Container platform');
  expect(screen.queryByLabelText('Workload')).toBeNull();
  fireEvent.change(select, { target: { value: 'k3s-fixture' } });
  await screen.findByLabelText('Workload');
  expect(getPlatformInventory).toHaveBeenCalledWith('k3s-fixture', 'csrf');
});

it('auto-selects a single live Docker platform and keeps the verified Compose flow', async () => {
  const docker: PlatformView = {
    ...view('docker-local', 'docker', 'rootless'),
    displayName: 'Docker (rootless)',
    distribution: 'docker-engine',
    authentication: 'broker-peer-uid-and-token',
    endpoint: {
      transport: 'unix-metadata-broker',
      label: 'Local rootless Docker metadata broker',
      fingerprint: 'sha256:00',
    },
    fixture: false,
  };
  vi.mocked(getPlatforms).mockResolvedValue({ platforms: [docker], autoSelected: 'docker-local' });
  vi.mocked(getComposeApplications).mockResolvedValue({
    source: 'docker-rootless',
    fixture: false,
    services: [
      {
        id: 'a'.repeat(64),
        compose: true,
        project: 'live',
        service: 'plugin',
        container: 'live-plugin-1',
        state: 'running',
        ports: [{ port: 9080, protocol: 'tcp' }],
        networks: ['private'],
        traefikLabels: {},
        hiddenTraefikSettings: false,
      },
    ],
    traefik: { networks: ['private'], entrypoints: ['web'], resolvers: [], dockerProvider: false },
  });
  await openAdd();
  expect(screen.getByText(/Docker \(rootless\) \(docker, rootless\).*selected automatically/)).toBeTruthy();
  expect(screen.queryByLabelText('Container platform')).toBeNull();
  expect(screen.queryByLabelText('Workload')).toBeNull();
  // Steps continue after the platform step: 1 platform, 2 application, 3 hostname.
  expect(screen.getAllByText(/^\d\. (Select|Configure) /).map((e) => e.textContent)).toEqual([
    '1. Select platform',
    '2. Select application',
    '3. Configure hostname',
  ]);
  fireEvent.change(screen.getByLabelText('Docker Compose application'), { target: { value: 'live' } });
  fireEvent.change(screen.getByLabelText('Service / container'), { target: { value: 'a'.repeat(64) } });
  expect((screen.getByLabelText('Internal application port') as HTMLSelectElement).value).toBe('9080');
  expect(getPlatformInventory).not.toHaveBeenCalled();
  expect(screen.queryByText(/Demonstration platform/)).toBeNull();
  expect(screen.queryByRole('button', { name: /apply|deploy/i })).toBeNull();
});
