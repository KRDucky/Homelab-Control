// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { useState } from 'react';
import type { Capability, Overview as HostOverview } from './api';
import { formatBytes, formatUptime } from './format';
import { Meter } from './Meter';
import { Section } from './ui';
import { ModuleOverview } from './ModuleOverview';
import { TelemetryTrends } from './TelemetryTrends';

// Dashboard settings are presentation only, stored on this browser. Host data and
// authorization are never persisted here; server capabilities remain authoritative.
export const DASHBOARD_IDS = ['host', 'cpu', 'memory', 'storage', 'network', 'modules', 'trends'] as const;
export type DashboardId = (typeof DASHBOARD_IDS)[number];
export type DashboardLayout = { version: 1; widgets: DashboardId[] };
const KEY = 'hc.dashboard.v1';
const DEFAULT: DashboardLayout = { version: 1, widgets: [...DASHBOARD_IDS] };
const TITLES: Record<DashboardId, string> = {
  host: 'Host',
  cpu: 'Processor',
  memory: 'Memory',
  storage: 'Storage',
  network: 'Network',
  modules: 'Modules',
  trends: 'Resource trends',
};
const validId = (id: unknown): id is DashboardId =>
  typeof id === 'string' && (DASHBOARD_IDS as readonly string[]).includes(id);

export function validateDashboardLayout(value: unknown): DashboardLayout {
  if (!value || typeof value !== 'object') return DEFAULT;
  const doc = value as { version?: unknown; widgets?: unknown };
  if (doc.version !== 1 || !Array.isArray(doc.widgets) || doc.widgets.length > DASHBOARD_IDS.length)
    return DEFAULT;
  if (!doc.widgets.every(validId) || new Set(doc.widgets).size !== doc.widgets.length) return DEFAULT;
  return { version: 1, widgets: doc.widgets as DashboardId[] };
}

function readLayout(): DashboardLayout {
  try {
    return validateDashboardLayout(JSON.parse(window.localStorage.getItem(KEY) ?? 'null'));
  } catch {
    return DEFAULT;
  }
}

function percentage(used: number | undefined, total: number | undefined): string {
  return used !== undefined && total && total > 0
    ? Math.min(100, Math.max(0, (used / total) * 100)).toFixed(0) + '%'
    : 'Unknown';
}

function Widget({ id, data, pages }: { id: DashboardId; data: HostOverview; pages: Capability[] }) {
  const { system, cpu, memory, filesystems, network } = data;
  switch (id) {
    case 'host':
      return (
        <Section title="Host" meta={system}>
          <p className="dashboard-value">{system.hostname ?? 'Unknown host'}</p>
          <p className="muted">{system.os.pretty_name ?? 'Operating system not reported'}</p>
          <dl className="dashboard-facts">
            <div>
              <dt>Uptime</dt>
              <dd>{formatUptime(system.uptime_seconds)}</dd>
            </div>
            <div>
              <dt>Kernel</dt>
              <dd>{system.kernel ?? 'Unknown'}</dd>
            </div>
            <div>
              <dt>Load (1 min)</dt>
              <dd>{system.load_average[0]?.toFixed(2) ?? 'Unknown'}</dd>
            </div>
          </dl>
        </Section>
      );
    case 'cpu':
      return (
        <Section title="Processor" meta={cpu}>
          <p className="dashboard-value">{percentage(cpu.usage_percent, 100)}</p>
          <Meter
            label="CPU busy"
            used={cpu.usage_percent}
            total={cpu.usage_percent === undefined ? undefined : 100}
            detail={cpu.logical_cpus + ' logical CPUs · ' + (cpu.model ?? 'Model not reported')}
          />
        </Section>
      );
    case 'memory':
      return (
        <Section title="Memory" meta={memory}>
          <p className="dashboard-value">{percentage(memory.used_bytes, memory.total_bytes)}</p>
          <Meter
            label="RAM in use"
            used={memory.used_bytes}
            total={memory.total_bytes}
            detail={formatBytes(memory.used_bytes) + ' of ' + formatBytes(memory.total_bytes)}
          />
        </Section>
      );
    case 'storage': {
      const measured = filesystems.items.filter(
        (f) => f.probed && f.used_bytes !== undefined && f.avail_bytes !== undefined,
      );
      const used = measured.reduce((sum, f) => sum + (f.used_bytes ?? 0), 0);
      const total = measured.reduce((sum, f) => sum + (f.used_bytes ?? 0) + (f.avail_bytes ?? 0), 0);
      return (
        <Section title="Storage" meta={filesystems}>
          <p className="dashboard-value">
            {measured.length} <span className="dashboard-unit">measured filesystems</span>
          </p>
          <Meter
            label="Measured filesystem usage"
            used={total ? used : undefined}
            total={total || undefined}
            detail={total ? formatBytes(used) + ' of ' + formatBytes(total) : 'No usage data available'}
          />
          <p className="muted">
            Totals summarize reported mount entries and may include overlapping filesystems.
          </p>
        </Section>
      );
    }
    case 'network':
      return (
        <Section title="Network" meta={network}>
          <p className="dashboard-value">
            {network.interfaces.length} <span className="dashboard-unit">interfaces</span>
          </p>
          <dl className="dashboard-facts">
            <div>
              <dt>Operationally up</dt>
              <dd>{network.interfaces.filter((n) => n.oper_state === 'up').length}</dd>
            </div>
            <div>
              <dt>Total received</dt>
              <dd>{formatBytes(network.interfaces.reduce((sum, n) => sum + n.rx_bytes, 0))}</dd>
            </div>
            <div>
              <dt>Total sent</dt>
              <dd>{formatBytes(network.interfaces.reduce((sum, n) => sum + n.tx_bytes, 0))}</dd>
            </div>
          </dl>
          <p className="muted">
            Counters are cumulative, not transfer rates; virtual interfaces may overlap.
          </p>
        </Section>
      );
    case 'modules':
      return <ModuleOverview pages={pages} />;
    case 'trends':
      return <TelemetryTrends data={data} />;
  }
}

export function Dashboard({ data, pages }: { data: HostOverview; pages: Capability[] }) {
  const [layout, setLayout] = useState<DashboardLayout>(readLayout);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<DashboardId[]>(layout.widgets);
  function persist(widgets: DashboardId[]) {
    const next: DashboardLayout = { version: 1, widgets };
    setLayout(next);
    try {
      window.localStorage.setItem(KEY, JSON.stringify(next));
    } catch {
      /* storage disabled */
    }
  }
  function move(id: DashboardId, step: number) {
    setDraft((before) => {
      const next = [...before];
      const from = next.indexOf(id);
      const to = from + step;
      if (from < 0 || to < 0 || to >= next.length) return before;
      const item = next[from];
      const neighbor = next[to];
      if (item === undefined || neighbor === undefined) return before;
      next[from] = neighbor;
      next[to] = item;
      return next;
    });
  }
  return (
    <section className="dashboard" aria-label="Customizable dashboard">
      <div className="dashboard-heading">
        <div>
          <h1 className="page-title">Dashboard</h1>
          <p className="page-sub">
            Live host overview · Collected {new Date(data.collected_at).toLocaleString()}
          </p>
        </div>
        <button
          type="button"
          className="ghost"
          onClick={() => {
            if (editing) setDraft(layout.widgets);
            else setDraft([...layout.widgets]);
            setEditing(!editing);
          }}
        >
          {editing ? 'Cancel editing' : 'Customize dashboard'}
        </button>
      </div>
      {editing && (
        <div className="panel dashboard-editor">
          <h2>Choose and arrange widgets</h2>
          <p className="muted">
            Changes stay in this browser. Use Move up / Move down for keyboard-accessible ordering.
          </p>
          <ul className="dashboard-editor-list">
            {DASHBOARD_IDS.map((id) => {
              const index = draft.indexOf(id);
              return (
                <li key={id}>
                  <label>
                    <input
                      type="checkbox"
                      checked={index !== -1}
                      onChange={(e) =>
                        setDraft((before) =>
                          e.target.checked ? [...before, id] : before.filter((x) => x !== id),
                        )
                      }
                    />{' '}
                    {TITLES[id]}
                  </label>
                  <div className="dashboard-edit-actions">
                    <button
                      type="button"
                      className="ghost small"
                      disabled={index <= 0}
                      aria-label={'Move ' + TITLES[id] + ' up'}
                      onClick={() => move(id, -1)}
                    >
                      Move up
                    </button>
                    <button
                      type="button"
                      className="ghost small"
                      disabled={index < 0 || index === draft.length - 1}
                      aria-label={'Move ' + TITLES[id] + ' down'}
                      onClick={() => move(id, 1)}
                    >
                      Move down
                    </button>
                  </div>
                </li>
              );
            })}
          </ul>
          <div className="toolbar">
            <button
              type="button"
              onClick={() => {
                persist(draft);
                setEditing(false);
              }}
            >
              Save layout
            </button>
            <button
              type="button"
              className="ghost"
              onClick={() => {
                persist([...DASHBOARD_IDS]);
                setDraft([...DASHBOARD_IDS]);
                setEditing(false);
              }}
            >
              Reset to default
            </button>
          </div>
        </div>
      )}
      {layout.widgets.length === 0 && (
        <p className="empty">No widgets selected. Customize the dashboard to add widgets.</p>
      )}
      <div className="dashboard-grid">
        {layout.widgets.map((id) => (
          <div className={'dashboard-card dashboard-card-' + id} key={id}>
            <Widget id={id} data={data} pages={pages} />
          </div>
        ))}
      </div>
    </section>
  );
}
