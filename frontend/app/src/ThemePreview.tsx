// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { useEffect, useRef } from 'react';
import { applyTheme, type ThemeDocument } from './theme';
import './ThemePreview.css';

export type PreviewScene = 'dashboard' | 'navigation' | 'module';
export type PreviewWidth = 'desktop' | 'tablet' | 'phone';

const pages = [
  { group: 'Workspace', entries: ['Dashboard'] },
  { group: 'System', entries: ['Services', 'Hardware', 'Host settings'] },
  { group: 'Storage & files', entries: ['Storage overview', 'ZFS pools', 'File manager'] },
  { group: 'Network & apps', entries: ['Network', 'Traefik'] },
  { group: 'Administration', entries: ['Audit log'] },
];

function MiniMeter({ label, percent, detail }: { label: string; percent: number; detail: string }) {
  return (
    <div className="theme-mini-meter">
      <div className="theme-mini-meter-head">
        <span>{label}</span>
        <strong>{percent}%</strong>
      </div>
      <div className="theme-mini-meter-track" role="img" aria-label={label + ': ' + percent + '%'}>
        <span style={{ width: percent + '%' }} />
      </div>
      <small>{detail}</small>
    </div>
  );
}

/** Mock content only: styles follow the draft tokens without touching document.documentElement. */
export function ThemePreview({
  draft,
  scene,
  width,
}: {
  draft: ThemeDocument;
  scene: PreviewScene;
  width: PreviewWidth;
}) {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (root.current) applyTheme(draft, root.current);
  }, [draft]);

  return (
    <div
      className={`studio-preview preview-${width} theme-mini-app`}
      data-preview-width={width}
      data-preview-scene={scene}
      ref={root}
      aria-label="Theme preview with example data"
    >
      <p className="theme-mini-disclaimer">Example data · isolated colour preview · no host connection</p>
      <div className="theme-mini-shell">
        {width !== 'phone' && (
          <aside className="theme-mini-sidebar" aria-label="Example sidebar">
            <strong>◈ HomeControl</strong>
            {pages.map((group) => (
              <div className="theme-mini-nav-group" key={group.group}>
                <span>{group.group}</span>
                {group.entries.slice(0, width === 'tablet' ? 2 : undefined).map((name) => (
                  <div
                    className={name === 'Dashboard' && scene === 'dashboard' ? 'theme-mini-selected' : ''}
                    key={name}
                  >
                    {name}
                  </div>
                ))}
              </div>
            ))}
          </aside>
        )}
        <div className="theme-mini-workspace">
          <div className="theme-mini-topbar">
            <strong>{width === 'phone' ? '☰  HomeControl' : 'HomeControl'}</strong>
            <span>Preview mode</span>
          </div>
          {scene === 'dashboard' && (
            <div className="theme-mini-content" aria-label="Dashboard scene">
              <div className="theme-mini-heading">
                <div>
                  <h3>Dashboard</h3>
                  <p>Example host · simulated telemetry</p>
                </div>
                <span className="theme-mini-pill theme-mini-ok">● Healthy</span>
              </div>
              <div className="theme-mini-tabs">
                <span className="theme-mini-active">Overview</span>
                <span>Detailed report</span>
              </div>
              <div className="theme-mini-cards">
                <article>
                  <h4>Host</h4>
                  <strong>homelab-node-01</strong>
                  <p>Debian Linux · 12 days uptime</p>
                </article>
                <article>
                  <h4>Processor</h4>
                  <MiniMeter label="CPU busy" percent={23} detail="8 logical CPUs" />
                </article>
                <article>
                  <h4>Memory</h4>
                  <MiniMeter label="RAM in use" percent={42} detail="13.4 of 32 GiB" />
                </article>
                <article>
                  <h4>Storage</h4>
                  <MiniMeter label="Measured usage" percent={67} detail="Example filesystem" />
                </article>
                <article className="theme-mini-wide">
                  <h4>Resource trends</h4>
                  <div className="theme-mini-chart" aria-label="Simulated CPU and RAM trend">
                    <svg viewBox="0 0 320 84" preserveAspectRatio="none" aria-hidden="true">
                      <path d="M0 80 H320 M0 40 H320" className="theme-mini-gridline" />
                      <polyline
                        points="0,61 40,56 80,64 120,38 160,47 200,30 240,42 280,22 320,32"
                        className="theme-mini-chart-one"
                      />
                      <polyline
                        points="0,41 40,39 80,40 120,36 160,35 200,36 240,32 280,34 320,31"
                        className="theme-mini-chart-two"
                      />
                    </svg>
                  </div>
                </article>
              </div>
            </div>
          )}
          {scene === 'navigation' && (
            <div className="theme-mini-content" aria-label="Navigation scene">
              <h3>Find a module</h3>
              <p>Grouped, capability-aware navigation</p>
              <label className="theme-mini-search">
                Search modules <input readOnly value="Storage" aria-label="Example module search" />
              </label>
              <div className="theme-mini-module-grid">
                {['Storage overview', 'ZFS pools', 'LVM volumes', 'File manager'].map((name) => (
                  <div key={name}>
                    <strong>{name}</strong>
                    <small>Available · Example</small>
                  </div>
                ))}
              </div>
              <p className="theme-mini-banner">
                ⓘ Unavailable modules display an explanation, not an action.
              </p>
            </div>
          )}
          {scene === 'module' && (
            <div className="theme-mini-content" aria-label="Module scene">
              <div className="theme-mini-heading">
                <div>
                  <h3>Services</h3>
                  <p>System module · Example inventory</p>
                </div>
                <span className="theme-mini-pill theme-mini-warn">● Attention</span>
              </div>
              <div className="theme-mini-tabs">
                <span className="theme-mini-active">Overview</span>
                <span>Inventory</span>
                <span>Settings</span>
              </div>
              <article className="theme-mini-module-panel">
                <h4>Service health</h4>
                <p>2 running · 1 warning</p>
                <div className="theme-mini-table" role="table" aria-label="Example services">
                  <div role="row">
                    <strong role="columnheader">Unit</strong>
                    <strong role="columnheader">State</strong>
                  </div>
                  <div role="row">
                    <span role="cell">backup.service</span>
                    <span role="cell" className="theme-mini-positive">
                      Active
                    </span>
                  </div>
                  <div role="row">
                    <span role="cell">media.service</span>
                    <span role="cell" className="theme-mini-caution">
                      Warning
                    </span>
                  </div>
                </div>
              </article>
              <p className="theme-mini-banner">
                ⓘ Example warning: review service health before making changes.
              </p>
              <div className="theme-mini-actions">
                <span className="theme-mini-primary">Primary action</span>
                <span>Secondary action</span>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
