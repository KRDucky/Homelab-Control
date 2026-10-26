// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { Icon } from './icons';

import { useEffect, useRef, useState, type ReactNode } from 'react';

export interface SectionMeta {
  status: 'ok' | 'partial' | 'unavailable';
  sources: string[];
  errors?: string[];
  notes?: string[];
}

export function Sources({ meta }: { meta: SectionMeta }) {
  // Notes of an unavailable section are already shown in the panel body.
  const problems = [...(meta.errors ?? []), ...(meta.status === 'unavailable' ? [] : (meta.notes ?? []))];
  return (
    <details className="sources">
      <summary>Where this data comes from</summary>
      <ul>
        {meta.sources.map((s) => (
          <li key={s}>
            <code>{s}</code>
          </li>
        ))}
      </ul>
      {problems.length > 0 && (
        <ul className="source-errors">
          {problems.map((e) => (
            <li key={e}>{e}</li>
          ))}
        </ul>
      )}
    </details>
  );
}

const statusText: Record<SectionMeta['status'], string> = {
  ok: '',
  partial: 'Partly available',
  unavailable: 'Unavailable',
};

/** A titled panel that always shows its data source and any availability problem. */
export function Section({
  title,
  meta,
  children,
  id,
  live,
}: {
  title: string;
  meta: SectionMeta;
  children: ReactNode;
  id?: string;
  live?: { state: LiveState; updated?: string; onRetry?: () => void };
}) {
  const hid = `h-${id ?? title.replace(/\W+/g, '-').toLowerCase()}`;
  const unavailableNotes = meta.status === 'unavailable' ? (meta.notes ?? []) : [];
  return (
    <section
      className={`panel${live?.state === 'stale' || live?.state === 'disconnected' ? ' hc-stale' : ''}`}
      aria-labelledby={hid}
    >
      <header className="panel-head">
        <h2 id={hid}>{title}</h2>
        {live && <LiveIndicator {...live} />}
        {meta.status !== 'ok' && (
          <span className={`status status-${meta.status}`}>{statusText[meta.status]}</span>
        )}
      </header>
      {unavailableNotes.length > 0 && <p className="empty">{unavailableNotes.join(' ')}</p>}
      {children}
      <Sources meta={meta} />
    </section>
  );
}

/** Small state indicator; the text carries the meaning, colour only reinforces it. */
export function StateDot({ tone }: { tone: 'ok' | 'warn' | 'crit' | 'idle' }) {
  return <span className={`dot dot-${tone}`} aria-hidden="true" />;
}

/**
 * Wrapper for a table that may scroll (narrow screens, long logs). When it does scroll it becomes a
 * labelled, focusable region, so keyboard users can scroll it with the arrow keys (WCAG 2.1.1).
 */
export function TableRegion({
  label,
  className,
  children,
}: {
  label: string;
  className?: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [scrolls, setScrolls] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const check = () =>
      setScrolls(el.scrollWidth > el.clientWidth + 1 || el.scrollHeight > el.clientHeight + 1);
    check();
    if (typeof ResizeObserver !== 'function') return;
    const ro = new ResizeObserver(check);
    ro.observe(el);
    if (el.firstElementChild) ro.observe(el.firstElementChild);
    return () => ro.disconnect();
  }, []);
  return (
    <div
      ref={ref}
      className={className ? `table-wrap ${className}` : 'table-wrap'}
      role={scrolls ? 'region' : undefined}
      aria-label={scrolls ? label : undefined}
      tabIndex={scrolls ? 0 : undefined}
    >
      {children}
    </div>
  );
}

export type StatusTone = 'ok' | 'warn' | 'crit' | 'info' | 'notice' | 'unknown';
const statusIcons = {
  ok: 'check',
  warn: 'warning',
  crit: 'critical',
  info: 'info',
  notice: 'info',
  unknown: 'unknown',
} as const;
export function StatusPill({ tone, label }: { tone: StatusTone; label: string }) {
  return (
    <span className={`hc-status-pill hc-status-${tone}`}>
      <Icon name={statusIcons[tone]} />
      {label}
    </span>
  );
}
export function StatusIndicator({ tone, label }: { tone: StatusTone; label: string }) {
  return (
    <span className={`hc-status-indicator hc-status-${tone}`}>
      <Icon name={statusIcons[tone]} />
      {label}
    </span>
  );
}
export function EmptyState({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="empty" role="status">
      <strong>{title}</strong>
      {children && <p>{children}</p>}
    </div>
  );
}
export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="banner" role="alert">
      <p>{message}</p>
      {onRetry && (
        <button type="button" className="ghost" onClick={onRetry}>
          Try again
        </button>
      )}
    </div>
  );
}
export type LiveState = 'live' | 'refreshing' | 'stale' | 'paused' | 'disconnected';
export function LiveIndicator({
  state,
  updated,
  onRetry,
}: {
  state: LiveState;
  updated?: string;
  onRetry?: () => void;
}) {
  return (
    <span className="live-indicator">
      <span role="status">{state.charAt(0).toUpperCase() + state.slice(1)}</span>
      {updated && <time dateTime={updated}>Updated {new Date(updated).toLocaleTimeString()}</time>}
      {onRetry && (state === 'stale' || state === 'disconnected') && (
        <button type="button" className="ghost small" onClick={onRetry}>
          Retry
        </button>
      )}
    </span>
  );
}
