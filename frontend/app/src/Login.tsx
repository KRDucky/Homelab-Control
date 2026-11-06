// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useEffect, useState, type FormEvent } from 'react';
import { ApiError, OIDC_START_PATH, getOIDCStatus, login, type SessionInfo } from './api';

export function Login({ onLogin }: { onLogin: (s: SessionInfo) => void }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(() =>
    new URLSearchParams(window.location.search).get('login') === 'sso_failed'
      ? 'Single sign-on didn’t complete. Try again, or sign in with your local account.'
      : null,
  );
  const [busy, setBusy] = useState(false);
  const [sso, setSso] = useState(false);

  useEffect(() => {
    getOIDCStatus()
      .then((s) => setSso(s.enabled))
      .catch(() => setSso(false));
  }, []);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const s = await login(username, password);
      setPassword('');
      onLogin(s);
    } catch (err) {
      if (err instanceof ApiError && err.status === 429) {
        setError('Too many failed attempts. Wait 15 minutes, then try again.');
      } else if (err instanceof ApiError && err.status === 401) {
        setError('That username and password don’t match an account on this server.');
      } else {
        setError('The server didn’t respond. Check that hc-api is running.');
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login">
      <form className="login-plate" onSubmit={submit} aria-describedby={error ? 'login-error' : undefined}>
        <h1 className="brand-mark">Homelab Control</h1>
        <p className="login-lede">Sign in to manage this server.</p>
        <label htmlFor="username">Username</label>
        <input
          id="username"
          name="username"
          autoComplete="username"
          autoCapitalize="none"
          spellCheck={false}
          required
          value={username}
          onChange={(e) => setUsername(e.target.value)}
        />
        <label htmlFor="password">Password</label>
        <input
          id="password"
          name="password"
          type="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        {error && (
          <p id="login-error" className="form-error" role="alert">
            {error}
          </p>
        )}
        <button type="submit" disabled={busy}>
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
        {sso && (
          <a className="login-sso" href={OIDC_START_PATH}>
            Sign in with single sign-on
          </a>
        )}
      </form>
    </main>
  );
}
