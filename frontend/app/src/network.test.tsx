// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { Ports } from './pages/Ports';
import { Bonds } from './pages/Bonds';
import { Firewall } from './pages/Firewall';
import { groupNavigation } from './Shell';
import {
  getBonds,
  getFirewall,
  getPorts,
  type BondSnapshot,
  type Capability,
  type FirewallSnapshot,
  type PortMap,
} from './api';

vi.mock('./api', async (importOriginal) => {
  const mod = await importOriginal<typeof import('./api')>();
  return { ...mod, getPorts: vi.fn(), getBonds: vi.fn(), getFirewall: vi.fn() };
});

const onUnauthorized = vi.fn();

beforeEach(() => vi.clearAllMocks());
afterEach(cleanup);

// Fixture data for tests only (labelled; never shown in production views).
const portMap: PortMap = {
  namespace: 'host (network namespace of PID 1)',
  sources: ['tcp', 'udp'],
  processesScanned: 40,
  processesHidden: 3,
  truncated: false,
  notes: [],
  sockets: [
    {
      proto: 'tcp',
      address: '0.0.0.0',
      port: 22,
      exposure: 'all',
      uid: 0,
      account: 'root',
      owners: [{ pid: 700, command: 'sshd', unit: 'ssh.service', source: 'process', confidence: 'observed' }],
    },
    {
      proto: 'tcp',
      address: '127.0.0.1',
      port: 631,
      exposure: 'loopback',
      uid: 0,
      account: 'root',
      owners: [],
    },
    {
      proto: 'tcp6',
      address: '::',
      port: 8080,
      exposure: 'all',
      uid: 0,
      owners: [
        {
          pid: 900,
          command: 'docker-proxy',
          container: '0123456789ab',
          source: 'process',
          confidence: 'observed',
        },
      ],
    },
    {
      proto: 'udp',
      address: '0.0.0.0',
      port: 5353,
      exposure: 'all',
      uid: 104,
      account: 'avahi',
      owners: [
        {
          pid: 0,
          command: 'avahi-daemon',
          unit: 'avahi-daemon.service',
          source: 'account',
          confidence: 'inferred',
        },
      ],
    },
    {
      proto: 'tcp6',
      address: '::',
      port: 22,
      exposure: 'all',
      uid: 0,
      account: 'root',
      owners: [
        {
          pid: 0,
          command: 'systemd',
          socket: 'ssh.socket',
          unit: 'ssh.service',
          source: 'systemd-socket',
          confidence: 'observed',
        },
      ],
    },
  ],
};

describe('Ports page', () => {
  it('lists sockets with owners and explains hidden processes', async () => {
    vi.mocked(getPorts).mockResolvedValue(portMap);
    render(<Ports csrf="t" onUnauthorized={onUnauthorized} />);
    const table = await screen.findByRole('table', { name: /listening tcp and bound udp sockets/i });
    expect(within(table).getByText('sshd (PID 700, ssh.service)')).toBeTruthy();
    expect(within(table).getByText('docker-proxy (PID 900, container 0123456789ab)')).toBeTruthy();
    expect(within(table).getByText('Not identified')).toBeTruthy();
    expect(within(table).getByText('avahi-daemon.service (inferred from the account)')).toBeTruthy();
    expect(within(table).getByText('systemd socket ssh.socket → ssh.service')).toBeTruthy();
    expect(screen.getByText(/open files of 3 of 43 processes\s+could not be read/)).toBeTruthy();
    expect(screen.getByText(/5 sockets, 4 bound to an address other than loopback/)).toBeTruthy();
    expect(getPorts).toHaveBeenCalledWith('t');
  });

  it('filters and hides loopback-only ports', async () => {
    vi.mocked(getPorts).mockResolvedValue(portMap);
    render(<Ports csrf="t" onUnauthorized={onUnauthorized} />);
    const table = await screen.findByRole('table');
    expect(within(table).getAllByRole('row')).toHaveLength(6);
    fireEvent.click(screen.getByRole('checkbox', { name: /hide ports bound to this host only/i }));
    expect(within(table).queryByText('631')).toBeNull();
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'docker' } });
    expect(within(screen.getByRole('table')).getAllByRole('row')).toHaveLength(2);
  });

  it('does not claim a bound port is reachable, because the firewall is not applied', async () => {
    vi.mocked(getPorts).mockResolvedValue(portMap);
    render(<Ports csrf="t" onUnauthorized={onUnauthorized} />);
    const table = await screen.findByRole('table');
    // The socket map comes from the kernel socket tables only. A port bound to
    // 0.0.0.0 can still be dropped by the firewall, so the page must describe
    // the bind scope, never reachability.
    expect(screen.queryByText(/reachable/i)).toBeNull();
    expect(within(table).getByRole('columnheader', { name: 'Bound to' })).toBeTruthy();
    expect(screen.getByText(/firewall rules are not applied/i)).toBeTruthy();
  });

  it('offers no write control', async () => {
    vi.mocked(getPorts).mockResolvedValue(portMap);
    render(<Ports csrf="t" onUnauthorized={onUnauthorized} />);
    await screen.findByRole('table');
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });
});

const lacp: BondSnapshot = {
  driverLoaded: true,
  stacks: ['systemd-networkd'],
  truncated: false,
  bonds: [
    {
      name: 'bond0',
      mode: '802.3ad',
      lacp: true,
      operState: 'up',
      miimonMs: '100',
      lacpRate: 'fast',
      xmitHashPolicy: 'layer3+4',
      aggregatorId: '1',
      slaves: [
        {
          name: 'enp1s0',
          state: 'active',
          miiStatus: 'up',
          linkFailures: 0,
          speed: '1000',
          aggregatorId: '1',
        },
        { name: 'enp2s0', state: 'backup', miiStatus: 'up', linkFailures: 4, aggregatorId: '2' },
      ],
      renderer: {
        id: 'systemd-networkd',
        via: 'netplan',
        source: '/run/systemd/netif/links/4',
        confidence: 'observed',
      },
    },
  ],
};

describe('Bonds page', () => {
  it('shows LACP details, a missing partner and a split aggregator', async () => {
    vi.mocked(getBonds).mockResolvedValue(lacp);
    render(<Bonds csrf="t" onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText('LACP (802.3ad)')).toBeTruthy();
    expect(screen.getByText(/No partner has answered/)).toBeTruthy();
    expect(
      screen.getByText(/systemd-networkd via netplan — observed in \/run\/systemd\/netif\/links\/4/),
    ).toBeTruthy();
    const table = screen.getByRole('table', { name: /ports of bond0/i });
    expect(within(table).getByText('2 (not in the active aggregator)')).toBeTruthy();
    expect(within(table).getByText('Unknown')).toBeTruthy();
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });

  it('explains a missing bonding driver', async () => {
    vi.mocked(getBonds).mockResolvedValue({ driverLoaded: false, stacks: [], truncated: false, bonds: [] });
    render(<Bonds csrf="t" onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/bonding driver is not loaded/)).toBeTruthy();
    expect(screen.getByText(/none detected from runtime state/)).toBeTruthy();
  });
});

const ufw: FirewallSnapshot = {
  active: 'ufw',
  activeReason: 'ufw is enabled in /etc/ufw/ufw.conf and ufw.service is active.',
  alsoActive: [],
  source: 'saved-configuration',
  notes: [],
  backends: [
    { id: 'firewalld', installed: false, role: 'absent' },
    { id: 'ufw', installed: true, role: 'active', unit: 'ufw.service', enabled: 'enabled' },
    { id: 'nftables', installed: true, role: 'installed', unit: 'nftables.service', enabled: 'disabled' },
    { id: 'iptables', installed: true, role: 'installed' },
  ],
  ufw: {
    enabled: true,
    defaultInput: 'drop',
    defaultOutput: 'accept',
    defaultForward: 'drop',
    logLevel: 'low',
    truncated: false,
    unreadable: [],
    rules: [
      {
        action: 'allow',
        direction: 'in',
        route: false,
        proto: 'tcp',
        to: '0.0.0.0/0',
        toPort: '22',
        from: '192.168.1.0/24',
        fromPort: 'any',
        ipv6: false,
        comment: 'lan ssh',
      },
    ],
  },
};

// eslint-disable-next-line @typescript-eslint/no-unused-vars
const { ufw: _ufwView, ...noUfw } = ufw;

describe('Firewall page', () => {
  it('shows only the active backend and lists the rest as installed', async () => {
    vi.mocked(getFirewall).mockResolvedValue(ufw);
    render(<Firewall csrf="t" onUnauthorized={onUnauthorized} />);
    expect(await screen.findByRole('heading', { name: 'Active: ufw' })).toBeTruthy();
    expect(
      screen.getByText(/Installed, not active: nftables \(unit disabled\), iptables \(saved rules\)/),
    ).toBeTruthy();
    expect(screen.getByRole('table', { name: /user rules/i })).toBeTruthy();
    expect(screen.getByText('lan ssh')).toBeTruthy();
    expect(screen.queryByRole('heading', { name: /firewalld zones/i })).toBeNull();
    expect(screen.getByText(/does not read\s+the live kernel ruleset/)).toBeTruthy();
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });

  it('explains when no firewall service is active', async () => {
    vi.mocked(getFirewall).mockResolvedValue({
      active: 'none',
      activeReason: 'No firewall service is active.',
      alsoActive: [],
      source: 'saved-configuration',
      notes: ['Rules loaded by other means are not shown.'],
      backends: [{ id: 'nftables', installed: true, role: 'installed' }],
    });
    render(<Firewall csrf="t" onUnauthorized={onUnauthorized} />);
    expect(await screen.findByRole('heading', { name: 'No active firewall service' })).toBeTruthy();
    expect(screen.getByText('Rules loaded by other means are not shown.')).toBeTruthy();
  });

  it('renders firewalld zones and nftables chains', async () => {
    vi.mocked(getFirewall).mockResolvedValue({
      ...noUfw,
      active: 'firewalld',
      firewalld: {
        defaultZone: 'public',
        truncated: false,
        unreadable: ['/etc/firewalld/zones/bad.xml'],
        zones: [
          {
            name: 'public',
            default: true,
            target: 'default',
            source: '/etc',
            interfaces: [],
            sources: [],
            services: ['ssh', 'dhcpv6-client'],
            ports: ['8443/tcp'],
            masquerade: false,
            forwardPorts: [],
            richRules: ['rule family=ipv4 service name=http accept'],
          },
        ],
      },
    });
    render(<Firewall csrf="t" onUnauthorized={onUnauthorized} />);
    expect(await screen.findByRole('heading', { name: 'firewalld zones' })).toBeTruthy();
    expect(screen.getByText('ssh, dhcpv6-client')).toBeTruthy();
    expect(screen.getByText(/Could not read: \/etc\/firewalld\/zones\/bad.xml/)).toBeTruthy();
    cleanup();
    vi.mocked(getFirewall).mockResolvedValue({
      ...noUfw,
      active: 'nftables',
      nftables: {
        file: '/etc/nftables.conf',
        flushRuleset: true,
        includes: [],
        truncated: false,
        unreadable: [],
        tables: [
          {
            family: 'inet',
            name: 'filter',
            sets: [],
            chains: [
              { name: 'input', type: 'filter', hook: 'input', policy: 'drop', rules: ['iif "lo" accept'] },
            ],
          },
        ],
      },
    });
    render(<Firewall csrf="t" onUnauthorized={onUnauthorized} />);
    expect(await screen.findByText(/filter hook input, policy drop \(1 rules\)/)).toBeTruthy();
    expect(screen.getByText(/replaces the whole ruleset/)).toBeTruthy();
  });
});

describe('navigation', () => {
  it('groups the M11 pages under Network & applications', () => {
    const cap = (id: Capability['id']): Capability => ({
      id,
      label: id,
      path: '/' + id,
      scope: 's',
      status: 'available',
    });
    const groups = groupNavigation([cap('ports'), cap('bonds'), cap('firewall'), cap('traefik')]);
    const net = groups.find((g) => g.title === 'Network & applications');
    expect(net?.pages.map((p) => p.id)).toEqual(['ports', 'bonds', 'firewall', 'traefik']);
  });
});
