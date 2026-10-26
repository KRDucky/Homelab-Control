// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useState } from 'react';
import { ApiError, getSessions, revokeSession, type SessionEntry } from '../api';
import { explain, useData } from '../useData';
import { DataTable } from '../DataTable';

function when(t: string): string {
  const d = new Date(t);
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString();
}

function minutes(s: number): string {
  return s % 3600 === 0 ? `${s / 3600} h` : `${Math.round(s / 60)} min`;
}

/** Account → Sessions: the signed-in user's own sessions, with revoke. */
export function Sessions({ csrf, onUnauthorized }: { csrf: string; onUnauthorized: () => void }) {
  const { data, error, reload } = useData(getSessions, { onUnauthorized });
  const [busy, setBusy] = useState<string | null>(null);
  const [msg, setMsg] = useState<string | null>(null);

  async function revoke(s: SessionEntry) {
    setBusy(s.id);
    setMsg(null);
    try {
      await revokeSession(s.id, csrf);
      if (s.current) {
        onUnauthorized();
        return;
      }
      setMsg('Session ended. That browser will have to sign in again.');
      reload();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      else if (err instanceof ApiError && err.status === 404) {
        setMsg('That session had already ended.');
        reload();
      } else setMsg(err instanceof Error ? explain(err) : String(err));
    } finally {
      setBusy(null);
    }
  }

  if (error && !data) {
    return (
      <div className="page">
        <h1 className="page-title">Sessions</h1>
        <p className="banner" role="alert">
          {explain(error)}
        </p>
      </div>
    );
  }
  if (!data) return <p className="muted">Reading sessions…</p>;

  const others = data.sessions.filter((s) => !s.current);
  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Sessions</h1>
        <p className="page-sub">
          Where your account is signed in. A session ends after {minutes(data.idle_timeout_seconds)} without
          activity or {minutes(data.max_lifetime_seconds)} after sign-in.{' '}
          {data.persistent
            ? 'Sessions survive a restart of Homelab Control.'
            : 'This server runs without its state database, so a restart signs everyone out.'}
        </p>
      </header>
      {msg && (
        <p className="banner" role="status">
          {msg}
        </p>
      )}
      <section className="panel" aria-labelledby="h-sessions">
        <h2 id="h-sessions" className="visually-hidden">
          Your sessions
        </h2>
        <DataTable
          caption="Your sessions"
          rows={data.sessions}
          rowKey={(s) => s.id}
          columns={[
            {
              id: 'browser',
              label: 'Browser',
              render: (s) => (
                <>
                  {s.user_agent || 'Unknown browser'}
                  {s.current && <span className="tag">this browser</span>}
                </>
              ),
            },
            { id: 'address', label: 'Address', render: (s) => <code>{s.remote_ip || '—'}</code> },
            { id: 'created', label: 'Signed in', render: (s) => when(s.created) },
            { id: 'seen', label: 'Last active', render: (s) => when(s.last_seen) },
            {
              id: 'actions',
              label: 'Actions',
              render: (s) => (
                <button
                  type="button"
                  className="ghost small"
                  disabled={busy !== null}
                  aria-label={
                    s.current
                      ? 'Sign out this browser'
                      : `End session from ${s.remote_ip || 'unknown address'}`
                  }
                  onClick={() => void revoke(s)}
                >
                  {s.current ? 'Sign out' : 'End session'}
                </button>
              ),
            },
          ]}
        />
        {others.length === 0 && <p className="empty">No other sessions.</p>}
        <p className="footnote">
          Last activity is updated at most every 30 seconds. Session cookies are never stored; the server
          keeps only a SHA-256 hash of each.
        </p>
      </section>
    </div>
  );
}
