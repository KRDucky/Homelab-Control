// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getMdadm, type MdadmInventory, type MdadmToolItem } from '../api';
import { InstallPlanDialog } from '../InstallPlanDialog';
import { explain, useData } from '../useData';

/**
 * Software RAID (read-only). Probe the mdadm binary, show a Software Center
 * search link when the package name is known and the binary is missing, and
 * list /proc/mdstat array lines as printed. No create, assemble, add, remove,
 * grow, reshape, or mdadm execution exists in the DOM.
 */
export function Mdadm({
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
  const fetchInventory = useCallback(() => getMdadm(csrf), [csrf]);
  const { data, error } = useData(fetchInventory, { onUnauthorized });

  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">Software RAID</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading software RAID inventory…'}</p>
      </div>
    );
  }
  return <MdadmView data={data} error={error} scopes={scopes} packagesEnabled={packagesEnabled} />;
}

export function MdadmView({
  data,
  error,
  scopes = [],
  packagesEnabled,
}: {
  data: MdadmInventory;
  error: Error | null;
  scopes?: string[];
  packagesEnabled: boolean;
}) {
  const { tooling, md } = data;
  const missingRequired = tooling.required.filter((t) => !t.installed);
  const canInstall = packagesEnabled && (scopes ?? []).includes('packages.read');

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Software RAID</h1>
        <p className="page-sub">
          Read-only observations. The mdadm binary is probed, not run. This page does not create, assemble, or
          reshape an array, and it does not add or remove disks. Arrays come from <code>/proc/mdstat</code>{' '}
          exactly as the kernel prints them; no device is opened and no superblock is read.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}

      {missingRequired.length > 0 && (
        <section className="panel" aria-labelledby="mdadm-offer-heading">
          <header className="panel-head">
            <h2 id="mdadm-offer-heading">mdadm tools</h2>
          </header>
          <p>mdadm is required to manage arrays elsewhere. This page does not run it.</p>
          {missingRequired.map((item) => (
            <InstallOffer key={item.id} item={item} canInstall={canInstall} />
          ))}
        </section>
      )}

      <section className="panel" aria-labelledby="mdadm-arrays-heading">
        <header className="panel-head">
          <h2 id="mdadm-arrays-heading">Arrays in /proc/mdstat</h2>
          {md.present && md.arrays.length > 0 && <span className="tag">observed</span>}
        </header>
        {md.present && md.personalities.length > 0 && (
          <p>
            Kernel RAID personalities listed: <code>{md.personalities.join(', ')}</code>
          </p>
        )}
        {md.arraysTruncated ? (
          <p className="banner" role="status">
            Showing {md.arrays.length} arrays from a truncated inventory; additional arrays may be omitted.
          </p>
        ) : null}
        {!md.present ? (
          <p className="banner" role="status">
            {md.reason || '/proc/mdstat could not be read safely; no arrays are shown.'}
          </p>
        ) : md.arrays.length > 0 ? (
          <table>
            <caption>Arrays as printed in /proc/mdstat</caption>
            <thead>
              <tr>
                <th scope="col">Array</th>
                <th scope="col">Level</th>
                <th scope="col">State</th>
                <th scope="col">Members listed</th>
              </tr>
            </thead>
            <tbody>
              {md.arrays.map((a) => (
                <tr key={a.name}>
                  <th scope="row">
                    <code>{a.name}</code>
                  </th>
                  <td>{a.level || 'Not printed'}</td>
                  <td>{a.state}</td>
                  <td>{a.memberCount === null ? 'Not printed' : String(a.memberCount)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="empty">No arrays are listed in /proc/mdstat.</p>
        )}
      </section>
    </div>
  );
}

function InstallOffer({ item, canInstall }: { item: MdadmToolItem; canInstall: boolean }) {
  const [open, setOpen] = useState(false);
  const pkg = item.package;
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
  const label = `Install ${pkg}`;
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        {label}
      </button>
      {open && <InstallPlanDialog title={label} pkg={pkg} onClose={() => setOpen(false)} />}
    </>
  );
}
