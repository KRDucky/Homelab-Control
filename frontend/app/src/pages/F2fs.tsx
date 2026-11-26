// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getF2fs, type F2fsFilesystem, type F2fsInventory, type F2fsToolItem } from '../api';
import { Fact, FilesystemReport } from '../FilesystemReport';
import { formatBytes } from '../format';
import { InstallPlanDialog } from '../InstallPlanDialog';
import { explain, useData } from '../useData';

/**
 * f2fs (read-only). Each mounted filesystem's live counters from the kernel's
 * /sys/fs/f2fs files, the observed mounts, and a Software Center link when
 * f2fs-tools is missing. No format, grow, or repair action exists.
 */
export function F2fs({
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
  const fetchInventory = useCallback(() => getF2fs(csrf), [csrf]);
  const { data, error } = useData(fetchInventory, { onUnauthorized });

  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">f2fs</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading f2fs inventory…'}</p>
      </div>
    );
  }
  return <F2fsView data={data} error={error} scopes={scopes} packagesEnabled={packagesEnabled} />;
}

export function F2fsView({
  data,
  error,
  scopes = [],
  packagesEnabled,
}: {
  data: F2fsInventory;
  error: Error | null;
  scopes?: string[];
  packagesEnabled: boolean;
}) {
  const { tooling, f2fs } = data;
  const kernelF2fs = data.kernelF2fs;
  const missingRequired = tooling.required.filter((t) => !t.installed);
  const missingToolNames = missingRequired.map((tool) => tool.id).join(', ');
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">f2fs</h1>
        <p className="page-sub">
          Read-only. Each mounted filesystem's segment and write counters come from the kernel&apos;s{' '}
          <code>/sys/fs/f2fs</code> files. The dump.f2fs binary is probed, not run. This page does not create
          or repair a filesystem, and it does not load kernel modules. Mounts come from /proc/self/mountinfo.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}

      <section className="panel" aria-labelledby="f2fs-kernel-heading">
        <header className="panel-head">
          <h2 id="f2fs-kernel-heading">Kernel f2fs</h2>
        </header>
        {kernelF2fs?.present ? (
          <p>
            Kernel f2fs is listed in <code>/proc/filesystems</code>.
          </p>
        ) : (
          <p className="banner" role="status">
            {kernelF2fs == null
              ? 'Kernel f2fs capability data is unavailable; its status has not been checked.'
              : kernelF2fs.reason ||
                'The kernel f2fs filesystem is not currently listed in /proc/filesystems (this page does not load modules).'}
          </p>
        )}
      </section>

      {missingRequired.length > 0 && (
        <section className="panel" aria-labelledby="f2fs-offer-heading">
          <header className="panel-head">
            <h2 id="f2fs-offer-heading">f2fs tools</h2>
          </header>
          <p>Missing required tools: {missingToolNames}. This page does not run them.</p>
          <p>Install options open Software Center planning; this page makes no changes.</p>
          <RequiredOffers items={missingRequired} canInstall={canInstall} />
        </section>
      )}

      {data.status && (
        <FilesystemReport
          id="f2fs"
          available={data.status.available}
          reason={data.status.reason}
          filesystems={data.status.filesystems}
          skipped={data.status.skipped}
          truncated={data.status.truncated}
          empty="No f2fs filesystem is mounted."
        >
          {(fs) => <F2fsCard key={fs.name} fs={fs} />}
        </FilesystemReport>
      )}

      <section className="panel" aria-labelledby="f2fs-mounts-heading">
        <header className="panel-head">
          <h2 id="f2fs-mounts-heading">f2fs mounts</h2>
          {f2fs.mounts.length > 0 && <span className="tag">observed</span>}
        </header>
        {f2fs.mountsTruncated ? (
          <p className="banner" role="status">
            Showing {f2fs.mounts.length} mounts; inventory incomplete and more mounts may exist.
          </p>
        ) : null}
        {f2fs.mounts.length > 0 ? (
          <ul className="pool-list">
            {f2fs.mounts.map((m, index) => (
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
            {f2fs.present ? 'No f2fs mounts found.' : f2fs.reason || 'No f2fs mounts found.'}
          </p>
        )}
      </section>
    </div>
  );
}

/** One filesystem's live counters as the kernel publishes them. */
function F2fsCard({ fs }: { fs: F2fsFilesystem }) {
  return (
    <article className="zfs-pool" aria-label={`Filesystem ${fs.mounts[0] ?? fs.name}`}>
      <h3 className="subhead">
        <code>{fs.mounts[0] ?? fs.name}</code>
      </h3>
      <dl className="facts">
        <Fact label="Free segments" value={fs.freeSegments} />
        <Fact label="Dirty segments" value={fs.dirtySegments} />
        <Fact label="Overprovision segments" value={fs.overprovisionSegments} />
        <Fact label="Reserved blocks" value={fs.reservedBlocks} />
        <Fact
          label="Lifetime writes"
          value={fs.lifetimeWriteKiB !== undefined ? formatBytes(fs.lifetimeWriteKiB * 1024) : undefined}
        />
        <Fact label="Encoding" value={fs.encoding} />
        <Fact label="Kernel device" value={<code>{fs.name}</code>} />
        <Fact label="Also mounted at" value={fs.mounts.slice(1).join(', ')} />
        <Fact label="Features" value={fs.features.join(', ')} />
      </dl>
    </article>
  );
}

function RequiredOffers({ items, canInstall }: { items: F2fsToolItem[]; canInstall: boolean }) {
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
  item: F2fsToolItem | undefined;
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
