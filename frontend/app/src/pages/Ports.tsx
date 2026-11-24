// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useMemo, useState } from 'react';
import { getPorts, type PortMap, type PortSocket } from '../api';
import { explain, useData } from '../useData';

/**
 * Port use (M11, read-only). One host-wide map of listening TCP and bound UDP
 * sockets with the account, process, systemd unit and container that own
 * them. Nothing on this page opens, closes or rebinds a port.
 */
export function Ports({ csrf, onUnauthorized }: { csrf: string; onUnauthorized: () => void }) {
  const fetchPorts = useCallback(() => getPorts(csrf), [csrf]);
  const { data, error } = useData(fetchPorts, { onUnauthorized });
  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">Port use</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading the host port map…'}</p>
      </div>
    );
  }
  return <PortsView data={data} error={error} />;
}

const EXPOSURE_LABEL: Record<PortSocket['exposure'], string> = {
  loopback: 'This host only',
  all: 'All addresses',
  address: 'One address',
};

function ownerText(s: PortSocket): string {
  if (s.owners.length === 0) return 'Not identified';
  return s.owners
    .map((o) => {
      if (o.source === 'systemd-socket') {
        return `systemd socket ${o.socket ?? ''}${o.unit ? ` → ${o.unit}` : ''}`;
      }
      const where = o.container ? `container ${o.container}` : o.unit;
      if (o.source === 'account') {
        return `${where ?? 'unknown unit'} (inferred from the account)`;
      }
      return `${o.command || '?'} (PID ${o.pid}${where ? `, ${where}` : ''})`;
    })
    .join('; ');
}

export function PortsView({ data, error }: { data: PortMap; error: Error | null }) {
  const [filter, setFilter] = useState('');
  const [exposedOnly, setExposedOnly] = useState(false);
  const rows = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return data.sockets.filter((s) => {
      if (exposedOnly && s.exposure === 'loopback') return false;
      if (!q) return true;
      const hay = `${s.port} ${s.proto} ${s.address} ${s.account ?? ''} ${ownerText(s)}`.toLowerCase();
      return hay.includes(q);
    });
  }, [data.sockets, filter, exposedOnly]);
  const exposed = data.sockets.filter((s) => s.exposure !== 'loopback').length;

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Port use</h1>
        <p className="page-sub">
          Read-only map of listening ports on this host, from the kernel socket tables of the host network
          namespace. Changing a port is done in the module that owns the service, not here.
        </p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}
      {data.truncated && (
        <p className="banner" role="status">
          The inventory hit a size limit; more sockets may exist than are shown.
        </p>
      )}
      {data.processesHidden > 0 && (
        <p className="banner" role="status">
          The open files of {data.processesHidden} of {data.processesScanned + data.processesHidden} processes
          could not be read. For their ports, the owner comes from systemd's socket list, is inferred from the
          account (labelled), or is shown as not identified.
        </p>
      )}

      <section className="panel" aria-labelledby="ports-heading">
        <header className="panel-head">
          <h2 id="ports-heading">Listening sockets</h2>
          <span className="tag">observed</span>
        </header>
        <p>
          {data.sockets.length} sockets, {exposed} reachable beyond this host. Sources:{' '}
          {data.sources.join(', ')}.
        </p>
        <div className="toolbar">
          <label>
            Filter <input type="search" value={filter} onChange={(e) => setFilter(e.target.value)} />
          </label>{' '}
          <label>
            <input type="checkbox" checked={exposedOnly} onChange={(e) => setExposedOnly(e.target.checked)} />{' '}
            Hide ports bound to this host only
          </label>
        </div>
        {rows.length > 0 ? (
          <table>
            <caption>Listening TCP and bound UDP sockets</caption>
            <thead>
              <tr>
                <th scope="col">Port</th>
                <th scope="col">Protocol</th>
                <th scope="col">Address</th>
                <th scope="col">Reachable from</th>
                <th scope="col">Account</th>
                <th scope="col">Owner</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <tr key={`${s.proto}|${s.address}|${s.port}`}>
                  <th scope="row">{s.port}</th>
                  <td>{s.proto}</td>
                  <td>
                    <code>{s.address}</code>
                  </td>
                  <td>{EXPOSURE_LABEL[s.exposure] ?? s.exposure}</td>
                  <td>{s.account || `uid ${s.uid}`}</td>
                  <td>{ownerText(s)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="empty">
            {data.sockets.length === 0 ? 'No listening sockets were found.' : 'No socket matches the filter.'}
          </p>
        )}
      </section>
    </div>
  );
}
