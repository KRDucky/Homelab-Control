// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getF2fs, type F2fsInventory, type F2fsToolItem } from '../api';
import { InstallPlanDialog } from '../InstallPlanDialog';
import { explain, useData } from '../useData';

/**
 * f2fs (M7 first checkpoint, read-only). Probe the dump.f2fs binary, show a
 * Software Center search link when the package name is known and missing, and
 * list observed f2fs mounts. No format, grow, or repair exists in the DOM.
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
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">f2fs</h1>
        <p className="page-sub">
          Read-only observations. The dump.f2fs binary is probed, not run. This page does not create or repair
          a filesystem, and it does not load kernel modules. Mounts come from /proc/self/mountinfo, not from
          running dump.f2fs.
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
            {kernelF2fs?.reason ||
              'The kernel f2fs filesystem is not currently listed in /proc/filesystems (this page does not load modules).'}
          </p>
        )}
      </section>

      {missingRequired.length > 0 && (
        <section className="panel" aria-labelledby="f2fs-offer-heading">
          <header className="panel-head">
            <h2 id="f2fs-offer-heading">f2fs tools</h2>
          </header>
          <p>Missing required tools: {missingRequired.map((tool) => tool.id).join(', ')}. This page does not run them.</p>
          <RequiredOffers items={missingRequired} canInstall={canInstall} />
        </section>
      )}

      <section className="panel" aria-labelledby="f2fs-mounts-heading">
        <header className="panel-head">
          <h2 id="f2fs-mounts-heading">f2fs mounts</h2>
          {f2fs.mounts.length > 0 && <span className="tag">observed</span>}
        </header>
        {f2fs.mountsTruncated ? (
          <p className="banner" role="status">
            too many mounts to list
          </p>
        ) : null}
        {f2fs.mounts.length > 0 ? (
          <ul className="pool-list">
            {f2fs.mounts.map((m) => (
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
            {f2fs.present ? 'No f2fs mounts found.' : f2fs.reason || 'No f2fs mounts found.'}
          </p>
        )}
      </section>
    </div>
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
