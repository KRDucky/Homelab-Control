// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useState, type FormEvent } from 'react';
import {
  ApiError,
  getHostSettings,
  previewHostSettings,
  type HostSetting,
  type HostSettingChange,
  type HostSettingsPreview,
  type HostSettingsPreviewRequest,
} from '../api';
import { StatusPill } from '../ui';
import { explain, useData } from '../useData';

// M2-6 skeleton: shows the observed host name and time zone, and previews
// requested changes. Nothing on this page changes the host; there is no apply.

const labels: Record<HostSettingChange['field'] | 'kernel_hostname', string> = {
  kernel_hostname: 'Kernel host name',
  static_hostname: 'Static host name',
  pretty_hostname: 'Pretty host name',
  timezone: 'Time zone',
};

const outcomeText: Record<HostSettingChange['outcome'], string> = {
  unchanged: 'Same as now',
  'would-change': 'Would change',
  invalid: 'Not accepted',
  'cannot-compare': 'Current value unknown',
};

function SettingValue({ s }: { s: HostSetting }) {
  if (s.status === 'observed') return <>{s.value}</>;
  return <span className="muted">{s.status === 'not-set' ? 'Not set' : 'Unavailable'}</span>;
}

export function HostSettingsPage({
  csrf,
  canPreview,
  onUnauthorized,
}: {
  csrf: string;
  canPreview: boolean;
  onUnauthorized: () => void;
}) {
  const { data, error } = useData(getHostSettings, { onUnauthorized });
  const [form, setForm] = useState({ static_hostname: '', pretty_hostname: '', timezone: '' });
  const [preview, setPreview] = useState<HostSettingsPreview | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (error && !data) {
    return (
      <div className="page">
        <h1 className="page-title">Host settings</h1>
        <p className="banner" role="alert">
          {explain(error)}
        </p>
      </div>
    );
  }
  if (!data) return <p className="muted">Reading host settings…</p>;
  const s = data.settings;

  async function submit(e: FormEvent) {
    e.preventDefault();
    const req: HostSettingsPreviewRequest = {};
    if (form.static_hostname.trim()) req.static_hostname = form.static_hostname.trim();
    if (form.pretty_hostname.trim()) req.pretty_hostname = form.pretty_hostname.trim();
    if (form.timezone.trim()) req.timezone = form.timezone.trim();
    if (Object.keys(req).length === 0) {
      setPreviewError('Enter at least one value to preview.');
      return;
    }
    setBusy(true);
    setPreviewError(null);
    try {
      setPreview(await previewHostSettings(req, csrf));
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      setPreview(null);
      setPreviewError(err instanceof ApiError ? err.message : explain(err as Error));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Host settings</h1>
        <p className="page-sub">
          Host name and time zone as this host reports them. Changes can be previewed here; Homelab Control
          cannot apply them yet.
        </p>
      </header>

      <section className="panel" aria-labelledby="h-host-current">
        <h2 id="h-host-current">Current settings</h2>
        <dl className="facts">
          {(['static_hostname', 'pretty_hostname', 'kernel_hostname', 'timezone'] as const).map((k) => (
            <div key={k}>
              <dt>{labels[k]}</dt>
              <dd>
                <SettingValue s={s[k]} />
                {s[k].note && <span className="muted"> — {s[k].note}</span>}
              </dd>
            </div>
          ))}
        </dl>
        <p className="muted">
          Read {new Date(data.collected_at).toLocaleString()} from{' '}
          {[s.static_hostname, s.pretty_hostname, s.kernel_hostname, s.timezone]
            .map((x) => x.source)
            .join(', ')}
          .
        </p>
      </section>

      <section className="panel" aria-labelledby="h-host-preview">
        <h2 id="h-host-preview">Preview a change</h2>
        <p className="banner" role="note">
          Preview only. Nothing is changed on this host, and there is no way to apply these values from
          Homelab Control yet.
        </p>
        {!canPreview ? (
          <p className="muted">Your account does not have the host.settings.preview permission.</p>
        ) : (
          <form onSubmit={(e) => void submit(e)}>
            <div className="toolbar">
              <label className="inline-field">
                Static host name
                <input
                  value={form.static_hostname}
                  maxLength={64}
                  placeholder="e.g. nas-01"
                  onChange={(e) => setForm({ ...form, static_hostname: e.target.value })}
                />
              </label>
              <label className="inline-field">
                Pretty host name
                <input
                  value={form.pretty_hostname}
                  maxLength={255}
                  onChange={(e) => setForm({ ...form, pretty_hostname: e.target.value })}
                />
              </label>
              <label className="inline-field">
                Time zone
                <input
                  value={form.timezone}
                  maxLength={128}
                  placeholder="e.g. Europe/Berlin"
                  onChange={(e) => setForm({ ...form, timezone: e.target.value })}
                />
              </label>
              <button type="submit" disabled={busy}>
                {busy ? 'Checking…' : 'Preview (does not apply)'}
              </button>
            </div>
          </form>
        )}
        {previewError && (
          <p className="field-error" role="alert">
            {previewError}
          </p>
        )}
        {preview && (
          <div aria-live="polite">
            <p role="status">
              <strong>{preview.notice}</strong>
            </p>
            <table>
              <caption className="muted">Preview result — not applied</caption>
              <thead>
                <tr>
                  <th scope="col">Setting</th>
                  <th scope="col">Now</th>
                  <th scope="col">Requested</th>
                  <th scope="col">Result</th>
                </tr>
              </thead>
              <tbody>
                {preview.changes.map((c) => (
                  <tr key={c.field}>
                    <th scope="row">{labels[c.field]}</th>
                    <td>{c.current || <span className="muted">Not set</span>}</td>
                    <td>{c.requested}</td>
                    <td>
                      <StatusPill
                        tone={
                          c.outcome === 'invalid' ? 'crit' : c.outcome === 'would-change' ? 'notice' : 'info'
                        }
                        label={outcomeText[c.outcome]}
                      />
                      {c.reason && <span className="muted"> {c.reason}</span>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {preview.warnings.map((w) => (
              <p key={w} className="muted">
                {w}
              </p>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
