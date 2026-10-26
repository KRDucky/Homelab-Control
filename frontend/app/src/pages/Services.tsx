// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useMemo, useState } from 'react';
import { getServices, type Capability, type Unit } from '../api';
import { LiveIndicator, StatusIndicator } from '../ui';
import { DataTable } from '../DataTable';
import { explain, useData } from '../useData';
import { UnitLogsPanel } from './UnitLogsPanel';

type Filter = 'all' | 'running' | 'failed' | 'inactive';

const filters: { id: Filter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'running', label: 'Running' },
  { id: 'failed', label: 'Failed' },
  { id: 'inactive', label: 'Inactive' },
];

function matches(u: Unit, f: Filter): boolean {
  switch (f) {
    case 'running':
      return u.active === 'active';
    case 'failed':
      return u.active === 'failed';
    case 'inactive':
      return u.active === 'inactive';
    default:
      return true;
  }
}

function tone(u: Unit): 'ok' | 'warn' | 'crit' | 'idle' {
  if (u.active === 'failed') return 'crit';
  if (u.active === 'active') return 'ok';
  if (u.active === 'activating' || u.active === 'deactivating' || u.active === 'reloading') return 'warn';
  return 'idle';
}

export function Services({
  onUnauthorized,
  logs,
}: {
  onUnauthorized: () => void;
  logs?: Capability | undefined;
}) {
  const { data, error, live } = useData(getServices, { intervalMs: 15_000, onUnauthorized });
  const [filter, setFilter] = useState<Filter>('all');
  const [type, setType] = useState('service');
  const [q, setQ] = useState('');
  const [showMissing, setShowMissing] = useState(false);
  const [logUnit, setLogUnit] = useState<string | null>(null);
  const logsUsable = logs?.status === 'available' || logs?.status === 'degraded';

  const types = useMemo(() => [...new Set((data?.units ?? []).map((u) => u.type))].sort(), [data]);
  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return (data?.units ?? []).filter(
      (u) =>
        (showMissing || u.load !== 'not-found') &&
        (type === 'all' || u.type === type) &&
        matches(u, filter) &&
        (!needle || u.name.toLowerCase().includes(needle) || u.description.toLowerCase().includes(needle)),
    );
  }, [data, filter, type, q, showMissing]);

  if (error && !data) {
    return (
      <div className="page">
        <h1 className="page-title">Services</h1>
        <p className="banner" role="alert">
          {explain(error)}
        </p>
      </div>
    );
  }
  if (!data) return <p className="muted">Reading systemd units…</p>;

  const failed = data.units.filter((u) => u.active === 'failed');
  const running = data.units.filter((u) => u.active === 'active').length;
  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Services</h1>
        <p className="page-sub">
          {data.units.length} units, {running} active, {failed.length} failed
          {data.systemd_version ? `. ${data.systemd_version}` : ''}. Read-only: you can read a unit’s recent
          logs; starting, stopping and enabling units is not available yet.
        </p>
      </header>

      <LiveIndicator {...live} />
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Showing the last successful reading.
        </p>
      )}
      {failed.length > 0 && (
        <section className="panel panel-alert" aria-labelledby="h-failed">
          <h2 id="h-failed">Failed units</h2>
          <ul className="failed-list">
            {failed.map((u) => (
              <li key={u.name}>
                <strong className="mono">{u.name}</strong> <span className="muted">{u.description}</span>
              </li>
            ))}
          </ul>
          <p className="footnote">
            Inspect with <code>systemctl status {failed[0]?.name}</code> and{' '}
            <code>journalctl -u {failed[0]?.name}</code>.
          </p>
        </section>
      )}

      <section className="panel" aria-labelledby="h-units">
        <h2 id="h-units" className="visually-hidden">
          Units
        </h2>
        <div className="toolbar">
          <fieldset className="segmented">
            <legend className="visually-hidden">Filter by state</legend>
            {filters.map((f) => (
              <label key={f.id} className={filter === f.id ? 'on' : ''}>
                <input
                  type="radio"
                  name="state"
                  value={f.id}
                  checked={filter === f.id}
                  onChange={() => setFilter(f.id)}
                />
                {f.label}
              </label>
            ))}
          </fieldset>
          <label className="inline-field">
            Type
            <select value={type} onChange={(e) => setType(e.target.value)}>
              <option value="all">All types</option>
              {types.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </label>
          <label className="inline-field grow">
            <span className="visually-hidden">Search units</span>
            <input
              type="search"
              placeholder="Search by name or description"
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </label>
        </div>
        <label className="inline-check">
          <input type="checkbox" checked={showMissing} onChange={(e) => setShowMissing(e.target.checked)} />
          Show units that aren’t installed (referenced but not found)
        </label>
        <p className="result-count" aria-live="polite">
          Showing {shown.length} of {data.units.length}
        </p>
        <DataTable
          caption="Units"
          rows={shown}
          rowKey={(u) => u.name}
          rowTone={(u) => (u.active === 'failed' ? 'crit' : undefined)}
          emptyState="No units match these filters."
          columns={[
            { id: 'name', label: 'Unit', sortValue: (u) => u.name, render: (u) => <code>{u.name}</code> },
            {
              id: 'state',
              label: 'State',
              sortValue: (u) => u.active,
              render: (u) => (
                <>
                  <StatusIndicator
                    tone={tone(u) === 'idle' ? 'unknown' : (tone(u) as 'ok' | 'warn' | 'crit')}
                    label={u.active}
                  />{' '}
                  <span className="muted">({u.sub})</span>
                  {u.load !== 'loaded' && <span className="tag">{u.load}</span>}
                </>
              ),
            },
            {
              id: 'boot',
              label: 'At boot',
              sortValue: (u) => u.unit_file_state ?? '',
              render: (u) => u.unit_file_state ?? '—',
            },
            {
              id: 'description',
              label: 'Description',
              sortValue: (u) => u.description,
              render: (u) => u.description,
            },
            ...(logsUsable
              ? [
                  {
                    id: 'logs',
                    label: 'Logs',
                    render: (u: Unit) =>
                      u.load !== 'not-found' && (
                        <button
                          type="button"
                          className="ghost small"
                          aria-label={`Show logs for ${u.name}`}
                          aria-pressed={logUnit === u.name}
                          onClick={() => setLogUnit(u.name)}
                        >
                          Logs
                        </button>
                      ),
                  },
                ]
              : []),
          ]}
        />
        {data.truncated && <p className="footnote">The list was truncated at 5,000 units.</p>}
        <details className="sources">
          <summary>Where this data comes from</summary>
          <p>
            <code>{data.source}</code>, via the privileged helper.
          </p>
        </details>
        {logs && !logsUsable && (
          <p className="footnote" role="note">
            Service logs: {logs.reason}
          </p>
        )}
      </section>

      {logsUsable && logUnit && (
        <UnitLogsPanel
          key={logUnit}
          unit={logUnit}
          onClose={() => setLogUnit(null)}
          onUnauthorized={onUnauthorized}
        />
      )}
    </div>
  );
}
