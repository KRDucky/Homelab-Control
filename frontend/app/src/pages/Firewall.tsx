// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback } from 'react';
import {
  getFirewall,
  type FirewalldView,
  type FirewallSnapshot,
  type IptablesView,
  type NftablesView,
  type UfwView,
} from '../api';
import { explain, useData } from '../useData';

/**
 * Firewall (M11, read-only). Shows only the active backend, with the other
 * installed stacks listed as "installed, not active". The data is the saved
 * configuration, not the live kernel ruleset. No rule can be changed here.
 */
export function Firewall({ csrf, onUnauthorized }: { csrf: string; onUnauthorized: () => void }) {
  const fetchFirewall = useCallback(() => getFirewall(csrf), [csrf]);
  const { data, error } = useData(fetchFirewall, { onUnauthorized });
  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">Firewall</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading firewall configuration…'}</p>
      </div>
    );
  }
  return <FirewallView data={data} error={error} />;
}

const NAMES: Record<string, string> = {
  firewalld: 'firewalld',
  ufw: 'ufw',
  nftables: 'nftables',
  iptables: 'iptables (saved rules)',
};

export function FirewallView({ data, error }: { data: FirewallSnapshot; error: Error | null }) {
  const others = data.backends.filter((b) => b.role === 'installed');
  const title =
    data.active === 'none'
      ? 'No active firewall service'
      : data.active === 'unknown'
        ? 'Active firewall unknown'
        : `Active: ${NAMES[data.active] ?? data.active}`;
  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Firewall</h1>
        <p className="page-sub">
          Read-only. This page shows the saved configuration of the active firewall service. It does not read
          the live kernel ruleset, and it cannot add, change or remove rules.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}
      <section className="panel" aria-labelledby="fw-active-heading">
        <header className="panel-head">
          <h2 id="fw-active-heading">{title}</h2>
          <span className="tag">saved configuration</span>
        </header>
        <p>{data.activeReason}</p>
        {data.notes.map((n) => (
          <p key={n} className="banner" role="status">
            {n}
          </p>
        ))}
        {others.length > 0 && (
          <p>
            Installed, not active:{' '}
            {others.map((b) => `${NAMES[b.id] ?? b.id}${b.enabled ? ` (unit ${b.enabled})` : ''}`).join(', ')}
            .
          </p>
        )}
      </section>
      {data.ufw && <UfwPanel v={data.ufw} />}
      {data.firewalld && <FirewalldPanel v={data.firewalld} />}
      {data.nftables && <NftPanel v={data.nftables} />}
      {data.iptables && <IptablesPanel v={data.iptables} />}
    </div>
  );
}

function Unreadable({ paths }: { paths: string[] }) {
  if (paths.length === 0) return null;
  return (
    <p className="banner" role="status">
      Could not read: {paths.join(', ')}. The view below may be incomplete.
    </p>
  );
}

function Truncated({ on }: { on: boolean }) {
  return on ? (
    <p className="banner" role="status">
      The configuration hit a size limit; not every rule is shown.
    </p>
  ) : null;
}

function UfwPanel({ v }: { v: UfwView }) {
  return (
    <section className="panel" aria-labelledby="fw-ufw-heading">
      <header className="panel-head">
        <h2 id="fw-ufw-heading">ufw rules</h2>
      </header>
      <Unreadable paths={v.unreadable} />
      <Truncated on={v.truncated} />
      <dl>
        <dt>Default incoming</dt>
        <dd>{v.defaultInput || 'Not set'}</dd>
        <dt>Default outgoing</dt>
        <dd>{v.defaultOutput || 'Not set'}</dd>
        <dt>Default routed</dt>
        <dd>{v.defaultForward || 'Not set'}</dd>
        <dt>Logging</dt>
        <dd>{v.logLevel || 'Not set'}</dd>
      </dl>
      {v.rules.length > 0 ? (
        <table>
          <caption>User rules in /etc/ufw/user.rules and user6.rules</caption>
          <thead>
            <tr>
              <th scope="col">Action</th>
              <th scope="col">Direction</th>
              <th scope="col">To</th>
              <th scope="col">From</th>
              <th scope="col">Comment</th>
            </tr>
          </thead>
          <tbody>
            {v.rules.map((r, i) => (
              <tr key={i}>
                <th scope="row">
                  {r.route ? 'route ' : ''}
                  {r.action}
                </th>
                <td>
                  {r.direction}
                  {r.interface ? ` on ${r.interface}` : ''}
                </td>
                <td>
                  <code>
                    {r.app ? r.app : `${r.toPort}/${r.proto}`} {r.to}
                  </code>
                </td>
                <td>
                  <code>
                    {r.from}
                    {r.fromPort !== 'any' ? `:${r.fromPort}` : ''}
                  </code>
                  {r.ipv6 ? ' (IPv6)' : ''}
                </td>
                <td>{r.comment || ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <p className="empty">No user rules are saved.</p>
      )}
    </section>
  );
}

function FirewalldPanel({ v }: { v: FirewalldView }) {
  return (
    <section className="panel" aria-labelledby="fw-firewalld-heading">
      <header className="panel-head">
        <h2 id="fw-firewalld-heading">firewalld zones</h2>
      </header>
      <Unreadable paths={v.unreadable} />
      <p>
        Default zone: <code>{v.defaultZone}</code>. Permanent configuration only; runtime-only changes and
        interfaces assigned by NetworkManager are not shown.
      </p>
      <ul className="pool-list">
        {v.zones.map((z) => (
          <li key={z.name}>
            <code>{z.name}</code>
            {z.default ? ' (default)' : ''} <span className="muted">from {z.source}</span>
            <dl>
              <dt>Target</dt>
              <dd>{z.target}</dd>
              <dt>Interfaces</dt>
              <dd>{z.interfaces.join(', ') || 'None in configuration'}</dd>
              <dt>Sources</dt>
              <dd>{z.sources.join(', ') || 'None'}</dd>
              <dt>Services</dt>
              <dd>{z.services.join(', ') || 'None'}</dd>
              <dt>Ports</dt>
              <dd>{z.ports.join(', ') || 'None'}</dd>
              <dt>Masquerade</dt>
              <dd>{z.masquerade ? 'Yes' : 'No'}</dd>
              {z.forwardPorts.length > 0 && (
                <>
                  <dt>Port forwards</dt>
                  <dd>{z.forwardPorts.join(', ')}</dd>
                </>
              )}
              {z.richRules.length > 0 && (
                <>
                  <dt>Rich rules</dt>
                  <dd>
                    <ul>
                      {z.richRules.map((r, i) => (
                        <li key={i}>
                          <code>{r}</code>
                        </li>
                      ))}
                    </ul>
                  </dd>
                </>
              )}
            </dl>
          </li>
        ))}
      </ul>
    </section>
  );
}

function NftPanel({ v }: { v: NftablesView }) {
  return (
    <section className="panel" aria-labelledby="fw-nft-heading">
      <header className="panel-head">
        <h2 id="fw-nft-heading">nftables</h2>
      </header>
      <Unreadable paths={v.unreadable} />
      <Truncated on={v.truncated} />
      <p>
        {v.file ? (
          <>
            Saved script <code>{v.file}</code>
            {v.flushRuleset ? ' (replaces the whole ruleset when loaded)' : ''}.
          </>
        ) : (
          'No saved nftables script was found.'
        )}
      </p>
      {v.includes.length > 0 && (
        <p>
          Includes (not shown): <code>{v.includes.join(', ')}</code>
        </p>
      )}
      {v.tables.map((t) => (
        <div key={`${t.family} ${t.name}`}>
          <h3>
            table <code>{`${t.family} ${t.name}`}</code>
          </h3>
          {t.chains.map((c) => (
            <details key={c.name}>
              <summary>
                chain <code>{c.name}</code>
                {c.hook ? ` — ${c.type ?? ''} hook ${c.hook}` : ''}
                {c.policy ? `, policy ${c.policy}` : ''} ({c.rules.length} rules)
              </summary>
              <ol>
                {c.rules.map((r, i) => (
                  <li key={i}>
                    <code>{r}</code>
                  </li>
                ))}
              </ol>
            </details>
          ))}
        </div>
      ))}
    </section>
  );
}

function IptablesPanel({ v }: { v: IptablesView }) {
  return (
    <section className="panel" aria-labelledby="fw-ipt-heading">
      <header className="panel-head">
        <h2 id="fw-ipt-heading">iptables saved rules</h2>
      </header>
      <Unreadable paths={v.unreadable} />
      <Truncated on={v.truncated} />
      {v.files.length === 0 && <p className="empty">No saved iptables rules file was found.</p>}
      {v.files.map((f) => (
        <div key={f.path}>
          <h3>
            <code>{f.path}</code>
            {f.ipv6 ? ' (IPv6)' : ''}
          </h3>
          {f.tables.map((t) =>
            t.chains.map((c) => (
              <details key={`${t.name}/${c.name}`}>
                <summary>
                  {t.name} / <code>{c.name}</code>
                  {c.policy !== '-' ? `, policy ${c.policy}` : ''} ({c.rules.length} rules)
                </summary>
                <ol>
                  {c.rules.map((r, i) => (
                    <li key={i}>
                      <code>{r}</code>
                    </li>
                  ))}
                </ol>
              </details>
            )),
          )}
        </div>
      ))}
    </section>
  );
}
