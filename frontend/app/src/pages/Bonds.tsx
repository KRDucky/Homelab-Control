// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback } from 'react';
import { getBonds, type Bond, type BondRenderer, type BondSnapshot } from '../api';
import { explain, useData } from '../useData';

/**
 * Bonds / LACP (M11 detect, read-only). Kernel bonding devices from sysfs and
 * the configuration stack observed to manage each one. There is no create,
 * change or remove action; the bonding driver is never loaded from here.
 */
export function Bonds({ csrf, onUnauthorized }: { csrf: string; onUnauthorized: () => void }) {
  const fetchBonds = useCallback(() => getBonds(csrf), [csrf]);
  const { data, error } = useData(fetchBonds, { onUnauthorized });
  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">Bonds / LACP</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading bond inventory…'}</p>
      </div>
    );
  }
  return <BondsView data={data} error={error} />;
}

function rendererText(r: BondRenderer): string {
  if (r.id === 'unknown') return 'Not identified (no runtime state file names this bond)';
  return `${r.id}${r.via ? ` via ${r.via}` : ''}${r.source ? ` — observed in ${r.source}` : ''}`;
}

export function BondsView({ data, error }: { data: BondSnapshot; error: Error | null }) {
  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Bonds / LACP</h1>
        <p className="page-sub">
          Read-only detection of kernel bonds from /sys/class/net. Bonds are managed by the host's own network
          configuration stack; this page does not create, change or remove them.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}
      <p>
        Configuration stacks running:{' '}
        {data.stacks.length > 0 ? data.stacks.join(', ') : 'none detected from runtime state'}.
      </p>
      {data.truncated && (
        <p className="banner" role="status">
          The inventory hit a size limit; more interfaces may exist than are shown.
        </p>
      )}
      {data.bonds.length === 0 ? (
        <p className="empty">
          {data.driverLoaded
            ? 'The bonding driver is loaded, but no bond devices exist.'
            : 'The kernel bonding driver is not loaded, so no bonds exist. This page does not load it.'}
        </p>
      ) : (
        data.bonds.map((b) => <BondPanel key={b.name} bond={b} />)
      )}
    </div>
  );
}

function BondPanel({ bond: b }: { bond: Bond }) {
  const id = `bond-${b.name}-heading`;
  return (
    <section className="panel" aria-labelledby={id}>
      <header className="panel-head">
        <h2 id={id}>
          <code>{b.name}</code>
        </h2>
        <span className="tag">{b.lacp ? 'LACP (802.3ad)' : b.mode || 'mode unknown'}</span>
        <span className="tag">{b.operState || 'state unknown'}</span>
      </header>
      <dl>
        <dt>Managed by</dt>
        <dd>
          {rendererText(b.renderer)} <span className="muted">(confidence: {b.renderer.confidence})</span>
        </dd>
        {b.miimonMs && (
          <>
            <dt>Link monitoring</dt>
            <dd>every {b.miimonMs} ms</dd>
          </>
        )}
        {b.activeSlave && (
          <>
            <dt>Active port</dt>
            <dd>
              <code>{b.activeSlave}</code>
            </dd>
          </>
        )}
        {b.lacp && (
          <>
            <dt>LACP rate</dt>
            <dd>{b.lacpRate || 'Not reported'}</dd>
            <dt>Transmit hash</dt>
            <dd>{b.xmitHashPolicy || 'Not reported'}</dd>
            <dt>LACP partner</dt>
            <dd>
              {b.partnerMac ? (
                <code>{b.partnerMac}</code>
              ) : (
                'No partner has answered. The switch ports may not be configured for LACP.'
              )}
            </dd>
          </>
        )}
      </dl>
      {b.slaves.length > 0 ? (
        <table>
          <caption>Ports of {b.name}</caption>
          <thead>
            <tr>
              <th scope="col">Port</th>
              <th scope="col">Role</th>
              <th scope="col">Link</th>
              <th scope="col">Speed</th>
              <th scope="col">Link failures</th>
              {b.lacp && <th scope="col">Aggregator</th>}
            </tr>
          </thead>
          <tbody>
            {b.slaves.map((s) => {
              const split = b.lacp && b.aggregatorId && s.aggregatorId && s.aggregatorId !== b.aggregatorId;
              return (
                <tr key={s.name}>
                  <th scope="row">
                    <code>{s.name}</code>
                  </th>
                  <td>{s.state || 'Not reported'}</td>
                  <td>{s.miiStatus || 'Not reported'}</td>
                  <td>{s.speed ? `${s.speed} Mb/s` : 'Unknown'}</td>
                  <td>{s.linkFailures}</td>
                  {b.lacp && (
                    <td>
                      {s.aggregatorId || '—'}
                      {split ? ' (not in the active aggregator)' : ''}
                    </td>
                  )}
                </tr>
              );
            })}
          </tbody>
        </table>
      ) : (
        <p className="empty">This bond has no ports.</p>
      )}
    </section>
  );
}
