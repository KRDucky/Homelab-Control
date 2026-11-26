// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getExt4, type Ext4Filesystem, type Ext4Inventory, type Ext4ToolItem } from '../api';
import { Fact, FilesystemReport } from '../FilesystemReport';
import { formatBytes } from '../format';
import { Meter } from '../Meter';
import { InstallPlanDialog } from '../InstallPlanDialog';
import { explain, useData } from '../useData';

/**
 * ext4 (M7 first checkpoint, read-only). Probe the dumpe2fs binary, show a
 * Software Center search link when the package name is known and missing, and
 * list observed ext4 mounts. No format, grow, or repair exists in the DOM.
 */
export function Ext4({
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
  const fetchInventory = useCallback(() => getExt4(csrf), [csrf]);
  const { data, error } = useData(fetchInventory, { onUnauthorized });

  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">ext4</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading ext4 inventory…'}</p>
      </div>
    );
  }
  return <Ext4View data={data} error={error} scopes={scopes} packagesEnabled={packagesEnabled} />;
}

export function Ext4View({
  data,
  error,
  scopes = [],
  packagesEnabled,
}: {
  data: Ext4Inventory;
  error: Error | null;
  scopes?: string[];
  packagesEnabled: boolean;
}) {
  const { tooling, ext4 } = data;
  const kernelExt4 = data.kernelExt4;
  const missingRequired = tooling.required.filter((t) => !t.installed);
  const missingToolNames = missingRequired.map((tool) => tool.id).join(', ');
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">ext4</h1>
        <p className="page-sub">
          Read-only. Each mounted filesystem's state, error record and counters come from{' '}
          <code>tune2fs -l</code>, run by the read-only storage helper. This page does not create or repair a
          filesystem, and it does not load kernel modules. Mounts come from /proc/self/mountinfo.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}

      <section className="panel" aria-labelledby="ext4-kernel-heading">
        <header className="panel-head">
          <h2 id="ext4-kernel-heading">Kernel ext4</h2>
        </header>
        {kernelExt4?.present ? (
          <p>
            Kernel ext4 is listed in <code>/proc/filesystems</code>.
          </p>
        ) : (
          <p className="banner" role="status">
            {kernelExt4 == null
              ? 'Kernel ext4 capability data is unavailable; its status has not been checked.'
              : kernelExt4.reason ||
                'The kernel ext4 filesystem is not currently listed in /proc/filesystems (this page does not load modules).'}
          </p>
        )}
      </section>

      {missingRequired.length > 0 && (
        <section className="panel" aria-labelledby="ext4-offer-heading">
          <header className="panel-head">
            <h2 id="ext4-offer-heading">ext4 tools</h2>
          </header>
          <p>Missing required tools: {missingToolNames}. This page does not run them.</p>
          <RequiredOffers items={missingRequired} canInstall={canInstall} />
        </section>
      )}

      <FilesystemReport
        id="ext4"
        available={data.status?.available ?? false}
        reason={data.status?.reason}
        filesystems={data.status?.report?.filesystems ?? []}
        skipped={data.status?.report?.skipped}
        truncated={data.status?.report?.truncated}
        empty="No ext4 filesystem is mounted."
      >
        {(fs) => <Ext4Card key={fs.device} fs={fs} />}
      </FilesystemReport>

      <section className="panel" aria-labelledby="ext4-mounts-heading">
        <header className="panel-head">
          <h2 id="ext4-mounts-heading">ext4 mounts</h2>
          {ext4.mounts.length > 0 && <span className="tag">observed</span>}
        </header>
        {ext4.mountsTruncated ? (
          <p className="banner" role="status">
            too many mounts to list
          </p>
        ) : null}
        {ext4.mounts.length > 0 ? (
          <ul className="pool-list">
            {ext4.mounts.map((m, index) => (
              <li key={`${m.mountpoint}\0${m.device}\0${index}`}>
                <code>{m.mountpoint}</code>
                <dl>
                  <dt>Device</dt>
                  <dd>
                    <code>{m.device}</code>
                  </dd>
                </dl>
              </li>
            ))}
          </ul>
        ) : (
          <p className="empty">
            {ext4.present ? 'No ext4 mounts found.' : ext4.reason || 'No ext4 mounts found.'}
          </p>
        )}
      </section>
    </div>
  );
}

/** One superblock as tune2fs reports it. The badge only restates tune2fs's own fields. */
function Ext4Card({ fs }: { fs: Ext4Filesystem }) {
  const errors = fs.error_count ?? 0;
  const [tone, word] = fs.error
    ? ['crit', 'Not described']
    : errors > 0 || fs.state?.includes('error')
      ? ['crit', errors > 0 ? `${errors} errors recorded` : 'Errors recorded']
      : fs.state && fs.state !== 'clean'
        ? ['warn', `State: ${fs.state}`]
        : ['ok', 'Clean, no errors recorded'];
  const bs = fs.block_size;
  const total = bs !== undefined && fs.block_count !== undefined ? fs.block_count * bs : undefined;
  const used =
    total !== undefined && fs.free_blocks !== undefined && bs !== undefined
      ? total - fs.free_blocks * bs
      : undefined;
  const mountChecks =
    fs.mount_count === undefined
      ? undefined
      : fs.max_mount_count !== undefined && fs.max_mount_count > 0
        ? `${fs.mount_count} of ${fs.max_mount_count} before a forced check`
        : `${fs.mount_count} (no forced check by count)`;
  return (
    <article className="zfs-pool" aria-label={`Filesystem ${fs.mounts[0] ?? fs.device}`}>
      <h3 className="subhead">
        <code>{fs.mounts[0] ?? fs.device}</code> <span className={`status status-${tone}`}>{word}</span>
      </h3>
      {fs.error ? (
        <p className="banner" role="status">
          {fs.error}
        </p>
      ) : (
        <>
          {total !== undefined && used !== undefined && (
            <Meter
              label="Used"
              used={used}
              total={total}
              detail={`${formatBytes(total - used)} free of ${formatBytes(total)} (superblock counters)`}
            />
          )}
          <dl className="facts">
            <Fact label="State" value={fs.state} />
            <Fact
              label="First error"
              value={
                fs.first_error_time &&
                [fs.first_error_time, fs.first_error_function].filter(Boolean).join(', ')
              }
            />
            <Fact
              label="Last error"
              value={
                fs.last_error_time && [fs.last_error_time, fs.last_error_function].filter(Boolean).join(', ')
              }
            />
            <Fact label="On errors" value={fs.errors_behavior} />
            <Fact label="Mount count" value={mountChecks} />
            <Fact label="Last checked" value={fs.last_checked} />
            <Fact label="Check interval" value={fs.check_interval} />
            <Fact label="Last write" value={fs.last_write_time} />
            <Fact label="Lifetime writes" value={fs.lifetime_writes} />
            <Fact
              label="Inodes"
              value={
                fs.inode_count !== undefined && fs.free_inodes !== undefined
                  ? `${fs.inode_count - fs.free_inodes} used of ${fs.inode_count}`
                  : undefined
              }
            />
            <Fact label="Block size" value={bs !== undefined ? formatBytes(bs) : undefined} />
            <Fact label="Label" value={fs.volume_name} />
            <Fact label="UUID" value={fs.uuid && <code>{fs.uuid}</code>} />
            <Fact label="Device" value={<code>{fs.device}</code>} />
            <Fact label="Also mounted at" value={fs.mounts.slice(1).join(', ')} />
            <Fact label="Features" value={fs.features.join(' ')} />
          </dl>
        </>
      )}
    </article>
  );
}

function RequiredOffers({ items, canInstall }: { items: Ext4ToolItem[]; canInstall: boolean }) {
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
  item: Ext4ToolItem | undefined;
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
