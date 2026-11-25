// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import {
  getBtrfs,
  type BtrfsDeviceStats,
  type BtrfsFilesystem,
  type BtrfsInventory,
  type BtrfsToolItem,
} from '../api';
import { DataTable } from '../DataTable';
import { formatBytes } from '../format';
import { Meter } from '../Meter';
import { InstallPlanDialog } from '../InstallPlanDialog';
import { explain, useData } from '../useData';

/**
 * Btrfs (read-only). Each mounted filesystem's allocation, device error
 * counters, scrub status and subvolumes as btrfs-progs reports them (run by
 * the read-only storage helper). A Software Center link is offered when the
 * btrfs binary is missing. No format, balance, scrub start or replace exists.
 */
export function Btrfs({
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
  const fetchInventory = useCallback(() => getBtrfs(csrf), [csrf]);
  const { data, error } = useData(fetchInventory, { onUnauthorized });

  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">Btrfs</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading Btrfs inventory…'}</p>
      </div>
    );
  }
  return <BtrfsView data={data} error={error} scopes={scopes} packagesEnabled={packagesEnabled} />;
}

export function BtrfsView({
  data,
  error,
  scopes = [],
  packagesEnabled,
}: {
  data: BtrfsInventory;
  error: Error | null;
  scopes?: string[];
  packagesEnabled: boolean;
}) {
  const { tooling, btrfs } = data;
  const kernelBtrfs = data.kernelBtrfs;
  const missingRequired = tooling.required.filter((t) => !t.installed);
  const missingToolNames = missingRequired.map((tool) => tool.id).join(', ');
  const report = data.status?.available ? data.status.report : undefined;
  // Btrfs mounts this page sees whose filesystem the helper did not report
  // (a mount on a device of a reported filesystem is one of its subvolumes).
  const uncovered = report
    ? btrfs.mounts
        .filter(
          (m) =>
            !report.filesystems.some(
              (fs) => fs.mounts.includes(m.mountpoint) || fs.devices.some((d) => d.path === m.device),
            ) && !(report.skipped ?? []).some((s) => s.mountpoint === m.mountpoint),
        )
        .map((m) => m.mountpoint)
    : [];
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Btrfs</h1>
        <p className="page-sub">
          Read-only. Allocation, device errors, scrub status and subvolumes come from{' '}
          <code>btrfs filesystem usage</code>, <code>btrfs device stats</code>,{' '}
          <code>btrfs scrub status</code> and <code>btrfs subvolume list</code>, run by the read-only storage
          helper. This page does not format, balance, start a scrub or replace a device, and it does not load
          kernel modules.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}

      <section className="panel" aria-labelledby="btrfs-kernel-heading">
        <header className="panel-head">
          <h2 id="btrfs-kernel-heading">Kernel Btrfs</h2>
        </header>
        {kernelBtrfs?.present ? (
          <p>
            Kernel Btrfs is listed in <code>/proc/filesystems</code>. Listed is not ready to format a
            filesystem.
          </p>
        ) : (
          <p className="banner" role="status">
            {kernelBtrfs == null
              ? 'Kernel Btrfs capability data is unavailable; its status has not been checked.'
              : kernelBtrfs.reason ||
                'Kernel Btrfs is not currently listed in /proc/filesystems (this page does not load modules).'}
          </p>
        )}
      </section>

      {missingRequired.length > 0 && (
        <section className="panel" aria-labelledby="btrfs-offer-heading">
          <header className="panel-head">
            <h2 id="btrfs-offer-heading">Btrfs tools</h2>
          </header>
          <p>Missing required tools: {missingToolNames}. This page does not run them.</p>
          <RequiredOffers items={missingRequired} canInstall={canInstall} />
        </section>
      )}

      <section className="panel" aria-labelledby="btrfs-fs-heading">
        <header className="panel-head">
          <h2 id="btrfs-fs-heading">Filesystems</h2>
          {report && <span className="tag">observed</span>}
        </header>
        {!data.status?.available ? (
          <p className="banner" role="status">
            {data.status?.reason || 'Filesystem status is unavailable.'}
          </p>
        ) : report && report.filesystems.length > 0 ? (
          report.filesystems.map((fs) => <Filesystem key={fs.mountpoint} fs={fs} />)
        ) : (
          <p className="empty">No Btrfs filesystem is mounted.</p>
        )}
        {uncovered.length > 0 && (
          <p className="banner" role="status">
            Not inspected: {uncovered.join(', ')}. The read-only storage helper cannot see mounts under /home,
            /root or /run/user.
          </p>
        )}
        {report?.skipped?.map((s) => (
          <p key={s.mountpoint} className="banner" role="status">
            <code>{s.mountpoint}</code>: {s.reason}
          </p>
        ))}
      </section>

      {!report && (
        <section className="panel" aria-labelledby="btrfs-mounts-heading">
          <header className="panel-head">
            <h2 id="btrfs-mounts-heading">Btrfs mounts</h2>
            {btrfs.mounts.length > 0 && <span className="tag">observed</span>}
          </header>
          {btrfs.mountsTruncated ? (
            <p className="banner" role="status">
              The mount list is incomplete. Some Btrfs mounts may be omitted.
            </p>
          ) : null}
          {btrfs.mounts.length > 0 ? (
            <ul className="pool-list">
              {btrfs.mounts.map((m, index) => (
                <li key={`${m.mountpoint}\0${m.device}\0${m.subvolId}\0${index}`}>
                  <code>{m.mountpoint}</code>
                  <dl>
                    <dt>Device</dt>
                    <dd>
                      <code>{m.device}</code>
                    </dd>
                    {m.subvol ? (
                      <>
                        <dt>Subvolume</dt>
                        <dd>
                          <code>{m.subvol}</code>
                        </dd>
                      </>
                    ) : null}
                    {m.subvolId ? (
                      <>
                        <dt>Subvolume ID</dt>
                        <dd>
                          <code>{m.subvolId}</code>
                        </dd>
                      </>
                    ) : null}
                  </dl>
                </li>
              ))}
            </ul>
          ) : (
            <p className="empty">
              {btrfs.present ? 'No Btrfs mounts found.' : btrfs.reason || 'No Btrfs mounts found.'}
            </p>
          )}
        </section>
      )}
    </div>
  );
}

function RequiredOffers({ items, canInstall }: { items: BtrfsToolItem[]; canInstall: boolean }) {
  if (items.length === 0) return null;
  const seen = new Set<string>();
  return (
    <>
      {items.map((item) => {
        const key = item.package || `missing:${item.id}`;
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
  item: BtrfsToolItem | undefined;
  canInstall: boolean;
}) {
  const [open, setOpen] = useState(false);
  const pkg = item?.package ?? '';
  if (!pkg) {
    return <p>Software Center cannot determine a package name for {item?.id} on this distribution.</p>;
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

const errorKeys = [
  'write_io_errs',
  'read_io_errs',
  'flush_io_errs',
  'corruption_errs',
  'generation_errs',
] as const;

function deviceErrors(d: BtrfsDeviceStats): number {
  return errorKeys.reduce((sum, k) => sum + (d[k] ?? 0), 0);
}

/** One filesystem as btrfs-progs reports it. The badge only restates the tool's counters. */
function Filesystem({ fs }: { fs: BtrfsFilesystem }) {
  const missing = (fs.overall.missing ?? 0) > 0;
  const errors = fs.devices.reduce((sum, d) => sum + deviceErrors(d), 0);
  const [tone, word] = missing
    ? ['crit', 'Devices missing']
    : errors > 0
      ? ['warn', `${errors} device errors recorded`]
      : ['ok', 'No device errors recorded'];
  const profiles = fs.profiles.filter((p) => p.type !== 'Unallocated');
  const { device_size: size, used } = fs.overall;
  return (
    <article className="zfs-pool" aria-label={`Filesystem ${fs.mountpoint}`}>
      <h3 className="subhead">
        <code>{fs.mountpoint}</code> <span className={`status status-${tone}`}>{word}</span>
      </h3>
      {size !== undefined && used !== undefined && (
        <Meter
          label="Used"
          used={used}
          total={size}
          detail={`${fs.overall.free_estimated !== undefined ? `${formatBytes(fs.overall.free_estimated)} free (estimated) · ` : ''}${formatBytes(size)} on ${fs.devices.length || '?'} device(s)`}
        />
      )}
      <dl className="facts">
        {profiles.map((p) => (
          <div key={p.type}>
            <dt>{p.type}</dt>
            <dd>
              {p.profile}
              {p.size !== undefined && p.used !== undefined
                ? ` · ${formatBytes(p.used)} of ${formatBytes(p.size)}`
                : ''}
            </dd>
          </div>
        ))}
        <div>
          <dt>Last scrub</dt>
          <dd>
            {fs.scrub.never_run
              ? 'Never run'
              : [fs.scrub.status, fs.scrub.started].filter(Boolean).join(', ') || 'Not reported'}
          </dd>
        </div>
        {fs.scrub.error_summary && !fs.scrub.never_run && (
          <div>
            <dt>Scrub errors</dt>
            <dd>{fs.scrub.error_summary}</dd>
          </div>
        )}
        {fs.uuid && (
          <div>
            <dt>UUID</dt>
            <dd>
              <code>{fs.uuid}</code>
            </dd>
          </div>
        )}
        <div>
          <dt>Mounted at</dt>
          <dd>{fs.mounts.join(', ')}</dd>
        </div>
      </dl>
      {fs.errors?.map((e) => (
        <p key={e} className="banner" role="status">
          Not reported: {e}
        </p>
      ))}
      <DataTable
        caption={`Device errors of ${fs.mountpoint}`}
        rows={fs.devices}
        rowKey={(d) => d.path}
        columns={[
          { id: 'path', label: 'Device', render: (d) => <code>{d.path}</code> },
          { id: 'write', label: 'Write', render: (d) => d.write_io_errs ?? '' },
          { id: 'read', label: 'Read', render: (d) => d.read_io_errs ?? '' },
          { id: 'flush', label: 'Flush', render: (d) => d.flush_io_errs ?? '' },
          { id: 'corruption', label: 'Corruption', render: (d) => d.corruption_errs ?? '' },
          { id: 'generation', label: 'Generation', render: (d) => d.generation_errs ?? '' },
        ]}
      />
      {fs.subvolumes.length > 0 && (
        <DataTable
          caption={`Subvolumes of ${fs.mountpoint}${fs.subvolumes_truncated ? ' (first 1024)' : ''}`}
          rows={fs.subvolumes}
          rowKey={(s) => String(s.id)}
          columns={[
            { id: 'id', label: 'ID', render: (s) => s.id, sortValue: (s) => s.id },
            { id: 'path', label: 'Path', render: (s) => <code>{s.path}</code>, sortValue: (s) => s.path },
            { id: 'parent', label: 'Parent ID', render: (s) => s.top_level },
          ]}
        />
      )}
    </article>
  );
}
