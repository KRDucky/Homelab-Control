import { useCallback, useEffect, useState } from 'react';
import {
  ApiError,
  getOverview,
  getSession,
  logout,
  type Overview as OverviewData,
  type SessionInfo,
} from './api';
import { Login } from './Login';
import { Overview } from './Overview';

type Phase = 'checking' | 'anonymous' | 'ready';

export function App() {
  const [phase, setPhase] = useState<Phase>('checking');
  const [session, setSession] = useState<SessionInfo | null>(null);
  const [data, setData] = useState<OverviewData | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getSession()
      .then((s) => {
        setSession(s);
        setPhase('ready');
      })
      .catch(() => setPhase('anonymous'));
  }, []);

  const refresh = useCallback(async () => {
    try {
      setData(await getOverview());
      setError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setSession(null);
        setPhase('anonymous');
      } else if (err instanceof ApiError && err.status === 403) {
        setError('Your account doesn’t have the system.read permission needed to view this page.');
      } else {
        setError('Couldn’t load host data. The server may be restarting.');
      }
    }
  }, []);

  useEffect(() => {
    if (phase !== 'ready') return;
    const first = window.setTimeout(() => void refresh(), 0);
    const t = window.setInterval(() => void refresh(), 10_000);
    return () => {
      window.clearTimeout(first);
      window.clearInterval(t);
    };
  }, [phase, refresh]);

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
    setSession(null);
    setData(null);
    setPhase('anonymous');
  }

  return (
    <div className="shell">
      <a className="skip" href="#main">
        Skip to content
      </a>
      <nav className="rail" aria-label="Main">
        <span className="brand-mark small">Homelab Control</span>
        <ul>
          <li>
            <a href="/" aria-current="page">
              Overview
            </a>
          </li>
          {session.legacy_url && (
            <li>
              <a href={session.legacy_url} rel="noopener noreferrer">
                Legacy Webmin
              </a>
            </li>
          )}
        </ul>
        <p className="rail-note">More sections arrive in later milestones. This build is read-only.</p>
      </nav>
      <div className="workspace">
        <header className="topbar">
          <span className="readonly-badge">Read-only</span>
          <span className="who">
            Signed in as <strong>{session.username}</strong>
          </span>
          <button type="button" className="ghost" onClick={() => void signOut()}>
            Sign out
          </button>
        </header>
        <main id="main" tabIndex={-1}>
          {error && (
            <p className="banner" role="alert">
              {error}
            </p>
          )}
          {data ? <Overview data={data} /> : !error && <p className="muted">Reading host data…</p>}
        </main>
        <footer className="foot">Homelab Control {session.version}</footer>
      </div>
    </div>
  );
}
