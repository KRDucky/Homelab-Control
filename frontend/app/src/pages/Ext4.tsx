// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getExt4, type Ext4Inventory, type Ext4ToolItem } from '../api';
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
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">ext4</h1>
        <p className="page-sub">
          Read-only observations. The dumpe2fs binary is probed, not run. This page does not create or repair
          a filesystem, and it does not load kernel modules. Mounts come from /proc/self/mountinfo, not from
          running dumpe2fs.
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
          <p>dumpe2fs is required. This page does not run it.</p>
          <RequiredOffers items={missingRequired} canInstall={canInstall} />
        </section>
      )}

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
            {ext4.mounts.map((m) => (
              <li key={`${m.mountpoint}\0${m.device}`}>
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

function RequiredOffers({ items, canInstall }: { items: Ext4ToolItem[]; canInstall: boolean }) {
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
  item: Ext4ToolItem | undefined;
  canInstall: boolean;
}) {
  const [open, setOpen] = useState(false);
  const pkg = item?.package ?? '';
  if (!pkg) {
    return <p>Software Center cannot determine the package name for this distribution.</p>;
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
