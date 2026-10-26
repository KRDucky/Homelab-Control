// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import type { Overview as OverviewData } from './api';
import { DataTable } from './DataTable';
import { Section } from './ui';
import { formatBytes, formatUptime } from './format';
import { Meter } from './Meter';

// Match df(1): Use% = used / (used + available), so blocks reserved for root
// count against free space the way administrators expect.
function dfTotal(f: { used_bytes?: number; avail_bytes?: number }): number | undefined {
  if (f.used_bytes === undefined || f.avail_bytes === undefined) return undefined;
  return f.used_bytes + f.avail_bytes;
}

export function Overview({ data }: { data: OverviewData }) {
  const { system, cpu, memory, filesystems, network, legacy_webmin: legacy } = data;
  const swapUsed = memory.swap_total_bytes > 0 ? memory.swap_total_bytes - memory.swap_free_bytes : undefined;
  return (
    <div className="overview">
      <section className="faceplate" aria-label="Host">
        <h1 className="faceplate-host">{system.hostname ?? 'Unknown host'}</h1>
        <dl className="faceplate-facts">
          <div>
            <dt>System</dt>
            <dd>{system.os.pretty_name ?? 'Unknown OS'}</dd>
          </div>
          <div>
            <dt>Kernel</dt>
            <dd>{system.kernel ?? '—'}</dd>
          </div>
          <div>
            <dt>Up for</dt>
            <dd>{formatUptime(system.uptime_seconds)}</dd>
          </div>
          <div>
            <dt>Load (1, 5, 15 min)</dt>
            <dd>{system.load_average.map((l) => l.toFixed(2)).join('  ')}</dd>
          </div>
          <div>
            <dt>Package family</dt>
            <dd>{system.distro_family === 'unknown' ? 'Not recognised' : system.distro_family}</dd>
          </div>
        </dl>
      </section>

      <div className="grid">
        <Section title="Processor" meta={cpu}>
          <Meter
            label="CPU busy"
            used={cpu.usage_percent}
            total={cpu.usage_percent === undefined ? undefined : 100}
            detail={`${cpu.logical_cpus} logical CPUs, ${cpu.model ?? 'model not reported'}`}
          />
        </Section>
        <Section title="Memory" meta={memory}>
          <Meter
            label="RAM in use"
            used={memory.used_bytes}
            total={memory.total_bytes}
            detail={`${formatBytes(memory.used_bytes)} of ${formatBytes(memory.total_bytes)}`}
          />
          {memory.swap_total_bytes > 0 ? (
            <Meter
              label="Swap in use"
              used={swapUsed}
              total={memory.swap_total_bytes}
              detail={`${formatBytes(swapUsed)} of ${formatBytes(memory.swap_total_bytes)}`}
            />
          ) : (
            <p className="meter-detail">No swap is configured on this host.</p>
          )}
        </Section>
      </div>

      <Section title="Filesystems" meta={filesystems}>
        {filesystems.note && <p className="muted">{filesystems.note}</p>}
        {filesystems.items.length === 0 ? (
          <p className="empty">No local filesystems were found in the mount table.</p>
        ) : (
          <DataTable
            caption="Filesystems"
            rows={filesystems.items}
            rowKey={(f) => f.mount_point}
            columns={[
              {
                id: 'mount',
                label: 'Mount point',
                render: (f) => (
                  <>
                    <code>{f.mount_point}</code>
                    {f.read_only && <span className="tag">read-only</span>}
                  </>
                ),
              },
              { id: 'type', label: 'Type', render: (f) => f.fs_type },
              {
                id: 'source',
                label: 'Kernel source',
                render: (f) => (
                  <code title="Kernel device names can change between boots">{f.kernel_source}</code>
                ),
              },
              {
                id: 'usage',
                label: 'Usage',
                render: (f) =>
                  f.probed ? (
                    <Meter
                      label={`${f.mount_point} usage`}
                      used={f.used_bytes}
                      total={dfTotal(f)}
                      detail={`${formatBytes(f.avail_bytes)} free of ${formatBytes(f.total_bytes)}`}
                    />
                  ) : (
                    <span className="muted">{f.skip_reason ?? 'Not checked'}</span>
                  ),
              },
            ]}
          />
        )}
        <p className="footnote">
          Kernel source names such as /dev/sda1 can change between boots. Disk identity by serial and WWN
          arrives with the storage inventory.
        </p>
      </Section>

      <Section title="Network interfaces" meta={network}>
        <DataTable
          caption="Network interfaces"
          rows={network.interfaces}
          rowKey={(n) => n.name}
          columns={[
            {
              id: 'name',
              label: 'Interface',
              render: (n) => (
                <>
                  <code>{n.name}</code>
                  {!n.physical && <span className="tag">virtual</span>}
                </>
              ),
            },
            { id: 'state', label: 'State', render: (n) => n.oper_state ?? 'unknown' },
            {
              id: 'addresses',
              label: 'Addresses',
              render: (n) => <code>{n.addresses?.join(', ') || '—'}</code>,
            },
            { id: 'link', label: 'Link', render: (n) => (n.speed_mbps ? `${n.speed_mbps} Mb/s` : '—') },
            { id: 'received', label: 'Received', render: (n) => formatBytes(n.rx_bytes) },
            { id: 'sent', label: 'Sent', render: (n) => formatBytes(n.tx_bytes) },
          ]}
        />
      </Section>

      <Section title="Legacy Webmin" meta={legacy}>
        {legacy.installed ? (
          <p>
            Upstream Webmin {legacy.version ?? '(version not readable)'} is installed on this host.
            {legacy.privilege_needed && <span className="muted"> {legacy.privilege_needed}.</span>}
          </p>
        ) : (
          <p>Upstream Webmin isn’t installed at /etc/webmin on this host.</p>
        )}
        {legacy.configured_url && (
          <p>
            <a href={legacy.configured_url} rel="noopener noreferrer">
              Open the legacy Webmin interface
            </a>
          </p>
        )}
      </Section>
      <p className="collected">Collected {new Date(data.collected_at).toLocaleString()}</p>
    </div>
  );
}
