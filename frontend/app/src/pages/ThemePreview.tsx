// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { useEffect, useRef, useState } from 'react';
import { DataTable } from '../DataTable';
import { StatusPill } from '../ui';
import { applyTheme, type ThemeDocument } from '../theme';
import './ThemePreview.css';

type Scene = 'dashboard' | 'navigation' | 'services';

const SCENES: { id: Scene; label: string }[] = [
  { id: 'dashboard', label: 'Dashboard' },
  { id: 'navigation', label: 'Navigation' },
  { id: 'services', label: 'Module page' },
];

/** All controls are local preview interactions. No navigation, API calls or host actions occur. */
export function ThemePreview({ draft, width }: { draft: ThemeDocument; width: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [scene, setScene] = useState<Scene>('dashboard');
  const [selected, setSelected] = useState('Overview');
  const [filter, setFilter] = useState('');
  useEffect(() => {
    if (ref.current) applyTheme(draft, ref.current);
  }, [draft]);

  const items = ['Overview', 'Services', 'Hardware', 'Storage', 'Network', 'Audit'];
  const services = [
    { name: 'backup.service', status: 'Active' },
    { name: 'media.service', status: 'Not checked' },
    { name: 'sync.service', status: 'Warning' },
  ].filter((item) => item.name.toLowerCase().includes(filter.toLowerCase()));

  return (
    <div
      className={`studio-preview preview-${width} studio-context-preview`}
      ref={ref}
      aria-label="Theme preview with example data"
      data-preview-width={width}
    >
      <p className="tag">Example data · isolated preview · no host actions</p>
      <div className="studio-scene-tabs" role="tablist" aria-label="Theme preview scenes">
        {SCENES.map(({ id, label }) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={scene === id}
            aria-controls="studio-scene-content"
            onClick={() => setScene(id)}
          >
            {label}
          </button>
        ))}
      </div>
      <div id="studio-scene-content" role="tabpanel" aria-label={SCENES.find((s) => s.id === scene)?.label}>
        {scene === 'dashboard' && (
          <div className="studio-scene">
            <div className="studio-scene-head">
              <div>
                <h2>Host overview</h2>
                <p>Independent modules. Clear system state.</p>
              </div>
              <StatusPill tone="ok" label="Healthy" />
            </div>
            <div className="studio-snapshot-grid">
              <div className="studio-snapshot-card">
                <span>CPU usage</span>
                <strong>23%</strong>
                <div className="studio-snapshot-meter" role="img" aria-label="Example CPU usage: 23 percent">
                  <span style={{ width: '23%' }} />
                </div>
              </div>
              <div className="studio-snapshot-card">
                <span>Memory</span>
                <strong>42%</strong>
                <div
                  className="studio-snapshot-meter"
                  role="img"
                  aria-label="Example memory usage: 42 percent"
                >
                  <span style={{ width: '42%' }} />
                </div>
              </div>
              <div className="studio-snapshot-card">
                <span>Storage</span>
                <strong>67%</strong>
                <div
                  className="studio-snapshot-meter"
                  role="img"
                  aria-label="Example storage usage: 67 percent"
                >
                  <span style={{ width: '67%' }} />
                </div>
              </div>
            </div>
            <section className="studio-snapshot-card">
              <h3>Resource trends</h3>
              <svg
                className="studio-snapshot-chart"
                viewBox="0 0 280 80"
                preserveAspectRatio="none"
                role="img"
                aria-label="Illustrative CPU and memory trends"
              >
                <polyline
                  points="0,58 28,50 56,56 84,31 112,43 140,23 168,34 196,20 224,38 252,26 280,36"
                  className="studio-cpu-line"
                />
                <polyline
                  points="0,42 28,40 56,40 84,38 112,37 140,35 168,38 196,35 224,34 252,35 280,33"
                  className="studio-memory-line"
                />
              </svg>
              <p>CPU and RAM · illustrative samples only</p>
            </section>
          </div>
        )}
        {scene === 'navigation' && (
          <div className="studio-scene">
            <h2>Sidebar & navigation</h2>
            <p>Inspect selected, hover, focus and disabled module states.</p>
            <div className="studio-nav-frame">
              <nav aria-label="Example module navigation">
                <strong>HomeControl</strong>
                <small>WORKSPACE</small>
                {items.slice(0, 1).map((item) => (
                  <button
                    key={item}
                    type="button"
                    aria-current={selected === item ? 'page' : undefined}
                    onClick={() => setSelected(item)}
                  >
                    {item}
                  </button>
                ))}
                <small>MODULES</small>
                {items.slice(1).map((item) => (
                  <button
                    key={item}
                    type="button"
                    aria-current={selected === item ? 'page' : undefined}
                    onClick={() => setSelected(item)}
                  >
                    {item}
                  </button>
                ))}
                <button type="button" disabled>
                  Unavailable module
                </button>
              </nav>
              <section>
                <h3>{selected}</h3>
                <p>Selected preview page. Try keyboard focus and sidebar states.</p>
                <div className="studio-scene-statuses">
                  <StatusPill tone="ok" label="Healthy" />
                  <StatusPill tone="warn" label="Warning" />
                  <StatusPill tone="crit" label="Critical" />
                  <StatusPill tone="unknown" label="Not checked" />
                </div>
                <p className="banner">Warnings always keep their text and icon.</p>
              </section>
            </div>
          </div>
        )}
        {scene === 'services' && (
          <div className="studio-scene">
            <h2>Services module</h2>
            <p>Data tables, inputs and actions in a representative module page.</p>
            <div className="studio-scene-statuses">
              <StatusPill tone="ok" label="Healthy" />
              <StatusPill tone="warn" label="Warning" />
              <StatusPill tone="crit" label="Critical" />
              <StatusPill tone="unknown" label="Not checked" />
            </div>
            <label className="studio-field">
              Filter example services
              <input
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
                placeholder="Search services"
              />
            </label>
            <DataTable
              caption="Example services"
              rows={services}
              rowKey={(item) => item.name}
              columns={[
                { id: 'name', label: 'Unit', render: (item) => <code>{item.name}</code> },
                { id: 'status', label: 'State', render: (item) => item.status },
              ]}
            />
            <div className="toolbar">
              <button type="button" onClick={() => setFilter('')}>
                Clear filter
              </button>
              <button type="button" className="ghost" disabled>
                Unavailable action
              </button>
            </div>
            <p className="banner">No changes are sent to your host from this preview.</p>
          </div>
        )}
      </div>
    </div>
  );
}
