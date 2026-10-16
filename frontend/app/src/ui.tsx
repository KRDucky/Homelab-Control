// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import type { ReactNode } from 'react';

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
}: {
  title: string;
  meta: SectionMeta;
  children: ReactNode;
  id?: string;
}) {
  const hid = `h-${id ?? title.replace(/\W+/g, '-').toLowerCase()}`;
  const unavailableNotes = meta.status === 'unavailable' ? (meta.notes ?? []) : [];
  return (
    <section className="panel" aria-labelledby={hid}>
      <header className="panel-head">
        <h2 id={hid}>{title}</h2>
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
