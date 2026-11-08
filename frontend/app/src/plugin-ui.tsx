// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useEffect, useState } from 'react';
import { EXTENSION_COMPONENTS } from './extensions.generated';
import { CONTRIBUTION_IDS, isContributionEnabled, type ContributionId } from './plugin';

/*
 * Compiled-in UI contributions for the read-only sample plugin (M3 plugin SDK first checkpoint).
 *
 * Contribution IDs are validated against an allow-list in `plugin.ts`; a hostile manifest can never
 * mount UI here because the only reachable components are the statically imported ones in
 * `CONTRIBUTION_COMPONENTS`. No `eval`, no dynamic `import()` of user files, no executable payload: the
 * map from ID to component is generated at build time from a core-owned table over known IDs.
 */

/** Which slot each known contribution renders into. Only the Overview slot exists this checkpoint. */
const CONTRIBUTION_SLOTS: Record<ContributionId, 'overview'> = {
  'overview.sample-card': 'overview',
};

/**
 * The only compiled UI a plugin contribution can reach. Anything else is never rendered. The components
 * come from the pinned Extensions import as static imports (scripts/build-extensions.mjs).
 */
const CONTRIBUTION_COMPONENTS = EXTENSION_COMPONENTS;

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
