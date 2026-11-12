// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { useEffect, useRef, useState, type FormEvent } from 'react';
import {
  ApiError,
  getTraefikInventory,
  getTraefikDiagnostics,
  getComposeApplications,
  previewCompose,
  applyComposeFile,
  applyComposeLabels,
  applyTraefikSetup,
  getPlatforms,
  type PlatformView,
  type TraefikInventory,
  type TraefikDiagnostics,
  type TraefikModel,
  type ComposeCatalog,
  type ComposeProposal,
  type ComposeService,
} from '../api';
import { DataTable } from '../DataTable';
import { explain } from '../useData';
import { PlatformProxy } from './PlatformProxy';
import './Traefik.css';
const sections = [
  'Overview',
  'Proxy Hosts',
  'Services',
  'Middleware',
  'Certificates',
  'Settings',
  'Diagnostics',
] as const;
type Section = (typeof sections)[number];
const unknown = 'Unknown';
function hostsOf(rule?: string): string[] {
  return rule ? [...rule.matchAll(/Host\(`([a-z0-9.-]+)`\)/g)].flatMap((m) => (m[1] ? [m[1]] : [])) : [];
}
function pathsOf(rule?: string): string[] {
  return rule
    ? [...rule.matchAll(/Path(?:Prefix)?\(`(\/[A-Za-z0-9._~/-]*)`\)/g)].flatMap((m) => (m[1] ? [m[1]] : []))
    : [];
}
function routeHeadline(rule?: string): string {
  const hosts = hostsOf(rule);
  if (hosts.length) return hosts.join(', ');
  const paths = pathsOf(rule);
  return paths.length ? paths.join(', ') : unknown;
}
function isTraefikProxy(s: ComposeService): boolean {
  return s.officialTraefik === true || (s.compose && s.service?.toLowerCase() === 'traefik');
}
export function Traefik({ csrf, onUnauthorized }: { csrf: string; onUnauthorized: () => void }) {
  const [inventory, setInventory] = useState<TraefikInventory | null>(null);
  const [diagnostics, setDiagnostics] = useState<TraefikDiagnostics | null>(null);
  const [catalog, setCatalog] = useState<ComposeCatalog | null>(null);
  const [discoveryError, setDiscoveryError] = useState('Loading application discovery…');
  const [error, setError] = useState<Error | null>(null);
  const [section, setSection] = useState<Section>('Overview');
  const [adding, setAdding] = useState(false);
  const [revision, setRevision] = useState(0);
  const [application, setApplication] = useState('');
  const [serviceId, setServiceId] = useState('');
  const [protocol, setProtocol] = useState<'http' | 'https'>('http');
  const [proposal, setProposal] = useState<ComposeProposal | null>(null);
  const [platforms, setPlatforms] = useState<PlatformView[]>([]);
  const [platformId, setPlatformId] = useState('');
  const [busy, setBusy] = useState(false);
  const [wrote, setWrote] = useState('');
  const [labelsWrote, setLabelsWrote] = useState('');
  const [setupWrote, setSetupWrote] = useState('');
  const previewBody = useRef<{
    serviceId: string;
    network: string;
    model: TraefikModel;
    confirmUnverifiedPort?: boolean;
  } | null>(null);
  const previewVersion = useRef(0);
  useEffect(() => {
    let cancelled = false;
    Promise.allSettled([
      getTraefikInventory(csrf),
      getTraefikDiagnostics(csrf),
      getComposeApplications(csrf),
      getPlatforms(csrf),
    ]).then(([items, checks, apps, registered]) => {
      if (cancelled) return;
      // The platform registry is optional; without it the Compose flow is unchanged.
      const visible = registered.status === 'fulfilled' ? registered.value.platforms : [];
      setPlatforms(visible);
      setPlatformId((current) =>
        visible.some((p) => p.id === current)
          ? current
          : registered.status === 'fulfilled'
            ? (registered.value.autoSelected ?? '')
            : '',
      );
      const denied = [items, checks].some(
        (v) =>
          v.status === 'rejected' && v.reason instanceof ApiError && [401, 403].includes(v.reason.status),
      );
      setInventory(!denied && items.status === 'fulfilled' ? items.value : null);
      setDiagnostics(!denied && checks.status === 'fulfilled' ? checks.value : null);
      setCatalog(!denied && apps.status === 'fulfilled' ? apps.value : null);
      setDiscoveryError(
        apps.status === 'rejected'
          ? 'Compose discovery is unavailable or not permitted. Check source availability and your docker.compose.read permission.'
          : '',
      );
      const failure =
        items.status === 'rejected' ? items.reason : checks.status === 'rejected' ? checks.reason : null;
      if (failure instanceof ApiError && failure.status === 401) onUnauthorized();
      setError(failure instanceof Error ? failure : null);
    });
    return () => {
      cancelled = true;
    };
  }, [csrf, onUnauthorized, revision]);
  const proxiedApps = catalog?.services.filter((s) => s.compose && !isTraefikProxy(s)) ?? [];
  const services = proxiedApps.filter((s) => s.project === application);
  const selected = services.find((s) => s.id === serviceId);
  const tcpPorts = selected?.ports.filter((p) => p.protocol === 'tcp') ?? [];
  const platform = platforms.find((p) => p.id === platformId);
  // Docker platforms keep the verified Phase 6/7 Compose flow and endpoints.
  const legacyFlow = platforms.length === 0 || platform?.kind === 'docker';
  // Steps are numbered after the optional "Select platform" step.
  const firstStep = platforms.length > 0 ? 2 : 1;
  const warnings =
    (inventory?.routers.filter((r) => !r.rule).length ?? 0) +
    (catalog?.traefik.dockerProvider === false ? 1 : 0);
  async function generate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setProposal(null);
    setWrote('');
    setLabelsWrote('');
    setError(null);
    setBusy(true);
    const version = previewVersion.current;
    const data = new FormData(event.currentTarget);
    const model: TraefikModel = {
      name: String(data.get('name')),
      host: String(data.get('host')),
      port: Number(data.get('port')),
      entrypoint: String(data.get('entrypoint')),
      protocol,
      upstreamScheme: data.get('upstreamScheme') === 'https' ? 'https' : 'http',
    };
    const path = String(data.get('path') ?? '').trim();
    if (path) model.path = path;
    if (protocol === 'https')
      model.tls = data.get('resolver') ? { certificateResolver: String(data.get('resolver')) } : {};
    const body = {
      serviceId,
      network: String(data.get('network')),
      model,
      ...(data.get('confirmUnverifiedPort') === 'on' ? { confirmUnverifiedPort: true } : {}),
    };
    previewBody.current = body;
    try {
      const result = await previewCompose(body, csrf);
      if (version === previewVersion.current) setProposal(result);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) onUnauthorized();
      setError(e instanceof Error ? e : new Error('Preview failed'));
    } finally {
      setBusy(false);
    }
  }
  async function writeFileYAML() {
    if (!previewBody.current) return;
    setBusy(true);
    setError(null);
    try {
      const result = await applyComposeFile(previewBody.current, csrf);
      setWrote(result.wrote);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) onUnauthorized();
      setError(e instanceof Error ? e : new Error('File YAML could not be written'));
    } finally {
      setBusy(false);
    }
  }
  async function writeComposeLabels() {
    if (!previewBody.current) return;
    setBusy(true);
    setError(null);
    try {
      const result = await applyComposeLabels(previewBody.current, csrf);
      setLabelsWrote(result.wrote);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) onUnauthorized();
      setError(e instanceof Error ? e : new Error('Compose labels could not be written'));
    } finally {
      setBusy(false);
    }
  }
  async function writeOfficialSetup() {
    setBusy(true);
    setError(null);
    try {
      const result = await applyTraefikSetup(csrf);
      setSetupWrote(result.wrote.join(', '));
      setRevision((n) => n + 1);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) onUnauthorized();
      setError(e instanceof Error ? e : new Error('Official Traefik compose could not be written'));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="page traefik-page">
      <p className="muted" aria-label="Feature location">
        Networking → Reverse Proxy → Traefik
      </p>
      <div className="proxy-heading">
        <div>
          <h1 className="page-title">Traefik</h1>
          <p className="muted">Reverse proxy hosts, services and connection health</p>
        </div>
        <button
          type="button"
          onClick={() => {
            setInventory(null);
            setDiagnostics(null);
            setProposal(null);
            previewVersion.current++;
            setRevision((n) => n + 1);
          }}
        >
          Refresh inventory and diagnostics
        </button>
      </div>
      <p className="banner" role="status">
        Preview generates labels. Apply writes them into the selected Compose file. Recreate that service
        separately so Traefik reloads them.
      </p>
      <nav aria-label="Traefik sections" className="proxy-tabs">
        {sections.map((item) => (
          <button
            type="button"
            key={item}
            aria-current={section === item ? 'page' : undefined}
            disabled={item === 'Certificates'}
            onClick={() => setSection(item)}
          >
            {item}
            {item === 'Certificates' ? ' · Planned' : ''}
          </button>
        ))}
      </nav>
      {error && (
        <p className="banner" role="alert">
          {explain(error)}
        </p>
      )}
      {section === 'Overview' && (
        <>
          <div className="proxy-summary" aria-label="Traefik overview">
            {[
              ['Connection health', diagnostics?.status ?? unknown],
              [
                'Active routers',
                inventory ? inventory.routers.filter((r) => r.status === 'enabled').length : unknown,
              ],
              ['Registered services', inventory?.services.length ?? unknown],
              ['Middleware', inventory?.middlewares.length ?? unknown],
              ['Configuration warnings', inventory ? warnings : unknown],
            ].map(([label, value]) => (
              <section className="proxy-card" key={label}>
                <h2>{label}</h2>
                <strong>{value}</strong>
              </section>
            ))}
          </div>
          <section>
            <h2>Manage your proxy hosts</h2>
            <p>
              Select a Docker Compose or Kubernetes workload, set hostname and optional path, then review
              file YAML, Compose labels, or Kubernetes manifests. Nothing is applied.
            </p>
            <button type="button" onClick={() => setSection('Proxy Hosts')}>
              View Proxy Hosts
            </button>
            <p className="muted">
              Certificates management is planned. Middleware assignment and multiple hostnames are not in
              this preview model. Path prefix is optional.
            </p>
            {warnings > 0 && (
              <p className="banner">
                Some inventory details are unknown or Docker provider support is unavailable. Review
                Diagnostics and Settings before planning changes.
              </p>
            )}
          </section>
          {catalog && (
            <section>
              <h2>Traefik instance</h2>
              {catalog.traefik.instance?.present ? (
                <p>
                  Traefik detected: {catalog.traefik.instance.container}
                  {catalog.traefik.instance.running ? ' (running)' : ' (stopped)'}. Configure proxy hosts
                  for your applications.
                </p>
              ) : catalog.setupAvailable ? (
                <>
                  <p>
                    No Traefik container found. Write official Traefik compose to get started. Host ports 80
                    and 443 stay free. CORE does not start the stack.
                  </p>
                  <button type="button" disabled={busy} onClick={() => void writeOfficialSetup()}>
                    {busy ? 'Writing compose…' : 'Write Traefik compose'}
                  </button>
                  {setupWrote ? (
                    <p>Wrote {setupWrote}. Start the stack with docker compose from that directory.</p>
                  ) : null}
                </>
              ) : (
                <p>No Traefik container found. Easy-setup is not configured on this CORE.</p>
              )}
            </section>
          )}
        </>
      )}
      {section === 'Proxy Hosts' && (
        <section>
          <div className="proxy-heading">
            <h2>Proxy Hosts</h2>
            <button
              type="button"
              disabled={(!catalog && platforms.length === 0) || !inventory}
              onClick={() => {
                previewVersion.current++;
                setAdding(!adding);
                setProposal(null);
                setWrote('');
                setLabelsWrote('');
              }}
            >
              {adding ? 'Close configuration' : 'Add Proxy Host'}
            </button>
          </div>
          {!catalog && platforms.length === 0 && <p className="banner">{discoveryError}</p>}
          <DataTable
            caption="Traefik proxy hosts"
            rows={inventory?.routers ?? []}
            rowKey={(r) => r.name}
            columns={[
              {
                id: 'host',
                label: 'Hostname',
                render: (r) => (
                  <>
                    <span>{routeHeadline(r.rule)}</span>
                    <br />
                    <small className="muted">{r.rule ? `${r.name} · ${r.rule}` : r.name}</small>
                  </>
                ),
              },
              { id: 'app', label: 'Target application', render: () => unknown },
              { id: 'port', label: 'Target port', render: () => unknown },
              {
                id: 'protocol',
                label: 'HTTP / HTTPS',
                render: (r) => (r.tls === undefined ? unknown : r.tls ? 'HTTPS' : 'HTTP'),
              },
              { id: 'status', label: 'Router status', render: (r) => r.status },
              { id: 'provider', label: 'Provider', render: (r) => r.provider },
              {
                id: 'warnings',
                label: 'Configuration warnings',
                render: (r) =>
                  !r.rule ? 'Incomplete inventory details' : 'Target application and port unverified',
              },
            ]}
          />
          <p className="muted">
            Application and internal port are unknown unless verified by a discovery adapter. Router identity
            and service references do not establish Compose ownership.
          </p>
          {adding && (catalog || platforms.length > 0) && (
            <section className="proxy-config">
              <h2>Add Proxy Host</h2>
              {platforms.length > 0 && (
                <fieldset>
                  <legend>1. Select platform</legend>
                  {platforms.length === 1 && platform ? (
                    <p>
                      Platform: {platform.displayName} ({platform.kind}, {platform.mode}) — the only
                      registered platform, selected automatically.
                    </p>
                  ) : (
                    <>
                      <label htmlFor="proxy-platform">Container platform</label>
                      <select
                        id="proxy-platform"
                        required
                        value={platformId}
                        onChange={(e) => {
                          previewVersion.current++;
                          setProposal(null);
                          setPlatformId(e.target.value);
                        }}
                      >
                        <option value="">Select a platform</option>
                        {platforms.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.displayName} · {p.distribution} · {p.mode}
                            {p.fixture ? ' (demonstration)' : ''}
                          </option>
                        ))}
                      </select>
                    </>
                  )}
                </fieldset>
              )}
              {platform && !legacyFlow && (
                <PlatformProxy
                  key={platform.id}
                  csrf={csrf}
                  platform={platform}
                  onUnauthorized={onUnauthorized}
                />
              )}
              {legacyFlow && !catalog && <p className="banner">{discoveryError}</p>}
              {legacyFlow && catalog && catalog.fixture && (
                <p className="banner">
                  Demonstration applications — fixture data, not your Docker applications.
                </p>
              )}
              {legacyFlow && catalog && (
                <form
                  onSubmit={generate}
                  onChange={() => {
                    previewVersion.current++;
                    setProposal(null);
                    setWrote('');
                    setLabelsWrote('');
                  }}
                  className="proxy-form"
                >
                  <fieldset>
                    <legend>{firstStep}. Select application</legend>
                    <label htmlFor="proxy-app">Docker Compose application</label>
                    <select
                      id="proxy-app"
                      value={application}
                      required
                      onChange={(e) => {
                        setApplication(e.target.value);
                        setServiceId('');
                      }}
                    >
                      <option value="">Select an application</option>
                      {[...new Set(proxiedApps.map((s) => s.project))].map((project) => (
                        <option key={project} value={project}>
                          {project}
                        </option>
                      ))}
                    </select>
                    <label htmlFor="proxy-service">Service / container</label>
                    <select
                      id="proxy-service"
                      required
                      value={serviceId}
                      onChange={(e) => setServiceId(e.target.value)}
                    >
                      <option value="">Select a service</option>
                      {services.map((s) => (
                        <option
                          key={s.id}
                          value={s.id}
                          disabled={
                            s.unsupportedDeployment || s.unsupportedNetworking || s.state !== 'running'
                          }
                        >
                          {s.service} · {s.container} ({s.state})
                          {s.unsupportedDeployment
                            ? ' · Unsupported deployment'
                            : s.unsupportedNetworking
                              ? ' · Unsupported networking'
                              : ''}
                        </option>
                      ))}
                    </select>
                    <label htmlFor="proxy-port">Internal application port</label>
                    {tcpPorts.length > 0 ? (
                      <select
                        id="proxy-port"
                        name="port"
                        key={serviceId}
                        required
                        defaultValue={tcpPorts.length === 1 ? String(tcpPorts[0]?.port) : ''}
                      >
                        <option value="">Select an internal TCP port</option>
                        {tcpPorts.map((p) => (
                          <option key={p.port} value={p.port}>
                            {p.port}
                          </option>
                        ))}
                      </select>
                    ) : (
                      <>
                        <input
                          id="proxy-port"
                          name="port"
                          key={serviceId}
                          type="number"
                          min={1}
                          max={65535}
                          step={1}
                          required
                        />
                        <label>
                          <input type="checkbox" name="confirmUnverifiedPort" required />I supplied this
                          internal port explicitly; it is not verified by Docker metadata.
                        </label>
                      </>
                    )}
                    {selected && (
                      <section aria-label="Selected application metadata">
                        <p>
                          Container status: {selected.state}. Observed replicas:{' '}
                          {selected.replicaCount ?? unknown}.
                        </p>
                        <p>
                          Docker networks:{' '}
                          {selected.networks.length ? selected.networks.join(', ') : 'None reported'}.
                        </p>
                        {selected.replicaCount !== undefined && selected.replicaCount > 1 && (
                          <p className="banner">
                            Multiple replicas require manual review; preview cannot select a trusted Compose
                            source.
                          </p>
                        )}
                        {selected.hiddenTraefikSettings && (
                          <p className="banner">
                            Unsupported or sensitive existing Traefik configuration needs manual review;
                            values are not exposed.
                          </p>
                        )}
                        <details>
                          <summary>Existing reverse-proxy configuration</summary>
                          <pre>{JSON.stringify(selected.traefikLabels, null, 2)}</pre>
                        </details>
                      </section>
                    )}
                    <p className="muted">
                      Only running, supported Compose containers can be selected. Stopped and unsupported
                      services remain visible with their status.
                    </p>
                    <p className="muted">
                      Published host ports are never substituted for internal application ports. Standalone
                      containers are excluded.
                    </p>
                  </fieldset>
                  <fieldset>
                    <legend>{firstStep + 1}. Configure hostname</legend>
                    <label htmlFor="proxy-host">Public hostname</label>
                    <input
                      id="proxy-host"
                      name="host"
                      placeholder="notes.example.com"
                      required
                      maxLength={253}
                      pattern={'[a-z0-9.\\-]+\\.[a-z0-9\\-]+'}
                    />
                    <label htmlFor="proxy-path">Path prefix (optional)</label>
                    <input
                      id="proxy-path"
                      name="path"
                      placeholder="/app"
                      maxLength={201}
                      pattern={'/[A-Za-z0-9._~/-]*'}
                    />
                    <p className="muted">Leave empty for the whole host. Example: /app</p>
                    <label htmlFor="proxy-protocol">Incoming protocol</label>
                    <select
                      id="proxy-protocol"
                      name="protocol"
                      value={protocol}
                      onChange={(e) => setProtocol(e.target.value === 'https' ? 'https' : 'http')}
                    >
                      <option value="http">HTTP</option>
                      <option value="https">HTTPS (TLS)</option>
                    </select>
                    {protocol === 'https' && (
                      <>
                        <label htmlFor="proxy-resolver">Existing certificate resolver (optional)</label>
                        <select id="proxy-resolver" name="resolver">
                          <option value="">Existing/default certificate (unverified)</option>
                          {catalog.traefik.resolvers.map((r) => (
                            <option key={r}>{r}</option>
                          ))}
                        </select>
                        <p className="muted">
                          Incoming TLS and upstream encryption are independent. A resolver must already exist;
                          issuance and certificate management are unavailable.
                        </p>
                      </>
                    )}
                  </fieldset>
                  <details>
                    <summary>Advanced configuration</summary>
                    <label htmlFor="proxy-name">Router and service name</label>
                    <input
                      id="proxy-name"
                      name="name"
                      defaultValue="proxy-host"
                      required
                      pattern={'[a-z][a-z0-9\\-]{0,62}'}
                    />
                    <label htmlFor="proxy-entrypoint">Entrypoint</label>
                    <select id="proxy-entrypoint" name="entrypoint" required>
                      {catalog.traefik.entrypoints.map((e) => (
                        <option key={e}>{e}</option>
                      ))}
                    </select>
                    <label htmlFor="proxy-upstream">Upstream scheme</label>
                    <select id="proxy-upstream" name="upstreamScheme">
                      <option value="http">HTTP</option>
                      <option value="https">HTTPS (certificate verification required)</option>
                    </select>
                    <label htmlFor="proxy-network">Docker network</label>
                    <select id="proxy-network" name="network" required>
                      {catalog.traefik.networks.map((n) => (
                        <option key={n}>{n}</option>
                      ))}
                    </select>
                    <p className="muted">
                      Only confirmed Traefik networks and entrypoints are selectable. Multiple hostnames,
                      middleware selection, independent service names and extra TLS options require a future
                      validated model.
                    </p>
                  </details>
                  <button
                    type="submit"
                    disabled={
                      busy ||
                      !selected ||
                      selected.state !== 'running' ||
                      selected.unsupportedDeployment ||
                      selected.unsupportedNetworking
                    }
                  >
                    {busy ? 'Generating preview…' : 'Preview configuration'}
                  </button>
                </form>
              )}
            </section>
          )}
          {proposal && (
            <section aria-label="Configuration proposal">
              <h2>Proposed Reverse Proxy Configuration</h2>
              <dl className="proxy-review-summary">
                <dt>Application</dt>
                <dd>{proposal.summary?.application ?? proposal.project}</dd>
                <dt>Public hostname</dt>
                <dd>{proposal.summary?.hostname ?? unknown}</dd>
                <dt>Path prefix</dt>
                <dd>{proposal.summary?.path || '/'}</dd>
                <dt>Target container/service</dt>
                <dd>
                  {proposal.summary
                    ? `${proposal.summary.targetService} · ${proposal.summary.container}`
                    : proposal.service}
                </dd>
                <dt>Internal port</dt>
                <dd>{proposal.summary?.internalPort ?? unknown}</dd>
                <dt>HTTP / HTTPS</dt>
                <dd>{proposal.summary?.protocol.toUpperCase() ?? unknown}</dd>
                <dt>Certificate resolver</dt>
                <dd>
                  {proposal.summary
                    ? proposal.summary.protocol === 'http'
                      ? 'Not requested'
                      : proposal.summary.certificateResolver || 'Existing/default certificate (unverified)'
                    : unknown}
                </dd>
                <dt>Docker network</dt>
                <dd>{proposal.summary?.network ?? unknown}</dd>
              </dl>
              <p>
                Apply labels writes Traefik keys into the selected Compose service. Recreate that service so
                Traefik&apos;s Docker provider sees them. File YAML is the file-provider fallback.
              </p>
              {proposal.fileYAML && (
                <>
                  <h3>File provider YAML</h3>
                  <p className="muted">
                    Copy into the dynamic directory (for example /config/dynamic), or write it when Apply
                    file YAML is available.
                  </p>
                  <pre aria-label="File provider YAML">{proposal.fileYAML}</pre>
                </>
              )}
              {proposal.blockers.map((v, i) => (
                <p className="banner" key={`b${i}`}>
                  <strong>Blocked:</strong> {v.message}
                </p>
              ))}
              {proposal.warnings.map((v, i) => (
                <p key={`w${i}`}>{v.message}</p>
              ))}
              <details>
                <summary>Advanced Details</summary>
                <h3>Generated Traefik labels</h3>
                <pre aria-label="Compose labels">{JSON.stringify(proposal.proposed.labels, null, 2)}</pre>
                <h3>Network patch</h3>
                <pre>
                  {JSON.stringify(
                    { existing: proposal.existing.networks, proposed: proposal.proposed.networks },
                    null,
                    2,
                  )}
                </pre>
                <div className="proxy-diff">
                  <section>
                    <h3>Existing configuration</h3>
                    <pre>{JSON.stringify(proposal.existing, null, 2)}</pre>
                  </section>
                  <section>
                    <h3>Proposed configuration</h3>
                    <pre>{JSON.stringify(proposal.proposed, null, 2)}</pre>
                  </section>
                </div>
                <h3>Exact proposed key changes</h3>
                <pre>{JSON.stringify(proposal.changes, null, 2)}</pre>
                <details>
                  <summary>JSON output</summary>
                  <pre>{JSON.stringify(proposal, null, 2)}</pre>
                </details>
              </details>
              {proposal.labelApplyAvailable ? (
                <>
                  <button type="button" disabled={busy} onClick={() => void writeComposeLabels()}>
                    {busy ? 'Applying labels…' : 'Apply labels'}
                  </button>
                  {labelsWrote ? (
                    <p>
                      Wrote Traefik labels into {labelsWrote}. Recreate the Compose service so Traefik
                      reloads them.
                    </p>
                  ) : null}
                </>
              ) : proposal.applyAvailable ? (
                <>
                  <button type="button" disabled={busy} onClick={() => void writeFileYAML()}>
                    {busy ? 'Writing file YAML…' : 'Write file YAML'}
                  </button>
                  {wrote ? <p>Wrote {wrote} into Traefik&apos;s dynamic directory.</p> : null}
                </>
              ) : (
                <p>No apply or deployment operation is available.</p>
              )}
            </section>
          )}
        </section>
      )}
      {(section === 'Services' || section === 'Middleware') && (
        <section>
          <h2>{section}</h2>
          <DataTable
            caption={`Traefik ${section.toLowerCase()}`}
            rows={(section === 'Services' ? inventory?.services : inventory?.middlewares) ?? []}
            rowKey={(r) => r.name}
            columns={[
              { id: 'name', label: 'Name', render: (r) => r.name },
              { id: 'provider', label: 'Provider', render: (r) => r.provider },
              { id: 'status', label: 'Status', render: (r) => r.status },
            ]}
          />
        </section>
      )}
      {section === 'Settings' && (
        <section>
          <h2>Read-only integration settings</h2>
          <p>No credentials are available to the browser.</p>
          <p>
            {catalog
              ? `Discovery source: ${catalog.source}${catalog.fixture ? ' (demonstration only)' : ''}`
              : discoveryError}
          </p>
          <p>
            Docker provider:{' '}
            {catalog?.traefik.dockerProvider === undefined || catalog?.traefik.dockerProvider === null
              ? unknown
              : catalog.traefik.dockerProvider
                ? 'Enabled'
                : 'Disabled'}
          </p>
          <p>
            Traefik container:{' '}
            {catalog?.traefik.instance?.present
              ? `${catalog.traefik.instance.container}${catalog.traefik.instance.running ? ' (running)' : ' (stopped)'}`
              : catalog
                ? 'Not found'
                : unknown}
          </p>
          <p>
            Compose-label apply writes Traefik keys into an allowlisted Compose file. CORE has no Docker
            socket. Certificate management stays unavailable. Application discovery uses the configured CORE
            metadata provider.
          </p>
        </section>
      )}
      {section === 'Diagnostics' && (
        <section aria-label="API connectivity">
          <h2>API connectivity: {diagnostics?.status ?? unknown}</h2>
          <ul>
            {diagnostics?.checks.map((c) => (
              <li key={c.resource}>
                {c.resource}: {c.status}
                {c.hint ? ` — ${c.hint}` : ''}
              </li>
            ))}
          </ul>
          <p>{discoveryError}</p>
          <p>
            Inventory omits credentials, authentication middleware configuration and upstream URLs. Missing
            details are shown as unknown.
          </p>
        </section>
      )}
    </div>
  );
}
