// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, getUnitLogs, type LogEntry, type UnitLogs } from '../api';
import { explain } from '../useData';
import { TableRegion } from '../ui';

const lineChoices = [50, 100, 200, 500];

// syslog priorities as journald reports them
const priorityName = ['emerg', 'alert', 'crit', 'err', 'warning', 'notice', 'info', 'debug'];

function priorityTone(p?: number): string {
  if (p === undefined) return '';
  if (p <= 3) return 'log-crit';
  if (p === 4) return 'log-warn';
  if (p === 7) return 'log-debug';
  return '';
}

function formatTime(t?: string): string {
  if (!t) return '—';
  const d = new Date(t);
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString();
}

function logsError(err: Error): string {
  if (err instanceof ApiError) {
    if (err.status === 404) return 'systemd no longer lists this unit.';
    if (err.status === 403) return 'Your account does not have permission to read service logs.';
    if (err.status === 400) return err.message;
  }
  return explain(err);
}

/** Read-only view of the most recent journal entries of one unit. */
export function UnitLogsPanel({
  unit,
  onClose,
  onUnauthorized,
}: {
  unit: string;
  onClose: () => void;
  onUnauthorized: () => void;
}) {
  const [lines, setLines] = useState(100);
  const [data, setData] = useState<UnitLogs | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(true);
  const heading = useRef<HTMLHeadingElement>(null);
  const seq = useRef(0);

  const load = useCallback(async () => {
    const mine = ++seq.current;
    setLoading(true);
    try {
      const d = await getUnitLogs(unit, lines);
      if (mine !== seq.current) return;
      setData(d);
      setError(null);
    } catch (err) {
      if (mine !== seq.current) return;
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      setData(null);
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      if (mine === seq.current) setLoading(false);
    }
  }, [unit, lines, onUnauthorized]);

  useEffect(() => {
    const t = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(t);
  }, [load]);

  useEffect(() => {
    heading.current?.focus();
  }, [unit]);

  return (
    <section className="panel logs-panel" aria-labelledby="h-logs" aria-busy={loading}>
      <div className="panel-head">
        <h2 id="h-logs" ref={heading} tabIndex={-1}>
          Logs: {unit}
        </h2>
        <div className="toolbar compact">
          <label className="inline-field">
            Entries
            <select value={lines} onChange={(e) => setLines(Number(e.target.value))}>
              {lineChoices.map((n) => (
                <option key={n} value={n}>
                  last {n}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="ghost" onClick={() => void load()} disabled={loading}>
            {loading ? 'Reading…' : 'Refresh'}
          </button>
          <button type="button" className="ghost" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
      {error && (
        <p className="banner" role="alert">
          {logsError(error)}
        </p>
      )}
      {data && <LogTable data={data} />}
      {!data && !error && <p className="muted">Reading the journal…</p>}
    </section>
  );
}

function LogTable({ data }: { data: UnitLogs }) {
  if (data.entries.length === 0) {
    return <p className="empty">The journal has no entries for {data.unit}.</p>;
  }
  return (
    <>
      <p className="result-count" aria-live="polite">
        {data.entries.length} most recent {data.entries.length === 1 ? 'entry' : 'entries'}, oldest first
      </p>
      <TableRegion label={`Logs for ${data.unit}`} className="logs-wrap">
        <table className="logs">
          <thead>
            <tr>
              <th scope="col">Time</th>
              <th scope="col">Level</th>
              <th scope="col">Process</th>
              <th scope="col">Message</th>
            </tr>
          </thead>
          <tbody>
            {data.entries.map((e, i) => (
              <LogRow key={i} e={e} />
            ))}
          </tbody>
        </table>
      </TableRegion>
      {data.truncated && (
        <p className="footnote">Older entries were left out to keep this response under 1 MiB.</p>
      )}
      {!!data.skipped && (
        <p className="footnote">
          {data.skipped} journal {data.skipped === 1 ? 'record was' : 'records were'} unreadable and skipped.
        </p>
      )}
      <details className="sources">
        <summary>Where this data comes from</summary>
        <p>
          <code>{data.source}</code>, via the privileged helper. Reading logs is recorded in the audit log;
          the log content is not.
        </p>
      </details>
    </>
  );
}

function LogRow({ e }: { e: LogEntry }) {
  const proc = e.identifier ? `${e.identifier}${e.pid ? `[${e.pid}]` : ''}` : e.pid ? `pid ${e.pid}` : '—';
  return (
    <tr className={priorityTone(e.priority)}>
      <td className="nowrap">{formatTime(e.time)}</td>
      <td>{e.priority === undefined ? '—' : priorityName[e.priority]}</td>
      <td className="nowrap">{proc}</td>
      <td className="log-msg">
        {e.message}
        {e.truncated && <span className="tag">cut at 4 KiB</span>}
      </td>
    </tr>
  );
}
