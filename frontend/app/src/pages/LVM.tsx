// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getLVM, type LVMInventory, type LVMLogicalVolume, type LVMToolItem } from '../api';
import { DataTable } from '../DataTable';
import { formatBytes } from '../format';
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
  const { tooling } = data;
  const kernelDm = data.kernelDm;
  const report = data.status?.available ? data.status.report : undefined;
  const hasThinPool = (report?.volumes ?? []).some((v) => v.segments.some((s) => s.type === 'thin-pool'));
  const missingRequired = tooling.required.filter((t) => !t.installed);
  const missingThin = hasThinPool
    ? tooling.optional.filter((t) => t.id === 'thin_check' && !t.installed && t.package)
    : [];
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">LVM</h1>
        <p className="page-sub">
          Read-only. Volume groups, logical volumes and physical volumes come from <code>vgs</code>,{' '}
          <code>lvs</code> and <code>pvs</code> (<code>--readonly --nolocking</code>), run by the read-only
          storage helper. This page does not create, change or remove anything, and it does not load kernel
          modules.
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
          <p>lvm is required for the LVM reports.</p>
          <RequiredOffers items={missingRequired} canInstall={canInstall} />
        </section>
      )}

      {missingThin.length > 0 && (
        <section className="panel" aria-labelledby="lvm-thin-heading">
          <header className="panel-head">
            <h2 id="lvm-thin-heading">Thin pool check</h2>
          </header>
          <p>
            thin_check is optional. It is offered because LVM reports a thin pool. This page does not run it.
          </p>
          <RequiredOffers items={missingThin} canInstall={canInstall} />
        </section>
      )}

      <section className="panel" aria-labelledby="lvm-vg-heading">
        <header className="panel-head">
          <h2 id="lvm-vg-heading">Volume groups</h2>
          {report && <span className="tag">observed</span>}
        </header>
        {!report ? (
          <p className="banner" role="status">
            {data.status?.reason || 'LVM status is unavailable.'}
          </p>
        ) : report.volume_groups.length === 0 ? (
          <p className="empty">LVM reports no volume groups.</p>
        ) : (
          <DataTable
            caption="Volume groups"
            rows={report.volume_groups}
            rowKey={(v) => v.uuid}
            columns={[
              { id: 'name', label: 'Name', render: (v) => <code>{v.name}</code>, sortValue: (v) => v.name },
              {
                id: 'size',
                label: 'Size',
                render: (v) => size(v.size_bytes),
                sortValue: (v) => v.size_bytes ?? -1,
              },
              {
                id: 'free',
                label: 'Free',
                render: (v) => size(v.free_bytes),
                sortValue: (v) => v.free_bytes ?? -1,
              },
              { id: 'pvs', label: 'PVs', render: (v) => v.pv_count ?? '' },
              { id: 'lvs', label: 'LVs', render: (v) => v.lv_count ?? '' },
              {
                id: 'missing',
                label: 'Missing PVs',
                render: (v) =>
                  (v.missing_pv_count ?? 0) > 0 ? (
                    <span className="status status-crit">{v.missing_pv_count}</span>
                  ) : (
                    (v.missing_pv_count ?? '')
                  ),
              },
            ]}
          />
        )}
      </section>

      {report && (
        <section className="panel" aria-labelledby="lvm-lv-heading">
          <header className="panel-head">
            <h2 id="lvm-lv-heading">Logical volumes</h2>
          </header>
          {report.volumes.length === 0 ? (
            <p className="empty">LVM reports no logical volumes.</p>
          ) : (
            <DataTable
              caption="Logical volumes"
              rows={report.volumes}
              rowKey={(v) => v.uuid}
              columns={[
                { id: 'name', label: 'Name', render: (v) => <code>{v.name}</code>, sortValue: (v) => v.name },
                { id: 'vg', label: 'Volume group', render: (v) => v.vg_name, sortValue: (v) => v.vg_name },
                {
                  id: 'size',
                  label: 'Size',
                  render: (v) => size(v.size_bytes),
                  sortValue: (v) => v.size_bytes ?? -1,
                },
                { id: 'type', label: 'Type', render: lvType },
                { id: 'health', label: 'Health', render: (v) => v.health },
                {
                  id: 'sync',
                  label: 'Sync',
                  render: (v) => (v.sync_percent === undefined ? '' : `${v.sync_percent}%`),
                },
              ]}
            />
          )}
        </section>
      )}

      {report && (
        <section className="panel" aria-labelledby="lvm-pv-heading">
          <header className="panel-head">
            <h2 id="lvm-pv-heading">Physical volumes</h2>
          </header>
          {report.physical_volumes.length === 0 ? (
            <p className="empty">LVM reports no physical volumes.</p>
          ) : (
            <DataTable
              caption="Physical volumes"
              rows={report.physical_volumes}
              rowKey={(p) => p.uuid}
              columns={[
                {
                  id: 'device',
                  label: 'Device',
                  render: (p) =>
                    p.name ? <code>{p.name}</code> : p.ambiguous ? 'Ambiguous (seen on several devices)' : '',
                },
                { id: 'vg', label: 'Volume group', render: (p) => p.vg_name ?? '' },
                { id: 'size', label: 'Size', render: (p) => size(p.size_bytes) },
                {
                  id: 'state',
                  label: 'State',
                  render: (p) =>
                    p.missing ? <span className="status status-crit">Missing</span> : 'Present',
                },
              ]}
            />
          )}
        </section>
      )}
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

function size(v?: number): string {
  return v === undefined ? 'Not reported' : formatBytes(v);
}

/** The segment types LVM reports (linear, striped, raid1, thin-pool, thin, cache, ...). */
function lvType(v: LVMLogicalVolume): string {
  return [...new Set(v.segments.map((s) => s.type))].join(', ');
}
