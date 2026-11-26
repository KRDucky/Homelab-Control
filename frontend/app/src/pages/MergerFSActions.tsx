// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useState } from 'react';
import { applyMergerFS, planMergerFS, stepUp, type MergerFSPlan, type MergerFSWriteBody } from '../api';
import { explain } from '../useData';

const policies = ['epmfs', 'mfs', 'ff', 'lfs', 'pfrd', 'rand'];

function lines(value: string): string[] {
  return value
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean);
}

/**
 * Plan-then-confirm controls. Buttons review an operation. They do not apply
 * it. mergerfs.read does not show them. configure and maintain are separate.
 */
export function MergerFSActions({
  csrf,
  scopes,
  onRunStarted,
}: {
  csrf: string;
  scopes: string[];
  /** Called after a sync, scrub or fix was confirmed and the helper replied. */
  onRunStarted?: () => void;
}) {
  const configure = scopes.includes('mergerfs.configure');
  const maintain = scopes.includes('mergerfs.maintain');
  const [mountpoint, setMountpoint] = useState('/mnt/pool');
  const [branches, setBranches] = useState('/mnt/disk1\n/mnt/disk2');
  const [policy, setPolicy] = useState('epmfs');
  const [minFree, setMinFree] = useState('');
  const [parity, setParity] = useState('/mnt/parity1/snapraid.parity');
  const [content, setContent] = useState('/var/snapraid/content\n/mnt/disk1/snapraid.content');
  const [percent, setPercent] = useState('');
  const [file, setFile] = useState('');
  const [ack, setAck] = useState(false);
  const [plan, setPlan] = useState<MergerFSPlan | null>(null);
  const [kind, setKind] = useState<'pool' | 'maintain'>('pool');
  const [body, setBody] = useState<MergerFSWriteBody | null>(null);
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (!configure && !maintain) return null;

  async function review(nextKind: 'pool' | 'maintain', next: MergerFSWriteBody) {
    setBusy(true);
    setError(null);
    setMessage(null);
    setPlan(null);
    try {
      const got = await planMergerFS(nextKind, next, csrf);
      setKind(nextKind);
      setBody(next);
      setPlan(got);
    } catch (e) {
      setError(explain(e instanceof Error ? e : new Error(String(e))));
    } finally {
      setBusy(false);
    }
  }

  async function confirm() {
    if (!plan || !body) return;
    setBusy(true);
    setError(null);
    try {
      if (plan.step_up_required) {
        if (!password) {
          setError('Enter your password to confirm this operation.');
          return;
        }
        await stepUp(password, csrf);
      }
      const out = await applyMergerFS(kind, { ...body, token: plan.token }, csrf);
      setMessage(out.message);
      setPlan(null);
      if (kind === 'maintain') onRunStarted?.();
      setPassword('');
    } catch (e) {
      setError(explain(e instanceof Error ? e : new Error(String(e))));
    } finally {
      setBusy(false);
    }
  }

  function poolBody(action: string): MergerFSWriteBody {
    const next: MergerFSWriteBody = { action, mountpoint, branches: lines(branches), policy };
    if (minFree !== '') next.minFree = minFree;
    return next;
  }

  function scrubBody(): MergerFSWriteBody {
    const next: MergerFSWriteBody = { action: 'scrub' };
    if (percent.trim() !== '') next.scrubPercent = Number(percent);
    return next;
  }

  return (
    <section className="panel" aria-labelledby="mergerfs-actions-heading">
      <header className="panel-head">
        <h2 id="mergerfs-actions-heading">Pool and SnapRAID actions</h2>
      </header>
      <p>
        Each button reviews the closed command. Nothing is mounted, saved, synced, scrubbed, or recovered
        until you confirm. Disks are not formatted. Sync and scrub stop when a disk is missing or files were
        removed. There is no override.
      </p>
      {configure && (
        <>
          <label>
            Mountpoint
            <input value={mountpoint} onChange={(e) => setMountpoint(e.target.value)} />
          </label>
          <label>
            Branches, one directory per line
            <textarea value={branches} onChange={(e) => setBranches(e.target.value)} rows={3} />
          </label>
          <label>
            Create policy
            <select value={policy} onChange={(e) => setPolicy(e.target.value)}>
              {policies.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </label>
          <label>
            Minimum free space (optional, for example 20G)
            <input value={minFree} onChange={(e) => setMinFree(e.target.value)} />
          </label>
          <p>
            <button type="button" disabled={busy} onClick={() => void review('pool', poolBody('mount'))}>
              Review pool mount
            </button>
          </p>
          <label>
            Parity files, one per line
            <textarea value={parity} onChange={(e) => setParity(e.target.value)} rows={2} />
          </label>
          <label>
            Content files, one per line
            <textarea value={content} onChange={(e) => setContent(e.target.value)} rows={2} />
          </label>
          <p>
            <button
              type="button"
              disabled={busy}
              onClick={() =>
                void review('pool', {
                  ...poolBody('persist'),
                  parity: lines(parity),
                  content: lines(content),
                })
              }
            >
              Review array definition
            </button>
          </p>
        </>
      )}
      {maintain && (
        <>
          <p>
            <button type="button" disabled={busy} onClick={() => void review('maintain', { action: 'sync' })}>
              Review SnapRAID sync
            </button>
          </p>
          <label>
            Scrub percent (empty scrubs the whole array, or 1–100)
            <input value={percent} onChange={(e) => setPercent(e.target.value)} />
          </label>
          <p>
            <button type="button" disabled={busy} onClick={() => void review('maintain', scrubBody())}>
              Review SnapRAID scrub
            </button>
          </p>
          <label>
            Missing file, relative to the array
            <input value={file} onChange={(e) => setFile(e.target.value)} />
          </label>
          <label>
            <input type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} /> This restores
            the selected missing file with snapraid fix -m -f. It is not an undelete with -e, and it is not a
            full fix.
          </label>
          <p>
            <button
              type="button"
              disabled={busy || !ack || file.trim() === ''}
              onClick={() => void review('maintain', { action: 'fix', files: [file.trim()] })}
            >
              Review SnapRAID recovery
            </button>
          </p>
        </>
      )}
      {error && (
        <p className="banner" role="alert">
          {error}
        </p>
      )}
      {message && <p role="status">{message}</p>}
      {message && kind === 'maintain' && (
        <p>
          <a href="#snapraid-heading">Go to SnapRAID status</a>
        </p>
      )}
      {plan && (
        <div>
          <p className="preview-bar">Preview · nothing has been changed yet</p>
          <p>{plan.effect}</p>
          <ul>
            {plan.consequences.map((c) => (
              <li key={c}>{c}</li>
            ))}
          </ul>
          {plan.argv.length > 0 && (
            <p>
              <code>{plan.argv.join(' ')}</code>
            </p>
          )}
          {plan.step_up_required && (
            <label>
              Password
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
            </label>
          )}
          <p>
            <button type="button" disabled={busy} onClick={() => void confirm()}>
              Confirm reviewed action
            </button>
          </p>
        </div>
      )}
    </section>
  );
}
