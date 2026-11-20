// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import {
  ApiError,
  createFileEntry,
  downloadFile,
  getFileEntries,
  getFileRoots,
  renameFile,
  uploadFile,
  type FileEntry,
  type FileRoot,
} from '../api';
import { DataTable } from '../DataTable';
import {
  KIND_LABEL,
  childPath,
  cleanPath,
  displayName,
  displayPath,
  filterByName,
  locationSearch,
  parentPath,
  parseMode,
  readLocation,
  type FilesLocation,
} from '../filesModel';
import { formatBytes } from '../format';
import { Icon } from '../icons';
import { explain } from '../useData';
import { DeleteConfirmDialog, FileContentDialog, NamePromptDialog } from './FileDialogs';

// Read-only HCIS FileBrowser port: tree + table + breadcrumbs + details. New
// folder, Permissions and Download from the reference mock are not shown and
// never POST. Nothing downloads content. files.read never implies a write. The
// server enforces the confined roots; the UI only sends root ids and relative
// paths it was given (ADR 0012).

function dirKey(root: string, path: string): string {
  return `${root}\0${path}`;
}

function formatMtime(iso: string): string {
  const t = Date.parse(iso);
  return Number.isFinite(t) ? new Date(t).toLocaleString() : iso;
}

function kindOf(e: FileEntry) {
  return parseMode(e.mode)?.kind ?? (e.dir ? 'directory' : 'other');
}

function FilesTree({
  roots,
  currentRoot,
  currentPath,
  dirs,
  onOpen,
}: {
  roots: FileRoot[];
  currentRoot: string;
  currentPath: string;
  dirs: Record<string, string[]>;
  onOpen: (root: string, path: string) => void;
}) {
  function Nodes({ rootId, base }: { rootId: string; base: string }) {
    const kids = dirs[dirKey(rootId, base)] ?? [];
    if (kids.length === 0) return null;
    return (
      <ul>
        {kids.map((name, i) => {
          const p = childPath(base, name);
          const current = rootId === currentRoot && p === currentPath;
          const open = rootId === currentRoot && (currentPath === p || currentPath.startsWith(`${p}/`));
          return (
            <li key={`${i}\u0000${name}`}>
              <button
                type="button"
                aria-current={current ? 'true' : undefined}
                onClick={() => onOpen(rootId, p)}
              >
                <Icon name="folder" />
                <span className="mono">{displayName(name)}</span>
              </button>
              {open && <Nodes rootId={rootId} base={p} />}
            </li>
          );
        })}
      </ul>
    );
  }
  return (
    <nav className="files-tree" aria-label="Folders">
      <ul>
        {roots.map((r) => {
          const current = r.id === currentRoot && currentPath === '';
          return (
            <li key={r.id}>
              <button
                type="button"
                aria-current={current ? 'true' : undefined}
                onClick={() => onOpen(r.id, '')}
              >
                <Icon name="folder" />
                {displayName(r.label)}
              </button>
              {r.id === currentRoot && <Nodes rootId={r.id} base="" />}
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

function EntryDetails({
  entry,
  canWrite,
  canDestroy,
  onClose,
  onOpenContent,
  onDownload,
  onRename,
  onDelete,
}: {
  entry: FileEntry;
  canWrite: boolean;
  canDestroy: boolean;
  onClose: () => void;
  onOpenContent: () => void;
  onDownload: () => void;
  onRename: () => void;
  onDelete: () => void;
}) {
  const m = parseMode(entry.mode);
  const special = m ? [m.setuid && 'setuid', m.setgid && 'setgid', m.sticky && 'sticky'].filter(Boolean) : [];
  const isFile = !entry.dir && kindOf(entry) !== 'symlink';
  return (
    <section className="files-details" aria-labelledby="files-details-title">
      <header className="files-details-head">
        <h2 id="files-details-title">Details</h2>
        <button type="button" className="ghost small" onClick={onClose}>
          Close details
        </button>
      </header>
      <div className="files-actions row" aria-label="File actions">
        {isFile && (
          <>
            <button type="button" className="primary small" onClick={onOpenContent}>
              Open
            </button>
            <button type="button" className="ghost small" onClick={onDownload}>
              Download
            </button>
          </>
        )}
        {canWrite && (
          <button type="button" className="ghost small" onClick={onRename}>
            Rename
          </button>
        )}
        {canDestroy && (
          <button type="button" className="warn-action small" onClick={onDelete}>
            Delete
          </button>
        )}
      </div>
      <dl className="facts">
        <div>
          <dt>Name</dt>
          <dd className="mono">{displayName(entry.name)}</dd>
        </div>
        <div>
          <dt>Type</dt>
          <dd>{KIND_LABEL[kindOf(entry)]}</dd>
        </div>
        <div>
          <dt>Size</dt>
          <dd>
            {entry.dir ? (
              <span className="muted">Not shown for folders</span>
            ) : (
              <>
                {formatBytes(entry.size)} <span className="muted">({entry.size.toLocaleString()} bytes)</span>
              </>
            )}
          </dd>
        </div>
        <div>
          <dt>Owner (uid:gid)</dt>
          <dd className="mono">{entry.owner}</dd>
        </div>
        <div>
          <dt>Mode</dt>
          <dd>
            <span className="mono">{entry.mode}</span>
            {m && (
              <>
                {' '}
                <span className="muted">
                  octal <span className="mono">{m.octal}</span>
                  {special.length > 0 && `, ${special.join(', ')}`}
                </span>
              </>
            )}
          </dd>
        </div>
        <div>
          <dt>Modified</dt>
          <dd>
            {entry.mtime ? (
              <>
                {formatMtime(entry.mtime)} <span className="muted mono">{entry.mtime}</span>
              </>
            ) : (
              <span className="muted">Not reported</span>
            )}
          </dd>
        </div>
      </dl>
      {kindOf(entry) === 'symlink' && (
        <p className="muted">Symbolic links are listed but never followed or opened.</p>
      )}
    </section>
  );
}

function FilesBrowser({
  csrf,
  canWrite,
  canDestroy,
  onUnauthorized,
}: {
  csrf: string;
  canWrite: boolean;
  canDestroy: boolean;
  onUnauthorized: () => void;
}) {
  const [roots, setRoots] = useState<FileRoot[] | null>(null);
  const [browseAvailable, setBrowseAvailable] = useState(true);
  const [rootId, setRootId] = useState('');
  const [path, setPath] = useState('');
  const [entries, setEntries] = useState<FileEntry[] | null>(null);
  const [dirs, setDirs] = useState<Record<string, string[]>>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [filter, setFilter] = useState('');
  const [selected, setSelected] = useState<number | null>(null);
  const [announce, setAnnounce] = useState('');
  const [omitted, setOmitted] = useState(0);
  const [openEntry, setOpenEntry] = useState<{ name: string; path: string } | null>(null);
  const [deleteEntry, setDeleteEntry] = useState<{ name: string; path: string } | null>(null);
  const [prompt, setPrompt] = useState<{
    kind: 'create-file' | 'create-dir' | 'rename';
    name: string;
    path: string;
  } | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const uploadRef = useRef<HTMLInputElement>(null);
  // Only the latest request may update the view: a slow earlier listing must not
  // replace a newer one the user already navigated to.
  const generation = useRef(0);
  const locationRef = useRef<HTMLHeadingElement>(null);

  const loadEntries = useCallback(
    async (id: string, rel: string, history: 'push' | 'replace' | 'none', focus: boolean) => {
      const gen = ++generation.current;
      setBusy(true);
      setError(null);
      try {
        const next = await getFileEntries(id, rel, csrf);
        if (gen !== generation.current) return;
        const cleaned = cleanPath(next.path);
        setRootId(next.root);
        setPath(cleaned);
        setEntries(next.entries);
        setOmitted(next.omitted ?? 0);
        setFilter('');
        setSelected(null);
        setDirs((prev) => ({
          ...prev,
          [dirKey(next.root, cleaned)]: next.entries.filter((e) => e.dir).map((e) => e.name),
        }));
        const n = next.entries.length;
        setAnnounce(`${n} ${n === 1 ? 'item' : 'items'} in ${displayPath(cleaned) || 'the root folder'}`);
        if (history !== 'none') {
          const url = `${window.location.pathname}${locationSearch({ root: next.root, path: cleaned })}`;
          if (history === 'push') window.history.pushState(null, '', url);
          else window.history.replaceState(null, '', url);
        }
        if (focus) window.setTimeout(() => locationRef.current?.focus(), 0);
      } catch (err) {
        if (gen !== generation.current) return;
        if (err instanceof ApiError && err.status === 401) onUnauthorized();
        setEntries(null);
        setOmitted(0);
        setSelected(null);
        setError(err instanceof ApiError ? err.message : explain(err as Error));
      } finally {
        if (gen === generation.current) setBusy(false);
      }
    },
    [csrf, onUnauthorized],
  );

  const open = useCallback(
    (id: string, rel: string) => void loadEntries(id, rel, 'push', true),
    [loadEntries],
  );

  const refresh = useCallback(() => {
    if (rootId) void loadEntries(rootId, path, 'none', false);
  }, [loadEntries, rootId, path]);

  const runAction = useCallback(
    async (fn: () => Promise<unknown>) => {
      setActionError(null);
      try {
        await fn();
        refresh();
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          onUnauthorized();
          return;
        }
        setActionError(err instanceof ApiError ? err.message : explain(err as Error));
        throw err;
      }
    },
    [refresh, onUnauthorized],
  );

  const submitCreate = useCallback(
    async (name: string) => {
      const type = prompt?.kind === 'create-dir' ? 'dir' : 'file';
      await runAction(() => createFileEntry(rootId, path, csrf, name, type));
    },
    [prompt, rootId, path, csrf, runAction],
  );

  const submitRename = useCallback(
    async (name: string) => {
      if (!prompt) return;
      await runAction(() => renameFile(rootId, prompt.path, csrf, name));
    },
    [prompt, rootId, csrf, runAction],
  );

  const submitUpload = useCallback(
    async (file: File) => {
      await runAction(() => uploadFile(rootId, path, csrf, file));
    },
    [rootId, path, csrf, runAction],
  );

  const startAt = useCallback(
    (list: FileRoot[], loc: FilesLocation | null, history: 'replace' | 'none') => {
      const wanted = loc && list.some((r) => r.id === loc.root) ? loc : null;
      const first = list[0];
      if (wanted) void loadEntries(wanted.root, wanted.path, history, false);
      else if (first) void loadEntries(first.id, '', history, false);
    },
    [loadEntries],
  );

  const load = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const r = await getFileRoots(csrf);
      setBrowseAvailable(r.browse_available);
      setRoots(r.roots);
      if (r.browse_available && r.roots.length > 0) {
        startAt(r.roots, readLocation(window.location.search), 'replace');
        return;
      }
      setBusy(false);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      setRoots([]);
      setBrowseAvailable(false);
      setError(err instanceof ApiError ? err.message : explain(err as Error));
      setBusy(false);
    }
  }, [csrf, onUnauthorized, startAt]);

  useEffect(() => {
    const t = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(t);
  }, [load]);

  // Back/forward restore the folder from the URL without adding history.
  useEffect(() => {
    const onPop = () => {
      if (roots && roots.length > 0 && window.location.pathname === '/files') {
        startAt(roots, readLocation(window.location.search), 'none');
      }
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, [roots, startAt]);

  const currentRoot = roots?.find((r) => r.id === rootId);
  const rootLabel = currentRoot?.label ?? rootId;
  const parts = path.split('/').filter(Boolean);
  const up = parentPath(path);
  const shown = entries ? filterByName(entries, filter) : [];
  const detail = selected !== null && entries ? (entries[selected] ?? null) : null;
  const indexOfEntry = useMemo(() => {
    const m = new Map<FileEntry, number>();
    (entries ?? []).forEach((e, i) => m.set(e, i));
    return m;
  }, [entries]);

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.altKey && e.key === 'ArrowUp' && up !== null && !busy) {
      e.preventDefault();
      open(rootId, up);
    }
  }

  return (
    <>
      <p className="banner" role="note">
        Listing, read, download, write, upload, create, rename and delete inside allow-listed roots. Writes
        require files.write and deletes require files.destroy; host paths are never shown.
      </p>
      {actionError && (
        <p className="banner" role="alert">
          {actionError}
        </p>
      )}
      {error && (
        <p className="banner" role="alert">
          {error}
        </p>
      )}
      <p className="visually-hidden" role="status" aria-live="polite">
        {announce}
      </p>
      {roots && (!browseAvailable || roots.length === 0) && (
        <p className="banner" role="status">
          No browseable file roots are configured on this server.
        </p>
      )}
      {roots && browseAvailable && roots.length > 0 && (
        <div className="files-browser" onKeyDown={onKeyDown}>
          <FilesTree roots={roots} currentRoot={rootId} currentPath={path} dirs={dirs} onOpen={open} />
          <div className="files-main">
            <div className="files-path">
              <h2 className="visually-hidden" tabIndex={-1} ref={locationRef}>
                {path ? `${displayName(rootLabel)}: ${displayPath(path)}` : displayName(rootLabel)}
              </h2>
              <nav className="files-crumbs" aria-label="Breadcrumb">
                <ol>
                  <li>
                    {path === '' ? (
                      <span aria-current="page">{displayName(rootLabel)}</span>
                    ) : (
                      <button type="button" onClick={() => open(rootId, '')}>
                        {displayName(rootLabel)}
                      </button>
                    )}
                  </li>
                  {parts.map((part, i) => {
                    const rel = parts.slice(0, i + 1).join('/');
                    const last = i === parts.length - 1;
                    return (
                      <li key={rel}>
                        {last ? (
                          <span className="mono" aria-current="page">
                            {displayName(part)}
                          </span>
                        ) : (
                          <button type="button" className="mono" onClick={() => open(rootId, rel)}>
                            {displayName(part)}
                          </button>
                        )}
                      </li>
                    );
                  })}
                </ol>
              </nav>
            </div>
            <div className="files-toolbar">
              <button
                type="button"
                className="ghost small"
                disabled={up === null || busy}
                onClick={() => up !== null && open(rootId, up)}
                aria-keyshortcuts="Alt+ArrowUp"
              >
                Up one level
              </button>
              <button
                type="button"
                className="ghost small"
                disabled={busy || !rootId}
                onClick={() => void loadEntries(rootId, path, 'none', false)}
              >
                Refresh
              </button>
              {canWrite && (
                <>
                  <button
                    type="button"
                    className="ghost small"
                    disabled={busy || !rootId}
                    onClick={() => setPrompt({ kind: 'create-file', name: '', path: '' })}
                  >
                    New file
                  </button>
                  <button
                    type="button"
                    className="ghost small"
                    disabled={busy || !rootId}
                    onClick={() => setPrompt({ kind: 'create-dir', name: '', path: '' })}
                  >
                    New folder
                  </button>
                  <button
                    type="button"
                    className="ghost small"
                    disabled={busy || !rootId}
                    onClick={() => uploadRef.current?.click()}
                  >
                    Upload
                  </button>
                  <input
                    ref={uploadRef}
                    type="file"
                    className="visually-hidden"
                    aria-label="Upload file"
                    onChange={(e) => {
                      const f = e.target.files?.[0];
                      if (f) void submitUpload(f).catch(() => {});
                      e.target.value = '';
                    }}
                  />
                </>
              )}
              <label className="inline-field grow">
                Filter by name
                <input
                  type="search"
                  value={filter}
                  maxLength={255}
                  onChange={(e) => setFilter(e.target.value)}
                  disabled={!entries}
                />
              </label>
              {entries && (
                <span className="muted" aria-live="polite">
                  {filter.trim() ? `${shown.length} of ${entries.length} items` : `${entries.length} items`}
                </span>
              )}
              {entries && omitted > 0 && (
                <span className="muted" role="status">
                  {omitted} {omitted === 1 ? 'name' : 'names'} omitted — unrepresentable or unreadable
                  metadata.
                </span>
              )}
            </div>
            {entries && (
              <DataTable
                caption={
                  path === '' ? `Contents of ${displayName(rootLabel)}` : `Contents of ${displayPath(path)}`
                }
                rowKey={(e) => `${indexOfEntry.get(e) ?? -1}\u0000${e.name}`}
                emptyState={
                  busy
                    ? 'Reading directory…'
                    : filter.trim()
                      ? 'No names match the filter.'
                      : 'This directory is empty.'
                }
                columns={[
                  {
                    id: 'name',
                    label: 'Name',
                    rowHeader: true,
                    sortValue: (e) => `${e.dir ? '0' : '1'}${e.name}`,
                    render: (e) => (
                      <span className="files-name">
                        <Icon name={e.dir ? 'folder' : 'file'} />
                        {e.dir ? (
                          <button
                            type="button"
                            className="mono"
                            onClick={() => open(rootId, childPath(path, e.name))}
                          >
                            {displayName(e.name)}
                          </button>
                        ) : (
                          <span className="mono">{displayName(e.name)}</span>
                        )}
                      </span>
                    ),
                  },
                  {
                    id: 'type',
                    label: 'Type',
                    sortValue: (e) => KIND_LABEL[kindOf(e)],
                    render: (e) => KIND_LABEL[kindOf(e)],
                  },
                  {
                    id: 'size',
                    label: 'Size',
                    sortValue: (e) => (e.dir ? -1 : e.size),
                    render: (e) => (e.dir ? <span className="muted">—</span> : formatBytes(e.size)),
                  },
                  {
                    id: 'owner',
                    label: 'Owner',
                    sortValue: (e) => e.owner,
                    render: (e) => <span className="mono">{e.owner}</span>,
                  },
                  {
                    id: 'mode',
                    label: 'Mode',
                    sortValue: (e) => e.mode,
                    render: (e) => <span className="mono">{e.mode}</span>,
                  },
                  {
                    id: 'modified',
                    label: 'Modified',
                    sortValue: (e) => e.mtime,
                    render: (e) => e.mtime && formatMtime(e.mtime),
                  },
                  {
                    id: 'details',
                    label: 'Details',
                    render: (e) => {
                      const idx = indexOfEntry.get(e) ?? -1;
                      return (
                        <button
                          type="button"
                          className="ghost small"
                          aria-label={`Details for ${displayName(e.name)}`}
                          aria-pressed={selected === idx}
                          onClick={() => setSelected(selected === idx ? null : idx)}
                        >
                          Details
                        </button>
                      );
                    },
                  },
                ]}
                rows={shown}
              />
            )}
            {busy && !entries && <p className="muted">Reading directory…</p>}
            {detail && (
              <EntryDetails
                entry={detail}
                canWrite={canWrite}
                canDestroy={canDestroy}
                onClose={() => setSelected(null)}
                onOpenContent={() => setOpenEntry({ name: detail.name, path: childPath(path, detail.name) })}
                onDownload={() =>
                  void downloadFile(rootId, childPath(path, detail.name), detail.name, csrf).catch((err) =>
                    setActionError(err instanceof ApiError ? err.message : explain(err as Error)),
                  )
                }
                onRename={() =>
                  setPrompt({ kind: 'rename', name: detail.name, path: childPath(path, detail.name) })
                }
                onDelete={() => setDeleteEntry({ name: detail.name, path: childPath(path, detail.name) })}
              />
            )}
          </div>
        </div>
      )}
      {openEntry && (
        <FileContentDialog
          root={rootId}
          path={openEntry.path}
          name={openEntry.name}
          csrf={csrf}
          canWrite={canWrite}
          onClose={() => setOpenEntry(null)}
          onSaved={() => setOpenEntry(null)}
        />
      )}
      {deleteEntry && (
        <DeleteConfirmDialog
          root={rootId}
          path={deleteEntry.path}
          name={deleteEntry.name}
          csrf={csrf}
          onClose={() => setDeleteEntry(null)}
          onDeleted={() => setDeleteEntry(null)}
        />
      )}
      {prompt && (
        <NamePromptDialog
          title={
            prompt.kind === 'rename' ? 'Rename' : prompt.kind === 'create-dir' ? 'New folder' : 'New file'
          }
          label={prompt.kind === 'rename' ? 'New name' : 'Name'}
          initial={prompt.name}
          submitLabel={prompt.kind === 'rename' ? 'Rename' : 'Create'}
          onClose={() => setPrompt(null)}
          onSubmit={prompt.kind === 'rename' ? submitRename : submitCreate}
        />
      )}
    </>
  );
}

export function FilesPage({
  csrf,
  canRead,
  canWrite,
  canDestroy,
  onUnauthorized,
}: {
  csrf: string;
  canRead: boolean;
  canWrite: boolean;
  canDestroy: boolean;
  onUnauthorized: () => void;
}) {
  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Files</h1>
        <p className="page-sub">
          Names, owners and modes inside allow-listed roots. Host paths are never shown. Read and download
          need files.read; write, upload, create and rename need files.write; delete needs files.destroy.
          Alt+Up opens the parent folder.
        </p>
      </header>
      {!canRead ? (
        <p className="banner" role="status">
          Your account does not have the files.read permission.
        </p>
      ) : (
        <FilesBrowser
          csrf={csrf}
          canWrite={canWrite}
          canDestroy={canDestroy}
          onUnauthorized={onUnauthorized}
        />
      )}
    </div>
  );
}
