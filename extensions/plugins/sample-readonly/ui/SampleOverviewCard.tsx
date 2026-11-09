// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import type { ReactNode } from 'react';

export function SampleOverviewCard(): ReactNode {
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
