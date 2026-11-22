// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { useState } from 'react';
import type { Overview } from './api';

export type TrendSample = { at: string; cpu?: number; memory?: number };
export const MAX_TREND_SAMPLES = 60;

/** Browser-memory samples only. Never persist host telemetry in localStorage. */
export function appendTrendSample(history: TrendSample[], data: Overview): TrendSample[] {
  const at = data.collected_at;
  if (!Number.isFinite(Date.parse(at)) || history.some((entry) => entry.at === at)) return history;
  const cpu =
    data.cpu.status !== 'unavailable' && Number.isFinite(data.cpu.usage_percent)
      ? Math.max(0, Math.min(100, data.cpu.usage_percent ?? 0))
      : undefined;
  const memory =
    data.memory.status !== 'unavailable' && data.memory.total_bytes > 0
      ? Math.max(0, Math.min(100, (100 * data.memory.used_bytes) / data.memory.total_bytes))
      : undefined;
  return [
    ...history,
    { at, ...(cpu === undefined ? {} : { cpu }), ...(memory === undefined ? {} : { memory }) },
  ].slice(-MAX_TREND_SAMPLES);
}

export function trendPoints(history: TrendSample[], key: 'cpu' | 'memory'): string {
  return history
    .map((sample, index) => {
      const value = sample[key];
      if (value === undefined) return null;
      const x = history.length < 2 ? 50 : (index * 100) / (history.length - 1);
      return { x, y: 100 - value };
    })
    .reduce<string[]>((paths, item) => {
      if (item === null) return [...paths, ''];
      const point = item.x.toFixed(2) + ',' + item.y.toFixed(2);
      const last = paths.length - 1;
      if (last < 0 || paths[last] === '') return [...paths, point];
      paths[last] += ' ' + point;
      return paths;
    }, [])
    .join('|');
}

function Trend({
  label,
  history,
  metric,
}: {
  label: string;
  history: TrendSample[];
  metric: 'cpu' | 'memory';
}) {
  const segments = trendPoints(history, metric).split('|').filter(Boolean);
  const measured = history.filter((sample) => sample[metric] !== undefined);
  return (
    <div className="dashboard-trend">
      <h3>{label}</h3>
      {measured.length < 2 ? (
        <p className="muted">Collecting samples while this dashboard is open…</p>
      ) : (
        <svg
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
          role="img"
          aria-label={label + ' trend over ' + measured.length + ' recent readings'}
          className="dashboard-trend-plot"
        >
          <path d="M0 25H100 M0 50H100 M0 75H100" className="dashboard-trend-grid" />
          {segments.map(
            (points, i) =>
              points.includes(' ') && <polyline key={i} points={points} className="dashboard-trend-line" />,
          )}
        </svg>
      )}
      <p className="muted">
        {measured.length} readings · up to {MAX_TREND_SAMPLES} retained in memory
      </p>
    </div>
  );
}

/** The API supplies snapshots, not historical telemetry. Charts reflect this browser session only. */
export function TelemetryTrends({ data }: { data: Overview }) {
  // React's guarded same-component state adjustment avoids cascading effect renders.
  const [observed, setObserved] = useState(() => ({
    collectedAt: data.collected_at,
    history: appendTrendSample([], data),
  }));
  if (observed.collectedAt !== data.collected_at) {
    setObserved({
      collectedAt: data.collected_at,
      history: appendTrendSample(observed.history, data),
    });
  }
  const history = observed.history;
  const [table, setTable] = useState(false);
  return (
    <section className="panel dashboard-trends" aria-label="Recent resource trends">
      <header className="panel-head">
        <h2>Recent resource trends</h2>
      </header>
      <p className="muted">
        CPU and RAM percentages sampled from the live host overview. No historical server data is available.
      </p>
      <div className="dashboard-trends-grid">
        <Trend label="CPU busy" history={history} metric="cpu" />
        <Trend label="RAM used" history={history} metric="memory" />
      </div>
      <button type="button" className="ghost small" aria-expanded={table} onClick={() => setTable(!table)}>
        {table ? 'Hide readings table' : 'Show readings table'}
      </button>
      {table && (
        <div className="table-wrap" role="region" aria-label="Recent resource readings" tabIndex={0}>
          <table>
            <caption>Recent resource readings (newest first)</caption>
            <thead>
              <tr>
                <th scope="col">Collected</th>
                <th scope="col">CPU</th>
                <th scope="col">RAM</th>
              </tr>
            </thead>
            <tbody>
              {[...history].reverse().map((sample) => (
                <tr key={sample.at}>
                  <th scope="row">{new Date(sample.at).toLocaleTimeString()}</th>
                  <td>{sample.cpu === undefined ? 'Unknown' : sample.cpu.toFixed(1) + '%'}</td>
                  <td>{sample.memory === undefined ? 'Unknown' : sample.memory.toFixed(1) + '%'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
