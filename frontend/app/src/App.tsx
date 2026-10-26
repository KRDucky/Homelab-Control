// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useState } from 'react';
import {
  getCapabilities,
  getSession,
  logout,
  type Capabilities,
  type Capability,
  type SessionInfo,
} from './api';
import { ThemeStudio } from './pages/ThemeStudio';
import { Login } from './Login';
import { Audit } from './pages/Audit';
import { Hardware } from './pages/Hardware';
import { Legacy } from './pages/Legacy';
import { OverviewPage } from './pages/OverviewPage';
import { Services } from './pages/Services';
import { Sessions } from './pages/Sessions';
import { navigate, usePath } from './router';
import { Shell, statusNote } from './Shell';

type Phase = 'checking' | 'anonymous' | 'ready';

// The signed-in user's own account pages (always available with a session).
const ACCOUNT_PATH = '/account/sessions';

function Unavailable({ cap }: { cap: Capability }) {
  return (
    <div className="page">
      <h1 className="page-title">{cap.label}</h1>
      <p className="banner" role="status">
        {statusNote[cap.status]}: {cap.reason}
      </p>
    </div>
  );
}

export function App() {
  const [phase, setPhase] = useState<Phase>('checking');
  const [session, setSession] = useState<SessionInfo | null>(null);
  const [caps, setCaps] = useState<Capabilities | null>(null);
  const path = usePath();

  useEffect(() => {
    getSession()
      .then((s) => {
        setSession(s);
        setPhase('ready');
      })
      .catch(() => setPhase('anonymous'));
  }, []);

  const signedOut = useCallback(() => {
    setSession(null);
    setCaps(null);
    setPhase('anonymous');
  }, []);

  useEffect(() => {
    if (phase !== 'ready') return;
    const load = () =>
      getCapabilities()
        .then(setCaps)
        .catch(() => undefined);
    const first = window.setTimeout(load, 0);
    const t = window.setInterval(load, 30_000);
    return () => {
      window.clearTimeout(first);
      window.clearInterval(t);
    };
  }, [phase]);

  if (phase === 'checking') return <div className="boot" aria-busy="true" />;
  if (phase === 'anonymous' || !session) {
    return (
      <Login
        onLogin={(s) => {
          setSession(s);
          setPhase('ready');
        }}
      />
    );
  }

  async function signOut() {
    if (session) await logout(session.csrf_token).catch(() => undefined);
    signedOut();
    navigate('/');
  }

  const capList = caps?.capabilities ?? [];
  // Several capabilities can share a page (service logs live on /services);
  // the navigation shows one entry per page.
  const pages = capList.filter((c, i) => capList.findIndex((x) => x.path === c.path) === i);
  const logsCap = capList.find((c) => c.id === 'service_logs');
  const route =
    path === ACCOUNT_PATH || path === '/appearance/themes'
      ? undefined
      : (capList.find((c) => c.path === path) ?? capList.find((c) => c.path === '/'));
  const routeUsable = route && (route.status === 'available' || route.status === 'degraded');

  let page = <p className="muted">Loading…</p>;
  const onAccount = path === ACCOUNT_PATH;
  const onThemes = path === '/appearance/themes';
  if (onThemes) page = <ThemeStudio />;
  else if (onAccount) page = <Sessions csrf={session.csrf_token} onUnauthorized={signedOut} />;
  else if (route && !routeUsable) page = <Unavailable cap={route} />;
  else if (route?.id === 'services') page = <Services onUnauthorized={signedOut} logs={logsCap} />;
  else if (route?.id === 'audit') page = <Audit onUnauthorized={signedOut} />;
  else if (route?.id === 'hardware') page = <Hardware onUnauthorized={signedOut} />;
  else if (route?.id === 'legacy')
    page = <Legacy legacyUrl={session.legacy_url} onUnauthorized={signedOut} />;
  else if (route?.id === 'overview') page = <OverviewPage onUnauthorized={signedOut} />;

  return (
    <Shell
      pages={pages}
      currentId={route?.id}
      username={session.username}
      accountPath={ACCOUNT_PATH}
      onAccount={onAccount}
      helperReason={caps?.broker.status === 'unavailable' ? caps.broker.reason : undefined}
      onSignOut={() => void signOut()}
      version={session.version}
    >
      {caps?.broker.status === 'unavailable' && path !== '/' && routeUsable && (
        <p className="banner" role="status">
          {caps.broker.reason}
        </p>
      )}
      {page}
    </Shell>
  );
}
