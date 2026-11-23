// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react';
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

/** Keep navigation organized without coupling page visibility to menu structure.
 * Unknown future capabilities remain discoverable in Other modules.
 */
const NAV_SECTIONS: { title: string; ids: readonly string[] }[] = [
  { title: 'Dashboard', ids: ['overview'] },
  { title: 'System', ids: ['services', 'hardware', 'host_settings', 'packages'] },
  { title: 'Storage & files', ids: ['storage', 'mergerfs', 'zfs', 'lvm', 'f2fs', 'files'] },
  { title: 'Network & applications', ids: ['traefik'] },
  { title: 'Administration', ids: ['audit'] },
];

export function groupNavigation(pages: Capability[]): { title: string; pages: Capability[] }[] {
  const used = new Set<string>();
  const groups = NAV_SECTIONS.map(({ title, ids }) => {
    const found = ids.flatMap((id) => pages.filter((page) => page.id === id && !used.has(page.id)));
    found.forEach((page) => used.add(page.id));
    return { title, pages: found };
  }).filter((group) => group.pages.length > 0);
  const other = pages.filter((page) => !used.has(page.id));
  if (other.length) groups.push({ title: 'Other modules', pages: other });
  return groups;
}

/**
 * The application frame (HCIS AppShell): glass header, capability-driven sidebar that collapses to an
 * icon rail, a navigation drawer on phones, and the Appearance menu. Layout only: it never changes
 * what a page shows or what the user may do.
 */
export function Shell({
  pages,
  currentId,
  title,
  username,
  accountPath,
  onAccount,
  helperReason,
  onSignOut,
  version,
  canOperate = false,
  children,
}: {
  pages: Capability[];
  currentId: string | undefined;
  /** Name of the page being shown; becomes the document title so each page is distinguishable. */
  title: string;
  username: string;
  accountPath: string;
  onAccount: boolean;
  helperReason: string | undefined;
  onSignOut: () => void;
  version: string;
  /** The account holds services.operate or services.configure, so the interface is not read-only. */
  canOperate?: boolean;
  children: ReactNode;
}) {
  const { prefs, update, reset } = usePreferences();
  useLayoutEffect(() => {
    document.title = title ? `${title} · Homelab Control` : 'Homelab Control';
  }, [title]);
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
          {!canOperate && <span className="readonly-badge">Read-only</span>}
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
        <div className="nav-groups">
          {groupNavigation(pages).map((group) => (
            <section className="nav-group" key={group.title} aria-label={group.title}>
              <h2 className="nav-group-title">{group.title}</h2>
              <ul className="nav-list">
                {group.pages.map((c) => (
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
            </section>
          ))}
        </div>
        <div className="sidebar-foot">
          <p className="rail-note">
            {canOperate
              ? 'You can change services (start, stop, restart, or their setting at boot, as your permissions allow). Each change is reviewed, confirmed and audited; everything else is read-only.'
              : 'Read-only for your account. Nothing here changes your system.'}
          </p>
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
        {groupNavigation(pages)
          .filter((group) => group.pages.some((page) => page.id === currentId) && group.title !== 'Dashboard')
          .map((group) => {
            const related = group.pages.filter(
              (page) => page.status === 'available' || page.status === 'degraded',
            );
            return related.length > 1 ? (
              <nav key={group.title} className="module-tabs" aria-label={group.title + ' modules'}>
                <span className="module-tabs-label">{group.title}</span>
                {related.map((page) => (
                  <a
                    key={page.id}
                    href={page.path}
                    onClick={linkHandler(page.path)}
                    aria-current={page.id === currentId ? 'page' : undefined}
                  >
                    {page.label}
                  </a>
                ))}
              </nav>
            ) : null;
          })}
        <main id="main" tabIndex={-1}>
          {children}
        </main>
        <footer className="foot">Homelab Control {version}</footer>
      </div>
    </div>
  );
}
