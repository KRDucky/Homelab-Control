// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react';
import type { Capability } from './api';
import { CommandPalette } from './CommandPalette';
import { AppearanceMenu } from './AppearanceMenu';
import { Icon, NAV_ICONS } from './icons';
import { usePreferences } from './preferences';
import { linkHandler } from './router';

// Shell breakpoints (HCIS AppShell): ≥1100px sidebar, 720–1100px icon rail, <720px drawer.
const NARROW = '(max-width: 719.98px)';
const WIDE = '(min-width: 1100px)';

function useMedia(query: string): boolean {
  return useSyncExternalStore(
    (cb) => {
      if (typeof window.matchMedia !== 'function') return () => undefined;
      const m = window.matchMedia(query);
      m.addEventListener('change', cb);
      return () => m.removeEventListener('change', cb);
    },
    () => (typeof window.matchMedia === 'function' ? window.matchMedia(query).matches : false),
    () => false,
  );
}

export const statusNote: Partial<Record<Capability['status'], string>> = {
  degraded: 'Limited',
  unavailable: 'Unavailable',
  unsupported: 'Not supported here',
  forbidden: 'No permission',
};

function NavItem({
  cap,
  current,
  rail,
  onNavigate,
}: {
  cap: Capability;
  current: boolean;
  rail: boolean;
  onNavigate: () => void;
}) {
  const usable = cap.status === 'available' || cap.status === 'degraded';
  const noteId = `nav-note-${cap.id}`;
  const icon = <Icon name={NAV_ICONS[cap.id] ?? 'unknown'} />;
  if (!usable) {
    return (
      <li className="nav-off">
        {/* Focusable in the icon rail so keyboard users can read why the module is unavailable. */}
        <span className="nav-item" tabIndex={rail ? 0 : undefined} aria-describedby={noteId}>
          {icon}
          <span className="nav-text">
            <span className="nav-label">{cap.label}</span>
            <span id={noteId} className="nav-note">
              {statusNote[cap.status]}: {cap.reason}
            </span>
          </span>
        </span>
      </li>
    );
  }
  return (
    <li>
      <a
        className="nav-item"
        href={cap.path}
        onClick={(e) => {
          linkHandler(cap.path)(e);
          if (e.defaultPrevented) onNavigate();
        }}
        aria-current={current ? 'page' : undefined}
        aria-describedby={cap.status === 'degraded' ? noteId : undefined}
      >
        {icon}
        <span className="nav-text">
          <span className="nav-label">{cap.label}</span>
          {cap.status === 'degraded' && <span className="nav-tag">{statusNote.degraded}</span>}
        </span>
      </a>
      {cap.status === 'degraded' && (
        <span id={noteId} className="visually-hidden">
          {cap.reason}
        </span>
      )}
    </li>
  );
}

/**
 * The application frame (HCIS AppShell): glass header, capability-driven sidebar that collapses to an
 * icon rail, a navigation drawer on phones, and the Appearance menu. Layout only: it never changes
 * what a page shows or what the user may do.
 */
export function Shell({
  pages,
  currentId,
  username,
  accountPath,
  onAccount,
  helperReason,
  onSignOut,
  version,
  children,
}: {
  pages: Capability[];
  currentId: string | undefined;
  username: string;
  accountPath: string;
  onAccount: boolean;
  helperReason: string | undefined;
  onSignOut: () => void;
  version: string;
  children: ReactNode;
}) {
  const { prefs, update, reset } = usePreferences();
  const narrow = useMedia(NARROW);
  const wide = useMedia(WIDE);
  const [drawer, setDrawer] = useState(false);
  const menuButton = useRef<HTMLButtonElement>(null);
  const nav = useRef<HTMLElement>(null);
  const restoreFocus = useRef(false);

  const drawerOpen = narrow && drawer;
  const collapsed = prefs.sidebar === 'collapsed';
  const rail = !narrow && (!wide || collapsed);

  // Drawer: focus moves in on open; Escape closes; focus returns to the menu button.
  useEffect(() => {
    if (!drawerOpen) return;
    nav.current?.querySelector<HTMLElement>('a, [tabindex], button')?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeDrawer(true);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [drawerOpen]);

  // The header is inert while the drawer is open, so focus returns only after it has closed.
  useEffect(() => {
    if (!drawerOpen && restoreFocus.current) {
      restoreFocus.current = false;
      menuButton.current?.focus();
    }
  }, [drawerOpen]);

  function closeDrawer(returnFocus: boolean) {
    restoreFocus.current = returnFocus;
    setDrawer(false);
  }

  return (
    <div
      className="shell"
      data-sidebar={narrow ? 'drawer' : rail ? 'rail' : 'expanded'}
      data-drawer={drawerOpen ? 'open' : undefined}
    >
      <a className="skip" href="#main">
        Skip to content
      </a>
      <header className="topbar glass" inert={drawerOpen}>
        <button
          ref={menuButton}
          type="button"
          className="ghost icon-btn show-narrow"
          aria-expanded={drawerOpen}
          aria-controls="main-nav"
          onClick={() => setDrawer(true)}
        >
          <Icon name="menu" />
          <span className="visually-hidden">Open navigation</span>
        </button>
        <span className="brand-mark small">Homelab Control</span>
        <span className="topbar-status">
          <span className="readonly-badge">Read-only</span>
          {helperReason && (
            <span className="helper-off" title={helperReason}>
              Privileged helper not running
            </span>
          )}
        </span>
        <span className="topbar-spacer" />
        <CommandPalette pages={pages} accountPath={accountPath} />
        <AppearanceMenu prefs={prefs} onChange={update} onReset={reset} />
        <span className="topbar-account">
          <span className="who">
            <span className="who-label">Signed in as </span>
            <strong>{username}</strong>
          </span>
          <a
            className="topbar-link"
            href={accountPath}
            onClick={linkHandler(accountPath)}
            aria-current={onAccount ? 'page' : undefined}
          >
            Sessions
          </a>
          <button type="button" className="ghost" onClick={onSignOut}>
            Sign out
          </button>
        </span>
      </header>
      {drawerOpen && <div className="drawer-scrim" aria-hidden="true" onClick={() => closeDrawer(true)} />}
      <nav ref={nav} id="main-nav" className="sidebar glass" aria-label="Main">
        {drawerOpen && (
          <div className="drawer-head">
            <span className="brand-mark small">Homelab Control</span>
            <button type="button" className="ghost icon-btn" onClick={() => closeDrawer(true)}>
              <Icon name="close" />
              <span className="visually-hidden">Close navigation</span>
            </button>
          </div>
        )}
        <ul className="nav-list">
          {pages.map((c) => (
            <NavItem
              key={c.id}
              cap={c}
              current={!onAccount && currentId === c.id}
              rail={rail}
              onNavigate={() => {
                if (drawerOpen) closeDrawer(false);
              }}
            />
          ))}
        </ul>
        <div className="sidebar-foot">
          <p className="rail-note">Read-only build. Nothing here changes your system.</p>
          <button
            type="button"
            className="ghost icon-btn sidebar-toggle"
            onClick={() => update('sidebar', collapsed ? 'expanded' : 'collapsed')}
          >
            <Icon name="sidebar" />
            <span className="btn-text">{collapsed ? 'Expand sidebar' : 'Collapse sidebar'}</span>
          </button>
        </div>
      </nav>
      <div className="workspace" inert={drawerOpen}>
        <main id="main" tabIndex={-1}>
          {children}
        </main>
        <footer className="foot">Homelab Control {version}</footer>
      </div>
    </div>
  );
}
