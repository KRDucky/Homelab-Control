// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useState } from 'react';

/*
 * Appearance and layout preferences (HCIS step 3; docs/ui/06 §6.5, ADR 0010).
 *
 * The theme is the only appearance setting. Density, interface font, text size, transparency,
 * motion and the sidebar are independent layout/accessibility preferences: switching theme never
 * changes them, and none of them changes permissions or behaviour.
 *
 * Storage is per browser (localStorage) until the server-side settings store exists (docs/ui/10,
 * phase U5, needs M2 approval). Stored values are untrusted input: every field is checked against
 * an allow-list and anything else falls back to the default. Preferences are applied as data-hc-*
 * attributes on <html>, which tokens.css and shell.css map to CSS; no inline styles (CSP).
 */

export const THEMES = ['system', 'midnight', 'daybreak', 'graphite', 'oled'] as const;
export const DENSITIES = ['compact', 'standard', 'spacious'] as const;
export const FONTS = ['inter', 'system'] as const;
export const TEXT_SCALES = ['90', '100', '112', '125'] as const;
export const TRANSPARENCIES = ['full', 'reduced'] as const;
export const MOTIONS = ['system', 'full', 'reduced', 'none'] as const;
export const SIDEBARS = ['expanded', 'collapsed'] as const;

export interface Preferences {
  theme: (typeof THEMES)[number];
  density: (typeof DENSITIES)[number];
  font: (typeof FONTS)[number];
  textScale: (typeof TEXT_SCALES)[number];
  transparency: (typeof TRANSPARENCIES)[number];
  motion: (typeof MOTIONS)[number];
  sidebar: (typeof SIDEBARS)[number];
}

export const DEFAULT_PREFERENCES: Preferences = {
  theme: 'system',
  density: 'standard',
  font: 'inter',
  textScale: '100',
  transparency: 'full',
  motion: 'system',
  sidebar: 'expanded',
};

export const STORAGE_KEY = 'hc.preferences.v1';

const allowed: { [K in keyof Preferences]: readonly string[] } = {
  theme: THEMES,
  density: DENSITIES,
  font: FONTS,
  textScale: TEXT_SCALES,
  transparency: TRANSPARENCIES,
  motion: MOTIONS,
  sidebar: SIDEBARS,
};

/** Validates untrusted input (stored JSON). Unknown keys are ignored; bad values get the default. */
export function parsePreferences(raw: string | null): Preferences {
  const out: Preferences = { ...DEFAULT_PREFERENCES };
  if (!raw) return out;
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return out;
  }
  if (!data || typeof data !== 'object' || Array.isArray(data)) return out;
  const rec = data as Record<string, unknown>;
  if (rec.schemaVersion !== 1) return out;
  for (const key of Object.keys(allowed) as (keyof Preferences)[]) {
    const v = Object.prototype.hasOwnProperty.call(rec, key) ? rec[key] : undefined;
    if (typeof v === 'string' && allowed[key].includes(v)) {
      (out as unknown as Record<string, string>)[key] = v;
    }
  }
  return out;
}

export function loadPreferences(): Preferences {
  try {
    return parsePreferences(window.localStorage.getItem(STORAGE_KEY));
  } catch {
    return { ...DEFAULT_PREFERENCES }; // storage blocked (private mode, policy)
  }
}

export function savePreferences(p: Preferences): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ schemaVersion: 1, ...p }));
  } catch {
    // Storage blocked or full: the preference still applies for this page view.
  }
}

/** Sets the data-hc-* attributes on <html>. "system" theme removes the attribute (OS preference). */
export function applyPreferences(p: Preferences, root: HTMLElement = document.documentElement): void {
  if (p.theme === 'system') root.removeAttribute('data-hc-theme');
  else root.setAttribute('data-hc-theme', p.theme);
  root.setAttribute('data-hc-density', p.density);
  root.setAttribute('data-hc-font', p.font);
  root.setAttribute('data-hc-text-scale', p.textScale);
  root.setAttribute('data-hc-transparency', p.transparency);
  root.setAttribute('data-hc-motion', p.motion);
}

/** Current preferences, a setter that persists and applies them, and a reset. Syncs across tabs. */
export function usePreferences() {
  const [prefs, setPrefs] = useState<Preferences>(loadPreferences);

  useEffect(() => {
    applyPreferences(prefs);
  }, [prefs]);

  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY) setPrefs(parsePreferences(e.newValue));
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);

  const update = useCallback(<K extends keyof Preferences>(key: K, value: Preferences[K]) => {
    setPrefs((p) => {
      const next = { ...p, [key]: value };
      savePreferences(next);
      return next;
    });
  }, []);

  const reset = useCallback(() => {
    savePreferences(DEFAULT_PREFERENCES);
    setPrefs({ ...DEFAULT_PREFERENCES });
  }, []);

  return { prefs, update, reset };
}
