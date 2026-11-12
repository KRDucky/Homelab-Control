// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useEffect, useState, type FormEvent } from 'react';
import {
  ApiError,
  getPackageSearch,
  getPackagesInstalled,
  getPackagesSources,
  getPackagesUpdates,
  type PackageList,
  type SoftwarePackage,
  type SourceList,
  type UpdateList,
} from '../api';
import { DataTable } from '../DataTable';
import { explain } from '../useData';

type Tab = 'search' | 'installed' | 'updates' | 'sources';

const TABS: { id: Tab; label: string }[] = [
  { id: 'search', label: 'Search' },
  { id: 'installed', label: 'Installed' },
  { id: 'updates', label: 'Updates' },
  { id: 'sources', label: 'Sources' },
];

type SoftwareData = PackageList | UpdateList | SourceList;

// M8: seed the local-cache search from ?q= so another module can offer a
// Software Center search link (e.g. Install mergerfs). Trimmed and capped to
// the same 64-char limit as the input.
function seedQuery(): string {
  const q = new URLSearchParams(window.location.search).get('q');
  if (!q) return '';
  return q.trim().slice(0, 64);
}

function isPackageList(d: SoftwareData): d is PackageList {
  return 'packages' in d;
}
function isUpdateList(d: SoftwareData): d is UpdateList {
  return 'updates' in d;
}
function isSourceList(d: SoftwareData): d is SourceList {
  return 'sources' in d;
}

/**
 * Read-only Software Center (M4-1b): search, installed packages, pending-update
 * preview and configured sources, all from the local cache. No Install, Remove,
 * Update or Add-repo control exists in the DOM, not even disabled.
 */
export function Software({ csrf, onUnauthorized }: { csrf: string; onUnauthorized: () => void }) {
  const seeded = seedQuery();
  const [tab, setTab] = useState<Tab>('search');
  const [query, setQuery] = useState(seeded);
  const [submitted, setSubmitted] = useState(seeded);
  const [data, setData] = useState<SoftwareData | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    // Search does not fetch until a non-empty query is submitted (hc-ui nit 1);
    // the render shows an empty state, so nothing is reset here.
    if (tab === 'search' && submitted === '') {
      return;
    }
    let cancelled = false;
    const fetchTab = async () => {
      setLoading(true);
      setError(null);
      try {
        let next: SoftwareData;
        switch (tab) {
          case 'search':
            next = await getPackageSearch(submitted, csrf);
            break;
          case 'installed':
            next = await getPackagesInstalled(csrf);
            break;
          case 'updates':
            next = await getPackagesUpdates(csrf);
            break;
          case 'sources':
            next = await getPackagesSources(csrf);
            break;
        }
        if (!cancelled) setData(next);
      } catch (e) {
        if (cancelled) return;
        if (e instanceof ApiError && e.status === 401) onUnauthorized();
        if (e instanceof ApiError && (e.status === 401 || e.status === 403)) setData(null);
        setError(e instanceof Error ? e : new Error(String(e)));
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void fetchTab();
    return () => {
      cancelled = true;
    };
  }, [tab, submitted, csrf, onUnauthorized]);

  function onSearch(e: FormEvent) {
    e.preventDefault();
    setSubmitted(query.trim());
  }

  return (
    <div className="page">
      <h1 className="page-title">Software</h1>
      <p className="banner" role="status">
        Read-only preview — changes are not available. Package data comes from the local cache; it is never
        refreshed and never changed here.
      </p>
      <div className="tabs" role="tablist" aria-label="Software Center views">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            id={`software-tab-${t.id}`}
            aria-selected={tab === t.id}
            aria-controls={`software-panel-${t.id}`}
            onClick={() => {
              setTab(t.id);
              setData(null);
              setError(null);
              setLoading(false);
            }}
          >
            {t.label}
          </button>
        ))}
      </div>
      {TABS.map((t) => (
        <div
          key={t.id}
          role="tabpanel"
          id={`software-panel-${t.id}`}
          aria-labelledby={`software-tab-${t.id}`}
          className="software-panel"
          hidden={tab !== t.id}
        >
          {tab === t.id && (
            <>
              {t.id === 'search' && (
                <form className="inline-field grow" onSubmit={onSearch}>
                  <label htmlFor="software-query">Search packages</label>
                  <input
                    id="software-query"
                    value={query}
                    maxLength={64}
                    placeholder="package name"
                    onChange={(e) => setQuery(e.target.value)}
                  />
                  <button type="submit">Search</button>
                </form>
              )}
              {t.id === 'search' && !loading && !error && submitted === '' && (
                <p className="empty">Enter a package name to search the local cache.</p>
              )}
              {loading && <p className="muted">Reading the local package cache…</p>}
              {!loading && error && (
                <p className="banner" role="alert">
                  {explain(error)}
                </p>
              )}
              {!loading && !error && data && t.id === 'search' && isPackageList(data) && (
                <SoftwarePackages packages={data.packages} source={data.source} />
              )}
              {!loading && !error && data && t.id === 'installed' && isPackageList(data) && (
                <SoftwarePackages packages={data.packages} source={data.source} />
              )}
              {!loading && !error && data && t.id === 'updates' && isUpdateList(data) && (
                <SoftwareUpdates updates={data} />
              )}
              {!loading && !error && data && t.id === 'sources' && isSourceList(data) && (
                <SoftwareSources sources={data} />
              )}
            </>
          )}
        </div>
      ))}
    </div>
  );
}

function SoftwarePackages({ packages, source }: { packages: SoftwarePackage[]; source: string }) {
  if (packages.length === 0) return <p className="empty">No packages matched this search.</p>;
  return (
    <>
      <DataTable
        caption="Packages"
        rows={packages}
        rowKey={(p) => p.name + (p.arch ? `\u0000${p.arch}` : '')}
        columns={[
          { id: 'name', label: 'Package', render: (p) => <code>{p.name}</code> },
          { id: 'version', label: 'Version', render: (p) => p.version ?? '—' },
          { id: 'arch', label: 'Architecture', render: (p) => p.arch ?? '—' },
          {
            id: 'origin',
            label: 'Origin',
            render: (p) =>
              p.origin ? (
                <>
                  <code>{p.origin}</code>
                  {p.installed && <span className="tag">installed</span>}
                </>
              ) : p.installed ? (
                <span className="tag">installed</span>
              ) : (
                '—'
              ),
          },
          { id: 'summary', label: 'Summary', render: (p) => p.summary ?? '—' },
        ]}
      />
      <p className="footnote">{source}</p>
    </>
  );
}

function SoftwareUpdates({ updates }: { updates: UpdateList }) {
  const asOf = updates.as_of ? new Date(updates.as_of).toLocaleString() : null;
  return (
    <>
      {asOf && (
        <p className="banner" role="status">
          As of last metadata refresh {asOf}. The repositories were not contacted.
        </p>
      )}
      {updates.updates.length === 0 ? (
        <p className="empty">No pending updates.</p>
      ) : (
        <DataTable
          caption="Pending updates"
          rows={updates.updates}
          rowKey={(u) => u.name + (u.arch ? `\u0000${u.arch}` : '')}
          columns={[
            { id: 'name', label: 'Package', render: (u) => <code>{u.name}</code> },
            { id: 'arch', label: 'Architecture', render: (u) => u.arch ?? '—' },
            { id: 'current', label: 'Installed', render: (u) => u.current || '—' },
            { id: 'new', label: 'Available', render: (u) => u.new },
            { id: 'origin', label: 'Origin', render: (u) => (u.origin ? <code>{u.origin}</code> : '—') },
          ]}
        />
      )}
      <p className="footnote">{updates.source}</p>
    </>
  );
}

function SoftwareSources({ sources }: { sources: SourceList }) {
  if (sources.sources.length === 0) return <p className="empty">No configured sources found.</p>;
  return (
    <>
      <DataTable
        caption="Configured sources"
        rows={sources.sources}
        rowKey={(s) => s.name + '\u0000' + s.origin}
        columns={[
          { id: 'name', label: 'Name', render: (s) => <code>{s.name}</code> },
          { id: 'origin', label: 'Origin', render: (s) => <code>{s.origin}</code> },
          { id: 'enabled', label: 'Enabled', render: (s) => (s.enabled ? 'Yes' : 'No') },
          {
            id: 'flags',
            label: 'Flags',
            render: (s) => (
              <>
                {!s.signed && <span className="tag">unsigned</span>}
                {s.third_party && <span className="tag">third-party</span>}
              </>
            ),
          },
        ]}
      />
      <p className="footnote">
        {sources.source}. Third-party and unsigned sources are flagged and never used automatically.
      </p>
    </>
  );
}
