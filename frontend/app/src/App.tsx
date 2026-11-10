// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useLayoutEffect, useState } from 'react';
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
import { Storage } from './pages/Storage';
import { FilesPage } from './pages/Files';
import { Software } from './pages/Software';
import { HostSettingsPage } from './pages/HostSettings';
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

  useLayoutEffect(() => {
    if (phase === 'anonymous') document.title = 'Sign in · Homelab Control';
  }, [phase]);

  const signedOut = useCallback(() => {
    setSession(null);
    setCaps(null);
    setPhase('anonymous');
    document.title = 'Sign in · Homelab Control';
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
  const visible = capList.filter((c) => c.status !== 'forbidden');
  const pages = visible.filter((c, i) => visible.findIndex((x) => x.path === c.path) === i);
  const logsCap = capList.find((c) => c.id === 'service_logs');
  const filesCap = capList.find((c) => c.id === 'unit_files');
  const auditCap = capList.find((c) => c.id === 'audit');
  // Accounts with services.operate can change the host (service start/stop/
  // restart); everyone else has a read-only interface.
  const canOperate = (session.scopes ?? []).some(
    (sc) => sc === 'services.operate' || sc === 'services.configure',
  );
  // M2-3 module registry: a module the host administrator disabled has no
  // navigation; its page says so instead of falling back to another page.
  const disabledModule = (caps?.modules ?? []).find((m) => !m.enabled && m.path === path);
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
  else if (disabledModule)
    page = (
      <div className="page">
        <h1 className="page-title">{disabledModule.title}</h1>
        <p className="banner" role="status">
          This module is disabled on this host. {disabledModule.reason}
        </p>
      </div>
    );
  else if (route && !routeUsable) page = <Unavailable cap={route} />;
  else if (route?.id === 'services')
    page = (
      <Services
        onUnauthorized={signedOut}
        logs={logsCap}
        files={filesCap}
        csrf={session.csrf_token}
        stepUpUntil={session.step_up_until ?? null}
        auditAvailable={auditCap?.status === 'available'}
      />
    );
  else if (route?.id === 'audit') page = <Audit onUnauthorized={signedOut} />;
  else if (route?.id === 'storage')
    page = <Storage csrf={session.csrf_token} scopes={session.scopes ?? []} onUnauthorized={signedOut} />;
  else if (route?.id === 'host_settings')
    page = (
      <HostSettingsPage
        csrf={session.csrf_token}
        canPreview={(session.scopes ?? []).includes('host.settings.preview')}
        canApply={(session.scopes ?? []).includes('host.settings.apply')}
        auditAvailable={auditCap?.status === 'available'}
        onUnauthorized={signedOut}
      />
    );
  else if (route?.id === 'files')
    page = (
      <FilesPage
        csrf={session.csrf_token}
        canRead={(session.scopes ?? []).includes('files.read')}
        onUnauthorized={signedOut}
      />
    );
  else if (route?.id === 'hardware') page = <Hardware onUnauthorized={signedOut} />;
  else if (route?.id === 'legacy')
    page = <Legacy legacyUrl={session.legacy_url} onUnauthorized={signedOut} />;
  else if (route?.id === 'overview') page = <OverviewPage onUnauthorized={signedOut} />;
  else if (route?.id === 'packages') page = <Software csrf={session.csrf_token} onUnauthorized={signedOut} />;

  return (
    <Shell
      pages={pages}
      currentId={disabledModule ? undefined : route?.id}
      title={
        onThemes
          ? 'Themes & Theme Studio'
          : onAccount
            ? 'Sessions'
            : (disabledModule?.title ?? route?.label ?? '')
      }
      username={session.username}
      accountPath={ACCOUNT_PATH}
      onAccount={onAccount}
      helperReason={caps?.broker.status === 'unavailable' ? caps.broker.reason : undefined}
      onSignOut={() => void signOut()}
      canOperate={canOperate}
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
