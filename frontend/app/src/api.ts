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
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  /** Seconds the server asked us to wait (Retry-After), when valid. */
  readonly retryAfterSeconds?: number;
  constructor(status: number, code: string, message: string, retryAfterSeconds?: number) {
    super(message);
    this.status = status;
    this.code = code;
    if (retryAfterSeconds !== undefined) this.retryAfterSeconds = retryAfterSeconds;
  }
}

/** Maximum accepted Retry-After in seconds (one hour). Larger values are
 * treated as malformed and fall back to bounded backoff. */
const MAX_RETRY_AFTER_SECONDS = 3600;

/** Parse a Retry-After header (integer seconds). Returns undefined for a
 * missing, non-decimal, zero, negative, fractional, non-numeric, unsafe, or
 * oversized value so callers fall back to bounded backoff instead of retrying
 * immediately or overflowing a timer delay. Only plain digits are accepted
 * (no exponent, hex, sign, or fraction). */
export function parseRetryAfterSeconds(value: string | null): number | undefined {
  if (value === null) return undefined;
  const s = value.trim();
  if (!/^[0-9]+$/.test(s)) return undefined;
  const n = Number(s);
  if (!Number.isSafeInteger(n) || n <= 0 || n > MAX_RETRY_AFTER_SECONDS) return undefined;
  return n;
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
    throw new ApiError(
      res.status,
      b.error ?? 'http_error',
      b.message ?? `Request failed (${res.status})`,
      parseRetryAfterSeconds(res.headers.get('Retry-After')),
    );
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

// ---- M1: capabilities, services, hardware -----------------------------------

export type CapabilityStatus = 'available' | 'degraded' | 'unavailable' | 'unsupported' | 'forbidden';

export interface Capability {
  id:
    | 'overview'
    | 'services'
    | 'service_logs'
    | 'unit_files'
    | 'hardware'
    | 'audit'
    | 'storage'
    | 'host_settings'
    | 'files'
    | 'packages'
    | 'mergerfs'
    | 'zfs'
    | 'lvm'
    | 'btrfs'
    | 'ext4'
    | 'mdadm'
    | 'f2fs'
    | 'xfs'
    | 'ports'
    | 'bonds'
    | 'firewall'
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

export function getUnitLogs(unit: string, lines: number, csrf: string): Promise<UnitLogs> {
  return request<UnitLogs>(
    `/api/v1/services/units/${encodeURIComponent(unit)}/logs?lines=${encodeURIComponent(String(lines))}`,
    { headers: { 'X-CSRF-Token': csrf } },
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

export function getCapabilities(): Promise<Capabilities> {
  return request<Capabilities>('/api/v1/capabilities');
}

export function getServices(): Promise<UnitList> {
  return request<UnitList>('/api/v1/services');
}

export function getHardware(): Promise<Hardware> {
  return request<Hardware>('/api/v1/hardware');
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

export function getUnitContents(unit: string, csrf: string): Promise<UnitContents> {
  return request<UnitContents>(`${unitPath(unit)}/contents`, { headers: { 'X-CSRF-Token': csrf } });
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

/** On-demand read-only probe: enumerates and revalidates devices server-side
 * and returns the enriched inventory in the same shape as getStorage. This is
 * an explicit CSRF-protected POST (scope storage.probe), never a polling GET. */
export function probeStorage(csrf: string): Promise<StorageInventory> {
  return request<StorageInventory>('/api/v1/storage/probe', {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrf },
  });
}

export interface PartitionPlanStep {
  action: string;
  detail: string;
  destructive: boolean;
  executable: false;
}

export interface PartitionPlanReport {
  ok: boolean;
  errors: string[];
  steps: PartitionPlanStep[];
  /** Always false. The server does not partition or format from this call. */
  executes: false;
}

/** Validate a proposed layout. storage.read. Nothing is partitioned or formatted. */
export function dryRunPartitionPlan(
  csrf: string,
  body: { disk_bytes: number; parts: { start_bytes: number; size_bytes: number; filesystem: string }[] },
): Promise<PartitionPlanReport> {
  return postJSON<PartitionPlanReport>('/api/v1/storage/plan/dry-run', csrf, body);
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
  /** Number of directory names omitted because they are not faithfully
   * representable (invalid UTF-8) or not navigable (a backslash). */
  omitted: number;
}

export function getFileRoots(csrf: string): Promise<FileRootsResponse> {
  return request<FileRootsResponse>('/api/v1/files/roots', { headers: { 'X-CSRF-Token': csrf } });
}

export function getFileEntries(root: string, path: string, csrf: string): Promise<FileEntriesResponse> {
  const q = new URLSearchParams({ root, path });
  return request<FileEntriesResponse>(`/api/v1/files/entries?${q}`, { headers: { 'X-CSRF-Token': csrf } });
}

// File-manager read/write slice (2026-11-20). Read and download keep
// files.read; content write, upload, create and rename need files.write;
// delete needs files.destroy. All write verbs carry the session CSRF token.

export type FileEncoding = 'utf8' | 'binary';

export interface FileContent {
  bytes: Uint8Array;
  /** Full stat size, not the (possibly capped) read length. */
  size: number;
  truncated: boolean;
  encoding: FileEncoding;
}

export interface FileEntryResult {
  root: string;
  path: string;
  entry: FileEntry;
}

export interface FileDeletePlan {
  token: string;
  root: string;
  path: string;
  kind: string;
  size: number;
}

async function fileError(res: Response): Promise<ApiError> {
  let body: unknown = null;
  try {
    body = await res.json();
  } catch {
    // non-JSON error body
  }
  const b = (body ?? {}) as { error?: string; message?: string };
  return new ApiError(
    res.status,
    b.error ?? 'http_error',
    b.message ?? `Request failed (${res.status})`,
    parseRetryAfterSeconds(res.headers.get('Retry-After')),
  );
}

export async function getFileContent(root: string, path: string, csrf: string): Promise<FileContent> {
  const q = new URLSearchParams({ root, path });
  const res = await fetch(`/api/v1/files/content?${q}`, {
    credentials: 'same-origin',
    headers: { 'X-CSRF-Token': csrf },
  });
  if (!res.ok) throw await fileError(res);
  return {
    bytes: new Uint8Array(await res.arrayBuffer()),
    size: Number(res.headers.get('X-Content-Size') ?? 0),
    truncated: res.headers.get('X-Content-Truncated') === 'true',
    encoding: res.headers.get('X-File-Encoding') === 'binary' ? 'binary' : 'utf8',
  };
}

export async function downloadFile(root: string, path: string, name: string, csrf: string): Promise<void> {
  const q = new URLSearchParams({ root, path });
  const res = await fetch(`/api/v1/files/download?${q}`, {
    credentials: 'same-origin',
    headers: { 'X-CSRF-Token': csrf },
  });
  if (!res.ok) throw await fileError(res);
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

export function putFileContent(
  root: string,
  path: string,
  csrf: string,
  data: string | Uint8Array,
): Promise<FileEntryResult> {
  const q = new URLSearchParams({ root, path });
  return request<FileEntryResult>(`/api/v1/files/content?${q}`, {
    method: 'PUT',
    headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/octet-stream' },
    body: data as BodyInit,
  });
}

export function uploadFile(root: string, dir: string, csrf: string, file: File): Promise<FileEntryResult> {
  const q = new URLSearchParams({ root, path: dir });
  const form = new FormData();
  form.append('file', file, file.name);
  return request<FileEntryResult>(`/api/v1/files/upload?${q}`, {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrf },
    body: form,
  });
}

export function createFileEntry(
  root: string,
  dir: string,
  csrf: string,
  name: string,
  type: 'file' | 'dir',
): Promise<FileEntryResult> {
  const q = new URLSearchParams({ root, path: dir });
  return postJSON<FileEntryResult>(`/api/v1/files/entries?${q}`, csrf, { name, type });
}

export function renameFile(root: string, path: string, csrf: string, to: string): Promise<FileEntryResult> {
  const q = new URLSearchParams({ root, path });
  return request<FileEntryResult>(`/api/v1/files/entries?${q}`, {
    method: 'PUT',
    headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/json' },
    body: JSON.stringify({ to }),
  });
}

export function planFileDelete(root: string, path: string, csrf: string): Promise<FileDeletePlan> {
  const q = new URLSearchParams({ root, path });
  return request<FileDeletePlan>(`/api/v1/files/delete/plan?${q}`, {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrf },
  });
}

export function applyFileDelete(
  token: string,
  csrf: string,
): Promise<{ root: string; path: string; status: string }> {
  return postJSON<{ root: string; path: string; status: string }>('/api/v1/files/delete/apply', csrf, {
    token,
  });
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
  version: number;
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
export function updateChassis(
  id: number,
  spec: ChassisSpec,
  version: number,
  csrf: string,
): Promise<DriveLayout> {
  return checkedLayout(
    sendJSON<DriveLayout>('PUT', `${layoutBase}/chassis/${id}`, csrf, { ...spec, version }),
  );
}
export function deleteChassis(id: number, version: number, csrf: string): Promise<DriveLayout> {
  return checkedLayout(
    request<DriveLayout>(`${layoutBase}/chassis/${id}?version=${version}`, {
      method: 'DELETE',
      headers: { 'X-CSRF-Token': csrf },
    }),
  );
}
/** The drive is named only by its persistent identity, never a kernel name. */
export function assignBay(
  id: number,
  row: number,
  col: number,
  persistentID: string,
  version: number,
  csrf: string,
) {
  return checkedLayout(
    sendJSON<DriveLayout>('PUT', bayPath(id, row, col), csrf, { persistent_id: persistentID, version }),
  );
}
export function unassignBay(id: number, row: number, col: number, version: number, csrf: string) {
  return checkedLayout(
    request<DriveLayout>(`${bayPath(id, row, col)}?version=${version}`, {
      method: 'DELETE',
      headers: { 'X-CSRF-Token': csrf },
    }),
  );
}

/** Import a previously exported chassis array (geometry only). The server
 * discards ids, timestamps and assignment identities and adds additively. */
export function importLayout(chassis: unknown[], csrf: string): Promise<DriveLayout> {
  return checkedLayout(sendJSON<DriveLayout>('POST', `${layoutBase}/import`, csrf, { chassis }));
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

// M4 APT preview is informational only. No package write operation exists here.
export interface AptPreviewChange {
  action: 'install' | 'remove' | 'configure';
  name: string;
  installed_version?: string;
  candidate_version?: string;
}
export interface AptPreview {
  changes: AptPreviewChange[];
  install_count: number;
  remove_count: number;
  configure_count: number;
}

export function getAptPreview(
  action: 'install' | 'remove' | 'upgrade',
  name: string,
  csrf: string,
): Promise<AptPreview> {
  const query = new URLSearchParams({ action, package: name });
  return request<AptPreview>(`/api/v1/packages/preview?${query.toString()}`, {
    headers: { 'X-CSRF-Token': csrf },
  });
}

// Owner 2026-11-24: APT settings file and archive mirror changes (plan → confirm
// → apply through the opt-in hc-apt-apply helper). Nothing installs packages.
export interface AptTunables {
  install_recommends?: boolean;
  install_suggests?: boolean;
  skip_translations?: boolean;
  force_ipv4?: boolean;
  pdiffs?: boolean;
  keep_downloaded?: boolean;
  retries?: number;
  timeout_seconds?: number;
  queue_mode?: 'host' | 'access';
  http_proxy?: string;
}
export interface AptEffective {
  install_recommends: boolean;
  install_suggests: boolean;
  skip_translations: boolean;
  force_ipv4: boolean;
  pdiffs: boolean;
  keep_downloaded: boolean;
  retries: number;
  timeout_seconds: number;
  queue_mode: string;
  http_proxy_set: boolean;
}
export interface AptConfigView {
  effective?: AptEffective;
  effective_error?: string;
  managed: AptTunables;
  managed_present: boolean;
  managed_changed_by_hand: boolean;
  path: string;
}
export interface AptPlan {
  action: 'conf.write' | 'conf.remove' | 'mirror.set';
  risk: 'reversible';
  step_up_required: boolean;
  confirmation_required: boolean;
  token: string;
  expires_at: string;
  file: string;
  before?: string;
  after?: string;
  lines?: { line: number; before: string; after: string }[];
  effect: string;
  consequences: string[];
}
export interface AptOutcome {
  operation_id: string;
  action: string;
  outcome: 'succeeded' | 'failed' | 'uncertain';
  message: string;
  backup?: string;
  overridden_by_other_files?: string[];
}
export interface AptSourceEntry {
  file: string;
  format: 'one-line' | 'deb822';
  line: number;
  enabled: boolean;
  types: string[];
  uri: string;
  suites: string[];
  components: string[];
  signed_by?: string;
  role: 'archive' | 'security' | 'ports' | 'other';
  switchable: boolean;
  reason?: string;
}
export interface AptMirrors {
  distro: { id: string; codename: string };
  files: { path: string; format: string; digest: string; error?: string }[];
  entries: AptSourceEntry[];
  default_country: string;
}
export interface MirrorResult {
  uri: string;
  host: string;
  source: 'current' | 'official';
  status: 'ok' | 'stale' | 'mismatch' | 'expired' | 'unreachable';
  reason?: string;
  first_byte_ms?: number;
  total_ms?: number;
  kb_per_second?: number;
  index_date?: string;
  rank?: number;
  current?: boolean;
}
export interface MirrorProbe {
  country: string;
  codename: string;
  results: MirrorResult[];
  note?: string;
}
export type AptConfigChange = { reset: true } | { tunables: AptTunables };
export interface MirrorChange {
  file: string;
  from: string;
  to: string;
}

export function getAptConfig(csrf: string): Promise<AptConfigView> {
  return request<AptConfigView>('/api/v1/packages/apt-config', { headers: { 'X-CSRF-Token': csrf } });
}
export function planAptConfig(change: AptConfigChange, csrf: string): Promise<AptPlan> {
  return postJSON<AptPlan>('/api/v1/packages/apt-config/plan', csrf, change);
}
export function applyAptConfig(change: AptConfigChange, token: string, csrf: string): Promise<AptOutcome> {
  return postJSON<AptOutcome>('/api/v1/packages/apt-config/apply', csrf, { ...change, token });
}
export function getAptMirrors(csrf: string): Promise<AptMirrors> {
  return request<AptMirrors>('/api/v1/packages/mirrors', { headers: { 'X-CSRF-Token': csrf } });
}
export function probeAptMirrors(country: string, csrf: string): Promise<MirrorProbe> {
  return postJSON<MirrorProbe>('/api/v1/packages/mirrors/probe', csrf, { country });
}
export function planAptMirror(change: MirrorChange, csrf: string): Promise<AptPlan> {
  return postJSON<AptPlan>('/api/v1/packages/mirrors/plan', csrf, change);
}
export function applyAptMirror(change: MirrorChange, token: string, csrf: string): Promise<AptOutcome> {
  return postJSON<AptOutcome>('/api/v1/packages/mirrors/apply', csrf, { ...change, token });
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

export interface MergerFSBranchUsage {
  path: string;
  measured: boolean;
  totalBytes?: number;
  usedBytes?: number;
  availBytes?: number;
  reason?: string;
}

export interface MergerFSPool {
  mountpoint: string;
  branches: string[];
  /** Where the branches came from; fstab/systemd when the mount's fsname= hides them. */
  branchesFrom?: 'mountinfo' | 'fstab' | 'systemd';
  options: string[];
  usage?: MergerFSBranchUsage[];
  usageTruncated?: boolean;
}

export interface MergerFSConfiguredMount {
  origin: 'fstab' | 'systemd';
  path: string;
  unit?: string;
  mountpoint: string;
  branches: string[];
  options: string[];
  mounted: boolean;
  branchesTruncated?: boolean;
}

export interface MergerFSInfo {
  present: boolean;
  reason: string;
  pools: MergerFSPool[];
  configured: MergerFSConfiguredMount[];
  configuredTruncated?: boolean;
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

export interface MergerFSKernelRelease {
  present: boolean;
  release: string;
  reason: string;
}

export interface MergerFSWarning {
  code:
    | 'duplicate-branch'
    | 'parity-in-pool'
    | 'content-on-pool'
    | 'content-on-parity'
    | 'content-not-redundant'
    | 'fuse-allow-other';
  pool: string;
  branch: string;
  other: string;
  detail: string;
}

export interface MergerFSFuseAccess {
  present: boolean;
  userAllowOther: boolean;
  reason: string;
}

export interface MergerFSInventory {
  tooling: MergerFSTooling;
  mergerfs: MergerFSInfo;
  snapraid: SnapRAIDInfo;
  kernelFuse: MergerFSKernelFuse;
  kernelRelease: MergerFSKernelRelease;
  fuseAccess: MergerFSFuseAccess;
  warnings: MergerFSWarning[];
  warningsTruncated: boolean;
}

// Like storage/files/packages: this GET returns host mount and configuration
// paths, so it is access-audited and requires the session CSRF token.
export function getMergerFS(csrf: string): Promise<MergerFSInventory> {
  return request<MergerFSInventory>('/api/v1/mergerfs', { headers: { 'X-CSRF-Token': csrf } });
}

// Parsed `snapraid status`, run by the root MergerFS helper (SnapRAID locks the
// array for status). The scrub graph and file paths are omitted. This call does
// not run sync, diff, fix, scrub, or smart.
export interface SnapraidStatus {
  exitCode: number;
  syncInProgress: boolean;
  /** Another SnapRAID command held the array lock, so nothing was read. */
  locked?: boolean;
  /** One row per data disk; sizes in GB as SnapRAID prints them. */
  disks: SnapraidDisk[];
  summary: string[];
  scrubAge?: string;
  /** The report was cut off, or had more lines or disks than are returned. */
  truncated?: boolean;
  note: string;
  source: string;
}

export interface SnapraidDisk {
  name: string;
  files: number;
  fragmentedFiles: number;
  excessFragments: number;
  /** Absent when SnapRAID prints "-". */
  wastedGB?: number;
  usedGB: number;
  /** Absent before the first sync. */
  freeGB?: number;
  usePercent?: number;
}

export function getSnapraidStatus(csrf: string): Promise<SnapraidStatus> {
  return request<SnapraidStatus>('/api/v1/mergerfs/snapraid/status', { headers: { 'X-CSRF-Token': csrf } });
}

export interface MergerFSPlan {
  action: string;
  risk: string;
  step_up_required: boolean;
  token: string;
  expires_at: string;
  effect: string;
  consequences: string[];
  argv: string[];
}

export interface MergerFSOutcome {
  operation_id: string;
  action: string;
  outcome: string;
  message: string;
  exit_code?: number;
}

export interface MergerFSWriteBody {
  action: string;
  mountpoint?: string;
  branches?: string[];
  policy?: string;
  minFree?: string;
  parity?: string[];
  content?: string[];
  scrubPercent?: number;
  files?: string[];
  token?: string;
}

export function planMergerFS(
  kind: 'pool' | 'maintain',
  body: MergerFSWriteBody,
  csrf: string,
): Promise<MergerFSPlan> {
  return postJSON<MergerFSPlan>(`/api/v1/mergerfs/${kind}/plan`, csrf, body);
}

export function applyMergerFS(
  kind: 'pool' | 'maintain',
  body: MergerFSWriteBody,
  csrf: string,
): Promise<MergerFSOutcome> {
  return postJSON<MergerFSOutcome>(`/api/v1/mergerfs/${kind}/apply`, csrf, body);
}

// ---- M7 first checkpoint: OpenZFS (read-only) --------------------------------

export interface ZFSToolItem {
  id: string;
  package: string;
  installed: boolean;
  binary: string;
}

export interface ZFSTooling {
  family: string;
  required: ZFSToolItem[];
  optional: ZFSToolItem[];
}

export interface ZFSMount {
  mountpoint: string;
  dataset: string;
}

export interface ZFSInfo {
  present: boolean;
  reason: string;
  mounts: ZFSMount[];
  mountsTruncated: boolean;
}

export interface ZFSKernel {
  present: boolean;
  reason: string;
}

/** One row of the zpool status config tree. Depth 0 is the pool or a group heading. */
export interface ZFSVdev {
  name: string;
  depth: number;
  group?: boolean;
  state?: string;
  read_errors?: number;
  write_errors?: number;
  checksum_errors?: number;
  note?: string;
}

export interface ZFSPool {
  name: string;
  health: string;
  size?: number;
  alloc?: number;
  free?: number;
  frag_pct?: number;
  cap_pct?: number;
  state?: string;
  status?: string;
  action?: string;
  scan?: string;
  errors?: string;
  vdevs: ZFSVdev[];
}

export interface ZFSDataset {
  name: string;
  type: string;
  used?: number;
  avail?: number;
  refer?: number;
  mountpoint?: string;
  compression?: string;
}

/** Pools and datasets from zpool list/status and zfs list (broker op zfs.status). */
export interface ZFSStatus {
  available: boolean;
  reason?: string;
  report?: { pools: ZFSPool[]; datasets: ZFSDataset[]; truncated?: boolean };
}

export interface ZFSInventory {
  tooling: ZFSTooling;
  zfs: ZFSInfo;
  kernelZfs: ZFSKernel;
  status: ZFSStatus;
}

export function getZFS(csrf: string): Promise<ZFSInventory> {
  return request<ZFSInventory>('/api/v1/zfs', { headers: { 'X-CSRF-Token': csrf } });
}

// ---- M7 first checkpoint: LVM (read-only) ------------------------------------

export interface LVMToolItem {
  id: string;
  package: string;
  installed: boolean;
  binary: string;
}

export interface LVMTooling {
  family: string;
  required: LVMToolItem[];
  optional: LVMToolItem[];
}

export interface LVMKernel {
  present: boolean;
  reason: string;
}

/** One `vgs` row. */
export interface LVMVolumeGroup {
  uuid: string;
  name: string;
  size_bytes?: number;
  free_bytes?: number;
  attributes?: string;
  pv_count?: number;
  lv_count?: number;
  missing_pv_count?: number;
}

/** One logical volume from `lvs --segments` (the fields this page shows). */
export interface LVMLogicalVolume {
  uuid: string;
  name: string;
  vg_name: string;
  size_bytes?: number;
  attributes?: string;
  health: string;
  sync_percent?: number;
  segments: { type: string }[];
}

/** One physical volume from `pvs` (the fields this page shows). */
export interface LVMPhysicalVolume {
  uuid: string;
  name?: string;
  vg_name?: string;
  size_bytes?: number;
  missing: boolean;
  ambiguous?: boolean;
}

/** pvs, lvs and vgs through the read-only storage helper (broker op lvm.status). */
export interface LVMStatus {
  available: boolean;
  reason?: string;
  report?: {
    volume_groups: LVMVolumeGroup[];
    volumes: LVMLogicalVolume[];
    physical_volumes: LVMPhysicalVolume[];
  };
}

export interface LVMInventory {
  tooling: LVMTooling;
  kernelDm: LVMKernel;
  status?: LVMStatus;
}

export function getLVM(csrf: string): Promise<LVMInventory> {
  return request<LVMInventory>('/api/v1/lvm', { headers: { 'X-CSRF-Token': csrf } });
}

// ---- M7 first checkpoint: Btrfs (read-only) ---------------------------------

export interface BtrfsToolItem {
  id: string;
  package: string;
  installed: boolean;
  binary: string;
}

export interface BtrfsTooling {
  family: string;
  required: BtrfsToolItem[];
  optional: BtrfsToolItem[];
}

export interface BtrfsMount {
  mountpoint: string;
  device: string;
  subvol: string;
  subvolId: string;
}

export interface BtrfsInfo {
  present: boolean;
  reason: string;
  mounts: BtrfsMount[];
  mountsTruncated: boolean;
}

export interface BtrfsKernel {
  present: boolean;
  reason: string;
}

/** One block-group type with its RAID profile (Data,RAID1); type Unallocated lists free device space. */
export interface BtrfsProfile {
  type: string;
  profile?: string;
  size?: number;
  used?: number;
  devices: { path: string; bytes: number }[];
}

/** btrfs device stats counters for one device. */
export interface BtrfsDeviceStats {
  path: string;
  write_io_errs?: number;
  read_io_errs?: number;
  flush_io_errs?: number;
  corruption_errs?: number;
  generation_errs?: number;
}

export interface BtrfsFilesystem {
  uuid?: string;
  mountpoint: string;
  mounts: string[];
  overall: {
    device_size?: number;
    allocated?: number;
    unallocated?: number;
    missing?: number;
    used?: number;
    free_estimated?: number;
    data_ratio?: number;
    metadata_ratio?: number;
  };
  profiles: BtrfsProfile[];
  devices: BtrfsDeviceStats[];
  scrub: {
    started?: string;
    status?: string;
    duration?: string;
    error_summary?: string;
    never_run?: boolean;
  };
  subvolumes: { id: number; top_level: number; path: string }[];
  subvolumes_truncated?: boolean;
  errors?: string[];
}

/** Per-filesystem reports from btrfs-progs (broker op btrfs.status). */
export interface BtrfsStatus {
  available: boolean;
  reason?: string;
  report?: {
    filesystems: BtrfsFilesystem[];
    skipped?: SkippedMount[];
    truncated?: boolean;
  };
}

/** A mounted filesystem the read-only storage helper did not inspect, and why. */
export interface SkippedMount {
  mountpoint: string;
  reason: string;
}

/** A per-filesystem report the read-only storage helper produced, relayed by a broker op. */
export interface FilesystemToolStatus<T> {
  available: boolean;
  reason?: string;
  report?: { filesystems: T[]; skipped?: SkippedMount[]; truncated?: boolean };
}

export interface BtrfsInventory {
  tooling: BtrfsTooling;
  btrfs: BtrfsInfo;
  kernelBtrfs?: BtrfsKernel;
  status?: BtrfsStatus;
}

export function getBtrfs(csrf: string): Promise<BtrfsInventory> {
  return request<BtrfsInventory>('/api/v1/btrfs', { headers: { 'X-CSRF-Token': csrf } });
}

// ---- M7 first checkpoint: ext4 (read-only) ---------------------------------

export interface Ext4ToolItem {
  id: string;
  package: string;
  installed: boolean;
  binary: string;
}

export interface Ext4Tooling {
  family: string;
  required: Ext4ToolItem[];
  optional: Ext4ToolItem[];
}

export interface Ext4Mount {
  mountpoint: string;
  device: string;
}

export interface Ext4Info {
  present: boolean;
  reason: string;
  mounts: Ext4Mount[];
  mountsTruncated: boolean;
}

export interface Ext4Kernel {
  present: boolean;
  reason: string;
}

/**
 * One mounted ext4 superblock from `tune2fs -l` (broker op ext4.status). Device
 * is the kernel's major:minor. Times are tune2fs's own text. A counter is
 * absent when tune2fs did not print it; the error fields exist only after the
 * kernel recorded a filesystem error.
 */
export interface Ext4Filesystem {
  device: string;
  mounts: string[];
  volume_name?: string;
  uuid?: string;
  state?: string;
  errors_behavior?: string;
  features: string[];
  block_size?: number;
  block_count?: number;
  free_blocks?: number;
  reserved_blocks?: number;
  inode_count?: number;
  free_inodes?: number;
  mount_count?: number;
  /** -1 when the mount-count check is off. */
  max_mount_count?: number;
  check_interval?: string;
  last_checked?: string;
  created?: string;
  last_mount_time?: string;
  last_write_time?: string;
  lifetime_writes?: string;
  error_count?: number;
  first_error_time?: string;
  first_error_function?: string;
  last_error_time?: string;
  last_error_function?: string;
  error?: string;
}

export interface Ext4Inventory {
  tooling: Ext4Tooling;
  ext4: Ext4Info;
  kernelExt4?: Ext4Kernel;
  status?: FilesystemToolStatus<Ext4Filesystem>;
}

export function getExt4(csrf: string): Promise<Ext4Inventory> {
  return request<Ext4Inventory>('/api/v1/ext4', { headers: { 'X-CSRF-Token': csrf } });
}
// ---- Read-only Linux software RAID (mdadm, /proc/mdstat) ----------------------

export interface MdadmToolItem {
  id: string;
  package: string;
  installed: boolean;
  binary: string;
}

export interface MdadmTooling {
  family: string;
  required: MdadmToolItem[];
  optional: MdadmToolItem[];
}

/** One /proc/mdstat array line, as printed. */
export interface MdadmArray {
  name: string;
  level: string;
  state: string;
  memberCount: number | null;
}

export interface MdadmMD {
  present: boolean;
  reason: string;
  personalities: string[];
  arrays: MdadmArray[];
  arraysTruncated: boolean;
}

/** One `mdadm --detail` report; state and member states are mdadm's own words. */
export interface MdadmDetail {
  device: string;
  name?: string;
  uuid?: string;
  level?: string;
  state?: string;
  metadata?: string;
  array_size_kib?: number;
  raid_devices?: number;
  active_devices?: number;
  working_devices?: number;
  failed_devices?: number;
  spare_devices?: number;
  layout?: string;
  chunk_size?: string;
  progress?: string;
  error?: string;
  members: { slot?: number; state: string; device?: string }[];
  /** The device table had more rows than the helper returns; members holds the first 256. */
  members_truncated?: boolean;
}

/** mdadm --detail of each array (broker op mdadm.status). */
export interface MdadmStatus {
  available: boolean;
  reason?: string;
  report?: { arrays: MdadmDetail[]; truncated?: boolean };
}

export interface MdadmInventory {
  tooling: MdadmTooling;
  md: MdadmMD;
  status?: MdadmStatus;
}

export function getMdadm(csrf: string): Promise<MdadmInventory> {
  return request<MdadmInventory>('/api/v1/mdadm', { headers: { 'X-CSRF-Token': csrf } });
}

// ---- M7 first checkpoint: f2fs (read-only) ---------------------------------

export interface F2fsToolItem {
  id: string;
  package: string;
  installed: boolean;
  binary: string;
}

export interface F2fsTooling {
  family: string;
  required: F2fsToolItem[];
  optional: F2fsToolItem[];
}

export interface F2fsMount {
  mountpoint: string;
  device: string;
}

export interface F2fsInfo {
  present: boolean;
  reason: string;
  mounts: F2fsMount[];
  mountsTruncated: boolean;
}

export interface F2fsKernel {
  present: boolean;
  reason: string;
}

/** One mounted f2fs's live kernel counters from /sys/fs/f2fs/<disk>/. */
export interface F2fsFilesystem {
  name: string;
  mounts: string[];
  features: string[];
  encoding?: string;
  freeSegments?: number;
  dirtySegments?: number;
  overprovisionSegments?: number;
  reservedBlocks?: number;
  lifetimeWriteKiB?: number;
}

export interface F2fsInventory {
  tooling: F2fsTooling;
  f2fs: F2fsInfo;
  kernelF2fs?: F2fsKernel;
  /**
   * available is false (with reason) when the mount table could not be read.
   * Otherwise every f2fs mount is in filesystems, in skipped with the reason, or counted by truncated.
   */
  status?: {
    available: boolean;
    reason?: string;
    filesystems: F2fsFilesystem[];
    skipped?: SkippedMount[];
    truncated?: boolean;
  };
}

export function getF2fs(csrf: string): Promise<F2fsInventory> {
  return request<F2fsInventory>('/api/v1/f2fs', { headers: { 'X-CSRF-Token': csrf } });
}
// ---- M7 first checkpoint: XFS (read-only) ---------------------------------

export interface XFSToolItem {
  id: string;
  package: string;
  installed: boolean;
  binary: string;
}

export interface XFSTooling {
  family: string;
  required: XFSToolItem[];
  optional: XFSToolItem[];
}

export interface XFSMount {
  mountpoint: string;
  device: string;
}

export interface XFSInfo {
  present: boolean;
  reason: string;
  mounts: XFSMount[];
  mountsTruncated: boolean;
}

export interface XFSKernel {
  present: boolean;
  reason: string;
}

/** One mounted XFS's geometry from xfs_info (broker op xfs.status). */
export interface XFSFilesystem {
  mountpoint: string;
  mounts: string[];
  block_size?: number;
  blocks?: number;
  ag_count?: number;
  ag_size_blocks?: number;
  inode_size?: number;
  sector_size?: number;
  sunit_blocks?: number;
  swidth_blocks?: number;
  /** On-disk features xfs_info reports as enabled (crc, reflink, ...). */
  features: string[];
  log?: string;
  log_blocks?: number;
  realtime?: string;
  error?: string;
}

export interface XFSInventory {
  tooling: XFSTooling;
  xfs: XFSInfo;
  kernelXfs?: XFSKernel;
  status?: FilesystemToolStatus<XFSFilesystem>;
}

export function getXFS(csrf: string): Promise<XFSInventory> {
  return request<XFSInventory>('/api/v1/xfs', { headers: { 'X-CSRF-Token': csrf } });
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

// ---- M11: read-only networking (port use, bonds/LACP, firewall) -----------

export interface PortOwner {
  /** 0 for a systemd socket unit or an account inference. */
  pid: number;
  command: string;
  source: 'process' | 'systemd-socket' | 'account';
  confidence: 'observed' | 'inferred';
  socket?: string;
  unit?: string;
  container?: string;
}

export interface PortSocket {
  proto: 'tcp' | 'tcp6' | 'udp' | 'udp6';
  address: string;
  port: number;
  /** Bind scope of the address only. Firewall rules are not applied, so this is not reachability. */
  exposure: 'loopback' | 'all' | 'address';
  uid: number;
  account?: string;
  owners: PortOwner[];
}

export interface PortMap {
  sockets: PortSocket[];
  truncated: boolean;
  namespace: string;
  processesScanned: number;
  processesHidden: number;
  sources: string[];
  notes: string[];
}

export function getPorts(csrf: string): Promise<PortMap> {
  return request<PortMap>('/api/v1/ports', { headers: { 'X-CSRF-Token': csrf } });
}

export interface BondSlave {
  name: string;
  state: string;
  miiStatus: string;
  linkFailures: number;
  speed?: string;
  aggregatorId?: string;
}

export interface BondRenderer {
  id: 'systemd-networkd' | 'NetworkManager' | 'ifupdown' | 'unknown';
  via?: string;
  source?: string;
  confidence: 'observed' | 'unknown';
}

export interface Bond {
  name: string;
  mode: string;
  lacp: boolean;
  operState: string;
  miimonMs?: string;
  lacpRate?: string;
  xmitHashPolicy?: string;
  aggregatorId?: string;
  partnerMac?: string;
  activeSlave?: string;
  slaves: BondSlave[];
  renderer: BondRenderer;
}

export interface BondSnapshot {
  driverLoaded: boolean;
  bonds: Bond[];
  stacks: string[];
  truncated: boolean;
}

export function getBonds(csrf: string): Promise<BondSnapshot> {
  return request<BondSnapshot>('/api/v1/bonds', { headers: { 'X-CSRF-Token': csrf } });
}

export type FirewallBackendId = 'firewalld' | 'ufw' | 'nftables' | 'iptables';

export interface FirewallBackend {
  id: FirewallBackendId;
  installed: boolean;
  binary?: string;
  unit?: string;
  activeState?: string;
  enabled?: string;
  role: 'active' | 'installed' | 'absent';
}

export interface UfwRule {
  action: string;
  direction: string;
  interface?: string;
  route: boolean;
  proto: string;
  to: string;
  toPort: string;
  from: string;
  fromPort: string;
  app?: string;
  comment?: string;
  ipv6: boolean;
}

export interface UfwView {
  enabled: boolean;
  logLevel?: string;
  defaultInput?: string;
  defaultOutput?: string;
  defaultForward?: string;
  ipv6?: string;
  rules: UfwRule[];
  truncated: boolean;
  unreadable: string[];
}

export interface FirewalldZone {
  name: string;
  default: boolean;
  target: string;
  source: string;
  interfaces: string[];
  sources: string[];
  services: string[];
  ports: string[];
  masquerade: boolean;
  forwardPorts: string[];
  richRules: string[];
}

export interface FirewalldView {
  defaultZone: string;
  zones: FirewalldZone[];
  truncated: boolean;
  unreadable: string[];
}

export interface NftChain {
  name: string;
  type?: string;
  hook?: string;
  priority?: string;
  policy?: string;
  rules: string[];
}

export interface NftablesView {
  file: string;
  tables: { family: string; name: string; chains: NftChain[]; sets: string[] }[];
  includes: string[];
  flushRuleset: boolean;
  truncated: boolean;
  unreadable: string[];
}

export interface IptablesView {
  files: {
    path: string;
    ipv6: boolean;
    tables: { name: string; chains: { name: string; policy: string; rules: string[] }[] }[];
  }[];
  truncated: boolean;
  unreadable: string[];
}

export interface FirewallSnapshot {
  active: FirewallBackendId | 'none' | 'unknown';
  activeReason: string;
  alsoActive: string[];
  backends: FirewallBackend[];
  source: 'saved-configuration';
  firewalld?: FirewalldView;
  ufw?: UfwView;
  nftables?: NftablesView;
  iptables?: IptablesView;
  notes: string[];
}

export function getFirewall(csrf: string): Promise<FirewallSnapshot> {
  return request<FirewallSnapshot>('/api/v1/firewall', { headers: { 'X-CSRF-Token': csrf } });
}
