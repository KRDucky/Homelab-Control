// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { getLegacy } from '../api';
import { StateDot } from '../ui';
import { explain, useData } from '../useData';

const toneFor = { ok: 'ok', warn: 'warn', info: 'idle' } as const;
const levelText = { ok: 'OK', warn: 'Review', info: 'Note' } as const;

export function Legacy({
  legacyUrl,
  onUnauthorized,
}: {
  legacyUrl?: string | undefined;
  onUnauthorized: () => void;
}) {
  const { data, error } = useData(getLegacy, { onUnauthorized });
  if (error && !data) {
    return (
      <div className="page">
        <h1 className="page-title">Legacy Webmin</h1>
        <p className="banner" role="alert">
          {explain(error)}
        </p>
      </div>
    );
  }
  if (!data) return <p className="muted">Checking for upstream Webmin…</p>;
  const st = data.status;
  const fb = data.fallback;
  const installed = st ? st.installed : fb?.installed;

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Legacy Webmin</h1>
        <p className="page-sub">
          Upstream Webmin runs separately with its own login. Homelab Control only reads its configuration and
          never changes it.
        </p>
      </header>

      {data.source === 'unprivileged' && (
        <p className="banner" role="status">
          Limited check: {data.reason}
        </p>
      )}

      <section className="panel" aria-labelledby="h-legacy">
        <h2 id="h-legacy">Installation</h2>
        {!installed && <p>Upstream Webmin is not installed at /etc/webmin on this host.</p>}
        {installed && st && (
          <dl className="facts">
            <div>
              <dt>Version</dt>
              <dd>{st.version ?? 'Not readable'}</dd>
            </div>
            <div>
              <dt>Listening on</dt>
              <dd>
                {st.listens_on_all_interfaces ? 'All interfaces' : st.bind}
                {st.port ? `, port ${st.port}` : ''}
              </dd>
            </div>
            <div>
              <dt>Encryption</dt>
              <dd>{st.tls ? 'TLS' : 'None (plain HTTP)'}</dd>
            </div>
            <div>
              <dt>Server process</dt>
              <dd>{st.running === undefined ? 'Unknown' : st.running ? 'Running' : 'Not running'}</dd>
            </div>
            <div>
              <dt>Webmin accounts</dt>
              <dd>{st.user_count}</dd>
            </div>
          </dl>
        )}
        {installed && !st && fb && (
          <p>
            Upstream Webmin {fb.version ?? ''} is installed. {fb.privilege_needed}
          </p>
        )}
        {(legacyUrl ?? fb?.configured_url) && (
          <p>
            <a href={legacyUrl ?? fb?.configured_url} rel="noopener noreferrer">
              Open the legacy Webmin interface
            </a>
          </p>
        )}
      </section>

      {st && st.checks.length > 0 && (
        <section className="panel" aria-labelledby="h-posture">
          <h2 id="h-posture">Security review</h2>
          <ul className="checks">
            {st.checks.map((c) => (
              <li key={c.id} className={`check check-${c.level}`}>
                <StateDot tone={toneFor[c.level]} />
                <span className="check-level">{levelText[c.level]}</span>
                <span>{c.message}</span>
              </li>
            ))}
          </ul>
          <p className="footnote">
            These findings come from Webmin's own configuration files. Change them in Webmin or its config
            files; Homelab Control does not edit Webmin.
          </p>
        </section>
      )}
    </div>
  );
}
