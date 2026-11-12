// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback } from 'react';
import { getMergerFS, type MergerFSInventory, type MergerFSToolItem } from '../api';
import { linkHandler } from '../router';
import { explain, useData } from '../useData';

/**
 * MergerFS + optional SnapRAID (M8 first checkpoint, read-only). One page:
 * probe required/optional tooling first, show an install offer (a Software
 * Center search link, never a package transaction) when a required package is
 * missing, hide pools/policy until mergerfs is present, and show a SnapRAID
 * config block only when a config is present. No pool create, mount, policy
 * write, or snapraid sync/scrub/fix control exists in the DOM, not even
 * disabled.
 */
export function MergerFS({
  csrf,
  scopes = [],
  packagesEnabled,
  onUnauthorized,
}: {
  csrf: string;
  scopes?: string[];
  packagesEnabled: boolean;
  onUnauthorized: () => void;
}) {
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
  return <MergerFSView data={data} error={error} scopes={scopes} packagesEnabled={packagesEnabled} />;
}

export function MergerFSView({
  data,
  error,
  scopes = [],
  packagesEnabled,
}: {
  data: MergerFSInventory;
  error: Error | null;
  scopes?: string[];
  packagesEnabled: boolean;
}) {
  const { tooling, mergerfs, snapraid } = data;
  const required = tooling.required.find((t) => t.id === 'mergerfs');
  const optional = tooling.optional.find((t) => t.id === 'snapraid');
  const requiredMissing = required ? !required.installed : false;
  const optionalMissing = optional ? !optional.installed : false;
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

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
        {requiredMissing ? (
          <div className="empty" role="status">
            <p>MergerFS is not available — {mergerfs.reason || 'the mergerfs binary is missing'}.</p>
            <InstallOffer
              label={`Install ${required?.package || 'mergerfs'}`}
              item={required}
              canInstall={canInstall}
            />
          </div>
        ) : mergerfs.pools.length > 0 ? (
          <Pools pools={mergerfs.pools} />
        ) : (
          <p className="empty">No mergerfs pools found.</p>
        )}
      </section>

      {optionalMissing && (
        <section className="panel" aria-labelledby="snapraid-offer-heading">
          <header className="panel-head">
            <h2 id="snapraid-offer-heading">SnapRAID</h2>
          </header>
          <p>SnapRAID is optional.</p>
          <InstallOffer
            label={`Add ${optional?.package || 'snapraid'}`}
            item={optional}
            canInstall={canInstall}
          />
        </section>
      )}

      {snapraid.present && (
        <section className="panel" aria-labelledby="snapraid-heading">
          <header className="panel-head">
            <h2 id="snapraid-heading">SnapRAID configuration</h2>
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

function InstallOffer({
  label,
  item,
  canInstall,
}: {
  label: string;
  item: MergerFSToolItem | undefined;
  canInstall: boolean;
}) {
  const pkg = item?.package ?? '';
  if (!pkg) {
    return <p>Software Center cannot determine the package name for this distribution (verify).</p>;
  }
  if (!canInstall) {
    return (
      <p>
        Install requires the Software Center module to be enabled and the <code>packages.read</code>{' '}
        permission on your account.
      </p>
    );
  }
  const path = `/software?q=${encodeURIComponent(pkg)}`;
  return (
    <a href={path} onClick={linkHandler(path)}>
      {label}
    </a>
  );
}

function Pools({ pools }: { pools: MergerFSInventory['mergerfs']['pools'] }) {
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
