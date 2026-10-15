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
  filesystems: Meta & { items: Filesystem[] };
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
