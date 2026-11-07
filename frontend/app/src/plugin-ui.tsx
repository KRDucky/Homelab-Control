// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useEffect, useState, type ReactNode } from 'react';
import { CONTRIBUTION_IDS, isContributionEnabled, type ContributionId } from './plugin';

/*
 * Compiled-in UI contributions for the read-only sample plugin (M3 plugin SDK first checkpoint).
 *
 * Contribution IDs are validated against an allow-list in `plugin.ts`; a hostile manifest can never
 * mount UI here because the only reachable components are the explicitly compiled ones below. No
 * `eval`, no dynamic `import()` of user files, no executable payload: the map from ID to component is
 * a fixed, authored switch over known IDs.
 */

/** Which slot each known contribution renders into. Only the Overview slot exists this checkpoint. */
const CONTRIBUTION_SLOTS: Record<ContributionId, 'overview'> = {
  'overview.sample-card': 'overview',
};

function SampleOverviewCard(): ReactNode {
  return (
    <section className="panel" aria-labelledby="sample-plugin-card-title">
      <header className="panel-head">
        <h2 id="sample-plugin-card-title">Read-only sample plugin</h2>
        <span className="tag">Sample plugin · example only</span>
      </header>
      <p>
        This card is a compiled-in example contribution. It is static fixture content — not live host data —
        and it cannot run code, read the host, or change permissions.
      </p>
    </section>
  );
}

/** The only compiled UI a plugin contribution can reach. Anything else is never rendered. */
const CONTRIBUTION_COMPONENTS: Record<ContributionId, () => ReactNode> = {
  'overview.sample-card': SampleOverviewCard,
};

/**
 * Renders enabled plugin contributions for one slot. Subscribes to `hc-plugins-change` so load/unload
 * on the Appearance menu updates any already-mounted page without a reload.
 */
export function PluginContributions({ slot }: { slot: 'overview' }) {
  const [enabled, setEnabled] = useState<ReadonlySet<ContributionId>>(() => new Set());

  useEffect(() => {
    const sync = () => {
      setEnabled(new Set(CONTRIBUTION_IDS.filter((id) => isContributionEnabled(id))));
    };
    sync();
    window.addEventListener('hc-plugins-change', sync);
    window.addEventListener('storage', sync);
    return () => {
      window.removeEventListener('hc-plugins-change', sync);
      window.removeEventListener('storage', sync);
    };
  }, []);

  const ids = CONTRIBUTION_IDS.filter((id) => enabled.has(id) && CONTRIBUTION_SLOTS[id] === slot);
  if (ids.length === 0) return null;
  return (
    <>
      {ids.map((id) => {
        const Component = CONTRIBUTION_COMPONENTS[id];
        return <Component key={id} />;
      })}
    </>
  );
}
