// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useId, useState } from 'react';
import {
  ApiError,
  applyFileDelete,
  downloadFile,
  getFileContent,
  planFileDelete,
  putFileContent,
  stepUp,
  type FileDeletePlan,
} from '../api';
import { Dialog } from '../Dialog';
import { explain } from '../useData';

function ErrorBanner({ error }: { error: string | null }) {
  if (!error) return null;
  return (
    <p className="banner" role="alert">
      {error}
    </p>
  );
}

// ---- content read/edit/download -------------------------------------------

export function FileContentDialog({
  root,
  path,
  name,
  csrf,
  canWrite,
  onClose,
  onSaved,
}: {
  root: string;
  path: string;
  name: string;
  csrf: string;
  canWrite: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [error, setError] = useState<string | null>(null);
  const [encoding, setEncoding] = useState<'utf8' | 'binary'>('utf8');
  const [truncated, setTruncated] = useState(false);
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [size, setSize] = useState(0);

  const load = useCallback(async () => {
    setState('loading');
    setError(null);
    try {
      const c = await getFileContent(root, path, csrf);
      setEncoding(c.encoding);
      setTruncated(c.truncated);
      setSize(c.size);
      if (c.encoding === 'utf8') setText(new TextDecoder().decode(c.bytes));
      setState('ready');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : explain(err as Error));
      setState('error');
    }
  }, [root, path, csrf]);

  useEffect(() => {
    const t = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(t);
  }, [load]);

  async function save() {
    setBusy(true);
    setError(null);
    try {
      await putFileContent(root, path, csrf, text);
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : explain(err as Error));
      setBusy(false);
    }
  }

  const isBinary = encoding === 'binary';

  return (
    <Dialog title={name} onClose={onClose}>
      <div className="file-content">
        {state === 'loading' && <p className="muted">Reading file…</p>}
        {state === 'error' && <ErrorBanner error={error} />}
        {state === 'ready' && (
          <>
            {isBinary || (truncated && encoding !== 'utf8') ? (
              <p className="banner" role="note">
                This is a binary file and cannot be edited here. Use Download to save its content.
              </p>
            ) : (
              <>
                {truncated && (
                  <p className="banner" role="note">
                    This file is larger than the 4 MiB preview limit and is shown truncated. Editing is
                    disabled to avoid losing data; download it instead.
                  </p>
                )}
                <textarea
                  aria-label="File content"
                  className="mono"
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  disabled={truncated}
                  rows={16}
                />
              </>
            )}
            <p className="muted">
              {size.toLocaleString()} bytes{truncated ? ' (truncated preview)' : ''}
            </p>
            <div className="row">
              {!isBinary && canWrite && (
                <button
                  type="button"
                  className="primary"
                  disabled={busy || truncated}
                  onClick={() => void save()}
                >
                  Save
                </button>
              )}
              <button
                type="button"
                className="ghost"
                onClick={() => void downloadFile(root, path, name, csrf)}
              >
                Download
              </button>
              <button type="button" className="ghost" onClick={onClose}>
                Close
              </button>
            </div>
          </>
        )}
      </div>
    </Dialog>
  );
}

// ---- name prompt (create / rename) ----------------------------------------

export function NamePromptDialog({
  title,
  label,
  initial,
  submitLabel,
  onSubmit,
  onClose,
}: {
  title: string;
  label: string;
  initial: string;
  submitLabel: string;
  onSubmit: (name: string) => Promise<void>;
  onClose: () => void;
}) {
  const [value, setValue] = useState(initial);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const id = useId();
  async function submit() {
    setBusy(true);
    setError(null);
    try {
      await onSubmit(value);
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : explain(err as Error));
      setBusy(false);
    }
  }
  return (
    <Dialog title={title} onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (!busy) void submit();
        }}
      >
        <ErrorBanner error={error} />
        <label className="field" htmlFor={id}>
          {label}
          <input id={id} value={value} maxLength={255} onChange={(e) => setValue(e.target.value)} autoFocus />
        </label>
        <div className="row">
          <button type="submit" className="primary" disabled={busy || value.trim() === ''}>
            {submitLabel}
          </button>
          <button type="button" className="ghost" onClick={onClose}>
            Cancel
          </button>
        </div>
      </form>
    </Dialog>
  );
}

// ---- delete: plan → confirm (step-up when required) -----------------------

export function DeleteConfirmDialog({
  root,
  path,
  name,
  csrf,
  onClose,
  onDeleted,
}: {
  root: string;
  path: string;
  name: string;
  csrf: string;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const [plan, setPlan] = useState<FileDeletePlan | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [needPassword, setNeedPassword] = useState(false);
  const [password, setPassword] = useState('');
  const pwId = useId();

  useEffect(() => {
    let live = true;
    void planFileDelete(root, path, csrf).then(
      (p) => {
        if (live) setPlan(p);
      },
      (err: unknown) => {
        if (live) setError(err instanceof ApiError ? err.message : explain(err as Error));
      },
    );
    return () => {
      live = false;
    };
  }, [root, path, csrf]);

  async function apply() {
    if (!plan) return;
    setBusy(true);
    setError(null);
    try {
      await applyFileDelete(plan.token, csrf);
      onDeleted();
      onClose();
    } catch (err) {
      if (err instanceof ApiError && err.status === 403 && err.code === 'step_up_required') {
        setNeedPassword(true);
        setBusy(false);
        return;
      }
      setError(err instanceof ApiError ? err.message : explain(err as Error));
      setBusy(false);
    }
  }

  async function confirmWithPassword() {
    setBusy(true);
    setError(null);
    try {
      await stepUp(password, csrf);
      setNeedPassword(false);
      setPassword('');
      await apply();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : explain(err as Error));
      setBusy(false);
    }
  }

  return (
    <Dialog title="Delete file" onClose={onClose}>
      {!plan && !error && <p className="muted">Reviewing delete…</p>}
      <ErrorBanner error={error} />
      {plan && (
        <>
          <p>
            Delete <span className="mono">{name}</span> ({plan.kind}, {plan.size.toLocaleString()} bytes)?
            This cannot be undone.
          </p>
          {needPassword ? (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                if (!busy) void confirmWithPassword();
              }}
            >
              <label className="field" htmlFor={pwId}>
                Password (re-enter to confirm)
                <input
                  id={pwId}
                  type="password"
                  value={password}
                  autoFocus
                  onChange={(e) => setPassword(e.target.value)}
                />
              </label>
              <div className="row">
                <button type="submit" className="warn-action" disabled={busy || password === ''}>
                  Confirm delete
                </button>
                <button type="button" className="ghost" onClick={onClose}>
                  Cancel
                </button>
              </div>
            </form>
          ) : (
            <div className="row">
              <button type="button" className="warn-action" disabled={busy} onClick={() => void apply()}>
                Delete
              </button>
              <button type="button" className="ghost" onClick={onClose}>
                Cancel
              </button>
            </div>
          )}
        </>
      )}
    </Dialog>
  );
}
