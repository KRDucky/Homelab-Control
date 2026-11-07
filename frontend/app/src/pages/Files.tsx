// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useState } from 'react';
import { ApiError, getFileEntries, getFileRoots, type FileEntry, type FileRoot } from '../api';
import { DataTable } from '../DataTable';
import { formatBytes } from '../format';
import { Icon } from '../icons';
import { explain } from '../useData';

// Read-only HCIS FileBrowser port: tree + table + breadcrumbs. New folder,
// Permissions and Download from the reference mock are not shown and never POST.
// Preview never downloads content. files.read never implies a write.

function jailPath(path: string): string {
  return path === '.' ? '' : path;
}

function childPath(base: string, name: string): string {
  return base ? `${base}/${name}` : name;
}

function dirKey(root: string, path: string): string {
  return `${root}\0${path}`;
}

function formatMtime(iso: string): string {
  const t = Date.parse(iso);
  return Number.isFinite(t) ? new Date(t).toLocaleString() : iso;
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
        {kids.map((name) => {
          const p = childPath(base, name);
          const current = rootId === currentRoot && p === currentPath;
          const open = rootId === currentRoot && (currentPath === p || currentPath.startsWith(`${p}/`));
          return (
            <li key={name}>
              <button
                type="button"
                aria-current={current ? 'true' : undefined}
                onClick={() => onOpen(rootId, p)}
              >
                <Icon name="folder" />
                <span className="mono">{name}</span>
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
                {r.label}
              </button>
              {r.id === currentRoot && <Nodes rootId={r.id} base="" />}
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

function FilesBrowser({ csrf, onUnauthorized }: { csrf: string; onUnauthorized: () => void }) {
  const [roots, setRoots] = useState<FileRoot[] | null>(null);
  const [browseAvailable, setBrowseAvailable] = useState(true);
  const [rootId, setRootId] = useState('');
  const [path, setPath] = useState('');
  const [entries, setEntries] = useState<FileEntry[] | null>(null);
  const [dirs, setDirs] = useState<Record<string, string[]>>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const loadEntries = useCallback(
    async (id: string, rel: string) => {
      setBusy(true);
      setError(null);
      try {
        const next = await getFileEntries(id, rel, csrf);
        const cleaned = jailPath(next.path);
        setRootId(next.root);
        setPath(cleaned);
        setEntries(next.entries);
        setDirs((prev) => ({
          ...prev,
          [dirKey(next.root, cleaned)]: next.entries.filter((e) => e.dir).map((e) => e.name),
        }));
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) onUnauthorized();
        setEntries(null);
        setError(err instanceof ApiError ? err.message : explain(err as Error));
      } finally {
        setBusy(false);
      }
    },
    [csrf, onUnauthorized],
  );

  useEffect(() => {
    let cancelled = false;
    setBusy(true);
    getFileRoots(csrf)
      .then((r) => {
        if (cancelled) return;
        setBrowseAvailable(r.browse_available);
        setRoots(r.roots);
        if (r.browse_available && r.roots[0]) {
          setRootId(r.roots[0].id);
          return loadEntries(r.roots[0].id, '');
        }
        setBusy(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) onUnauthorized();
        setRoots([]);
        setBrowseAvailable(false);
        setError(err instanceof ApiError ? err.message : explain(err as Error));
        setBusy(false);
      });
    return () => {
      cancelled = true;
    };
  }, [csrf, loadEntries, onUnauthorized]);

  const currentRoot = roots?.find((r) => r.id === rootId);
  const parts = path.split('/').filter(Boolean);

  return (
    <>
      <p className="banner" role="note">
        Read-only listing inside allow-listed roots. Homelab Control does not create, rename, change
        permissions, upload, download or delete files here.
      </p>
      {error && (
        <p className="banner" role="alert">
          {error}
        </p>
      )}
      {roots && (!browseAvailable || roots.length === 0) && (
        <p className="banner" role="status">
          No browseable file roots are configured on this server.
        </p>
      )}
      {roots && browseAvailable && roots.length > 0 && (
        <div className="files-browser">
          <FilesTree
            roots={roots}
            currentRoot={rootId}
            currentPath={path}
            dirs={dirs}
            onOpen={(id, rel) => void loadEntries(id, rel)}
          />
          <div className="files-main">
            <div className="files-path">
              <nav className="files-crumbs" aria-label="Breadcrumb">
                <ol>
                  <li>
                    {path === '' ? (
                      <span aria-current="page">{currentRoot?.label ?? rootId}</span>
                    ) : (
                      <button type="button" onClick={() => void loadEntries(rootId, '')}>
                        {currentRoot?.label ?? rootId}
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
                            {part}
                          </span>
                        ) : (
                          <button
                            type="button"
                            className="mono"
                            onClick={() => void loadEntries(rootId, rel)}
                          >
                            {part}
                          </button>
                        )}
                      </li>
                    );
                  })}
                </ol>
              </nav>
            </div>
            {entries && (
              <DataTable
                caption={path === '' ? `Contents of ${currentRoot?.label ?? rootId}` : `Contents of ${path}`}
                rowKey={(e) => e.name}
                emptyState={busy ? 'Reading directory…' : 'This directory is empty.'}
                columns={[
                  {
                    id: 'name',
                    label: 'Name',
                    sortValue: (e) => `${e.dir ? '0' : '1'}${e.name}`,
                    render: (e) => (
                      <span className="files-name">
                        <Icon name={e.dir ? 'folder' : 'file'} />
                        {e.dir ? (
                          <button
                            type="button"
                            className="mono"
                            onClick={() => void loadEntries(rootId, childPath(path, e.name))}
                          >
                            {e.name}
                          </button>
                        ) : (
                          <span className="mono">{e.name}</span>
                        )}
                      </span>
                    ),
                  },
                  {
                    id: 'size',
                    label: 'Size',
                    sortValue: (e) => e.size,
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
                ]}
                rows={entries}
              />
            )}
            {busy && !entries && <p className="muted">Reading directory…</p>}
          </div>
        </div>
      )}
    </>
  );
}

export function FilesPage({
  csrf,
  canRead,
  onUnauthorized,
}: {
  csrf: string;
  canRead: boolean;
  onUnauthorized: () => void;
}) {
  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Files</h1>
        <p className="page-sub">
          Names, owners and modes inside allow-listed roots. Homelab Control never returns host paths, and
          this page does not download file contents.
        </p>
      </header>
      {!canRead ? (
        <p className="banner" role="status">
          Your account does not have the files.read permission.
        </p>
      ) : (
        <FilesBrowser csrf={csrf} onUnauthorized={onUnauthorized} />
      )}
    </div>
  );
}
