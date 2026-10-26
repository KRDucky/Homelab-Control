// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { getHardware, type Disk, type Hardware as HardwareData } from '../api';
import { formatBytes } from '../format';
import { DataTable } from '../DataTable';
import { Section } from '../ui';
import { explain, useData } from '../useData';

const confidenceText: Record<Disk['identity_confidence'], string> = {
  high: 'High — WWN',
  medium: 'Medium — serial number',
  low: 'Low — by-id link only',
  none: 'None — no stable identifier found',
};

const fieldLabels: Record<string, string> = {
  product_serial: 'System serial',
  product_uuid: 'System UUID',
  board_serial: 'Board serial',
  chassis_serial: 'Chassis serial',
};

function Identity({ id }: { id: HardwareData['identity'] }) {
  if (id.status !== 'ok' || !id.data) {
    return <p className="muted">Serial numbers and the system UUID are not shown: {id.reason}</p>;
  }
  if (!id.data.available) return <p className="muted">{id.data.reason}</p>;
  return (
    <dl className="facts">
      {Object.entries(fieldLabels).map(([k, label]) => {
        const f = id.data?.fields[k];
        return (
          <div key={k}>
            <dt>{label}</dt>
            <dd>
              {f?.status === 'present' && <span className="mono">{f.value}</span>}
              {f?.status === 'placeholder' && (
                <>
                  <span className="muted mono">{f.value}</span>{' '}
                  <span className="tag">vendor placeholder, not unique</span>
                </>
              )}
              {(f?.status === 'missing' || f?.status === 'unreadable' || !f) && (
                <span className="muted">Not reported</span>
              )}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}

export function Hardware({ onUnauthorized }: { onUnauthorized: () => void }) {
  const { data, error, live } = useData(getHardware, { onUnauthorized });
  if (error && !data) {
    return (
      <div className="page">
        <h1 className="page-title">Hardware</h1>
        <p className="banner" role="alert">
          {explain(error)}
        </p>
      </div>
    );
  }
  if (!data) return <p className="muted">Reading hardware inventory…</p>;
  const { system, pci, usb, disks, sensors } = data.inventory;
  const sys = [
    ['Manufacturer', system.vendor],
    ['Model', [system.product, system.version].filter(Boolean).join(' ')],
    ['Board', [system.board_vendor, system.board_name].filter(Boolean).join(' ')],
    ['Firmware', [system.bios_vendor, system.bios_version, system.bios_date].filter(Boolean).join(' ')],
    ['Chassis', system.chassis_type],
    ['Virtualisation', system.hypervisor],
  ].filter(([, v]) => v);

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Hardware</h1>
        <p className="page-sub">Read-only inventory. Nothing here changes your hardware.</p>
      </header>

      <Section title="This machine" live={live} meta={system} id="machine">
        {sys.length > 0 && (
          <dl className="facts">
            {sys.map(([k, v]) => (
              <div key={k}>
                <dt>{k}</dt>
                <dd>{v}</dd>
              </div>
            ))}
          </dl>
        )}
        {/* When SMBIOS is absent the section note already says so; don't repeat it. */}
        {!(data.identity.data && !data.identity.data.available) && (
          <>
            <h3 className="subhead">Identifiers</h3>
            <Identity id={data.identity} />
          </>
        )}
      </Section>

      <Section title="Storage devices" live={live} meta={disks} id="disks">
        {disks.devices.length > 0 && (
          <DataTable
            caption="Storage devices"
            rows={disks.devices}
            rowKey={(d) => d.identity ?? d.kernel_name}
            columns={[
              {
                id: 'device',
                label: 'Device',
                sortValue: (d) => d.model ?? '',
                render: (d) => (
                  <>
                    {d.model ?? 'Unknown model'}
                    <div className="cell-note" title={d.kernel_name_note}>
                      currently {d.kernel_name} (changes between boots)
                    </div>
                  </>
                ),
              },
              {
                id: 'size',
                label: 'Size',
                sortValue: (d) => d.size_bytes ?? 0,
                render: (d) => formatBytes(d.size_bytes),
              },
              {
                id: 'type',
                label: 'Type',
                render: (d) => (
                  <>
                    {d.transport}
                    {d.rotational === true && <span className="tag">HDD</span>}
                    {d.rotational === false && !d.virtual && <span className="tag">SSD</span>}
                    {d.removable && <span className="tag">removable</span>}
                  </>
                ),
              },
              { id: 'identity', label: 'Identity', render: (d) => <code>{d.identity ?? 'None'}</code> },
              {
                id: 'confidence',
                label: 'Confidence',
                render: (d) => (
                  <span className={`conf conf-${d.identity_confidence}`}>
                    {confidenceText[d.identity_confidence]}
                  </span>
                ),
              },
            ]}
          />
        )}
        <p className="footnote">
          Physical bay locations are not mapped yet. Identity comes from the drive's WWN or serial number,
          never from its kernel name.
        </p>
      </Section>

      <Section title="PCI devices" live={live} meta={pci} id="pci">
        {pci.devices.length > 0 && (
          <DataTable
            caption="PCI devices"
            rows={pci.devices}
            rowKey={(d) => d.address}
            columns={[
              {
                id: 'address',
                label: 'Address',
                sortValue: (d) => d.address,
                render: (d) => <code>{d.address}</code>,
              },
              {
                id: 'class',
                label: 'Class',
                sortValue: (d) => d.class || d.class_code,
                render: (d) => d.class || d.class_code,
              },
              {
                id: 'device',
                label: 'Device',
                render: (d) => (
                  <>
                    {d.device_name ?? `Device ${d.vendor_id}:${d.device_id}`}
                    {d.vendor_name && <div className="cell-note">{d.vendor_name}</div>}
                  </>
                ),
              },
              {
                id: 'driver',
                label: 'Driver',
                render: (d) => d.driver ?? <span className="muted">none bound</span>,
              },
            ]}
          />
        )}
      </Section>

      <div className="grid">
        <Section title="USB devices" live={live} meta={usb} id="usb">
          {usb.devices.length > 0 && (
            <ul className="plain-list">
              {usb.devices.map((d) => (
                <li key={d.bus_path}>
                  {[d.manufacturer, d.product].filter(Boolean).join(' ') || `${d.vendor_id}:${d.product_id}`}
                  {d.speed_mbps && <span className="muted"> ({d.speed_mbps} Mb/s)</span>}
                </li>
              ))}
            </ul>
          )}
        </Section>
        <Section title="Sensors" live={live} meta={sensors} id="sensors">
          {sensors.readings.length > 0 && (
            <ul className="plain-list">
              {sensors.readings.map((s) => (
                <li key={`${s.chip}-${s.label}`}>
                  <span className="muted">{s.chip}</span> {s.label}:{' '}
                  <strong>
                    {s.kind === 'temperature' ? s.value.toFixed(1) : Math.round(s.value)} {s.unit}
                  </strong>
                </li>
              ))}
            </ul>
          )}
          <p className="footnote">Monitoring only. Fan control is not available.</p>
        </Section>
      </div>
    </div>
  );
}
