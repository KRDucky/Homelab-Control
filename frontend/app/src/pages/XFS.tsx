// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getXFS, type XFSInventory, type XFSToolItem } from '../api';
import { InstallPlanDialog } from '../InstallPlanDialog';
import { explain, useData } from '../useData';

/**
 * XFS (M7 first checkpoint, read-only). Probe the xfs_info binary, show a
 * Software Center search link when the package name is known and missing, and
 * list observed xfs mounts. No format, grow, or repair exists in the DOM.
 */
export function XFS({
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
  const fetchInventory = useCallback(() => getXFS(csrf), [csrf]);
  const { data, error } = useData(fetchInventory, { onUnauthorized });

  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">XFS</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading XFS inventory…'}</p>
      </div>
    );
  }
  return <XFSView data={data} error={error} scopes={scopes} packagesEnabled={packagesEnabled} />;
}

export function XFSView({
  data,
  error,
  scopes = [],
  packagesEnabled,
}: {
  data: XFSInventory;
  error: Error | null;
  scopes?: string[];
  packagesEnabled: boolean;
}) {
  const { tooling, xfs } = data;
  const kernelXfs = data.kernelXfs;
  const missingRequired = tooling.required.filter((t) => !t.installed);
  const missingToolNames = missingRequired.map((tool) => tool.id).join(', ');
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">XFS</h1>
        <p className="page-sub">
          Read-only observations. The xfs_info binary is probed, not run. This page does not create or repair
          a filesystem, and it does not load kernel modules. Mounts come from /proc/self/mountinfo, not from
          running xfs_info.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}

      <section className="panel" aria-labelledby="xfs-kernel-heading">
        <header className="panel-head">
          <h2 id="xfs-kernel-heading">Kernel XFS</h2>
        </header>
        {kernelXfs?.present ? (
          <p>
            Kernel XFS is listed in <code>/proc/filesystems</code>.
          </p>
        ) : (
          <p className="banner" role="status">
            {kernelXfs == null
              ? 'Kernel XFS capability data is unavailable; its status has not been checked.'
              : kernelXfs.reason ||
                'The kernel XFS filesystem is not currently listed in /proc/filesystems (this page does not load modules).'}
          </p>
        )}
      </section>

      {missingRequired.length > 0 && (
        <section className="panel" aria-labelledby="xfs-offer-heading">
          <header className="panel-head">
            <h2 id="xfs-offer-heading">XFS tools</h2>
          </header>
          <p>Missing required tools: {missingToolNames}. This page does not run them.</p>
          <RequiredOffers items={missingRequired} canInstall={canInstall} />
        </section>
      )}

      <section className="panel" aria-labelledby="xfs-mounts-heading">
        <header className="panel-head">
          <h2 id="xfs-mounts-heading">XFS mounts</h2>
          {xfs.mounts.length > 0 && <span className="tag">observed</span>}
        </header>
        {xfs.mountsTruncated ? (
          <p className="banner" role="status">
            not every mount could be listed
          </p>
        ) : null}
        {xfs.mounts.length > 0 ? (
          <ul className="pool-list">
            {xfs.mounts.map((m, index) => (
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
            {xfs.present ? 'No xfs mounts found.' : xfs.reason || 'No xfs mounts found.'}
          </p>
        )}
      </section>
    </div>
  );
}

function RequiredOffers({ items, canInstall }: { items: XFSToolItem[]; canInstall: boolean }) {
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
  item: XFSToolItem | undefined;
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
