// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Typed client for the Homelab Control v1 API (schemas/openapi/homelab-control.v1.yaml).

export interface SessionInfo {
  username: string;
  scopes: string[] | null;
  csrf_token: string;
  version: string;
  legacy_url?: string;
}

export interface Meta {
  status: 'ok' | 'partial' | 'unavailable';
  sources: string[];
  errors?: string[];
}

export interface Filesystem {
  kernel_source: string;
  mount_point: string;
  fs_type: string;
  read_only: boolean;
  probed: boolean;
  skip_reason?: string;
  total_bytes?: number;
  used_bytes?: number;
  avail_bytes?: number;
}

export interface NetInterface {
  name: string;
  oper_state?: string;
  mac?: string;
  mtu?: number;
  physical: boolean;
  speed_mbps?: number;
  rx_bytes: number;
  tx_bytes: number;
  addresses?: string[];
}

export interface Overview {
  collected_at: string;
  system: Meta & {
    hostname?: string;
    kernel?: string;
    os: { id?: string; id_like?: string; name?: string; pretty_name?: string; version_id?: string };
    distro_family: string;
    uptime_seconds?: number;
    load_average: [number, number, number];
  };
  cpu: Meta & { model?: string; logical_cpus: number; usage_percent?: number; sample_millis: number };
  memory: Meta & {
    total_bytes: number;
    available_bytes: number;
    used_bytes: number;
    swap_total_bytes: number;
    swap_free_bytes: number;
  };
  filesystems: Meta & { view: 'host' | 'service'; note?: string; items: Filesystem[] };
  network: Meta & { interfaces: NetInterface[] };
  legacy_webmin: Meta & {
    installed: boolean;
    version?: string;
    config_readable: boolean;
    configured_url?: string;
    privilege_needed?: string;
  };
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(path, { credentials: 'same-origin', ...init });
  if (res.status === 204) return undefined as T;
  let body: unknown = null;
  try {
    body = await res.json();
  } catch {
    // non-JSON error body
  }
  if (!res.ok) {
    const b = (body ?? {}) as { error?: string; message?: string };
    throw new ApiError(res.status, b.error ?? 'http_error', b.message ?? `Request failed (${res.status})`);
  }
  return body as T;
}

export function getSession(): Promise<SessionInfo> {
  return request<SessionInfo>('/api/v1/auth/session');
}

export function login(username: string, password: string): Promise<SessionInfo> {
  return request<SessionInfo>('/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
}

export function logout(csrf: string): Promise<undefined> {
  return request<undefined>('/api/v1/auth/logout', { method: 'POST', headers: { 'X-CSRF-Token': csrf } });
}

export function getOverview(): Promise<Overview> {
  return request<Overview>('/api/v1/host/overview');
}

// ---- M1: capabilities, services, hardware, legacy -------------------------

export type CapabilityStatus = 'available' | 'degraded' | 'unavailable' | 'unsupported' | 'forbidden';

export interface Capability {
  id: 'overview' | 'services' | 'service_logs' | 'hardware' | 'legacy';
  label: string;
  path: string;
  scope: string;
  status: CapabilityStatus;
  reason?: string;
}

export interface Capabilities {
  broker: { status: 'available' | 'unavailable'; version?: string; reason?: string };
  capabilities: Capability[];
}

export interface Unit {
  name: string;
  type: string;
  load: string;
  active: string;
  sub: string;
  description: string;
  unit_file_state?: string;
  preset?: string;
}

export interface UnitList {
  systemd_version?: string;
  units: Unit[];
  counts: Record<string, number>;
  truncated: boolean;
  source: string;
}

export interface LogEntry {
  time?: string;
  priority?: number;
  pid?: number;
  identifier?: string;
  message: string;
  truncated?: boolean;
}

export interface UnitLogs {
  unit: string;
  requested: number;
  entries: LogEntry[];
  truncated: boolean;
  skipped?: number;
  source: string;
}

export function getUnitLogs(unit: string, lines: number): Promise<UnitLogs> {
  return request<UnitLogs>(
    `/api/v1/services/units/${encodeURIComponent(unit)}/logs?lines=${encodeURIComponent(String(lines))}`,
  );
}

export interface HwMeta {
  status: 'ok' | 'partial' | 'unavailable';
  sources: string[];
  notes?: string[];
}

export interface Identifier {
  kind: 'wwn' | 'serial' | 'by-id';
  value: string;
  source: string;
}

export interface Disk {
  kernel_name: string;
  kernel_name_note: string;
  size_bytes: number;
  model?: string;
  vendor?: string;
  transport: string;
  rotational?: boolean;
  removable: boolean;
  read_only: boolean;
  virtual: boolean;
  identifiers: Identifier[];
  identity_confidence: 'high' | 'medium' | 'low' | 'none';
  identity?: string;
}

export interface Hardware {
  inventory: {
    system: HwMeta & {
      vendor?: string;
      product?: string;
      version?: string;
      board_vendor?: string;
      board_name?: string;
      bios_vendor?: string;
      bios_version?: string;
      bios_date?: string;
      chassis_type?: string;
      hypervisor?: string;
    };
    pci: HwMeta & {
      devices: {
        address: string;
        class_code: string;
        class: string;
        vendor_id: string;
        device_id: string;
        vendor_name?: string;
        device_name?: string;
        driver?: string;
        iommu_group?: string;
      }[];
    };
    usb: HwMeta & {
      devices: {
        bus_path: string;
        vendor_id: string;
        product_id: string;
        manufacturer?: string;
        product?: string;
        speed_mbps?: string;
      }[];
    };
    disks: HwMeta & { devices: Disk[] };
    sensors: HwMeta & {
      readings: { chip: string; label: string; kind: 'temperature' | 'fan'; value: number; unit: string }[];
    };
  };
  identity: {
    status: 'ok' | 'unavailable';
    reason?: string;
    data?: {
      available: boolean;
      reason?: string;
      source: string;
      fields: Record<
        string,
        { value?: string; status: 'present' | 'placeholder' | 'missing' | 'unreadable' }
      >;
    };
  };
}

export interface WebminCheck {
  id: string;
  level: 'ok' | 'warn' | 'info';
  message: string;
}

export interface Legacy {
  source: 'broker' | 'unprivileged';
  reason?: string;
  status?: {
    installed: boolean;
    config_dir: string;
    version?: string;
    port?: number;
    tls?: boolean;
    bind?: string;
    listens_on_all_interfaces: boolean;
    running?: boolean;
    user_count: number;
    rpc_capable_users: string[];
    checks: WebminCheck[];
    notes?: string[];
  };
  fallback?: Overview['legacy_webmin'];
}

export function getCapabilities(): Promise<Capabilities> {
  return request<Capabilities>('/api/v1/capabilities');
}

export function getServices(): Promise<UnitList> {
  return request<UnitList>('/api/v1/services');
}

export function getHardware(): Promise<Hardware> {
  return request<Hardware>('/api/v1/hardware');
}

export function getLegacy(): Promise<Legacy> {
  return request<Legacy>('/api/v1/legacy/webmin');
}
