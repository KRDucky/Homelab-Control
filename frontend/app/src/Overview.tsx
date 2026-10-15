import type { ReactNode } from 'react';
import type { Meta, Overview as OverviewData } from './api';
import { formatBytes, formatUptime } from './format';
import { Meter } from './Meter';

// Match df(1): Use% = used / (used + available), so blocks reserved for root
// count against free space the way administrators expect.
function dfTotal(f: { used_bytes?: number; avail_bytes?: number }): number | undefined {
  if (f.used_bytes === undefined || f.avail_bytes === undefined) return undefined;
  return f.used_bytes + f.avail_bytes;
}

function Sources({ meta }: { meta: Meta }) {
  return (
    <details className="sources">
      <summary>Where this data comes from</summary>
      <ul>
        {meta.sources.map((s) => (
          <li key={s}>
            <code>{s}</code>
          </li>
        ))}
      </ul>
      {meta.errors && meta.errors.length > 0 && (
        <ul className="source-errors">
          {meta.errors.map((e) => (
            <li key={e}>{e}</li>
          ))}
        </ul>
      )}
    </details>
  );
}

function Section({ title, meta, children }: { title: string; meta: Meta; children: ReactNode }) {
  return (
    <section className="panel" aria-labelledby={`h-${title}`}>
      <header className="panel-head">
        <h2 id={`h-${title}`}>{title}</h2>
        {meta.status !== 'ok' && (
          <span className={`status status-${meta.status}`}>
            {meta.status === 'partial' ? 'Partly available' : 'Unavailable'}
          </span>
        )}
      </header>
      {children}
      <Sources meta={meta} />
    </section>
  );
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
        {filesystems.items.length === 0 ? (
          <p className="empty">No local filesystems were found in the mount table.</p>
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th scope="col">Mount point</th>
                  <th scope="col">Type</th>
                  <th scope="col">Kernel source</th>
                  <th scope="col">Usage</th>
                </tr>
              </thead>
              <tbody>
                {filesystems.items.map((f) => (
                  <tr key={f.mount_point}>
                    <th scope="row">
                      {f.mount_point}
                      {f.read_only && <span className="tag">read-only</span>}
                    </th>
                    <td>{f.fs_type}</td>
                    <td className="kernel-src" title="Kernel device names can change between boots">
                      {f.kernel_source}
                    </td>
                    <td className="usage-cell">
                      {f.probed ? (
                        <Meter
                          label={`${f.mount_point} usage`}
                          used={f.used_bytes}
                          total={dfTotal(f)}
                          detail={`${formatBytes(f.avail_bytes)} free of ${formatBytes(f.total_bytes)}`}
                        />
                      ) : (
                        <span className="muted">{f.skip_reason ?? 'Not checked'}</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <p className="footnote">
          Kernel source names such as /dev/sda1 can change between boots. Disk identity by serial and WWN
          arrives with the storage inventory.
        </p>
      </Section>

      <Section title="Network interfaces" meta={network}>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th scope="col">Interface</th>
                <th scope="col">State</th>
                <th scope="col">Addresses</th>
                <th scope="col">Link</th>
                <th scope="col">Received</th>
                <th scope="col">Sent</th>
              </tr>
            </thead>
            <tbody>
              {network.interfaces.map((n) => (
                <tr key={n.name}>
                  <th scope="row">
                    {n.name}
                    {!n.physical && <span className="tag">virtual</span>}
                  </th>
                  <td>
                    <span
                      className={`dot dot-${n.oper_state === 'up' ? 'up' : 'other'}`}
                      aria-hidden="true"
                    />
                    {n.oper_state ?? 'unknown'}
                  </td>
                  <td className="addr">
                    {n.addresses && n.addresses.length > 0 ? n.addresses.join(', ') : '—'}
                  </td>
                  <td>{n.speed_mbps ? `${n.speed_mbps} Mb/s` : '—'}</td>
                  <td>{formatBytes(n.rx_bytes)}</td>
                  <td>{formatBytes(n.tx_bytes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
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
