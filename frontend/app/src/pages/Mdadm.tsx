// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { getMdadm, type MdadmDetail, type MdadmInventory, type MdadmToolItem } from '../api';
import { DataTable } from '../DataTable';
import { formatBytes } from '../format';
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
  const report = data.status?.available ? data.status.report : undefined;

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Software RAID</h1>
        <p className="page-sub">
          Read-only. Array state, device counts, rebuild progress and members come from{' '}
          <code>mdadm --detail</code>, run by the read-only storage helper for each array in{' '}
          <code>/proc/mdstat</code>. This page does not create, assemble, reshape or fail an array, and it
          does not add or remove disks.
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
          <p>mdadm is required for array details.</p>
          {missingRequired.map((item) => (
            <InstallOffer key={item.id} item={item} canInstall={canInstall} />
          ))}
        </section>
      )}

      {report && (
        <section className="panel" aria-labelledby="mdadm-detail-heading">
          <header className="panel-head">
            <h2 id="mdadm-detail-heading">Arrays</h2>
            <span className="tag">observed</span>
          </header>
          {report.arrays.length > 0 ? (
            report.arrays.map((a) => <ArrayDetail key={a.device} a={a} />)
          ) : (
            <p className="empty">No arrays are listed in /proc/mdstat.</p>
          )}
          {report.truncated && (
            <p className="banner" role="status">
              Some arrays are not listed: the report exceeded its limit.
            </p>
          )}
        </section>
      )}

      {!report && (
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
          {data.status && !data.status.available && md.present && (
            <p className="banner" role="status">
              {data.status.reason}
            </p>
          )}
        </section>
      )}
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

/** Tone for mdadm's state words; the badge shows mdadm's text unchanged. */
function stateTone(state = ''): string {
  if (/degraded|fail|inactive/i.test(state)) return 'crit';
  if (/recover|resync|reshap|check/i.test(state)) return 'warn';
  return 'ok';
}

function count(v?: number): string {
  return v === undefined ? '?' : String(v);
}

/** One array as `mdadm --detail` describes it. */
function ArrayDetail({ a }: { a: MdadmDetail }) {
  return (
    <article className="zfs-pool" aria-label={`Array ${a.device}`}>
      <h3 className="subhead">
        <code>{a.device}</code>
        {a.name ? <> ({a.name})</> : null}{' '}
        {a.state && <span className={`status status-${stateTone(a.state)}`}>{a.state}</span>}
      </h3>
      {a.error ? (
        <p className="banner" role="status">
          Not described: {a.error}
        </p>
      ) : null}
      <dl className="facts">
        {a.level && (
          <div>
            <dt>Level</dt>
            <dd>{a.level}</dd>
          </div>
        )}
        {a.array_size_kib !== undefined && (
          <div>
            <dt>Size</dt>
            <dd>{formatBytes(a.array_size_kib * 1024)}</dd>
          </div>
        )}
        {a.raid_devices !== undefined && (
          <div>
            <dt>Devices</dt>
            <dd>
              {count(a.raid_devices)} in array · {count(a.active_devices)} active · {count(a.failed_devices)}{' '}
              failed · {count(a.spare_devices)} spare
            </dd>
          </div>
        )}
        {a.progress && (
          <div>
            <dt>In progress</dt>
            <dd>{a.progress}</dd>
          </div>
        )}
        {(a.layout || a.chunk_size) && (
          <div>
            <dt>Layout</dt>
            <dd>{[a.layout, a.chunk_size && `${a.chunk_size} chunk`].filter(Boolean).join(', ')}</dd>
          </div>
        )}
        {a.uuid && (
          <div>
            <dt>UUID</dt>
            <dd>
              <code>{a.uuid}</code>
            </dd>
          </div>
        )}
      </dl>
      {a.members.length > 0 && (
        <DataTable
          caption={`Members of ${a.device}`}
          rows={a.members.map((m, i) => ({ ...m, key: `${i}:${m.device ?? ''}` }))}
          rowKey={(m) => m.key}
          columns={[
            { id: 'slot', label: 'Slot', render: (m) => (m.slot === undefined ? '—' : m.slot) },
            { id: 'state', label: 'State', render: (m) => m.state },
            { id: 'device', label: 'Device', render: (m) => (m.device ? <code>{m.device}</code> : '') },
          ]}
        />
      )}
      {a.members_truncated && (
        <p className="banner" role="status">
          The member list is incomplete: only the first {a.members.length} rows of the mdadm device table are
          shown.
        </p>
      )}
    </article>
  );
}
