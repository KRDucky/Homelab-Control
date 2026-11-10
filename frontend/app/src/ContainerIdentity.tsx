// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { useEffect, useRef, useState, type FormEvent } from 'react';
import {
  ApiError,
  getIdentityProfiles,
  previewIdentity,
  type IdentityLayer,
  type IdentityMode,
  type IdentityPreview,
  type IdentityProfile,
  type IdentitySettings,
} from './api';
import type { ContainerRuntime } from './plugin';

const layerLabels: Record<IdentityLayer, string> = {
  unset: 'Image default (not set by HomeControl)',
  image: 'Image',
  platform: 'Platform/host defaults',
  application: 'Application defaults',
  profile: 'Profile',
  override: 'This container',
};

const modeLabels: Record<IdentityMode, string> = {
  image: 'Use image defaults',
  inherit: 'Use platform/host defaults',
  custom: 'Custom settings',
};

const ID_RE = /^\d{1,10}$/;

/** Parses the custom form; returns field errors instead of guessing. */
export function parseCustom(f: { uid: string; gid: string; umask: string; groups: string }): {
  settings: IdentitySettings;
  errors: string[];
} {
  const errors: string[] = [];
  const settings: IdentitySettings = {};
  const id = (v: string, label: string) => {
    if (!v.trim()) return undefined;
    const n = Number(v.trim());
    if (!ID_RE.test(v.trim()) || n > 4294967294) {
      errors.push(`${label} must be a whole number between 0 and 4294967294.`);
      return undefined;
    }
    return n;
  };
  const uid = id(f.uid, 'PUID');
  if (uid !== undefined) settings.uid = uid;
  const gid = id(f.gid, 'PGID');
  if (gid !== undefined) settings.gid = gid;
  if (f.umask.trim()) {
    if (/^0?[0-7]{3}$/.test(f.umask.trim())) settings.umask = f.umask.trim();
    else errors.push('UMASK must be three octal digits with an optional leading 0, for example 022.');
  }
  if (f.groups.trim()) {
    const groups: number[] = [];
    for (const g of f.groups.split(/[\s,]+/).filter(Boolean)) {
      const n = id(g, 'Supplemental group');
      if (n !== undefined) groups.push(n);
    }
    settings.supplementalGroups = groups;
  }
  return { settings, errors };
}

/**
 * Container Identity & Permissions: a CORE component any plugin installer or
 * Compose/manifest editor can embed. It only previews; nothing is applied.
 */
export function ContainerIdentity({
  csrf,
  platformId,
  runtime,
  workload,
  onUnauthorized,
}: {
  csrf: string;
  platformId: string;
  runtime: ContainerRuntime;
  workload?: string;
  onUnauthorized: () => void;
}) {
  const [profiles, setProfiles] = useState<IdentityProfile[]>([]);
  const [mode, setMode] = useState<IdentityMode>('image');
  const [profile, setProfile] = useState('');
  const [form, setForm] = useState({ uid: '', gid: '', umask: '', groups: '' });
  const [ack, setAck] = useState<number[]>([]);
  const [preview, setPreview] = useState<IdentityPreview | null>(null);
  const [error, setError] = useState('');
  const version = useRef(0);
  useEffect(() => {
    let cancelled = false;
    getIdentityProfiles(csrf).then(
      (r) => !cancelled && setProfiles(r.profiles),
      () => !cancelled && setProfiles([]),
    );
    return () => {
      cancelled = true;
    };
  }, [csrf]);
  const fixed = runtime.identity.method === 'fixed';

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const v = ++version.current;
    setError('');
    const { settings, errors } = parseCustom(form);
    if (mode === 'custom' && errors.length) {
      setPreview(null);
      setError(errors.join(' '));
      return;
    }
    try {
      const r = await previewIdentity(
        {
          platform: platformId,
          mode,
          runtime,
          ...(mode !== 'image' && profile ? { profile } : {}),
          ...(mode === 'custom' ? { override: settings } : {}),
          ...(ack.length ? { acknowledgePrivilegedGroups: ack } : {}),
          ...(workload ? { workload } : {}),
        },
        csrf,
      );
      if (v === version.current) setPreview(r.preview);
    } catch (err) {
      if (v !== version.current) return;
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      setPreview(null);
      setError(err instanceof Error ? err.message : 'Preview failed.');
    }
  }

  const privilegedBlocked =
    preview?.issues.filter((i) => i.code === 'privileged_group' && i.severity === 'blocker') ?? [];
  const requested = parseCustom(form).settings.supplementalGroups ?? [];
  const r = preview?.resolved;
  return (
    <section className="identity-config" aria-labelledby="identity-heading">
      <h3 id="identity-heading">Container Identity &amp; Permissions</h3>
      <p className="banner">
        Preview only. HomeControl does not change containers, host ownership or permissions here.
      </p>
      {fixed && (
        <p>
          This image runs with a fixed identity ({runtime.identity.fixedUid}:{runtime.identity.fixedGid});
          PUID/PGID cannot be changed.
        </p>
      )}
      <form
        onSubmit={submit}
        onChange={() => {
          version.current++;
          setPreview(null);
        }}
      >
        <fieldset>
          <legend>Identity source</legend>
          {(Object.keys(modeLabels) as IdentityMode[]).map((m) => (
            <label key={m}>
              <input
                type="radio"
                name="identity-mode"
                value={m}
                checked={mode === m}
                onChange={() => setMode(m)}
              />{' '}
              {modeLabels[m]}
            </label>
          ))}
        </fieldset>
        {mode !== 'image' && (
          <>
            <label htmlFor="identity-profile">Permission profile</label>
            <select id="identity-profile" value={profile} onChange={(e) => setProfile(e.target.value)}>
              <option value="">No profile</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </>
        )}
        {mode === 'custom' && (
          <fieldset>
            <legend>Custom settings</legend>
            <label htmlFor="identity-uid">PUID</label>
            <input
              id="identity-uid"
              inputMode="numeric"
              disabled={fixed}
              value={form.uid}
              onChange={(e) => setForm({ ...form, uid: e.target.value })}
            />
            <label htmlFor="identity-gid">PGID</label>
            <input
              id="identity-gid"
              inputMode="numeric"
              disabled={fixed}
              value={form.gid}
              onChange={(e) => setForm({ ...form, gid: e.target.value })}
            />
            <label htmlFor="identity-umask">UMASK</label>
            <input
              id="identity-umask"
              placeholder="022"
              disabled={runtime.identity.umask.method === 'none'}
              value={form.umask}
              onChange={(e) => setForm({ ...form, umask: e.target.value })}
            />
            <label htmlFor="identity-groups">Supplemental group IDs</label>
            <input
              id="identity-groups"
              placeholder="e.g. 44, 109"
              disabled={!runtime.identity.supplementalGroups}
              value={form.groups}
              onChange={(e) => setForm({ ...form, groups: e.target.value })}
            />
          </fieldset>
        )}
        {privilegedBlocked.length > 0 && (
          <fieldset>
            <legend>Privileged groups</legend>
            {requested.map((g) => (
              <label key={g}>
                <input
                  type="checkbox"
                  checked={ack.includes(g)}
                  onChange={(e) => setAck(e.target.checked ? [...ack, g] : ack.filter((x) => x !== g))}
                />{' '}
                I understand GID {g} may grant elevated authority
              </label>
            ))}
          </fieldset>
        )}
        <button type="submit">Preview identity</button>
      </form>
      {error && <p role="alert">{error}</p>}
      {preview && r && (
        <div aria-label="Identity preview">
          <table>
            <caption>Effective configuration</caption>
            <thead>
              <tr>
                <th scope="col">Setting</th>
                <th scope="col">Value</th>
                <th scope="col">Source</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <th scope="row">PUID</th>
                <td>
                  {r.uid.value ?? 'image default'}
                  {r.uid.host !== undefined && ` (host ${r.uid.host})`}
                </td>
                <td>{layerLabels[r.uid.source]}</td>
              </tr>
              <tr>
                <th scope="row">PGID</th>
                <td>
                  {r.gid.value ?? 'image default'}
                  {r.gid.host !== undefined && ` (host ${r.gid.host})`}
                </td>
                <td>{layerLabels[r.gid.source]}</td>
              </tr>
              <tr>
                <th scope="row">UMASK</th>
                <td>{r.umask.value ?? 'image default'}</td>
                <td>{layerLabels[r.umask.source]}</td>
              </tr>
              <tr>
                <th scope="row">Supplemental groups</th>
                <td>{r.supplementalGroups.value.length ? r.supplementalGroups.value.join(', ') : 'none'}</td>
                <td>{layerLabels[r.supplementalGroups.source]}</td>
              </tr>
            </tbody>
          </table>
          {r.umask.effect && (
            <p>
              New files up to {r.umask.effect.files}, directories up to {r.umask.effect.directories}.{' '}
              {r.umask.effect.explanation}
            </p>
          )}
          <p>
            {preview.verification === 'not-reported'
              ? 'Effective runtime identity: not reported by this platform (declared configuration only).'
              : preview.verification === 'observed-match'
                ? 'The platform reports the same identity as configured.'
                : 'The platform reports a different identity; the running workload has not been changed.'}
          </p>
          {preview.issues.length > 0 && (
            <ul aria-label="Compatibility findings">
              {preview.issues.map((i, n) => (
                <li key={n} data-severity={i.severity}>
                  <strong>
                    {i.severity === 'blocker' ? 'Blocked' : i.severity === 'warning' ? 'Warning' : 'Note'}:
                  </strong>{' '}
                  {i.message}
                </li>
              ))}
            </ul>
          )}
          {preview.volumes.length > 0 && (
            <ul aria-label="Volume permissions">
              {preview.volumes.map((v) => (
                <li key={v.name}>
                  {v.name} ({v.target}, {v.access}): {v.status}. {v.reason}
                </li>
              ))}
            </ul>
          )}
          <details>
            <summary>Generated configuration</summary>
            <pre>{JSON.stringify(preview.compose ?? preview.kubernetes ?? {}, null, 2)}</pre>
          </details>
          <p>No apply or deployment operation is available.</p>
        </div>
      )}
    </section>
  );
}
