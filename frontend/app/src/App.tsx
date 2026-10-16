import { useCallback, useEffect, useState } from 'react';
import {
  getCapabilities,
  getSession,
  logout,
  type Capabilities,
  type Capability,
  type SessionInfo,
} from './api';
import { Login } from './Login';
import { Hardware } from './pages/Hardware';
import { Legacy } from './pages/Legacy';
import { OverviewPage } from './pages/OverviewPage';
import { Services } from './pages/Services';
import { linkHandler, navigate, usePath } from './router';

type Phase = 'checking' | 'anonymous' | 'ready';

const statusNote: Partial<Record<Capability['status'], string>> = {
  degraded: 'Limited',
  unavailable: 'Unavailable',
  unsupported: 'Not supported here',
  forbidden: 'No permission',
};

function NavItem({ cap, current }: { cap: Capability; current: boolean }) {
  const usable = cap.status === 'available' || cap.status === 'degraded';
  const noteId = `nav-note-${cap.id}`;
  if (!usable) {
    return (
      <li className="nav-off">
        <span className="nav-label" aria-describedby={noteId}>
          {cap.label}
        </span>
        <span id={noteId} className="nav-note">
          {statusNote[cap.status]}: {cap.reason}
        </span>
      </li>
    );
  }
  return (
    <li>
      <a
        href={cap.path}
        onClick={linkHandler(cap.path)}
        aria-current={current ? 'page' : undefined}
        aria-describedby={cap.status === 'degraded' ? noteId : undefined}
      >
        {cap.label}
        {cap.status === 'degraded' && <span className="nav-tag">{statusNote.degraded}</span>}
      </a>
      {cap.status === 'degraded' && (
        <span id={noteId} className="visually-hidden">
          {cap.reason}
        </span>
      )}
    </li>
  );
}

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
  const route = capList.find((c) => c.path === path) ?? capList.find((c) => c.path === '/');
  const routeUsable = route && (route.status === 'available' || route.status === 'degraded');

  let page = <p className="muted">Loading…</p>;
  if (route && !routeUsable) page = <Unavailable cap={route} />;
  else if (route?.id === 'services') page = <Services onUnauthorized={signedOut} />;
  else if (route?.id === 'hardware') page = <Hardware onUnauthorized={signedOut} />;
  else if (route?.id === 'legacy')
    page = <Legacy legacyUrl={session.legacy_url} onUnauthorized={signedOut} />;
  else if (route?.id === 'overview') page = <OverviewPage onUnauthorized={signedOut} />;

  return (
    <div className="shell">
      <a className="skip" href="#main">
        Skip to content
      </a>
      <nav className="rail" aria-label="Main">
        <span className="brand-mark small">Homelab Control</span>
        <ul>
          {capList.map((c) => (
            <NavItem key={c.id} cap={c} current={route?.id === c.id} />
          ))}
        </ul>
        <p className="rail-note">Read-only build. Nothing here changes your system.</p>
      </nav>
      <div className="workspace">
        <header className="topbar">
          <span className="readonly-badge">Read-only</span>
          {caps?.broker.status === 'unavailable' && (
            <span className="helper-off" title={caps.broker.reason}>
              Privileged helper not running
            </span>
          )}
          <span className="who">
            Signed in as <strong>{session.username}</strong>
          </span>
          <button type="button" className="ghost" onClick={() => void signOut()}>
            Sign out
          </button>
        </header>
        <main id="main" tabIndex={-1}>
          {caps?.broker.status === 'unavailable' && path !== '/' && routeUsable && (
            <p className="banner" role="status">
              {caps.broker.reason}
            </p>
          )}
          {page}
        </main>
        <footer className="foot">Homelab Control {session.version}</footer>
      </div>
    </div>
  );
}
