// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { getOverview, type Capability } from '../api';
import { useState } from 'react';
import { Dashboard } from '../Dashboard';
import { LiveIndicator } from '../ui';
import { Overview } from '../Overview';
import { PluginContributions } from '../plugin-ui';
import { explain, useData } from '../useData';

export function OverviewPage({ onUnauthorized, pages }: { onUnauthorized: () => void; pages: Capability[] }) {
  const [tab, setTab] = useState<'dashboard' | 'details'>('dashboard');
  const { data, error, live } = useData(getOverview, { intervalMs: 10_000, onUnauthorized });
  return (
    <>
      <LiveIndicator {...live} />
      {error && (
        <p className="banner" role="alert">
          {explain(error)}
        </p>
      )}
      <div className="tabs dashboard-tabs" role="tablist" aria-label="Overview views">
        <button id="overview-tab-dashboard" type="button" role="tab" aria-selected={tab === 'dashboard'} aria-controls="overview-panel-dashboard" onClick={() => setTab('dashboard')}>Dashboard</button>
        <button id="overview-tab-details" type="button" role="tab" aria-selected={tab === 'details'} aria-controls="overview-panel-details" onClick={() => setTab('details')}>Detailed host report</button>
      </div>
      <div id={tab === 'dashboard' ? 'overview-panel-dashboard' : 'overview-panel-details'} role="tabpanel" aria-labelledby={tab === 'dashboard' ? 'overview-tab-dashboard' : 'overview-tab-details'}>
        {data ? tab === 'dashboard' ? <Dashboard data={data} pages={pages} /> : <Overview data={data} /> : !error && <p className="muted">Reading host data…</p>}
      </div>
      <PluginContributions slot="overview" />
    </>
  );
}
