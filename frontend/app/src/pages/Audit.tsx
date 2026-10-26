// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, getAudit, type AuditEvent } from '../api';
import { explain } from '../useData';
import { DataTable } from '../DataTable';

const filters = [
  { id: '', label: 'All events' },
  { id: 'auth.', label: 'Sign-ins and sessions' },
  { id: 'authz.', label: 'Permission denials' },
  { id: 'csrf.', label: 'Rejected requests' },
  { id: 'services.', label: 'Service logs read' },
  { id: 'audit.', label: 'Audit log events' },
];

const PAGE = 100;

function tone(outcome: string): string {
  if (outcome === 'success' || outcome === 'ok') return '';
  if (outcome === 'denied' || outcome === 'failure' || outcome === 'throttled') return 'row-crit';
  return 'row-warn';
}

/** Audit: the API audit trail (state database), newest first. */
export function Audit({ onUnauthorized }: { onUnauthorized: () => void }) {
  const [filter, setFilter] = useState('');
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [next, setNext] = useState<number | undefined>(undefined);
  const [source, setSource] = useState('');
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(true);
  const seq = useRef(0);

  const load = useCallback(
    async (before?: number) => {
      const mine = ++seq.current;
      setLoading(true);
      try {
        const p = await getAudit({ limit: PAGE, event: filter, ...(before ? { before } : {}) });
        if (mine !== seq.current) return;
        setEvents((prev) => (before ? [...prev, ...p.events] : p.events));
        setNext(p.next_before);
        setSource(p.source);
        setError(null);
      } catch (err) {
        if (mine !== seq.current) return;
        if (err instanceof ApiError && err.status === 401) onUnauthorized();
        setError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        if (mine === seq.current) setLoading(false);
      }
    },
    [filter, onUnauthorized],
  );

  useEffect(() => {
    const t = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(t);
  }, [load]);

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Audit</h1>
        <p className="page-sub">
          Sign-ins, sign-outs, permission denials, rejected requests and log reads recorded by Homelab
          Control, newest first. Records are hash-chained so that edits, deletions and reordering can be
          detected.
        </p>
      </header>
      <section className="panel" aria-labelledby="h-audit" aria-busy={loading}>
        <h2 id="h-audit" className="visually-hidden">
          Audit events
        </h2>
        <div className="toolbar">
          <label className="inline-field">
            Show
            <select value={filter} onChange={(e) => setFilter(e.target.value)}>
              {filters.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.label}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="ghost" onClick={() => void load()} disabled={loading}>
            {loading ? 'Reading…' : 'Refresh'}
          </button>
        </div>
        {error && (
          <p className="banner" role="alert">
            {error instanceof ApiError && error.status === 503 ? error.message : explain(error)}
          </p>
        )}
        <p className="result-count" aria-live="polite">
          {events.length} {events.length === 1 ? 'event' : 'events'} shown
        </p>
        <DataTable
          caption="Audit events"
          rows={events}
          rowKey={(e) => String(e.id)}
          rowTone={(e) =>
            tone(e.outcome) === 'row-crit' ? 'crit' : tone(e.outcome) === 'row-warn' ? 'warn' : undefined
          }
          emptyState={
            loading ? 'Reading audit events…' : error ? 'Audit records unavailable.' : 'No events match.'
          }
          columns={[
            { id: 'time', label: 'Time', render: (e) => new Date(e.time).toLocaleString() },
            { id: 'event', label: 'Event', render: (e) => <code>{e.event}</code> },
            { id: 'outcome', label: 'Outcome', render: (e) => e.outcome },
            { id: 'user', label: 'User', render: (e) => e.user || '—' },
            { id: 'remote', label: 'Address', render: (e) => <code>{e.remote || '—'}</code> },
            { id: 'detail', label: 'Detail', render: (e) => e.detail || '' },
          ]}
        />
        {next !== undefined && (
          <button type="button" className="ghost" onClick={() => void load(next)} disabled={loading}>
            Show older events
          </button>
        )}
        <details className="sources">
          <summary>Where this data comes from and how to check it</summary>
          <p>
            {source || 'The hc-api state database'}. To check the hash chain on the server, run{' '}
            <code>sudo hlctl audit verify --db /var/lib/homelab-control/state.db</code>, adding{' '}
            <code>--checkpoint SEQ:HASH</code> from a recent <code>audit checkpoint</code> line in{' '}
            <code>journalctl -u homelab-control</code> to also detect a truncated or rolled-back database. The
            privileged helper keeps its own log, checked with <code>hlctl audit verify --file</code>.
          </p>
        </details>
      </section>
    </div>
  );
}
