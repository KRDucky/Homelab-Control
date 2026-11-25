// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import {
  getZFS,
  type ZFSDataset,
  type ZFSInventory,
  type ZFSPool,
  type ZFSToolItem,
  type ZFSVdev,
} from '../api';
import { DataTable } from '../DataTable';
import { formatBytes } from '../format';
import { Meter } from '../Meter';
import { InstallPlanDialog } from '../InstallPlanDialog';
import { explain, useData } from '../useData';

/**
 * OpenZFS (M7 first checkpoint, read-only). Probe required tooling first,
 * show a Software Center search link when a Debian/Ubuntu package is
 * missing, and list observed zfs mounts. No pool create, import, export,
 * or zfs/zpool execution exists in the DOM.
 */
export function ZFS({
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
  const fetchInventory = useCallback(() => getZFS(csrf), [csrf]);
  const { data, error } = useData(fetchInventory, { onUnauthorized });

  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">ZFS</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading ZFS inventory…'}</p>
      </div>
    );
  }
  return <ZFSView data={data} error={error} scopes={scopes} packagesEnabled={packagesEnabled} />;
}

export function ZFSView({
  data,
  error,
  scopes = [],
  packagesEnabled,
}: {
  data: ZFSInventory;
  error: Error | null;
  scopes?: string[];
  packagesEnabled: boolean;
}) {
  const { tooling, zfs } = data;
  const kernelZfs = data.kernelZfs;
  const missingRequired = tooling.required.filter((t) => !t.installed);
  const report = data.status?.available ? data.status.report : undefined;
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">ZFS</h1>
        <p className="page-sub">
          Read-only. Pools, health, scrub status and the vdev tree come from <code>zpool list</code>,{' '}
          <code>zpool status</code> and <code>zfs list</code>, run by the read-only storage helper. This page
          does not import, export, create, scrub or destroy pools, and it does not load kernel modules.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}

      <section className="panel" aria-labelledby="zfs-kernel-heading">
        <header className="panel-head">
          <h2 id="zfs-kernel-heading">Kernel ZFS</h2>
        </header>
        {kernelZfs?.present ? (
          <p>
            Kernel ZFS is listed in <code>/proc/filesystems</code>.
          </p>
        ) : (
          <p className="banner" role="status">
            {kernelZfs?.reason ||
              'The kernel ZFS filesystem is not currently listed in /proc/filesystems (this page does not load modules).'}
          </p>
        )}
      </section>

      {missingRequired.length > 0 && (
        <section className="panel" aria-labelledby="zfs-offer-heading">
          <header className="panel-head">
            <h2 id="zfs-offer-heading">OpenZFS tools</h2>
          </header>
          <p>zfs and zpool are required. This page does not run them or add third-party repositories.</p>
          <RequiredOffers items={missingRequired} canInstall={canInstall} />
        </section>
      )}

      <section className="panel" aria-labelledby="zfs-pools-heading">
        <header className="panel-head">
          <h2 id="zfs-pools-heading">Pools</h2>
          {report && <span className="tag">observed</span>}
        </header>
        {!data.status?.available ? (
          <p className="banner" role="status">
            {data.status?.reason || 'Pool status is unavailable.'}
          </p>
        ) : report && report.pools.length > 0 ? (
          report.pools.map((pool) => <Pool key={pool.name} pool={pool} />)
        ) : (
          <p className="empty">No pools are imported.</p>
        )}
        {report?.truncated && (
          <p className="banner" role="status">
            The ZFS report exceeded its limits; some pools, devices or datasets are not listed.
          </p>
        )}
      </section>

      {report && report.datasets.length > 0 && (
        <section className="panel" aria-labelledby="zfs-datasets-heading">
          <header className="panel-head">
            <h2 id="zfs-datasets-heading">Datasets</h2>
            <span className="tag">observed</span>
          </header>
          <Datasets datasets={report.datasets} />
        </section>
      )}

      {!report && (
        <section className="panel" aria-labelledby="zfs-mounts-heading">
          <header className="panel-head">
            <h2 id="zfs-mounts-heading">Mounted datasets</h2>
            {zfs.mounts.length > 0 && <span className="tag">observed</span>}
          </header>
          {zfs.mountsTruncated ? (
            <p className="banner" role="status">
              too many mounts to list
            </p>
          ) : null}
          {zfs.mounts.length > 0 ? (
            <ul className="pool-list">
              {zfs.mounts.map((m) => (
                <li key={`${m.mountpoint}:${m.dataset}`}>
                  <code>{m.mountpoint}</code>
                  <dl>
                    <dt>Dataset</dt>
                    <dd>
                      <code>{m.dataset}</code>
                    </dd>
                  </dl>
                </li>
              ))}
            </ul>
          ) : (
            <p className="empty">
              {zfs.present ? 'No zfs mounts found.' : zfs.reason || 'No zfs mounts found.'}
            </p>
          )}
        </section>
      )}
    </div>
  );
}

function RequiredOffers({ items, canInstall }: { items: ZFSToolItem[]; canInstall: boolean }) {
  if (items.length === 0) return null;
  const seen = new Set<string>();
  return (
    <>
      {items.map((item) => {
        const key = item.package || '__no-package__';
        if (seen.has(key)) return null;
        seen.add(key);
        return (
          <InstallOffer
            key={item.id}
            label={`Install ${item.package || item.id}`}
            item={item}
            canInstall={canInstall}
          />
        );
      })}
    </>
  );
}

function InstallOffer({
  label,
  item,
  canInstall,
}: {
  label: string;
  item: ZFSToolItem | undefined;
  canInstall: boolean;
}) {
  const [open, setOpen] = useState(false);
  const pkg = item?.package ?? '';
  if (!pkg) {
    return (
      <p>
        Software Center cannot determine the package name for this distribution (OpenZFS may need a
        third-party repository; Homelab Control will not add one).
      </p>
    );
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

const healthTone: Record<string, string> = {
  ONLINE: 'ok',
  DEGRADED: 'warn',
  FAULTED: 'crit',
  UNAVAIL: 'crit',
  SUSPENDED: 'crit',
};

/** One pool: capacity, the zpool status report lines and the vdev tree, as zpool reports them. */
function Pool({ pool }: { pool: ZFSPool }) {
  return (
    <article className="zfs-pool" aria-label={`Pool ${pool.name}`}>
      <h3 className="subhead">
        <code>{pool.name}</code>{' '}
        <span className={`status status-${healthTone[pool.health] ?? 'warn'}`}>{pool.health}</span>
      </h3>
      {pool.size !== undefined && pool.alloc !== undefined && (
        <Meter
          label="Allocated"
          used={pool.alloc}
          total={pool.size}
          detail={`${formatBytes(pool.free ?? 0)} free of ${formatBytes(pool.size)}${pool.frag_pct !== undefined ? ` · ${pool.frag_pct}% fragmented` : ''}`}
        />
      )}
      <dl className="facts">
        {pool.status && (
          <div>
            <dt>Status</dt>
            <dd>{pool.status}</dd>
          </div>
        )}
        {pool.action && (
          <div>
            <dt>Action</dt>
            <dd>{pool.action}</dd>
          </div>
        )}
        <div>
          <dt>Last scrub or resilver</dt>
          <dd>{pool.scan || 'None recorded'}</dd>
        </div>
        <div>
          <dt>Errors</dt>
          <dd>{pool.errors || 'Not reported'}</dd>
        </div>
      </dl>
      <DataTable<ZFSVdev & { key: string }>
        caption={`Devices of ${pool.name}`}
        rows={pool.vdevs.map((v, i) => ({ ...v, key: `${i}:${v.name}` }))}
        rowKey={(v) => v.key}
        columns={[
          {
            id: 'name',
            label: 'Device',
            render: (v) => (
              <span className={`vdev-depth-${Math.min(v.depth, 4)}`}>
                {v.group ? <strong>{v.name}</strong> : <code>{v.name}</code>}
              </span>
            ),
          },
          { id: 'state', label: 'State', render: (v) => v.state ?? '' },
          { id: 'read', label: 'Read errors', render: (v) => v.read_errors ?? '' },
          { id: 'write', label: 'Write errors', render: (v) => v.write_errors ?? '' },
          { id: 'cksum', label: 'Checksum errors', render: (v) => v.checksum_errors ?? '' },
          { id: 'note', label: 'Note', render: (v) => v.note ?? '' },
        ]}
      />
    </article>
  );
}

function Datasets({ datasets }: { datasets: ZFSDataset[] }) {
  const size = (v?: number) => (v === undefined ? 'Not reported' : formatBytes(v));
  return (
    <DataTable
      caption="Datasets and volumes"
      rows={datasets}
      rowKey={(d) => d.name}
      columns={[
        { id: 'name', label: 'Name', render: (d) => <code>{d.name}</code>, sortValue: (d) => d.name },
        { id: 'type', label: 'Type', render: (d) => d.type },
        { id: 'used', label: 'Used', render: (d) => size(d.used), sortValue: (d) => d.used ?? -1 },
        { id: 'avail', label: 'Available', render: (d) => size(d.avail), sortValue: (d) => d.avail ?? -1 },
        { id: 'refer', label: 'Referenced', render: (d) => size(d.refer) },
        { id: 'mountpoint', label: 'Mountpoint', render: (d) => <code>{d.mountpoint ?? ''}</code> },
        { id: 'compression', label: 'Compression', render: (d) => d.compression ?? '' },
      ]}
    />
  );
}
