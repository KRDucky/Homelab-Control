// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getLVM, type LVMInventory, type LVMToolItem } from '../api';
import { InstallPlanDialog } from '../InstallPlanDialog';
import { explain, useData } from '../useData';

/**
 * LVM (M7 first checkpoint, read-only). Probe the lvm binary, show a Software
 * Center search link when the package name is known and missing, and list
 * device-mapper nodes whose uuid starts with LVM-. No pvcreate, vgcreate,
 * lvcreate, or lvm execution exists in the DOM.
 */
export function LVM({
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
  const fetchInventory = useCallback(() => getLVM(csrf), [csrf]);
  const { data, error } = useData(fetchInventory, { onUnauthorized });

  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">LVM</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading LVM inventory…'}</p>
      </div>
    );
  }
  return <LVMView data={data} error={error} scopes={scopes} packagesEnabled={packagesEnabled} />;
}

export function LVMView({
  data,
  error,
  scopes = [],
  packagesEnabled,
}: {
  data: LVMInventory;
  error: Error | null;
  scopes?: string[];
  packagesEnabled: boolean;
}) {
  const { tooling, lvm } = data;
  const kernelDm = data.kernelDm;
  const missingRequired = tooling.required.filter((t) => !t.installed);
  const missingThin = tooling.optional.filter((t) => t.id === 'thin_check' && !t.installed && t.package);
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">LVM</h1>
        <p className="page-sub">
          Read-only observations. The lvm binary is probed, not run. This page does not create or remove
          physical volumes, volume groups, or logical volumes, and it does not load kernel modules. Devices
          come from sysfs names whose uuid starts with LVM-, not from pvs.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}

      <section className="panel" aria-labelledby="lvm-kernel-heading">
        <header className="panel-head">
          <h2 id="lvm-kernel-heading">Kernel device-mapper</h2>
        </header>
        {kernelDm?.present ? (
          <p>
            Kernel device-mapper is listed in <code>/proc/devices</code>. Listed is not ready to create a
            physical volume.
          </p>
        ) : (
          <p className="banner" role="status">
            {kernelDm?.reason ||
              'The kernel device-mapper target is not currently listed in /proc/devices (this page does not load modules).'}
          </p>
        )}
      </section>

      {missingRequired.length > 0 && (
        <section className="panel" aria-labelledby="lvm-offer-heading">
          <header className="panel-head">
            <h2 id="lvm-offer-heading">LVM tools</h2>
          </header>
          <p>lvm is required. This page does not run it.</p>
          <RequiredOffers items={missingRequired} canInstall={canInstall} />
        </section>
      )}

      {missingThin.length > 0 && (
        <section className="panel" aria-labelledby="lvm-thin-heading">
          <header className="panel-head">
            <h2 id="lvm-thin-heading">Thin pool check</h2>
          </header>
          <p>
            thin_check is optional. It is offered only after a thin pool is observed. This page does not run
            it.
          </p>
          <RequiredOffers items={missingThin} canInstall={canInstall} />
        </section>
      )}

      <section className="panel" aria-labelledby="lvm-devices-heading">
        <header className="panel-head">
          <h2 id="lvm-devices-heading">LVM devices</h2>
          {lvm.devices.length > 0 && <span className="tag">observed</span>}
        </header>
        {lvm.devicesTruncated ? (
          <p className="banner" role="status">
            too many devices to list
          </p>
        ) : null}
        {lvm.devices.length > 0 ? (
          <ul className="pool-list">
            {lvm.devices.map((d) => (
              <li key={d.name}>
                <code>{d.name}</code>
                {d.mapper ? (
                  <dl>
                    <dt>Mapper</dt>
                    <dd>
                      <code>{d.mapper}</code>
                    </dd>
                  </dl>
                ) : null}
              </li>
            ))}
          </ul>
        ) : (
          <p className="empty">
            {lvm.present ? 'No LVM devices found.' : lvm.reason || 'No LVM devices found.'}
          </p>
        )}
      </section>
    </div>
  );
}

function RequiredOffers({ items, canInstall }: { items: LVMToolItem[]; canInstall: boolean }) {
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
  item: LVMToolItem | undefined;
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
