// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useMemo, useState } from 'react';
import { getServices, type Capability, type Unit } from '../api';
import { StateDot } from '../ui';
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
  const { data, error } = useData(getServices, { intervalMs: 15_000, onUnauthorized });
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

      {failed.length > 0 && (
        <section className="panel panel-alert" aria-labelledby="h-failed">
          <h2 id="h-failed">Failed units</h2>
          <ul className="failed-list">
            {failed.map((u) => (
              <li key={u.name}>
                <strong>{u.name}</strong> <span className="muted">{u.description}</span>
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
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th scope="col">Unit</th>
                <th scope="col">State</th>
                <th scope="col">At boot</th>
                <th scope="col">Description</th>
                {logsUsable && (
                  <th scope="col">
                    <span className="visually-hidden">Logs</span>
                  </th>
                )}
              </tr>
            </thead>
            <tbody>
              {shown.map((u) => (
                <tr key={u.name} className={u.active === 'failed' ? 'row-crit' : undefined}>
                  <th scope="row">{u.name}</th>
                  <td>
                    <StateDot tone={tone(u)} />
                    {u.active} <span className="muted">({u.sub})</span>
                    {u.load !== 'loaded' && <span className="tag">{u.load}</span>}
                  </td>
                  <td>{u.unit_file_state ?? '—'}</td>
                  <td className="muted">{u.description}</td>
                  {logsUsable && (
                    <td>
                      {u.load !== 'not-found' && (
                        <button
                          type="button"
                          className="ghost small"
                          aria-label={`Show logs for ${u.name}`}
                          aria-pressed={logUnit === u.name}
                          onClick={() => setLogUnit(u.name)}
                        >
                          Logs
                        </button>
                      )}
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {shown.length === 0 && <p className="empty">No units match these filters.</p>}
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
