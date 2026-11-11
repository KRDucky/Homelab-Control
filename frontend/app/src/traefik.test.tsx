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
  previewCompose,
  getPlatforms,
} from './api';
vi.mock('./api', async (original) => ({
  ...(await original<typeof import('./api')>()),
  getTraefikInventory: vi.fn(),
  getTraefikDiagnostics: vi.fn(),
  getComposeApplications: vi.fn(),
  previewCompose: vi.fn(),
  getPlatforms: vi.fn(),
}));
const unauthorized = vi.fn();
beforeEach(() => {
  vi.clearAllMocks();
  // No platform registry: the verified Compose flow must be unchanged.
  vi.mocked(getPlatforms).mockRejectedValue(new ApiError(503, 'platforms_unavailable', 'none'));
  vi.mocked(getTraefikInventory).mockResolvedValue({
    schemaVersion: 1,
    routers: [{ name: 'fixture@file', provider: 'file', status: 'enabled' }],
    services: [{ name: 'target@file', provider: 'file', status: 'enabled' }],
    middlewares: [{ name: 'headers@file', provider: 'file', status: 'enabled' }],
  });
  vi.mocked(getTraefikDiagnostics).mockResolvedValue({
    schemaVersion: 1,
    status: 'available',
    checks: [{ resource: 'routers', status: 'available', count: 1 }],
  });
  vi.mocked(getComposeApplications).mockResolvedValue({
    source: 'fixture',
    fixture: true,
    services: [
      {
        id: 'notes',
        compose: true,
        project: 'notes',
        service: 'web',
        container: 'notes-web-1',
        state: 'running',
        ports: [{ port: 8080, protocol: 'tcp' }],
        networks: ['private'],
        traefikLabels: {},
        hiddenTraefikSettings: false,
      },
    ],
    traefik: { networks: ['private'], entrypoints: ['web'], resolvers: ['staging'], dockerProvider: false },
  });
  vi.mocked(previewCompose).mockResolvedValue({
    schemaVersion: 1,
    source: 'fixture',
    fixture: true,
    project: 'notes',
    service: 'web',
    existing: { labels: {}, networks: ['private'] },
    proposed: { labels: { 'traefik.enable': 'true' }, networks: ['private'] },
    changes: [],
    blockers: [{ code: 'provider_disabled', message: 'Docker provider is disabled.' }],
    warnings: [],
    applyAvailable: false,
    sourceFileVerified: false,
    preservedFields: ['environment'],
  });
});
afterEach(cleanup);
async function open() {
  render(<Traefik csrf="csrf" onUnauthorized={unauthorized} />);
  await screen.findByText('available');
  fireEvent.click(screen.getByRole('button', { name: 'Proxy Hosts' }));
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add Proxy Host' }).hasAttribute('disabled')).toBe(false),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Add Proxy Host' }));
  fireEvent.change(screen.getByLabelText('Docker Compose application'), { target: { value: 'notes' } });
  fireEvent.change(screen.getByLabelText('Service / container'), { target: { value: 'notes' } });
  fireEvent.change(screen.getByLabelText('Internal application port'), { target: { value: '8080' } });
  fireEvent.change(screen.getByLabelText('Public hostname'), { target: { value: 'notes.example.com' } });
}
it('shows Host and PathPrefix file-provider routes instead of Unknown', async () => {
  vi.mocked(getTraefikInventory).mockResolvedValue({
    schemaVersion: 1,
    routers: [
      {
        name: 'app-http@file',
        provider: 'file',
        status: 'enabled',
        rule: 'Host(`app.isolated.test`) && PathPrefix(`/app`)',
        tls: false,
      },
      {
        name: 'hc-api@file',
        provider: 'file',
        status: 'enabled',
        rule: 'PathPrefix(`/api`)',
      },
    ],
    services: [],
    middlewares: [],
  });
  render(<Traefik csrf="csrf" onUnauthorized={unauthorized} />);
  await screen.findByText('available');
  fireEvent.click(screen.getByRole('button', { name: 'Proxy Hosts' }));
  expect(screen.getByText('app.isolated.test')).toBeTruthy();
  expect(screen.getByText('app-http@file · Host(`app.isolated.test`) && PathPrefix(`/app`)')).toBeTruthy();
  expect(screen.getByText('/api')).toBeTruthy();
  expect(screen.getByText('hc-api@file · PathPrefix(`/api`)')).toBeTruthy();
});

it('renders overview, real inventory tabs and honest unknown fields', async () => {
  render(<Traefik csrf="csrf" onUnauthorized={unauthorized} />);
  await screen.findByText('available');
  expect(screen.getByText('Active routers')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Proxy Hosts' }));
  expect(screen.getAllByText('Unknown').length).toBeGreaterThan(2);
  for (const [tab, row] of [
    ['Services', 'target@file'],
    ['Middleware', 'headers@file'],
  ] as const) {
    fireEvent.click(screen.getByRole('button', { name: tab }));
    expect(screen.getByText(row)).toBeTruthy();
  }
  expect(screen.getByRole('button', { name: /Certificates/ }).hasAttribute('disabled')).toBe(true);
  for (const name of ['Apply', 'Deploy', 'Restart', 'Save'])
    expect(screen.queryByRole('button', { name })).toBeNull();
  expect(getTraefikInventory).toHaveBeenCalledWith('csrf');
});
it('selects internal ports and previews backend proposal without mutation', async () => {
  await open();
  // Without a platform registry there is no platform step: two numbered steps.
  expect(screen.getAllByText(/^\d\. (Select|Configure) /).map((e) => e.textContent)).toEqual([
    '1. Select application',
    '2. Configure hostname',
  ]);
  fireEvent.click(screen.getByRole('button', { name: 'Preview configuration' }));
  await waitFor(() =>
    expect(previewCompose).toHaveBeenCalledWith(
      {
        serviceId: 'notes',
        network: 'private',
        model: {
          name: 'proxy-host',
          host: 'notes.example.com',
          port: 8080,
          entrypoint: 'web',
          protocol: 'http',
          upstreamScheme: 'http',
        },
      },
      'csrf',
    ),
  );
  expect(await screen.findByText('Docker provider is disabled.')).toBeTruthy();
  expect(screen.getByLabelText('Compose labels').textContent).toContain('traefik.enable');
  expect(screen.getByText('Proposed Reverse Proxy Configuration')).toBeTruthy();
  expect(screen.getByText('Advanced Details').parentElement?.hasAttribute('open')).toBe(false);
});
it('associates protocol label and separates incoming TLS from upstream HTTP', async () => {
  await open();
  const protocol = screen.getByLabelText('Incoming protocol');
  expect(protocol.getAttribute('name')).toBe('protocol');
  fireEvent.change(protocol, { target: { value: 'https' } });
  fireEvent.change(screen.getByLabelText('Existing certificate resolver (optional)'), {
    target: { value: 'staging' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Preview configuration' }));
  await waitFor(() =>
    expect(previewCompose).toHaveBeenCalledWith(
      expect.objectContaining({
        model: expect.objectContaining({
          protocol: 'https',
          upstreamScheme: 'http',
          tls: { certificateResolver: 'staging' },
        }),
      }),
      'csrf',
    ),
  );
});
it('shows unavailable discovery without inventing applications', async () => {
  vi.mocked(getComposeApplications).mockRejectedValue(
    new ApiError(503, 'discovery_unavailable', 'Unavailable'),
  );
  render(<Traefik csrf="csrf" onUnauthorized={unauthorized} />);
  await screen.findByText('available');
  fireEvent.click(screen.getByRole('button', { name: 'Proxy Hosts' }));
  expect(screen.getByRole('button', { name: 'Add Proxy Host' }).hasAttribute('disabled')).toBe(true);
  expect(screen.getByText(/Compose discovery is unavailable/)).toBeTruthy();
});
it('rejects invalid form input in the browser and clears obsolete previews', async () => {
  await open();
  const host = screen.getByLabelText('Public hostname') as HTMLInputElement;
  fireEvent.change(host, { target: { value: 'bad`host' } });
  expect(host.checkValidity()).toBe(false);
  expect(previewCompose).not.toHaveBeenCalled();
});
it('clears forbidden inventory on refresh', async () => {
  render(<Traefik csrf="csrf" onUnauthorized={unauthorized} />);
  await screen.findByText('available');
  vi.mocked(getTraefikInventory).mockRejectedValue(new ApiError(403, 'forbidden', 'Denied'));
  fireEvent.click(screen.getByRole('button', { name: 'Refresh inventory and diagnostics' }));
  await screen.findByRole('alert');
  fireEvent.click(screen.getByRole('button', { name: 'Proxy Hosts' }));
  expect(screen.queryByText('enabled')).toBeNull();
});

it('clears a completed proposal when routing inputs change', async () => {
  await open();
  fireEvent.click(screen.getByRole('button', { name: 'Preview configuration' }));
  await screen.findByLabelText('Compose labels');
  fireEvent.change(screen.getByLabelText('Public hostname'), { target: { value: 'other.example.com' } });
  expect(screen.queryByLabelText('Compose labels')).toBeNull();
});

it('shows selected networks and permits explicitly confirmed unknown internal ports', async () => {
  const discover = vi.mocked(getComposeApplications).getMockImplementation();
  if (!discover) throw new Error('fixture discovery missing');
  const catalog = await discover('csrf');
  const service = catalog.services[0];
  if (!service) throw new Error('fixture service missing');
  service.ports = [];
  service.replicaCount = 2;
  vi.mocked(getComposeApplications).mockResolvedValue(catalog);
  render(<Traefik csrf="csrf" onUnauthorized={unauthorized} />);
  await screen.findByText('available');
  fireEvent.click(screen.getByRole('button', { name: 'Proxy Hosts' }));
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add Proxy Host' }).hasAttribute('disabled')).toBe(false),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Add Proxy Host' }));
  fireEvent.change(screen.getByLabelText('Docker Compose application'), { target: { value: 'notes' } });
  fireEvent.change(screen.getByLabelText('Service / container'), { target: { value: 'notes' } });
  expect(screen.getByText(/Docker networks: private/)).toBeTruthy();
  expect(screen.getByText(/Multiple replicas require/)).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Internal application port'), { target: { value: '3000' } });
  fireEvent.click(screen.getByRole('checkbox'));
  fireEvent.change(screen.getByLabelText('Public hostname'), { target: { value: 'manual.example.com' } });
  fireEvent.click(screen.getByRole('button', { name: 'Preview configuration' }));
  await waitFor(() =>
    expect(previewCompose).toHaveBeenCalledWith(
      expect.objectContaining({
        confirmUnverifiedPort: true,
        model: expect.objectContaining({ port: 3000 }),
      }),
      'csrf',
    ),
  );
});
it('uses backend summary and keeps raw configuration collapsed', async () => {
  const preview = vi.mocked(previewCompose).getMockImplementation();
  if (!preview) throw new Error('fixture preview missing');
  const p = await preview(
    {
      serviceId: 'notes',
      network: 'private',
      model: {
        name: 'proxy-host',
        host: 'notes.example.com',
        port: 8080,
        entrypoint: 'web',
        protocol: 'http',
        upstreamScheme: 'http',
      },
    },
    'csrf',
  );
  p.summary = {
    application: 'notes',
    targetService: 'web',
    container: 'notes-web-1',
    hostname: 'validated.example.com',
    internalPort: 8080,
    protocol: 'https',
    upstreamScheme: 'http',
    certificateResolver: 'staging',
    network: 'private',
    entrypoint: 'web',
  };
  vi.mocked(previewCompose).mockResolvedValue(p);
  await open();
  fireEvent.click(screen.getByRole('button', { name: 'Preview configuration' }));
  expect(await screen.findByText('validated.example.com')).toBeTruthy();
  expect(screen.getByText('Advanced Details').parentElement?.hasAttribute('open')).toBe(false);
});
