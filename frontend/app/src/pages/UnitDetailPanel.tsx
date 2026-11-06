// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, getUnitContents, getUnitDetail, type UnitContents, type UnitDetail } from '../api';
import { explain } from '../useData';

// M2-3 read-only unit detail (HCIS ServiceDetail, read-only subset): what
// systemd reports about one unit. Absent values are shown as absent, never
// guessed. The unit file is loaded only on request (it needs services.files and
// every read is audited), and arrives already redacted by the broker.

const relationLabels: Record<string, string> = {
  Requires: 'Requires',
  Requisite: 'Requisite',
  Wants: 'Wants',
  BindsTo: 'Binds to',
  PartOf: 'Part of',
  Upholds: 'Upholds',
  RequiredBy: 'Required by',
  RequisiteOf: 'Requisite of',
  WantedBy: 'Wanted by',
  BoundBy: 'Bound by',
  UpheldBy: 'Upheld by',
  ConsistsOf: 'Consists of',
  Conflicts: 'Conflicts with',
  ConflictedBy: 'Conflicted by',
  Before: 'Before',
  After: 'After',
  Triggers: 'Triggers',
  TriggeredBy: 'Triggered by',
  OnFailure: 'On failure',
  OnSuccess: 'On success',
};

function detailError(err: Error): string {
  if (err instanceof ApiError) {
    if (err.status === 404) return 'This unit is no longer in systemd’s unit list or installed unit files.';
    if (err.status === 403) return 'Your account does not have permission for this view.';
    if (err.status === 400) return err.message;
  }
  return explain(err);
}

export function UnitDetailPanel({
  unit,
  canReadFile,
  fileReason,
  onClose,
  onUnauthorized,
}: {
  unit: string;
  /** services.files: unit files can contain sensitive values. */
  canReadFile: boolean;
  /** Why the unit file is not offered (from the unit_files capability). */
  fileReason?: string | undefined;
  onClose: () => void;
  onUnauthorized: () => void;
}) {
  const [data, setData] = useState<UnitDetail | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [file, setFile] = useState<UnitContents | null>(null);
  const [fileError, setFileError] = useState<Error | null>(null);
  const [fileLoading, setFileLoading] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);

  const load = useCallback(async () => {
    try {
      setData(await getUnitDetail(unit));
      setError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      setError(err instanceof Error ? err : new Error(String(err)));
    }
  }, [unit, onUnauthorized]);

  useEffect(() => {
    const t = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(t);
  }, [load]);
  useEffect(() => heading.current?.focus(), [unit]);

  async function loadFile() {
    setFileLoading(true);
    try {
      setFile(await getUnitContents(unit));
      setFileError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      setFileError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setFileLoading(false);
    }
  }

  const deps = Object.entries(data?.dependencies ?? {});
  return (
    <section className="panel logs-panel detail-panel" aria-labelledby="h-detail" aria-busy={!data && !error}>
      <div className="panel-head">
        <h2 id="h-detail" ref={heading} tabIndex={-1}>
          Details: <code>{unit}</code>
        </h2>
        <div className="toolbar compact">
          <button type="button" className="ghost" onClick={() => void load()}>
            Refresh
          </button>
          <button type="button" className="ghost" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
      {error && (
        <p className="banner" role="alert">
          {detailError(error)}
        </p>
      )}
      {!data && !error && <p className="muted">Reading the unit from systemd…</p>}
      {data && (
        <>
          {(data.notes ?? []).length > 0 && (
            <ul className="notes" aria-label="Notes">
              {(data.notes ?? []).map((n) => (
                <li key={n}>{n}</li>
              ))}
            </ul>
          )}
          {data.protected?.protected && (
            <p className="footnote" role="note">
              Protected: {data.protected.reason} It cannot be started, stopped, restarted, enabled or disabled
              here.
            </p>
          )}
          <dl className="kv detail-kv">
            <dt>Unit</dt>
            <dd>
              <code>{data.id ?? data.requested}</code>
              {data.kind === 'alias' && <span className="muted"> (requested as {data.requested})</span>}
              {data.kind === 'template' && <span className="tag">template</span>}
            </dd>
            {data.description && (
              <>
                <dt>Description</dt>
                <dd>{data.description}</dd>
              </>
            )}
            {data.names && data.names.length > 1 && (
              <>
                <dt>Also known as</dt>
                <dd>
                  {data.names
                    .filter((n) => n !== data.id)
                    .map((n) => (
                      <code key={n}>{n} </code>
                    ))}
                </dd>
              </>
            )}
            {data.active_state && (
              <>
                <dt>State</dt>
                <dd>
                  {data.active_state}
                  {data.sub_state ? ` (${data.sub_state})` : ''}
                  {data.result && data.result !== 'success' ? `, result ${data.result}` : ''}
                  {data.load_state && data.load_state !== 'loaded' ? `, load state ${data.load_state}` : ''}
                </dd>
              </>
            )}
            <dt>At boot</dt>
            <dd>
              {data.boot_state ?? 'not installed as a unit file'}
              {data.preset ? <span className="muted"> (distribution preset: {data.preset})</span> : null}
            </dd>
            {data.fragment_path && (
              <>
                <dt>Unit file</dt>
                <dd>
                  <code>{data.fragment_path}</code>
                </dd>
              </>
            )}
            {data.source_path && (
              <>
                <dt>Generated from</dt>
                <dd>
                  <code>{data.source_path}</code>
                </dd>
              </>
            )}
            {(data.drop_in_paths ?? []).length > 0 && (
              <>
                <dt>Drop-ins</dt>
                <dd>
                  <ul className="plain-list compact">
                    {(data.drop_in_paths ?? []).map((p) => (
                      <li key={p}>
                        <code>{p}</code>
                      </li>
                    ))}
                  </ul>
                </dd>
              </>
            )}
            {data.service_type && (
              <>
                <dt>Service type</dt>
                <dd>
                  {data.service_type}
                  {data.restart ? `, Restart=${data.restart}` : ''}
                </dd>
              </>
            )}
            {data.restarts !== undefined && (
              <>
                <dt>Automatic restarts</dt>
                <dd>{data.restarts} since the manager started</dd>
              </>
            )}
            {data.last_exit && (
              <>
                <dt>Last exit</dt>
                <dd>
                  {data.last_exit.code}
                  {data.last_exit.code === 'exited' ? ` with status ${data.last_exit.status}` : ''}
                  {data.last_exit.code === 'killed' || data.last_exit.code === 'dumped core'
                    ? ` by signal ${data.last_exit.status}`
                    : ''}{' '}
                  <span className="muted">at {data.last_exit.exited_at}</span>
                </dd>
              </>
            )}
            {data.main_pid ? (
              <>
                <dt>Main PID</dt>
                <dd>{data.main_pid}</dd>
              </>
            ) : null}
            {data.state_since && (
              <>
                <dt>State since</dt>
                <dd>{data.state_since}</dd>
              </>
            )}
          </dl>
          {deps.length > 0 && (
            <details className="sources" open>
              <summary>Dependencies ({deps.reduce((n, [, l]) => n + l.length, 0)})</summary>
              <dl className="kv detail-kv">
                {deps.map(([rel, names]) => (
                  <div key={rel} className="kv-row">
                    <dt>{relationLabels[rel] ?? rel}</dt>
                    <dd>
                      {names.map((n) => (
                        <code key={n} className="dep">
                          {n}
                        </code>
                      ))}
                    </dd>
                  </div>
                ))}
              </dl>
              {data.dependencies_truncated && <p className="footnote">Long lists are cut at 64 names.</p>}
            </details>
          )}
          <section aria-labelledby="h-unitfile" className="unit-file">
            <h3 id="h-unitfile">Unit file</h3>
            {!canReadFile ? (
              <p className="footnote">
                {fileReason ?? 'Reading unit files needs the services.files permission.'} Unit files can
                contain credentials and other sensitive configuration, so this permission is separate from
                service logs and never granted by default.
              </p>
            ) : !file ? (
              <>
                <p className="footnote">
                  Read-only. Files only root can read are not shown, and values that look like secrets
                  (environment values, credentials, password or token arguments) are replaced by [redacted].
                  This detection is heuristic: the file can still disclose sensitive configuration. The read
                  is recorded in the audit log.
                </p>
                <button
                  type="button"
                  className="ghost"
                  onClick={() => void loadFile()}
                  disabled={fileLoading}
                >
                  {fileLoading ? 'Reading…' : 'Show unit file'}
                </button>
              </>
            ) : null}
            {fileError && (
              <p className="banner" role="alert">
                {detailError(fileError)}
              </p>
            )}
            {file && (
              <>
                <p className="footnote">
                  {file.redactions} line{file.redactions === 1 ? '' : 's'} redacted
                  {file.truncated ? '; cut at 64 KiB or 2,000 lines' : ''}.
                </p>
                {(file.notes ?? []).map((n) => (
                  <p key={n} className="footnote" role="note">
                    systemd: {n}
                  </p>
                ))}
                {file.files.map((f) => (
                  <figure key={f.path} className="unit-file-text">
                    <figcaption>
                      <code>{f.path}</code>
                    </figcaption>
                    {f.unreadable ? (
                      <p className="muted">Not shown: only root can read this file.</p>
                    ) : (
                      <pre tabIndex={0} aria-label={`Contents of ${f.path}`}>
                        {f.text}
                      </pre>
                    )}
                  </figure>
                ))}
              </>
            )}
          </section>
          <details className="sources">
            <summary>Where this data comes from</summary>
            <p>
              <code>{data.source}</code>, via the privileged helper.
            </p>
          </details>
        </>
      )}
    </section>
  );
}
