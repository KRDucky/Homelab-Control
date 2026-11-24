// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useState, type ReactNode } from 'react';
import {
  ApiError,
  applyAptConfig,
  applyAptMirror,
  getAptConfig,
  getAptMirrors,
  planAptConfig,
  planAptMirror,
  probeAptMirrors,
  type AptConfigChange,
  type AptConfigView,
  type AptEffective,
  type AptMirrors,
  type AptOutcome,
  type AptPlan,
  type AptTunables,
  type MirrorChange,
  type MirrorProbe,
} from '../api';
import { DataTable } from '../DataTable';
import { Dialog } from '../Dialog';
import { StatusPill } from '../ui';
import { explain } from '../useData';

// Owner 2026-11-24: the APT settings file and the archive mirror can be
// changed here through plan → confirm → apply. Nothing installs, removes or
// refreshes packages.

type Props = { csrf: string; onUnauthorized: () => void };

function handleError(e: unknown, onUnauthorized: () => void): Error {
  if (e instanceof ApiError && e.status === 401) onUnauthorized();
  return e instanceof Error ? e : new Error(String(e));
}

function ErrorLine({ error }: { error: Error | null }) {
  if (!error) return null;
  const forbidden = error instanceof ApiError && error.status === 403;
  return (
    <p className="banner" role="alert">
      {forbidden
        ? 'Your account does not have the scope for this view or change (packages.configure).'
        : explain(error)}
    </p>
  );
}

/** Review → confirm → result for one APT settings or mirror change. */
export function AptChangeDialog({
  title,
  plan,
  apply,
  onClose,
  onUnauthorized,
}: {
  title: string;
  plan: () => Promise<AptPlan>;
  apply: (token: string) => Promise<AptOutcome>;
  onClose: (changed: boolean) => void;
  onUnauthorized: () => void;
}) {
  const [reviewed, setReviewed] = useState<AptPlan | null>(null);
  const [outcome, setOutcome] = useState<AptOutcome | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [busy, setBusy] = useState(true);

  useEffect(() => {
    let cancelled = false;
    plan()
      .then((p) => !cancelled && setReviewed(p))
      .catch((e: unknown) => !cancelled && setError(handleError(e, onUnauthorized)))
      .finally(() => !cancelled && setBusy(false));
    return () => {
      cancelled = true;
    };
  }, [plan, onUnauthorized]);

  async function confirm(p: AptPlan) {
    setBusy(true);
    setError(null);
    try {
      setOutcome(await apply(p.token));
    } catch (e) {
      setError(handleError(e, onUnauthorized));
    } finally {
      setBusy(false);
    }
  }

  let body: ReactNode = null;
  if (outcome) {
    const label = { succeeded: 'Done', failed: 'Failed', uncertain: 'Not verified' }[outcome.outcome];
    body = (
      <div role="status">
        <p className={`op-result op-${outcome.outcome}`}>
          <strong>{label}</strong> — {outcome.message}
        </p>
        {outcome.backup && (
          <p className="footnote">
            Previous file kept as <code>{outcome.backup}</code> in /var/lib/homelab-control-apt-apply/backups.
          </p>
        )}
        {(outcome.overridden_by_other_files ?? []).length > 0 && (
          <p className="banner" role="note">
            Overridden by a later APT configuration file:{' '}
            {(outcome.overridden_by_other_files ?? []).join(', ')}.
          </p>
        )}
        <p className="footnote">
          Operation <code>{outcome.operation_id.slice(0, 12)}</code>, recorded in the operation journal.
        </p>
      </div>
    );
  } else if (reviewed) {
    body = (
      <>
        <p className="preview-bar">Preview · nothing has been changed yet</p>
        <p>{reviewed.effect}</p>
        {reviewed.lines && reviewed.lines.length > 0 && (
          <table>
            <caption className="muted">Changed lines of {reviewed.file} — not applied</caption>
            <thead>
              <tr>
                <th scope="col">Line</th>
                <th scope="col">Now</th>
                <th scope="col">After</th>
              </tr>
            </thead>
            <tbody>
              {reviewed.lines.map((l) => (
                <tr key={l.line}>
                  <th scope="row">{l.line}</th>
                  <td>
                    <code>{l.before}</code>
                  </td>
                  <td>
                    <code>{l.after}</code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {reviewed.action !== 'mirror.set' && (
          <div className="kv">
            <p className="muted">Now ({reviewed.file})</p>
            <pre aria-label="Current file">{reviewed.before || '(the file does not exist)'}</pre>
            <p className="muted">After</p>
            <pre aria-label="New file">{reviewed.after || '(the file is removed)'}</pre>
          </div>
        )}
        <ul className="consequences">
          {reviewed.consequences.map((c) => (
            <li key={c}>{c}</li>
          ))}
        </ul>
      </>
    );
  } else if (busy) {
    body = (
      <p role="status" className="muted">
        Reviewing the change. Nothing is changed yet.
      </p>
    );
  }

  return (
    <Dialog title={title} onClose={() => !busy && onClose(outcome !== null)}>
      <ErrorLine error={error} />
      {body}
      <footer className="dialog-actions">
        <button type="button" className="ghost" disabled={busy} onClick={() => onClose(outcome !== null)}>
          {busy ? 'Working…' : 'Close'}
        </button>
        {reviewed && !outcome && (
          <button type="button" disabled={busy} onClick={() => void confirm(reviewed)}>
            Apply change
          </button>
        )}
      </footer>
    </Dialog>
  );
}

type Form = {
  install_recommends: boolean;
  install_suggests: boolean;
  skip_translations: boolean;
  force_ipv4: boolean;
  pdiffs: boolean;
  keep_downloaded: boolean;
  retries: string;
  timeout_seconds: string;
  queue_mode: 'host' | 'access';
  http_proxy: string;
};

function formFrom(view: AptConfigView): Form {
  const e: AptEffective = view.effective ?? {
    install_recommends: true,
    install_suggests: false,
    skip_translations: false,
    force_ipv4: false,
    pdiffs: true,
    keep_downloaded: false,
    retries: 0,
    timeout_seconds: 120,
    queue_mode: 'host',
    http_proxy_set: false,
  };
  return {
    install_recommends: e.install_recommends,
    install_suggests: e.install_suggests,
    skip_translations: e.skip_translations,
    force_ipv4: e.force_ipv4,
    pdiffs: e.pdiffs,
    keep_downloaded: e.keep_downloaded,
    retries: String(e.retries),
    timeout_seconds: String(e.timeout_seconds),
    queue_mode: e.queue_mode === 'access' ? 'access' : 'host',
    // The API never returns the proxy URL; it is only re-sent when typed.
    http_proxy: view.managed.http_proxy ?? '',
  };
}

function tunablesFrom(f: Form): AptTunables | string {
  const retries = Number(f.retries);
  const timeout = Number(f.timeout_seconds);
  if (!Number.isInteger(retries) || retries < 0 || retries > 10)
    return 'Retries must be a whole number from 0 to 10.';
  if (!Number.isInteger(timeout) || timeout < 5 || timeout > 300)
    return 'The timeout must be 5 to 300 seconds.';
  const proxy = f.http_proxy.trim();
  if (proxy && !/^https?:\/\/[A-Za-z0-9.[\]:-]+(:\d{1,5})?\/?$/.test(proxy)) {
    return 'The proxy must look like http://host:port, without a user name or password.';
  }
  const t: AptTunables = {
    install_recommends: f.install_recommends,
    install_suggests: f.install_suggests,
    force_ipv4: f.force_ipv4,
    pdiffs: f.pdiffs,
    keep_downloaded: f.keep_downloaded,
    retries,
    timeout_seconds: timeout,
    queue_mode: f.queue_mode,
  };
  if (f.skip_translations) t.skip_translations = true;
  if (proxy) t.http_proxy = proxy;
  return t;
}

const CHECKS: { key: keyof Form; label: string; help: string }[] = [
  { key: 'install_recommends', label: 'Install recommended packages', help: 'APT::Install-Recommends' },
  { key: 'install_suggests', label: 'Install suggested packages', help: 'APT::Install-Suggests' },
  { key: 'skip_translations', label: 'Skip translation downloads', help: 'Acquire::Languages "none"' },
  { key: 'force_ipv4', label: 'Force IPv4 for downloads', help: 'Acquire::ForceIPv4' },
  { key: 'pdiffs', label: 'Use index diffs (PDiffs)', help: 'Acquire::PDiffs' },
  {
    key: 'keep_downloaded',
    label: 'Keep downloaded .deb files',
    help: 'Binary::apt::APT::Keep-Downloaded-Packages',
  },
];

/** APT settings: checkboxes and a few values written to one managed file. */
export function AptSettings({ csrf, onUnauthorized }: Props) {
  const [view, setView] = useState<AptConfigView | null>(null);
  const [form, setForm] = useState<Form | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [invalid, setInvalid] = useState('');
  const [change, setChange] = useState<AptConfigChange | null>(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      const v = await getAptConfig(csrf);
      setView(v);
      setForm(formFrom(v));
    } catch (e) {
      setError(handleError(e, onUnauthorized));
    }
  }, [csrf, onUnauthorized]);

  useEffect(() => {
    // Deferred like the other review dialogs so the first render does not set state synchronously.
    const t = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(t);
  }, [load]);

  const planFn = useCallback(() => planAptConfig(change as AptConfigChange, csrf), [change, csrf]);
  const applyFn = useCallback(
    (token: string) => applyAptConfig(change as AptConfigChange, token, csrf),
    [change, csrf],
  );

  if (!form || !view) {
    return (
      <>
        <ErrorLine error={error} />
        {!error && <p className="muted">Reading the APT configuration…</p>}
      </>
    );
  }
  const set = (k: keyof Form, v: string | boolean) => {
    setInvalid('');
    setForm({ ...form, [k]: v } as Form);
  };

  return (
    <section aria-label="APT settings">
      <p className="banner" role="note">
        Values show APT&apos;s current configuration (apt-config dump). Applying writes them to{' '}
        <code>{view.path}</code> after a review; no other APT file is changed and nothing is installed.
      </p>
      {view.effective_error && (
        <p className="banner" role="alert">
          Current values could not be read ({view.effective_error}); APT defaults are shown.
        </p>
      )}
      {view.managed_changed_by_hand && (
        <p className="banner" role="note">
          The managed file was changed outside Homelab Control. Applying replaces it; a backup is kept.
        </p>
      )}
      <ErrorLine error={error} />
      <form
        onSubmit={(e) => {
          e.preventDefault();
          const t = tunablesFrom(form);
          if (typeof t === 'string') {
            setInvalid(t);
            return;
          }
          setChange({ tunables: t });
        }}
      >
        <fieldset>
          <legend>Package selection and downloads</legend>
          {CHECKS.map((c) => (
            <label key={c.key} className="field check">
              <input
                type="checkbox"
                checked={form[c.key] as boolean}
                onChange={(e) => set(c.key, e.target.checked)}
              />{' '}
              {c.label} <span className="muted mono">{c.help}</span>
            </label>
          ))}
        </fieldset>
        <fieldset>
          <legend>Network</legend>
          <label className="field">
            Retries (0–10)
            <input
              type="number"
              min={0}
              max={10}
              value={form.retries}
              onChange={(e) => set('retries', e.target.value)}
            />
          </label>
          <label className="field">
            HTTP and HTTPS timeout, seconds (5–300)
            <input
              type="number"
              min={5}
              max={300}
              value={form.timeout_seconds}
              onChange={(e) => set('timeout_seconds', e.target.value)}
            />
          </label>
          <label className="field">
            Download queue
            <select value={form.queue_mode} onChange={(e) => set('queue_mode', e.target.value)}>
              <option value="host">One connection per host</option>
              <option value="access">One connection per access method</option>
            </select>
          </label>
          <label className="field">
            HTTP proxy, optional (for example an apt-cacher-ng host)
            <input
              value={form.http_proxy}
              maxLength={200}
              placeholder={view.effective?.http_proxy_set ? 'A proxy is set elsewhere' : 'http://host:3142'}
              onChange={(e) => set('http_proxy', e.target.value)}
            />
          </label>
        </fieldset>
        {invalid && (
          <p className="banner" role="alert">
            {invalid}
          </p>
        )}
        <div className="toolbar">
          <button type="submit">Review settings</button>
          {view.managed_present && (
            <button type="button" className="ghost" onClick={() => setChange({ reset: true })}>
              Reset to APT defaults
            </button>
          )}
        </div>
      </form>
      {change && (
        <AptChangeDialog
          title={'reset' in change ? 'Reset APT settings' : 'Apply APT settings'}
          plan={planFn}
          apply={applyFn}
          onUnauthorized={onUnauthorized}
          onClose={(changed) => {
            setChange(null);
            if (changed) void load();
          }}
        />
      )}
    </section>
  );
}

const ROLE_TEXT: Record<string, string> = {
  archive: 'Distribution archive',
  security: 'Security updates',
  ports: 'Ports archive',
  other: 'Other repository',
};

const STATUS_TONE = {
  ok: 'ok',
  stale: 'notice',
  mismatch: 'crit',
  expired: 'crit',
  unreachable: 'crit',
} as const;

/** Mirrors: current entries, a speed and freshness probe, and switching. */
export function AptMirrorsView({ csrf, onUnauthorized }: Props) {
  const [inv, setInv] = useState<AptMirrors | null>(null);
  const [country, setCountry] = useState('');
  const [probe, setProbe] = useState<MirrorProbe | null>(null);
  const [probing, setProbing] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [change, setChange] = useState<MirrorChange | null>(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      const m = await getAptMirrors(csrf);
      setInv(m);
      setCountry((c) => c || m.default_country || 'US');
    } catch (e) {
      setError(handleError(e, onUnauthorized));
    }
  }, [csrf, onUnauthorized]);

  useEffect(() => {
    // Deferred like the other review dialogs so the first render does not set state synchronously.
    const t = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(t);
  }, [load]);

  const planFn = useCallback(() => planAptMirror(change as MirrorChange, csrf), [change, csrf]);
  const applyFn = useCallback(
    (token: string) => applyAptMirror(change as MirrorChange, token, csrf),
    [change, csrf],
  );

  async function runProbe() {
    setProbing(true);
    setError(null);
    setProbe(null);
    try {
      setProbe(await probeAptMirrors(country, csrf));
    } catch (e) {
      setError(handleError(e, onUnauthorized));
    } finally {
      setProbing(false);
    }
  }

  if (!inv) {
    return (
      <>
        <ErrorLine error={error} />
        {!error && <p className="muted">Reading the APT sources…</p>}
      </>
    );
  }
  const switchable = inv.entries.filter((e) => e.switchable);
  // One mirror change per file and current URI (one-line files list each suite separately).
  const targets = Array.from(new Map(switchable.map((e) => [e.file + '\u0000' + e.uri, e])).values());

  return (
    <section aria-label="APT mirrors">
      <p className="banner" role="note">
        Only the distribution archive can use another mirror. Security updates stay on the security server,
        signing keys never change, and package lists are not refreshed here.
      </p>
      <ErrorLine error={error} />
      <DataTable
        caption="Configured APT sources"
        rows={inv.entries}
        rowKey={(e) => `${e.file}:${e.line}`}
        columns={[
          { id: 'role', label: 'Role', render: (e) => ROLE_TEXT[e.role] ?? e.role },
          { id: 'uri', label: 'URI', render: (e) => <code>{e.uri}</code> },
          { id: 'suites', label: 'Suites', render: (e) => e.suites.join(' ') },
          { id: 'file', label: 'File', render: (e) => <code>{`${e.file}:${e.line}`}</code> },
          {
            id: 'mirror',
            label: 'Mirror can change',
            render: (e) => (e.switchable ? 'Yes' : <span className="muted">{e.reason ?? 'No'}</span>),
          },
        ]}
      />
      {inv.files
        .filter((f) => f.error)
        .map((f) => (
          <p key={f.path} className="banner" role="note">
            {f.path}: {f.error}
          </p>
        ))}
      <form
        className="inline-field"
        onSubmit={(e) => {
          e.preventDefault();
          void runProbe();
        }}
      >
        <label htmlFor="mirror-country">Country</label>
        <input
          id="mirror-country"
          value={country}
          maxLength={2}
          size={3}
          onChange={(e) => setCountry(e.target.value.toUpperCase())}
        />
        <button type="submit" disabled={probing || !/^[A-Z]{2}$/.test(country) || targets.length === 0}>
          Find fastest mirror
        </button>
      </form>
      {targets.length === 0 && <p className="empty">No archive entry here can change its mirror.</p>}
      {probing && (
        <p role="status" className="muted">
          Measuring mirrors (up to 40, 5 seconds each)…
        </p>
      )}
      {probe && (
        <>
          {probe.note && (
            <p className="banner" role="note">
              {probe.note}
            </p>
          )}
          <DataTable
            caption={`Mirrors for ${probe.codename} in ${probe.country}, fastest first (release index download)`}
            rows={probe.results}
            rowKey={(r) => r.uri}
            columns={[
              { id: 'rank', label: 'Rank', render: (r) => r.rank ?? '—' },
              {
                id: 'mirror',
                label: 'Mirror',
                render: (r) => (
                  <>
                    <code>{r.uri}</code>
                    {r.current && <span className="muted"> (current)</span>}
                  </>
                ),
              },
              {
                id: 'status',
                label: 'Status',
                render: (r) => (
                  <>
                    <StatusPill tone={STATUS_TONE[r.status]} label={r.status} />
                    {r.reason && <span className="muted"> {r.reason}</span>}
                  </>
                ),
              },
              {
                id: 'ttfb',
                label: 'First byte',
                render: (r) => (r.first_byte_ms != null ? `${r.first_byte_ms} ms` : '—'),
              },
              {
                id: 'total',
                label: 'Download',
                render: (r) => (r.total_ms != null ? `${r.total_ms} ms` : '—'),
              },
              {
                id: 'speed',
                label: 'Speed',
                render: (r) => (r.kb_per_second ? `${r.kb_per_second} KB/s` : '—'),
              },
              {
                id: 'use',
                label: 'Use',
                render: (r) =>
                  r.status === 'ok' && !r.current ? (
                    <span className="toolbar">
                      {targets.map((t) => (
                        <button
                          key={t.file + t.uri}
                          type="button"
                          onClick={() => setChange({ file: t.file, from: t.uri, to: r.uri })}
                          aria-label={`Use ${r.host} for ${t.uri} in ${t.file}`}
                        >
                          Use this mirror{targets.length > 1 ? ` for ${t.uri}` : ''}
                        </button>
                      ))}
                    </span>
                  ) : (
                    '—'
                  ),
              },
            ]}
          />
        </>
      )}
      {change && (
        <AptChangeDialog
          title="Change the archive mirror"
          plan={planFn}
          apply={applyFn}
          onUnauthorized={onUnauthorized}
          onClose={(changed) => {
            setChange(null);
            if (changed) {
              setProbe(null);
              void load();
            }
          }}
        />
      )}
    </section>
  );
}
