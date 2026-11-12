// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback } from 'react';
import { getMergerFS, type MergerFSInventory } from '../api';
import { explain, useData } from '../useData';

/**
 * MergerFS + optional SnapRAID (M8 first checkpoint, read-only). One page:
 * observed fuse.mergerfs pools, and a SnapRAID section only when a config is
 * present. No pool create, mount, policy write, or snapraid sync/scrub/fix
 * control exists in the DOM, not even disabled.
 */
export function MergerFS({ csrf, onUnauthorized }: { csrf: string; onUnauthorized: () => void }) {
  const fetchInventory = useCallback(() => getMergerFS(csrf), [csrf]);
  const { data, error } = useData(fetchInventory, { onUnauthorized });

  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">MergerFS</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading MergerFS inventory…'}</p>
      </div>
    );
  }
  return <MergerFSView data={data} error={error} />;
}

export function MergerFSView({ data, error }: { data: MergerFSInventory; error: Error | null }) {
  const { mergerfs, snapraid } = data;
  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">MergerFS</h1>
        <p className="page-sub">
          Read-only observations. No pool, mount or SnapRAID configuration is changed here.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}

      <section className="panel" aria-labelledby="mergerfs-pools-heading">
        <header className="panel-head">
          <h2 id="mergerfs-pools-heading">Pools</h2>
        </header>
        {mergerfs.present ? (
          <Pools pools={mergerfs.pools} />
        ) : (
          <div className="empty" role="status">
            <p>MergerFS is not available — {mergerfs.reason || 'no fuse.mergerfs mounts found'}.</p>
            <p>
              To use pools, install the <em>mergerfs</em> package for your distribution (name labelled{' '}
              <em>verify</em>).
            </p>
          </div>
        )}
      </section>

      {snapraid.present && (
        <section className="panel" aria-labelledby="snapraid-heading">
          <header className="panel-head">
            <h2 id="snapraid-heading">SnapRAID</h2>
          </header>
          <p>
            SnapRAID is optional. Read-only view of <code>{snapraid.configPath}</code>.
          </p>
          <PathList title="Data" paths={snapraid.data} />
          <PathList title="Parity" paths={snapraid.parity} />
          <PathList title="Content" paths={snapraid.content} />
        </section>
      )}
    </div>
  );
}

function Pools({ pools }: { pools: NonNullable<MergerFSInventory['mergerfs']['pools']> }) {
  return (
    <ul className="pool-list">
      {pools.map((pool) => (
        <li key={pool.mountpoint}>
          <code>{pool.mountpoint}</code>
          <dl>
            <dt>Branches</dt>
            <dd>{pool.branches.length > 0 ? pool.branches.join(', ') : '—'}</dd>
            <dt>Options</dt>
            <dd>{pool.options.length > 0 ? pool.options.join(', ') : '—'}</dd>
          </dl>
        </li>
      ))}
    </ul>
  );
}

function PathList({ title, paths }: { title: string; paths: string[] }) {
  if (paths.length === 0) return null;
  return (
    <div>
      <h3>{title}</h3>
      <ul>
        {paths.map((p) => (
          <li key={p}>
            <code>{p}</code>
          </li>
        ))}
      </ul>
    </div>
  );
}
