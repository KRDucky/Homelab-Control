// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import type { Capability } from './api';
import { groupNavigation } from './Shell';
import { linkHandler } from './router';
import { StatusPill } from './ui';

const usable = (cap: Capability) => cap.status === 'available' || cap.status === 'degraded';

/** Read-only module launchpad. Presence/permission comes only from the API, not a static catalogue. */
export function ModuleOverview({ pages }: { pages: Capability[] }) {
  const modules = pages.filter((cap) => cap.id !== 'overview' && cap.status !== 'forbidden');
  const groups = groupNavigation(modules);
  const ready = modules.filter(usable).length;
  const limited = modules.filter((cap) => cap.status === 'degraded').length;
  return <section className="panel dashboard-module-overview" aria-label="Modules">
    <header className="panel-head"><h2>Modules</h2></header>
    <p className="dashboard-value">{ready} <span className="dashboard-unit">accessible pages</span></p>
    <p className="muted">{limited ? limited + ' available with limitations. ' : ''}
      Module availability comes from the current host and your permissions.</p>
    {groups.length === 0
      ? <p className="muted">No additional modules are available for this session.</p>
      : <div className="dashboard-module-groups">{groups.map((group) =>
        <section key={group.title} aria-label={group.title}>
          <h3>{group.title}</h3>
          <ul className="dashboard-module-list">{group.pages.map((cap) =>
            <li key={cap.path} className="dashboard-module-item">
              <div className="dashboard-module-title">
                {usable(cap)
                  ? <a href={cap.path} onClick={linkHandler(cap.path)}>{cap.label}</a>
                  : <span>{cap.label}</span>}
                <StatusPill tone={cap.status === 'available' ? 'ok' : cap.status === 'degraded' ? 'warn' : 'unknown'}
                  label={cap.status === 'available' ? 'Available' : cap.status === 'degraded' ? 'Limited' : 'Unavailable'} />
              </div>
              {cap.status !== 'available' && cap.reason &&
                <p className="dashboard-module-reason">{cap.reason}</p>}
            </li>)}</ul>
        </section>)}</div>}
  </section>;
}
