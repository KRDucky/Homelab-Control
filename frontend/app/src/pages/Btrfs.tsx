// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getBtrfs, type BtrfsInventory, type BtrfsToolItem } from '../api';
import { InstallPlanDialog } from '../InstallPlanDialog';
import { explain, useData } from '../useData';

/**
 * Btrfs (M7 first checkpoint, read-only). Probe the btrfs binary, show a
 * Software Center search link when the package name is known and missing, and
 * list observed btrfs mounts. No format, balance, scrub, replace, or btrfs
 * execution exists in the DOM. RAID5/6 is not offered.
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
  const missingSmart = tooling.optional.filter(
    (t) => t.id === 'smartctl' && !t.installed && t.package && btrfs.mounts.length > 0,
  );
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Btrfs</h1>
        <p className="page-sub">
          Read-only observations. The btrfs binary is probed, not run. This page does not format, balance,
          scrub, or replace, and it does not load kernel modules. RAID5/6 is not offered. Mounts come from
          mountinfo device paths and subvolume options, not from running btrfs. One device path is not the
          full membership of a multi-device filesystem.
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
            {kernelBtrfs?.reason ||
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

      {missingSmart.length > 0 && (
        <section className="panel" aria-labelledby="btrfs-smart-heading">
          <header className="panel-head">
            <h2 id="btrfs-smart-heading">Disk health</h2>
          </header>
          <p>
            smartctl is optional. It is offered only after a btrfs mount is observed. This page does not run
            it and does not open a disk.
          </p>
          <RequiredOffers items={missingSmart} canInstall={canInstall} />
        </section>
      )}

      <section className="panel" aria-labelledby="btrfs-mounts-heading">
        <header className="panel-head">
          <h2 id="btrfs-mounts-heading">Btrfs mounts</h2>
          {btrfs.mounts.length > 0 && <span className="tag">observed</span>}
        </header>
        {btrfs.mountsTruncated ? (
          <p className="banner" role="status">
            too many mounts to list
          </p>
        ) : null}
        {btrfs.mounts.length > 0 ? (
          <ul className="pool-list">
            {btrfs.mounts.map((m) => (
              <li key={`${m.mountpoint}\0${m.device}\0${m.subvolId}`}>
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
