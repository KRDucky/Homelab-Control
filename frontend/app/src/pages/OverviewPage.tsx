// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { getOverview } from '../api';
import { LiveIndicator } from '../ui';
import { Overview } from '../Overview';
import { explain, useData } from '../useData';

export function OverviewPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const { data, error, live } = useData(getOverview, { intervalMs: 10_000, onUnauthorized });
  return (
    <>
      <LiveIndicator {...live} />
      {error && (
        <p className="banner" role="alert">
          {explain(error)}
        </p>
      )}
      {data ? <Overview data={data} /> : !error && <p className="muted">Reading host data…</p>}
    </>
  );
}
