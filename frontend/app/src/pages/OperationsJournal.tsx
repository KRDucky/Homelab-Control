// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { ApiError, getOperations, type OperationRecord } from '../api';
import { DataTable } from '../DataTable';
import { ErrorState, LiveIndicator } from '../ui';
import { explain, useData } from '../useData';

const outcomeText: Record<OperationRecord['outcome'], string> = {
  succeeded: 'Succeeded (verified)',
  failed: 'Failed',
  uncertain: 'Not verified',
  interrupted: 'Interrupted',
  pending: 'In progress',
};

function tone(o: OperationRecord['outcome']): 'crit' | 'warn' | undefined {
  if (o === 'failed' || o === 'interrupted') return 'crit';
  if (o === 'uncertain' || o === 'pending') return 'warn';
  return undefined;
}

/**
 * The broker's operation journal: every privileged operation with its
 * durable intent and observed outcome. Interrupted means the privileged helper
 * stopped between the two; such operations are never retried automatically.
 */
export function OperationsJournal({ onUnauthorized }: { onUnauthorized: () => void }) {
  const { data, error, loading, reload, live } = useData(getOperations, {
    intervalMs: 30_000,
    onUnauthorized,
  });
  return (
    <section className="panel" id="operations" aria-labelledby="h-ops" aria-busy={loading}>
      <header className="panel-head">
        <h2 id="h-ops">Privileged operations</h2>
        <LiveIndicator {...live} />
      </header>
      {error && (
        <ErrorState
          message={error instanceof ApiError && error.status === 503 ? error.message : explain(error)}
          onRetry={reload}
        />
      )}
      {(data || !error) && (
        <DataTable
          caption="Privileged operations"
          rows={data?.operations ?? []}
          rowKey={(o) => o.id}
          rowTone={(o) => tone(o.outcome)}
          emptyState={
            loading && !data ? 'Reading the operation journal…' : 'No privileged operations have run.'
          }
          columns={[
            {
              id: 'started',
              label: 'Started',
              render: (o) => new Date(o.started_at).toLocaleString(),
              sortValue: (o) => o.started_at,
            },
            {
              id: 'op',
              label: 'Operation',
              rowHeader: true,
              render: (o) => (
                <>
                  {o.action} <code>{o.unit}</code>
                </>
              ),
            },
            { id: 'outcome', label: 'Outcome', render: (o) => outcomeText[o.outcome] ?? o.outcome },
            { id: 'actor', label: 'User', render: (o) => o.actor },
            {
              id: 'observed',
              label: 'Observed state',
              render: (o) => (o.observed ? `${o.observed.active_state} (${o.observed.sub_state})` : '—'),
            },
            {
              id: 'detail',
              label: 'Detail',
              render: (o) => (
                <>
                  {o.message ?? ''} <span className="muted">[{o.id.slice(0, 12)}]</span>
                </>
              ),
            },
          ]}
        />
      )}
      <details className="sources">
        <summary>Where this data comes from</summary>
        <p>
          {data?.source ?? 'The broker operation journal'}:{' '}
          <code>/var/log/homelab-control-broker/operations.jsonl</code>. Check it on the server with{' '}
          <code>sudo hlctl audit verify --file /var/log/homelab-control-broker/operations.jsonl</code>.
        </p>
      </details>
    </section>
  );
}
