// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getMergerFS, type MergerFSInventory, type MergerFSToolItem } from '../api';
import { InstallPlanDialog } from '../InstallPlanDialog';
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
  const { tooling, mergerfs, snapraid, kernelFuse } = data;
  const missingRequired = tooling.required.filter((t) => !t.installed);
  const mergerfsTool = tooling.required.find((t) => t.id === 'mergerfs');
  const mergerfsMissing = mergerfsTool ? !mergerfsTool.installed : false;
  const optional = tooling.optional.find((t) => t.id === 'snapraid');
  const optionalMissing = optional ? !optional.installed : false;
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">MergerFS</h1>
        <p className="page-sub">
          Read-only observations. Pooling is not RAID. FUSE3 fusermount3 and kernel FUSE are probed here; this
          page does not load modules or change mounts.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}

      <section className="panel" aria-labelledby="mergerfs-fuse-heading">
        <header className="panel-head">
          <h2 id="mergerfs-fuse-heading">Kernel FUSE</h2>
        </header>
        {kernelFuse.present ? (
          <p>
            Kernel FUSE is listed in <code>/proc/filesystems</code>.
          </p>
        ) : (
          <p className="banner" role="status">
            {kernelFuse.reason || 'The kernel FUSE filesystem is not listed in /proc/filesystems.'} MergerFS
            cannot create mounts without it. This page does not load kernel modules.
          </p>
        )}
      </section>

      <section className="panel" aria-labelledby="mergerfs-pools-heading">
        <header className="panel-head">
          <h2 id="mergerfs-pools-heading">Pools</h2>
          {mergerfs.pools.length > 0 && <span className="tag">observed</span>}
        </header>
        {mergerfs.pools.length > 0 ? (
          <>
            {missingRequired.length > 0 && (
              <div className="banner" role="status">
                {mergerfsMissing ? (
                  <>
                    The mergerfs binary was not found in <code>/usr/bin</code> or <code>/usr/sbin</code>;
                    observed pools are listed below.
                  </>
                ) : (
                  <>Required FUSE tooling is missing; observed pools are listed below.</>
                )}
                <RequiredOffers items={missingRequired} canInstall={canInstall} />
              </div>
            )}
            <Pools pools={mergerfs.pools} />
          </>
        ) : mergerfsMissing ? (
          <div className="empty" role="status">
            <p>MergerFS is not available — {mergerfs.reason || 'the mergerfs binary is missing'}.</p>
            <RequiredOffers items={missingRequired} canInstall={canInstall} />
          </div>
        ) : mergerfs.present ? (
          <>
            <p className="empty">No mergerfs pools found.</p>
            <RequiredOffers items={missingRequired} canInstall={canInstall} />
          </>
        ) : (
          <p className="banner" role="alert">
            Mount inventory could not be read; observed pools cannot be listed.
          </p>
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

function RequiredOffers({ items, canInstall }: { items: MergerFSToolItem[]; canInstall: boolean }) {
  if (items.length === 0) return null;
  return (
    <>
      {items.map((item) => (
        <InstallOffer
          key={item.id}
          label={`Install ${item.package || item.id}`}
          item={item}
          canInstall={canInstall}
        />
      ))}
    </>
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
  const [open, setOpen] = useState(false);
  const pkg = item?.package ?? '';
  if (!pkg) {
    return <p>Software Center cannot determine the package name for this distribution (verify).</p>;
  }
  if (!canInstall) {
    return (
      <p>
        To install <code>{pkg}</code>, the Software Center module must be enabled and your account needs the{' '}
        <code>packages.read</code> permission.
      </p>
    );
  }
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        {label}
      </button>
      {open && <InstallPlanDialog title={label} pkg={pkg} onClose={() => setOpen(false)} />}
    </>
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
