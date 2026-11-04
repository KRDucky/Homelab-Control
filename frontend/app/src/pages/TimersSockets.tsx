// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { getSockets, getTimers, type SocketInfo, type TimerInfo } from '../api';
import { DataTable } from '../DataTable';
import { LiveIndicator, StatusIndicator, type StatusTone } from '../ui';
import { explain, useData } from '../useData';

// M2-3 read-only views of timer and socket units. Values come from systemd;
// anything it did not report is shown as unknown, never filled in.

function tone(active: string): StatusTone {
  if (active === 'failed') return 'crit';
  if (active === 'active') return 'ok';
  if (active === 'activating' || active === 'deactivating') return 'warn';
  return 'unknown';
}

function when(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString();
}

function Sources({ source, truncated }: { source: string; truncated: boolean }) {
  return (
    <>
      {truncated && <p className="footnote">The list was cut at 500 entries.</p>}
      <details className="sources">
        <summary>Where this data comes from</summary>
        <p>
          <code>{source}</code>, via the privileged helper.
        </p>
      </details>
    </>
  );
}

export function TimersView({
  onUnauthorized,
  onDetail,
}: {
  onUnauthorized: () => void;
  onDetail: (u: string) => void;
}) {
  const { data, error, live } = useData(getTimers, { intervalMs: 30_000, onUnauthorized });
  if (error && !data)
    return (
      <p className="banner" role="alert">
        {explain(error)}
      </p>
    );
  if (!data) return <p className="muted">Reading timer units…</p>;
  return (
    <>
      <LiveIndicator {...live} />
      <DataTable<TimerInfo>
        caption="Timers"
        rows={data.timers}
        rowKey={(t) => t.unit}
        rowTone={(t) => (t.active_state === 'failed' ? 'crit' : undefined)}
        emptyState="systemd reports no timer units."
        columns={[
          {
            id: 'unit',
            label: 'Timer',
            rowHeader: true,
            sortValue: (t) => t.unit,
            render: (t) => (
              <button type="button" className="link-button" onClick={() => onDetail(t.unit)}>
                <code>{t.unit}</code>
              </button>
            ),
          },
          {
            id: 'state',
            label: 'State',
            sortValue: (t) => t.active_state,
            render: (t) => (
              <>
                <StatusIndicator tone={tone(t.active_state)} label={t.active_state} />
                {t.sub_state ? <span className="muted"> ({t.sub_state})</span> : null}
              </>
            ),
          },
          {
            id: 'schedule',
            label: 'Schedule',
            render: (t) =>
              t.schedule && t.schedule.length > 0 ? (
                <ul className="plain-list compact">
                  {t.schedule.map((s) => (
                    <li key={s}>
                      <code>{s}</code>
                    </li>
                  ))}
                </ul>
              ) : (
                '—'
              ),
          },
          { id: 'next', label: 'Next run', sortValue: (t) => t.next ?? '', render: (t) => when(t.next) },
          { id: 'last', label: 'Last run', sortValue: (t) => t.last ?? '', render: (t) => when(t.last) },
          {
            id: 'activates',
            label: 'Activates',
            sortValue: (t) => t.activates ?? '',
            render: (t) => (t.activates ? <code>{t.activates}</code> : '—'),
          },
          {
            id: 'persistent',
            label: 'Catch up missed runs',
            render: (t) => (t.persistent === undefined ? 'unknown' : t.persistent ? 'yes' : 'no'),
          },
          {
            id: 'boot',
            label: 'At boot',
            sortValue: (t) => t.unit_file_state ?? '',
            render: (t) => t.unit_file_state ?? '—',
          },
        ]}
      />
      <Sources source={data.source} truncated={data.truncated} />
    </>
  );
}

export function SocketsView({
  onUnauthorized,
  onDetail,
}: {
  onUnauthorized: () => void;
  onDetail: (u: string) => void;
}) {
  const { data, error, live } = useData(getSockets, { intervalMs: 30_000, onUnauthorized });
  if (error && !data)
    return (
      <p className="banner" role="alert">
        {explain(error)}
      </p>
    );
  if (!data) return <p className="muted">Reading socket units…</p>;
  return (
    <>
      <LiveIndicator {...live} />
      <DataTable<SocketInfo>
        caption="Sockets"
        rows={data.sockets}
        rowKey={(s) => s.unit}
        rowTone={(s) => (s.active_state === 'failed' ? 'crit' : undefined)}
        emptyState="systemd reports no socket units."
        columns={[
          {
            id: 'unit',
            label: 'Socket',
            rowHeader: true,
            sortValue: (s) => s.unit,
            render: (s) => (
              <button type="button" className="link-button" onClick={() => onDetail(s.unit)}>
                <code>{s.unit}</code>
              </button>
            ),
          },
          {
            id: 'state',
            label: 'State',
            sortValue: (s) => s.active_state,
            render: (s) => (
              <>
                <StatusIndicator tone={tone(s.active_state)} label={s.active_state} />
                {s.sub_state ? <span className="muted"> ({s.sub_state})</span> : null}
              </>
            ),
          },
          {
            id: 'listen',
            label: 'Listens on',
            render: (s) => (
              <ul className="plain-list compact">
                {s.listen.map((l) => (
                  <li key={l}>
                    <code>{l}</code>
                  </li>
                ))}
              </ul>
            ),
          },
          {
            id: 'activates',
            label: 'Activates',
            render: (s) =>
              s.activates && s.activates.length > 0 ? s.activates.map((a) => <code key={a}>{a} </code>) : '—',
          },
          {
            id: 'boot',
            label: 'At boot',
            sortValue: (s) => s.unit_file_state ?? '',
            render: (s) => s.unit_file_state ?? '—',
          },
        ]}
      />
      <Sources source={data.source} truncated={data.truncated} />
    </>
  );
}
