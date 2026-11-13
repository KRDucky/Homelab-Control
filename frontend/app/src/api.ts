// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import type { StorageInventory } from './storageModel';
import type { ContainerRuntime } from './plugin';

// Typed client for the Homelab Control v1 API (schemas/openapi/homelab-control.v1.yaml).

export interface SessionInfo {
  username: string;
  scopes: string[] | null;
  csrf_token: string;
  version: string;
  legacy_url?: string;
  /** When this session's password re-entry (D-3) lapses. */
  step_up_until?: string;
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

/** Whether single sign-on is configured. Start is a plain navigation to
 * OIDC_START_PATH (a fetch or form POST cannot follow the provider redirect). */
export const OIDC_START_PATH = '/api/v1/auth/oidc/start';

export function getOIDCStatus(): Promise<{ enabled: boolean }> {
  return request<{ enabled: boolean }>('/api/v1/auth/oidc');
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
  id:
    | 'overview'
    | 'services'
    | 'service_logs'
    | 'unit_files'
    | 'hardware'
    | 'legacy'
    | 'audit'
    | 'storage'
    | 'host_settings'
    | 'files'
    | 'packages'
    | 'mergerfs'
    | 'traefik';
  label: string;
  path: string;
  scope: string;
  status: CapabilityStatus;
  reason?: string;
}

/** A compiled-in module (M2-3 registry). Disabled modules have no routes or navigation. */
export interface ModuleStatus {
  id: string;
  title: string;
  description: string;
  path: string;
  enabled: boolean;
  scopes: string[];
  reason?: string;
}

export interface Capabilities {
  compose_discovery?: { status: CapabilityStatus; reason: string };
  broker: { status: 'available' | 'unavailable'; version?: string; reason?: string };
  capabilities: Capability[];
  modules?: ModuleStatus[];
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

export interface Protection {
  protected: boolean;
  category?: string;
  reason?: string;
  matched?: string;
}

export interface UnitList {
  systemd_version?: string;
  units: Unit[];
  counts: Record<string, number>;
  truncated: boolean;
  source: string;
  /** .service units the built-in policy protects (labels only; the broker enforces it). */
  protected?: Record<string, Protection>;
  /** Whether start/stop/restart can be offered to this account. */
  operate?: { available: boolean; reason?: string };
  /** Whether boot-time enable/disable can be offered (services.configure, M2-3). */
  configure?: { available: boolean; reason?: string };
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

// ---- M2 PR-1a: sessions and audit -----------------------------------------

export interface SessionEntry {
  id: string;
  created: string;
  last_seen: string;
  remote_ip?: string;
  user_agent?: string;
  current: boolean;
}

export interface SessionList {
  persistent: boolean;
  idle_timeout_seconds: number;
  max_lifetime_seconds: number;
  sessions: SessionEntry[];
}

export function getSessions(): Promise<SessionList> {
  return request<SessionList>('/api/v1/auth/sessions');
}

export function revokeSession(id: string, csrf: string): Promise<undefined> {
  return request<undefined>(`/api/v1/auth/sessions/${encodeURIComponent(id)}/revoke`, {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrf },
  });
}

export interface AuditEvent {
  id: number;
  seq: number;
  time: string;
  event: string;
  user?: string;
  remote?: string;
  outcome: string;
  detail?: string;
  prev: string;
  hash: string;
}

export interface AuditPage {
  events: AuditEvent[];
  next_before?: number;
  source: string;
}

export function getAudit(opts: { before?: number; limit?: number; event?: string } = {}): Promise<AuditPage> {
  const q = new URLSearchParams();
  if (opts.limit) q.set('limit', String(opts.limit));
  if (opts.before) q.set('before', String(opts.before));
  if (opts.event) q.set('event', opts.event);
  const qs = q.toString();
  return request<AuditPage>(`/api/v1/audit${qs ? `?${qs}` : ''}`);
}

// ---- M2-2: service start/stop/restart (plan → confirm → execute) -------------

export type ServiceAction = 'start' | 'stop' | 'restart';
export type RiskClass = 'reversible' | 'disruptive' | 'destructive';

export interface UnitObserved {
  id: string;
  names?: string[];
  description?: string;
  load_state: string;
  active_state: string;
  sub_state: string;
  result?: string;
  unit_file_state?: string;
  main_pid?: number;
  exec_main_status?: number;
  invocation_id?: string;
  type?: string;
  state_since?: string;
}

export interface ServicePlan {
  unit: string;
  requested: string;
  action: ServiceAction;
  risk: RiskClass;
  step_up_required: boolean;
  token: string;
  expires_at: string;
  observed: UnitObserved;
  effect: string;
  consequences: string[];
  also_affected?: string[];
  /** Other, currently inactive units a start pulls in. */
  also_started?: string[];
}

export interface ServiceOutcome {
  operation_id: string;
  unit: string;
  action: ServiceAction;
  outcome: 'succeeded' | 'failed' | 'uncertain';
  message: string;
  exit_code?: number;
  observed?: UnitObserved;
  recovery?: string[];
  started_at: string;
  finished_at: string;
}

/** Same grammar as the server (servicepolicy.ValidUnit): only these units get controls. */
const operableUnitRE = /^[A-Za-z0-9][A-Za-z0-9:_.@-]{0,99}\.service$/;
export function isOperableName(name: string): boolean {
  return operableUnitRE.test(name) && !name.includes('@.service') && !name.includes('..');
}

function postJSON<T>(path: string, csrf: string, body: unknown): Promise<T> {
  return request<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(body),
  });
}

export function planServiceAction(unit: string, action: ServiceAction, csrf: string): Promise<ServicePlan> {
  return postJSON<ServicePlan>(`/api/v1/services/units/${encodeURIComponent(unit)}/plan`, csrf, { action });
}

export function applyServiceAction(
  unit: string,
  action: ServiceAction,
  token: string,
  csrf: string,
): Promise<ServiceOutcome> {
  return postJSON<ServiceOutcome>(`/api/v1/services/units/${encodeURIComponent(unit)}/apply`, csrf, {
    action,
    token,
  });
}

export function stepUp(password: string, csrf: string): Promise<{ step_up_until: string }> {
  return postJSON<{ step_up_until: string }>('/api/v1/auth/step-up', csrf, { password });
}

export interface OperationRecord {
  id: string;
  op: string;
  action: string;
  unit: string;
  actor: string;
  request_id?: string;
  risk: string;
  outcome: 'succeeded' | 'failed' | 'uncertain' | 'interrupted' | 'pending';
  message?: string;
  started_at: string;
  finished_at?: string;
  observed?: UnitObserved;
}

export function getOperations(): Promise<{ operations: OperationRecord[]; source: string }> {
  return request<{ operations: OperationRecord[]; source: string }>('/api/v1/operations');
}

// ---- M2-3: read-only unit views and boot-time enable/disable ----------------

export interface UnitExit {
  code: 'exited' | 'killed' | 'dumped core' | 'unknown';
  status: number;
  exited_at: string;
  started_at?: string;
}

export interface UnitDetail {
  requested: string;
  id?: string;
  kind: 'unit' | 'alias' | 'template';
  in_runtime: boolean;
  loaded_for_query?: boolean;
  names?: string[];
  description?: string;
  load_state?: string;
  active_state?: string;
  sub_state?: string;
  result?: string;
  unit_file_state?: string;
  boot_state?: string;
  preset?: string;
  fragment_path?: string;
  source_path?: string;
  drop_in_paths?: string[];
  generated?: boolean;
  transient?: boolean;
  need_daemon_reload?: boolean;
  service_type?: string;
  restart?: string;
  restarts?: number;
  main_pid?: number;
  last_exit?: UnitExit;
  active_since?: string;
  inactive_since?: string;
  state_since?: string;
  dependencies: Record<string, string[]>;
  dependencies_truncated?: boolean;
  notes?: string[];
  source: string;
  protected?: Protection;
}

export interface UnitContents {
  unit: string;
  files: { path: string; text: string; unreadable?: boolean }[];
  unreadable?: string[];
  redactions: number;
  truncated: boolean;
  notes?: string[];
  source: string;
}

export interface TimerInfo {
  unit: string;
  activates?: string;
  description?: string;
  active_state: string;
  sub_state?: string;
  unit_file_state?: string;
  next?: string;
  last?: string;
  schedule?: string[];
  persistent?: boolean;
}

export interface SocketInfo {
  unit: string;
  description?: string;
  listen: string[];
  activates?: string[];
  active_state: string;
  sub_state?: string;
  unit_file_state?: string;
}

const unitPath = (unit: string) => `/api/v1/services/units/${encodeURIComponent(unit)}`;

export function getUnitDetail(unit: string): Promise<UnitDetail> {
  return request<UnitDetail>(unitPath(unit));
}

export function getUnitContents(unit: string): Promise<UnitContents> {
  return request<UnitContents>(`${unitPath(unit)}/contents`);
}

export function getTimers(): Promise<{ timers: TimerInfo[]; truncated: boolean; source: string }> {
  return request<{ timers: TimerInfo[]; truncated: boolean; source: string }>('/api/v1/services/timers');
}

export function getSockets(): Promise<{ sockets: SocketInfo[]; truncated: boolean; source: string }> {
  return request<{ sockets: SocketInfo[]; truncated: boolean; source: string }>('/api/v1/services/sockets');
}

export type EnablementAction = 'enable' | 'disable';

export interface LinkObservation {
  path: string;
  present: boolean;
  /** Where a present link points, as stored in the link. */
  target?: string;
  /** Whether that is the unit file the review found. */
  points_to_unit?: boolean;
}

export interface EnablementPlan {
  unit: string;
  requested: string;
  action: EnablementAction;
  risk: RiskClass;
  step_up_required: boolean;
  token: string;
  expires_at: string;
  observed: UnitObserved;
  /** `systemctl is-enabled` (from disk) at review time. */
  boot_state: string;
  preset?: string;
  effect: string;
  consequences: string[];
  links: string[];
  wanted_by?: string[];
  aliases?: string[];
  also_affected?: string[];
  also_started?: string[];
  notes?: string[];
}

export interface EnablementOutcome {
  operation_id: string;
  unit: string;
  action: EnablementAction;
  outcome: 'succeeded' | 'failed' | 'uncertain';
  message: string;
  exit_code?: number;
  boot_state_before: string;
  boot_state?: string;
  links: LinkObservation[];
  /** Other links to the unit found afterwards that the plan did not describe. */
  unexpected_links?: string[];
  reported?: string[];
  observed?: UnitObserved;
  recovery?: string[];
  started_at: string;
  finished_at: string;
}

/** Same grammar as the server (servicepolicy.ValidEnablementUnit). */
export function isEnablementName(name: string): boolean {
  return isOperableName(name) && !name.includes('@');
}

export function planEnablement(
  unit: string,
  action: EnablementAction,
  csrf: string,
): Promise<EnablementPlan> {
  return postJSON<EnablementPlan>(`${unitPath(unit)}/enablement/plan`, csrf, { action });
}

export function applyEnablement(
  unit: string,
  action: EnablementAction,
  token: string,
  csrf: string,
): Promise<EnablementOutcome> {
  return postJSON<EnablementOutcome>(`${unitPath(unit)}/enablement/apply`, csrf, { action, token });
}

// The session CSRF token is required on this GET: a read is audited and may
// save a snapshot, so a same-site page (another port on this host) must not be
// able to trigger it with a cookie-bearing image or no-cors request.
export function getStorage(csrf: string): Promise<StorageInventory> {
  return request<StorageInventory>('/api/v1/storage', { headers: { 'X-CSRF-Token': csrf } });
}

// M2-6 skeleton: read-only host settings and a non-executing preview.
export interface HostSetting {
  value?: string;
  status: 'observed' | 'not-set' | 'unavailable';
  source: string;
  note?: string;
}

export interface HostSettings {
  kernel_hostname: HostSetting;
  static_hostname: HostSetting;
  pretty_hostname: HostSetting;
  timezone: HostSetting;
}

export interface HostSettingsResponse {
  collected_at: string;
  read_only: true;
  settings: HostSettings;
}

export interface HostSettingsPreviewRequest {
  static_hostname?: string;
  pretty_hostname?: string;
  timezone?: string;
}

export interface HostSettingChange {
  field: 'static_hostname' | 'pretty_hostname' | 'timezone';
  current: string;
  requested: string;
  outcome: 'unchanged' | 'would-change' | 'invalid' | 'cannot-compare';
  reason?: string;
}

export interface HostSettingsPreview {
  preview_only: true;
  executed: false;
  host_changed: false;
  apply_available: boolean;
  notice: string;
  valid: boolean;
  collected_at: string;
  current: HostSettings;
  changes: HostSettingChange[];
  warnings: string[];
}

export interface HostSettingsObserved {
  static_hostname?: string;
  static_status: HostSetting['status'];
  pretty_hostname?: string;
  pretty_status: HostSetting['status'];
  timezone?: string;
  timezone_status: HostSetting['status'];
  transient_hostname?: string;
  transient_status?: HostSetting['status'];
  notes?: string[];
}

export interface HostSettingsPlan {
  action: 'set';
  risk: 'reversible';
  step_up_required: boolean;
  confirmation_required: boolean;
  token: string;
  expires_at: string;
  observed: HostSettingsObserved;
  effect: string;
  consequences: string[];
  warnings: string[];
  changes: HostSettingChange[];
  fields: HostSettingChange['field'][];
}

export interface HostSettingsOutcome {
  operation_id: string;
  action: 'set';
  outcome: 'succeeded' | 'failed' | 'uncertain';
  message: string;
  fields: HostSettingChange['field'][];
  observed?: HostSettingsObserved;
  recovery?: string[];
  started_at: string;
  finished_at: string;
}

export function getHostSettings(): Promise<HostSettingsResponse> {
  return request<HostSettingsResponse>('/api/v1/host/settings');
}

/** Compares requested values with the host's. The server never applies them. */
export function previewHostSettings(
  req: HostSettingsPreviewRequest,
  csrf: string,
): Promise<HostSettingsPreview> {
  return postJSON<HostSettingsPreview>('/api/v1/host/settings/preview', csrf, req);
}

/** Review a hostname/timezone change. Requires host.settings.apply; preview never authorizes this. */
export function planHostSettings(req: HostSettingsPreviewRequest, csrf: string): Promise<HostSettingsPlan> {
  return postJSON<HostSettingsPlan>('/api/v1/host/settings/plan', csrf, req);
}

/** Execute a reviewed hostname/timezone change. Requires host.settings.apply and the plan token. */
export function applyHostSettings(
  req: HostSettingsPreviewRequest,
  token: string,
  csrf: string,
): Promise<HostSettingsOutcome> {
  return postJSON<HostSettingsOutcome>('/api/v1/host/settings/apply', csrf, { ...req, token });
}

// File manager (read-only checkpoint): confined-root listing. files.read is never
// default. Identifier-bearing GETs need the session CSRF token (same bar as layout).
// Host paths are never returned. There is no content download and no write.

export interface FileRoot {
  id: string;
  label: string;
}

export interface FileEntry {
  name: string;
  dir: boolean;
  size: number;
  owner: string;
  mode: string;
  mtime: string;
}

export interface FileRootsResponse {
  roots: FileRoot[];
  browse_available: boolean;
}

export interface FileEntriesResponse {
  root: string;
  path: string;
  entries: FileEntry[];
}

export function getFileRoots(csrf: string): Promise<FileRootsResponse> {
  return request<FileRootsResponse>('/api/v1/files/roots', { headers: { 'X-CSRF-Token': csrf } });
}

export function getFileEntries(root: string, path: string, csrf: string): Promise<FileEntriesResponse> {
  const q = new URLSearchParams({ root, path });
  return request<FileEntriesResponse>(`/api/v1/files/entries?${q}`, { headers: { 'X-CSRF-Token': csrf } });
}

// M2-5 Stage A: application-owned manual drive-bay layout. Every assignment is
// the administrator's statement (manual, unverified); nothing touches the host.
export type ChassisKind = 'server' | 'shelf' | 'external' | 'desktop';
export type CellKind =
  'hdd-3.5' | 'ssd-2.5' | 'nvme-m2' | 'nvme-u2' | 'tray' | 'empty' | 'disabled' | 'reserved';
export interface LayoutAssignment {
  identity: string;
  provenance: 'manual';
  confidence: 'unverified';
  assigned_by: string;
  assigned_at: string;
}
export interface LayoutCell {
  row: number;
  col: number;
  kind: CellKind;
  label: string;
  assignment?: LayoutAssignment;
}
export interface Chassis {
  id: number;
  name: string;
  kind: ChassisKind;
  rows: number;
  cols: number;
  created_at: string;
  updated_at: string;
  cells: LayoutCell[];
}
export interface ChassisSpec {
  name: string;
  kind: ChassisKind;
  rows: number;
  cols: number;
  cells?: { row: number; col: number; kind: CellKind; label: string }[];
}
export interface DriveLayout {
  chassis: Chassis[];
}

function sendJSON<T>(
  method: 'POST' | 'PUT' | 'DELETE',
  path: string,
  csrf: string,
  body?: unknown,
): Promise<T> {
  if (body === undefined) return request<T>(path, { method, headers: { 'X-CSRF-Token': csrf } });
  return request<T>(path, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(body),
  });
}
const layoutBase = '/api/v1/storage/layout';
const bayPath = (id: number, row: number, col: number) =>
  `${layoutBase}/chassis/${id}/bays/${row}/${col}/assignment`;

// A malformed answer is an error for this panel only; the Storage page stays usable.
async function checkedLayout(p: Promise<DriveLayout>): Promise<DriveLayout> {
  const layout = await p;
  if (!layout || !Array.isArray(layout.chassis)) throw new Error('invalid drive-bay layout response');
  return layout;
}
// Like getStorage: the layout holds stored serial/WWN identities, so each read
// is audited and requires the session CSRF token.
export function getLayout(csrf: string): Promise<DriveLayout> {
  return checkedLayout(request<DriveLayout>(layoutBase, { headers: { 'X-CSRF-Token': csrf } }));
}
export function createChassis(spec: ChassisSpec, csrf: string): Promise<DriveLayout> {
  return checkedLayout(sendJSON<DriveLayout>('POST', `${layoutBase}/chassis`, csrf, spec));
}
export function updateChassis(id: number, spec: ChassisSpec, csrf: string): Promise<DriveLayout> {
  return checkedLayout(sendJSON<DriveLayout>('PUT', `${layoutBase}/chassis/${id}`, csrf, spec));
}
export function deleteChassis(id: number, csrf: string): Promise<DriveLayout> {
  return checkedLayout(sendJSON<DriveLayout>('DELETE', `${layoutBase}/chassis/${id}`, csrf));
}
/** The drive is named only by its persistent identity, never a kernel name. */
export function assignBay(id: number, row: number, col: number, persistentID: string, csrf: string) {
  return checkedLayout(
    sendJSON<DriveLayout>('PUT', bayPath(id, row, col), csrf, { persistent_id: persistentID }),
  );
}
export function unassignBay(id: number, row: number, col: number, csrf: string) {
  return checkedLayout(sendJSON<DriveLayout>('DELETE', bayPath(id, row, col), csrf));
}

// ---- M4-1b: read-only Software Center --------------------------------------

export interface SoftwarePackage {
  name: string;
  version?: string;
  arch?: string;
  origin?: string;
  installed: boolean;
  upgradable?: boolean;
  summary?: string;
}

export interface SoftwareUpdate {
  name: string;
  arch?: string;
  origin?: string;
  current: string;
  new: string;
}

export interface SoftwareSource {
  name: string;
  origin: string;
  enabled: boolean;
  signed: boolean;
  third_party: boolean;
}

export interface PackageList {
  packages: SoftwarePackage[];
  truncated: boolean;
  source: string;
}

export interface UpdateList {
  updates: SoftwareUpdate[];
  truncated: boolean;
  source: string;
  as_of?: string;
}

export interface SourceList {
  sources: SoftwareSource[];
  truncated: boolean;
  source: string;
}

// Every packages GET is identifier-bearing (the search query is the caller's;
// installed/updates/sources are sensitive host data), so each is audited and
// requires the session CSRF token, the same bar as storage/files/layout.
export function getPackageSearch(q: string, csrf: string): Promise<PackageList> {
  return request<PackageList>(`/api/v1/packages/search?q=${encodeURIComponent(q)}`, {
    headers: { 'X-CSRF-Token': csrf },
  });
}

export function getPackagesInstalled(csrf: string): Promise<PackageList> {
  return request<PackageList>('/api/v1/packages/installed', { headers: { 'X-CSRF-Token': csrf } });
}

export function getPackagesUpdates(csrf: string): Promise<UpdateList> {
  return request<UpdateList>('/api/v1/packages/updates', { headers: { 'X-CSRF-Token': csrf } });
}

export function getPackagesSources(csrf: string): Promise<SourceList> {
  return request<SourceList>('/api/v1/packages/sources', { headers: { 'X-CSRF-Token': csrf } });
}

// ---- M8 first checkpoint: MergerFS + optional SnapRAID (read-only) ---------

export interface MergerFSToolItem {
  id: string;
  package: string;
  installed: boolean;
  binary: string;
}

export interface MergerFSTooling {
  family: string;
  required: MergerFSToolItem[];
  optional: MergerFSToolItem[];
}

export interface MergerFSPool {
  mountpoint: string;
  branches: string[];
  options: string[];
}

export interface MergerFSInfo {
  present: boolean;
  reason: string;
  pools: MergerFSPool[];
}

export interface SnapRAIDInfo {
  present: boolean;
  reason: string;
  configPath: string;
  data: string[];
  parity: string[];
  content: string[];
}

export interface MergerFSKernelFuse {
  present: boolean;
  reason: string;
}

export interface MergerFSInventory {
  tooling: MergerFSTooling;
  mergerfs: MergerFSInfo;
  snapraid: SnapRAIDInfo;
  kernelFuse: MergerFSKernelFuse;
}

// Like storage/files/packages: this GET returns host mount and configuration
// paths, so it is access-audited and requires the session CSRF token.
export function getMergerFS(csrf: string): Promise<MergerFSInventory> {
  return request<MergerFSInventory>('/api/v1/mergerfs', { headers: { 'X-CSRF-Token': csrf } });
}

// Read-only Traefik helper integration. The browser never receives helper credentials.
export interface TraefikResource {
  rule?: string;
  service?: string;
  entrypoints?: string[];
  tls?: boolean;
  name: string;
  provider: string;
  status: string;
}
export interface TraefikInventory {
  schemaVersion: number;
  routers: TraefikResource[];
  services: TraefikResource[];
  middlewares: TraefikResource[];
}
export interface TraefikDiagnostics {
  schemaVersion: number;
  status: string;
  checks: { resource: string; status: string; count?: number; code?: string; hint?: string }[];
}
export interface TraefikModel {
  name: string;
  host: string;
  port: number;
  entrypoint: string;
  protocol: 'http' | 'https';
  upstreamScheme: 'http' | 'https';
  tls?: { certificateResolver?: string };
  path?: string;
}
const traefikPath = '/api/v1/networking/reverse-proxy/traefik';
export const getTraefikInventory = (csrf: string) =>
  request<TraefikInventory>(`${traefikPath}/inventory`, { headers: { 'X-CSRF-Token': csrf } });
export const getTraefikDiagnostics = (csrf: string) =>
  request<TraefikDiagnostics>(`${traefikPath}/diagnostics`, { headers: { 'X-CSRF-Token': csrf } });
export const previewTraefikLabels = (model: TraefikModel, csrf: string) =>
  postJSON<{ labels: Record<string, string> }>(`${traefikPath}/labels`, csrf, model);

export interface ComposeService {
  replicaCount?: number;
  unsupportedNetworking?: boolean;
  unsupportedDeployment?: boolean;
  officialTraefik?: boolean;
  id: string;
  compose: boolean;
  project?: string;
  service?: string;
  container: string;
  state: string;
  ports: { port: number; protocol: string }[];
  networks: string[];
  traefikLabels: Record<string, string>;
  hiddenTraefikSettings: boolean;
}
export interface TraefikInstance {
  present: boolean;
  running: boolean;
  container?: string;
  project?: string;
  service?: string;
}
export interface ComposeCatalog {
  source: string;
  fixture: boolean;
  services: ComposeService[];
  traefik: {
    networks: string[];
    entrypoints: string[];
    resolvers: string[];
    dockerProvider: boolean | null;
    instance?: TraefikInstance;
  };
  setupAvailable?: boolean;
}
export interface ProxySummary {
  application: string;
  targetService: string;
  container: string;
  hostname: string;
  path?: string;
  internalPort: number;
  protocol: string;
  upstreamScheme: string;
  certificateResolver: string;
  network: string;
  entrypoint: string;
}
export interface ComposeProposal {
  summary?: ProxySummary;
  schemaVersion: number;
  source: string;
  fixture: boolean;
  project: string;
  service: string;
  existing: { labels: Record<string, string>; networks: string[] };
  proposed: { labels: Record<string, string>; networks: string[] };
  file?: Record<string, unknown>;
  fileYAML?: string;
  changes: { path: string[]; before: string | null; after: string }[];
  warnings: { code: string; message: string }[];
  blockers: { code: string; message: string }[];
  applyAvailable: boolean;
  labelApplyAvailable: boolean;
  sourceFileVerified: boolean;
  preservedFields: string[];
}
export const getComposeApplications = (csrf: string) =>
  request<ComposeCatalog>(`${traefikPath}/applications`, { headers: { 'X-CSRF-Token': csrf } });
export const previewCompose = (
  body: { serviceId: string; network: string; model: TraefikModel; confirmUnverifiedPort?: boolean },
  csrf: string,
) => postJSON<ComposeProposal>(`${traefikPath}/compose-preview`, csrf, body);
export const applyComposeFile = (
  body: { serviceId: string; network: string; model: TraefikModel; confirmUnverifiedPort?: boolean },
  csrf: string,
) => postJSON<{ wrote: string; applyAvailable: boolean }>(`${traefikPath}/file-apply`, csrf, body);
export const applyComposeLabels = (
  body: { serviceId: string; network: string; model: TraefikModel; confirmUnverifiedPort?: boolean },
  csrf: string,
) =>
  postJSON<{ wrote: string; added: number; labelApplyAvailable: boolean; recreated: boolean }>(
    `${traefikPath}/compose-apply`,
    csrf,
    body,
  );
export const applyTraefikSetup = (csrf: string) =>
  postJSON<{ wrote: string[]; started: boolean; httpPort: string }>(`${traefikPath}/setup`, csrf, {});

// Container platform registry (CORE). Connection details never reach the browser.
export type PlatformCapability =
  | 'workloads.read'
  | 'services.read'
  | 'ports.read'
  | 'networks.read'
  | 'namespaces.read'
  | 'health.read'
  | 'routing.preview.compose-labels'
  | 'routing.preview.kubernetes-ingress'
  | 'routing.preview.traefik-ingressroute'
  | 'routing.preview.gateway-httproute'
  | 'routing.apply';
export interface PlatformView {
  id: string;
  displayName: string;
  kind: 'docker' | 'podman' | 'kubernetes';
  distribution: string;
  mode: 'rootless' | 'rootful' | 'cluster';
  authentication: string;
  endpoint: { transport: string; label: string; fingerprint: string };
  fixture: boolean;
  capabilities: {
    declared: PlatformCapability[];
    detected: PlatformCapability[];
    unsupported: { capability: PlatformCapability; reason: string }[];
  };
  health: { status: string; checkedAt?: string; reason: string };
  policy: { readScope: string; previewScope: string; apply: string };
}
export interface PlatformRef {
  platform: string;
  kind: string;
  namespace?: string;
  name: string;
  uid: string;
}
export interface PlatformPort {
  port: number;
  protocol: string;
  name?: string;
  target?: string;
}
export interface PlatformWorkload {
  ref: PlatformRef;
  group?: string;
  component?: string;
  state: string;
  replicas: number;
  readyReplicas: number;
  ports: PlatformPort[];
  networks: string[];
  services: PlatformRef[];
  routingLabels: Record<string, string>;
  hiddenRouting: boolean;
  issues: string[];
}
export interface PlatformService {
  ref: PlatformRef;
  type: string;
  ports: PlatformPort[];
  selects: PlatformRef[];
}
export interface PlatformInventory {
  platform: string;
  fixture: boolean;
  distribution: string;
  version?: string;
  workloads: PlatformWorkload[];
  services: PlatformService[];
  networks: { ref: PlatformRef; traefikAttached: boolean }[];
  namespaces: string[];
  routing: { kind: string; available: boolean | null; classes: string[]; detail: string }[];
  entrypoints: string[];
  resolvers: string[];
  capabilities: PlatformView['capabilities'];
}
export interface PlatformPreview {
  schemaVersion: number;
  platform: PlatformView;
  target: PlatformCapability;
  format: 'compose-labels' | 'kubernetes-manifests';
  workload: PlatformRef;
  service?: PlatformRef;
  compose?: ComposeProposal;
  manifests?: unknown[];
  warnings: { code: string; message: string }[];
  blockers: { code: string; message: string }[];
  applyAvailable: boolean;
  deployable: boolean;
  validation: { check: string; status: string }[];
}
/** Stable selection key, identical to the server's Ref.Key(). */
export const platformRefKey = (r: PlatformRef) =>
  `${r.platform}/${r.kind}/${r.namespace || '-'}/${r.name}/${r.uid}`;
export const getPlatforms = (csrf: string) =>
  request<{ platforms: PlatformView[]; autoSelected: string | null }>('/api/v1/platforms', {
    headers: { 'X-CSRF-Token': csrf },
  });
export const getPlatformInventory = (id: string, csrf: string) =>
  request<{ platform: PlatformView; inventory: PlatformInventory }>(
    `/api/v1/platforms/${encodeURIComponent(id)}/inventory`,
    { headers: { 'X-CSRF-Token': csrf } },
  );
export const previewPlatform = (
  body: {
    platform: string;
    target: PlatformCapability;
    workload: string;
    service?: string;
    network?: string;
    routeClass?: string;
    model: TraefikModel;
  },
  csrf: string,
) => postJSON<PlatformPreview>(`${traefikPath}/platform-preview`, csrf, body);

// --- Container identity & permissions (CORE; read-only previews) ---

export type IdentityMode = 'image' | 'inherit' | 'custom';
export type IdentityLayer = 'unset' | 'image' | 'platform' | 'application' | 'profile' | 'override';

export interface IdentitySettings {
  uid?: number;
  gid?: number;
  umask?: string;
  supplementalGroups?: number[];
}

export interface IdentityProfile {
  id: string;
  name: string;
  description: string;
  umask?: string;
  groupRoles: string[];
  readOnlyVolumes: boolean;
}

export interface IdentityIssue {
  severity: 'blocker' | 'warning' | 'info';
  code: string;
  field?: string;
  message: string;
}

export interface IdentityPreview {
  schemaVersion: 1;
  mode: IdentityMode;
  platform: { id: string; kind: string; mode: string; mappingKnown: boolean };
  resolved: {
    uid: { value: number | null; source: IdentityLayer; host?: number };
    gid: { value: number | null; source: IdentityLayer; host?: number };
    umask: {
      value: string | null;
      source: IdentityLayer;
      effect?: { files: string; directories: string; explanation: string };
    };
    supplementalGroups: { value: number[]; source: IdentityLayer; host?: number[]; names?: string[] };
  };
  mechanism: { identity: string; umask: string; supplementalGroups: boolean; declared: boolean };
  compose?: { environment?: Record<string, string>; user?: string; group_add?: string[] };
  kubernetes?: {
    podSecurityContext?: { supplementalGroups?: number[] };
    containerSecurityContext?: {
      runAsUser?: number;
      runAsGroup?: number;
      runAsNonRoot?: boolean;
      allowPrivilegeEscalation?: boolean;
    };
    env?: Array<{ name: string; value: string }>;
  };
  volumes: Array<{
    name: string;
    target: string;
    access: 'ro' | 'rw';
    status: 'compatible' | 'incompatible' | 'unverified';
    reason: string;
    proposals?: string[];
  }>;
  issues: IdentityIssue[];
  verification: 'not-reported' | 'observed-match' | 'observed-mismatch';
  applied: false;
  deployable: false;
}

export const getIdentityProfiles = (csrf: string) =>
  request<{
    schemaVersion: 1;
    profiles: IdentityProfile[];
    privilegedGroupNames: string[];
    modes: IdentityMode[];
  }>('/api/v1/containers/identity/profiles', { headers: { 'X-CSRF-Token': csrf } });

export const previewIdentity = (
  body: {
    platform: string;
    mode: IdentityMode;
    runtime: ContainerRuntime;
    application?: IdentitySettings;
    profile?: string;
    override?: IdentitySettings;
    acknowledgePrivilegedGroups?: number[];
    workload?: string;
  },
  csrf: string,
) =>
  postJSON<{ platform: PlatformView; preview: IdentityPreview; runtime: ContainerRuntime }>(
    '/api/v1/containers/identity/preview',
    csrf,
    body,
  );
