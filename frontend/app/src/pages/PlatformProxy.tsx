// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { useEffect, useRef, useState, type FormEvent } from 'react';
import {
  ApiError,
  getPlatformInventory,
  platformRefKey,
  previewPlatform,
  type PlatformCapability,
  type PlatformInventory,
  type PlatformPreview,
  type PlatformView,
  type TraefikModel,
} from '../api';

const targetLabels: Partial<Record<PlatformCapability, string>> = {
  'routing.preview.compose-labels': 'Container routing labels (Traefik Docker provider)',
  'routing.preview.kubernetes-ingress': 'Kubernetes Ingress',
  'routing.preview.traefik-ingressroute': 'Traefik IngressRoute (CRD)',
  'routing.preview.gateway-httproute': 'Gateway API HTTPRoute',
};
const providerFor: Partial<Record<PlatformCapability, string>> = {
  'routing.preview.kubernetes-ingress': 'kubernetes-ingress',
  'routing.preview.gateway-httproute': 'gateway-api-httproute',
};

/** Platform-aware Add Proxy Host flow for registered non-legacy platforms. */
export function PlatformProxy({
  csrf,
  platform,
  onUnauthorized,
}: {
  csrf: string;
  platform: PlatformView;
  onUnauthorized: () => void;
}) {
  const [inventory, setInventory] = useState<PlatformInventory | null>(null);
  const [loadError, setLoadError] = useState('');
  const [target, setTarget] = useState<PlatformCapability | ''>('');
  const [workload, setWorkload] = useState('');
  const [service, setService] = useState('');
  const [protocol, setProtocol] = useState<'http' | 'https'>('http');
  const [preview, setPreview] = useState<PlatformPreview | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const version = useRef(0);
  const kubernetes = platform.kind === 'kubernetes';
  useEffect(() => {
    // The parent keys this component by platform ID, so a platform change
    // remounts it with fresh state; the effect only loads inventory.
    let cancelled = false;
    getPlatformInventory(platform.id, csrf).then(
      (r) => {
        if (cancelled) return;
        setInventory(r.inventory);
        const targets = r.inventory.capabilities.detected.filter((c) => c in targetLabels);
        setTarget(targets.length === 1 ? (targets[0] ?? '') : '');
      },
      (e) => {
        if (cancelled) return;
        if (e instanceof ApiError && e.status === 401) onUnauthorized();
        setLoadError('Platform inventory is unavailable or not permitted for your account.');
      },
    );
    return () => {
      cancelled = true;
    };
  }, [platform.id, csrf, onUnauthorized]);
  if (loadError) return <p className="banner">{loadError}</p>;
  if (!inventory) return <p className="muted">Loading platform inventory…</p>;
  const targets = inventory.capabilities.detected.filter((c) => c in targetLabels);
  const selected = inventory.workloads.find((w) => platformRefKey(w.ref) === workload);
  const services = kubernetes
    ? inventory.services.filter((s) => selected?.services.some((r) => r.uid === s.ref.uid))
    : [];
  const selectedService = services.find((s) => platformRefKey(s.ref) === service);
  const ports = (kubernetes ? (selectedService?.ports ?? []) : (selected?.ports ?? [])).filter(
    (p) => p.protocol === 'tcp',
  );
  const providerKind = target ? providerFor[target] : undefined;
  const classes = inventory.routing.find((r) => r.kind === providerKind)?.classes ?? [];
  const supported = (w: (typeof inventory.workloads)[number]) =>
    kubernetes
      ? w.services.length > 0
      : w.state === 'running' && !w.issues.some((i) => i !== 'replicas_unsupported');
  async function generate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!target) return;
    setPreview(null);
    setError('');
    setBusy(true);
    const v = version.current;
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
    try {
      const result = await previewPlatform(
        {
          platform: platform.id,
          target,
          workload,
          ...(kubernetes ? { service } : { network: String(data.get('network')) }),
          ...(data.get('routeClass') ? { routeClass: String(data.get('routeClass')) } : {}),
          model,
        },
        csrf,
      );
      if (v === version.current) setPreview(result);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) onUnauthorized();
      setError(
        'The preview was rejected. Check the workload, Service, port, provider and TLS selections, and that Traefik inventory is reachable.',
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      {inventory.fixture && (
        <p className="banner">
          Demonstration platform — fixture data, not your {kubernetes ? 'cluster' : 'containers'}.
        </p>
      )}
      <p className="muted">
        {platform.displayName}: {platform.distribution}
        {inventory.version ? ` ${inventory.version}` : ''} · {platform.mode} · endpoint{' '}
        {platform.endpoint.label}
      </p>
      <form
        className="proxy-form"
        onSubmit={generate}
        onChange={() => {
          version.current++;
          setPreview(null);
        }}
      >
        <fieldset>
          <legend>2. Select application</legend>
          <label htmlFor="platform-target">Routing resource</label>
          <select
            id="platform-target"
            required
            value={target}
            onChange={(e) => setTarget(e.target.value as PlatformCapability)}
          >
            <option value="">Select a detected routing provider</option>
            {targets.map((t) => (
              <option key={t} value={t}>
                {targetLabels[t]}
              </option>
            ))}
          </select>
          {targets.length === 0 && (
            <p className="banner">
              No routing provider was detected on this platform; previews are unavailable.
            </p>
          )}
          <label htmlFor="platform-workload">{kubernetes ? 'Workload' : 'Container'}</label>
          <select
            id="platform-workload"
            required
            value={workload}
            onChange={(e) => {
              setWorkload(e.target.value);
              setService('');
            }}
          >
            <option value="">Select a workload</option>
            {inventory.workloads.map((w) => (
              <option key={platformRefKey(w.ref)} value={platformRefKey(w.ref)} disabled={!supported(w)}>
                {w.group ? `${w.group} / ` : ''}
                {w.component ?? w.ref.name} · {w.ref.kind} ({w.state}
                {kubernetes ? ` ${w.readyReplicas}/${w.replicas}` : ''})
                {!supported(w) ? ' · Unsupported' : ''}
              </option>
            ))}
          </select>
          {kubernetes && (
            <>
              <label htmlFor="platform-service">Kubernetes Service</label>
              <select
                id="platform-service"
                required
                value={service}
                onChange={(e) => setService(e.target.value)}
              >
                <option value="">Select a Service that selects this workload</option>
                {services.map((s) => (
                  <option key={platformRefKey(s.ref)} value={platformRefKey(s.ref)}>
                    {s.ref.name} ({s.type})
                  </option>
                ))}
              </select>
            </>
          )}
          <label htmlFor="platform-port">{kubernetes ? 'Service port' : 'Internal application port'}</label>
          <select
            id="platform-port"
            name="port"
            required
            key={`${workload}|${service}`}
            defaultValue={ports.length === 1 ? String(ports[0]?.port) : ''}
          >
            <option value="">Select a TCP port</option>
            {ports.map((p) => (
              <option key={p.port} value={p.port}>
                {p.port}
                {p.name ? ` (${p.name})` : ''}
                {p.target ? ` → ${p.target}` : ''}
              </option>
            ))}
          </select>
          {selected?.hiddenRouting && (
            <p className="banner">
              Unsupported or sensitive existing routing settings need manual review; values are not exposed.
            </p>
          )}
          <p className="muted">
            {kubernetes
              ? 'Only Services that select the workload are offered; container and node ports are never used directly.'
              : 'Published host ports are never substituted for internal application ports.'}
          </p>
        </fieldset>
        <fieldset>
          <legend>3. Configure hostname</legend>
          <label htmlFor="platform-host">Public hostname</label>
          <input
            id="platform-host"
            name="host"
            placeholder="notes.example.com"
            required
            maxLength={253}
            pattern={'[a-z0-9.\\-]+\\.[a-z0-9\\-]+'}
          />
          <label htmlFor="platform-path">Path prefix (optional)</label>
          <input
            id="platform-path"
            name="path"
            placeholder="/app"
            maxLength={201}
            pattern={'/[A-Za-z0-9._~/-]*'}
          />
          <label htmlFor="platform-protocol">Incoming protocol</label>
          <select
            id="platform-protocol"
            value={protocol}
            onChange={(e) => setProtocol(e.target.value === 'https' ? 'https' : 'http')}
          >
            <option value="http">HTTP</option>
            <option value="https">HTTPS (TLS)</option>
          </select>
          {protocol === 'https' && (
            <>
              <label htmlFor="platform-resolver">Existing certificate resolver (optional)</label>
              <select id="platform-resolver" name="resolver">
                <option value="">Existing/default certificate (unverified)</option>
                {inventory.resolvers.map((r) => (
                  <option key={r}>{r}</option>
                ))}
              </select>
            </>
          )}
        </fieldset>
        <details>
          <summary>Advanced configuration</summary>
          <label htmlFor="platform-name">Resource / router name</label>
          <input
            id="platform-name"
            name="name"
            defaultValue="proxy-host"
            required
            pattern={'[a-z][a-z0-9\\-]{0,62}'}
          />
          <label htmlFor="platform-entrypoint">Entrypoint</label>
          <select id="platform-entrypoint" name="entrypoint" required>
            {inventory.entrypoints.map((e) => (
              <option key={e}>{e}</option>
            ))}
          </select>
          <label htmlFor="platform-upstream">Upstream scheme</label>
          <select id="platform-upstream" name="upstreamScheme">
            <option value="http">HTTP</option>
            <option value="https">HTTPS (certificate verification required)</option>
          </select>
          {!kubernetes && (
            <>
              <label htmlFor="platform-network">Traefik network</label>
              <select id="platform-network" name="network" required>
                {inventory.networks
                  .filter((n) => n.traefikAttached)
                  .map((n) => (
                    <option key={n.ref.name}>{n.ref.name}</option>
                  ))}
              </select>
            </>
          )}
          {classes.length > 1 && (
            <>
              <label htmlFor="platform-class">
                {providerKind === 'gateway-api-httproute' ? 'Gateway' : 'IngressClass'}
              </label>
              <select id="platform-class" name="routeClass" required>
                {classes.map((c) => (
                  <option key={c}>{c}</option>
                ))}
              </select>
            </>
          )}
        </details>
        <button
          type="submit"
          disabled={busy || !target || !selected || !supported(selected) || (kubernetes && !selectedService)}
        >
          {busy ? 'Generating preview…' : 'Preview configuration'}
        </button>
      </form>
      {error && (
        <p className="banner" role="alert">
          {error}
        </p>
      )}
      {preview && (
        <section aria-label="Platform configuration preview">
          <h2>Proposed {targetLabels[preview.target]}</h2>
          <p className="banner" role="status">
            Preview only — not deployable as shown. Apply is unavailable.
          </p>
          {preview.blockers.length > 0 && (
            <>
              <h3>Blockers</h3>
              <ul>
                {preview.blockers.map((b, i) => (
                  <li key={`${b.code}-${i}`}>{b.message}</li>
                ))}
              </ul>
            </>
          )}
          {preview.warnings.length > 0 && (
            <>
              <h3>Warnings</h3>
              <ul>
                {preview.warnings.map((w, i) => (
                  <li key={`${w.code}-${i}`}>{w.message}</li>
                ))}
              </ul>
            </>
          )}
          <h3>Not verified by this preview</h3>
          <ul aria-label="Unverified checks">
            {preview.validation.map((v) => (
              <li key={v.check}>{v.check.replaceAll('-', ' ')}</li>
            ))}
          </ul>
          <details open>
            <summary>
              {preview.format === 'kubernetes-manifests' ? 'Kubernetes resources' : 'Proposed routing labels'}
            </summary>
            <pre>
              {JSON.stringify(
                preview.format === 'kubernetes-manifests' ? preview.manifests : preview.compose?.proposed,
                null,
                2,
              )}
            </pre>
          </details>
        </section>
      )}
    </>
  );
}
