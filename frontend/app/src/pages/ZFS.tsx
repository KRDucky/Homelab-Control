// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getZFS, type ZFSInventory, type ZFSToolItem } from '../api';
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
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">ZFS</h1>
        <p className="page-sub">
          Read-only observations. zfs and zpool are probed, not run. This page does not import, export,
          create, or destroy pools, and it does not load kernel modules. Mounts come from
          /proc/self/mountinfo, not zpool list.
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
