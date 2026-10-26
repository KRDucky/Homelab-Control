// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: MIT

import palettes from './theme-palettes.json';

export type BaseTheme = keyof typeof palettes;
export interface ThemeDocument {
  schemaVersion: 1;
  id: string;
  name: string;
  version: string;
  license: string;
  base: BaseTheme;
  colors: Record<string, string>;
}
export const BASES = Object.keys(palettes) as BaseTheme[];
export const COLOR_KEYS = Object.keys(palettes.midnight).filter(
  (k) =>
    ![
      'ink',
      'panel',
      'panel-raised',
      'rule',
      'text',
      'muted',
      'signal',
      'signal-ink',
      'ok',
      'warn',
      'crit',
      'focus',
    ].includes(k),
);
export const THEME_STORAGE = 'hc.themes.v1';
export const ACTIVE_THEME = 'hc.theme.active.v1';
export const REQUIREMENTS =
  'All custom themes must follow HCIS requirements for responsive layouts, accessibility, readable contrast, component consistency, and security. Themes may customize appearance but cannot alter functionality, permissions, safety controls, or application layout.';
const HEX = /^#[\da-f]{6}([\da-f]{2})?$/i;
// eslint-disable-next-line no-control-regex -- reject untrusted metadata control characters
const UNSAFE_TEXT = /[<>\u0000-\u001f\u007f\u202a-\u202e\u2066-\u2069]/;

/** Strict JSON scanner: reject duplicate decoded keys, prototype keys, excess work and surrogates. */
export function strictParse(text: string, library = false): unknown {
  if (new TextEncoder().encode(text).length > (library ? 1400000 : 65536))
    throw new Error('Theme exceeds 64 KiB.');
  let i = 0,
    count = 0;
  const fail = (): never => {
    throw new Error(`Invalid JSON near character ${i + 1}.`);
  };
  const ws = () => {
    while (/[\t\r\n ]/.test(text[i] ?? '') && i < text.length) i++;
  };
  function string(): string {
    const start = i++;
    while (i < text.length) {
      const ch = text[i++];
      if (ch === '\\') {
        i++;
        continue;
      }
      if (ch === '"') {
        const s: string = JSON.parse(text.slice(start, i));
        if (s.length > 280) throw new Error('String exceeds 280 characters.');
        for (let j = 0; j < s.length; j++) {
          const code = s.charCodeAt(j);
          if (code >= 0xd800 && code <= 0xdbff) {
            const low = s.charCodeAt(++j);
            if (!(low >= 0xdc00 && low <= 0xdfff)) throw new Error('Invalid Unicode surrogate.');
          } else if (code >= 0xdc00 && code <= 0xdfff) throw new Error('Invalid Unicode surrogate.');
        }
        return s;
      }
    }
    return fail();
  }
  function value(depth: number): unknown {
    ws();
    if (++count > (library ? 40000 : 2000) || depth > 8) throw new Error('Theme is too complex.');
    if (text[i] === '"') return string();
    if (text[i] === '{') {
      i++;
      ws();
      const out: Record<string, unknown> = Object.create(null);
      if (text[i] === '}') {
        i++;
        return out;
      }
      while (i < text.length) {
        ws();
        if (text[i] !== '"') return fail();
        const key = string();
        if (['__proto__', 'constructor', 'prototype'].includes(key))
          throw new Error(`Forbidden key: ${key}.`);
        if (Object.hasOwn(out, key)) throw new Error(`Duplicate key: ${key}.`);
        ws();
        if (text[i++] !== ':') return fail();
        out[key] = value(depth + 1);
        ws();
        const end = text[i++];
        if (end === '}') return out;
        if (end !== ',') return fail();
      }
    }
    if (text[i] === '[') {
      i++;
      ws();
      const out: unknown[] = [];
      if (text[i] === ']') {
        i++;
        return out;
      }
      while (i < text.length) {
        out.push(value(depth + 1));
        ws();
        const end = text[i++];
        if (end === ']') return out;
        if (end !== ',') return fail();
      }
    }
    const match = /^(true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(text.slice(i));
    if (match) {
      i += match[0].length;
      const out: unknown = JSON.parse(match[0]);
      if (typeof out === 'number' && !Number.isFinite(out)) return fail();
      return out;
    }
    return fail();
  }
  const result = value(0);
  ws();
  if (i !== text.length) return fail();
  return result;
}

function object(x: unknown): x is Record<string, unknown> {
  return !!x && typeof x === 'object' && !Array.isArray(x);
}
export function parseTheme(text: string): ThemeDocument {
  const d = strictParse(text);
  if (!object(d)) throw new Error('Expected a theme object.');
  const allowed = ['schemaVersion', 'id', 'name', 'version', 'license', 'base', 'colors'];
  for (const key of Object.keys(d))
    if (!allowed.includes(key))
      throw new Error(`Unknown theme key: ${key}. Layout, typography and motion are separate preferences.`);
  if (d.schemaVersion !== 1) throw new Error('Unsupported theme schema version.');
  if (typeof d.id !== 'string' || !/^[a-z0-9]+(?:\.[a-z0-9-]+){1,4}$/.test(d.id) || d.id.length > 100)
    throw new Error('id must be a reverse-DNS identifier.');
  if (typeof d.name !== 'string' || !d.name.trim() || d.name.length > 80 || UNSAFE_TEXT.test(d.name))
    throw new Error('name must be plain text of 1–80 characters.');
  if (typeof d.version !== 'string' || !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(d.version))
    throw new Error('version must be major.minor.patch.');
  if (typeof d.license !== 'string' || !/^[a-zA-Z0-9.+-]{2,40}$/.test(d.license))
    throw new Error('license must be a simple SPDX identifier.');
  if (!BASES.includes(d.base as BaseTheme))
    throw new Error('base must be midnight, daybreak, graphite or oled.');
  if (!object(d.colors)) throw new Error('colors must be an object of HCIS colour roles.');
  for (const [key, v] of Object.entries(d.colors)) {
    if (!COLOR_KEYS.includes(key)) throw new Error(`Unknown colour role: ${key}.`);
    if (typeof v !== 'string' || !HEX.test(v)) throw new Error(`${key}: expected #RRGGBB or #RRGGBBAA.`);
  }
  return d as unknown as ThemeDocument;
}

export function resolvedColors(d: ThemeDocument): Record<string, string> {
  return Object.fromEntries(
    COLOR_KEYS.map((key) => [
      key,
      d.colors[key] ?? (palettes[d.base] as Record<string, string>)[key] ?? '#000000',
    ]),
  );
}
function rgba(hex: string): number[] {
  return [
    parseInt(hex.slice(1, 3), 16),
    parseInt(hex.slice(3, 5), 16),
    parseInt(hex.slice(5, 7), 16),
    hex.length === 9 ? parseInt(hex.slice(7, 9), 16) / 255 : 1,
  ];
}
function composite(fg: number[], bg: number[]): number[] {
  const a = fg[3] ?? 1;
  return [0, 1, 2].map((i) => (fg[i] ?? 0) * a + (bg[i] ?? 0) * (1 - a)).concat(1);
}
function luminance(rgb: number[]): number {
  return rgb
    .slice(0, 3)
    .map((n) => {
      const s = n / 255;
      return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
    })
    .reduce((sum, n, i) => sum + n * ([0.2126, 0.7152, 0.0722][i] ?? 0), 0);
}
export function contrast(fg: string, bg: string): number {
  const b = composite(rgba(bg), [0, 0, 0, 1]),
    f = composite(rgba(fg), b);
  const a = luminance(f),
    z = luminance(b);
  return (Math.max(a, z) + 0.05) / (Math.min(a, z) + 0.05);
}
export interface ThemeIssue {
  path: string;
  message: string;
}
export function themeIssues(d: ThemeDocument): ThemeIssue[] {
  const c = resolvedColors(d),
    issues: ThemeIssue[] = [];
  const pairs: [string, string, number][] = [];
  for (const surface of [
    'surface-base',
    'surface-raised',
    'surface-overlay',
    'surface-sidebar',
    'surface-sunken',
    'surface-hover',
    'surface-selected',
    'input-bg',
    'glass-fallback',
  ]) {
    for (const text of ['text-primary', 'text-secondary', 'text-link']) pairs.push([text, surface, 4.5]);
    pairs.push(['focus-ring', surface, 3]);
    if (['surface-base', 'surface-raised', 'surface-overlay', 'input-bg'].includes(surface))
      pairs.push(['border-strong', surface, 3]);
  }
  pairs.push(['accent-contrast', 'accent-base', 4.5], ['accent-contrast', 'accent-hover', 4.5]);
  for (const state of ['ok', 'warn', 'crit', 'info']) {
    pairs.push([`status-${state}-text`, `status-${state}-subtle`, 4.5]);
    for (const surface of ['surface-base', 'surface-raised', 'surface-overlay'])
      pairs.push([`status-${state}-text`, surface, 4.5]);
  }
  for (const surface of ['surface-base', 'surface-raised', 'surface-overlay']) {
    pairs.push(['status-unknown-text', surface, 4.5], ['status-notice', surface, 4.5]);
  }
  for (const key of [
    'surface-base',
    'surface-raised',
    'surface-overlay',
    'surface-sidebar',
    'input-bg',
    'glass-fallback',
  ]) {
    if (c[key] && rgba(c[key] ?? '#000000')[3] !== 1)
      issues.push({ path: key, message: 'Data and fallback surfaces must be opaque.' });
  }
  for (const [fg, bg, min] of pairs) {
    if (!c[fg] || !c[bg]) continue;
    // Check composited alpha against the underlying raised surface.
    const background = composite(rgba(c[bg] ?? '#000000'), rgba(c['surface-raised'] ?? '#000000'));
    const a = luminance(composite(rgba(c[fg] ?? '#ffffff'), background)),
      b = luminance(background);
    const ratio = (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
    if (ratio < min)
      issues.push({ path: fg, message: `${fg} on ${bg}: ${ratio.toFixed(2)}:1; needs ${min}:1.` });
  }
  return issues;
}

export function applyTheme(d: ThemeDocument | null, root: HTMLElement = document.documentElement) {
  for (const key of COLOR_KEYS) root.style.removeProperty(`--hc-color-${key}`);
  if (d) {
    root.setAttribute('data-hc-theme', d.base);
    for (const [key, color] of Object.entries(resolvedColors(d)))
      root.style.setProperty(`--hc-color-${key}`, color);
  }
}
interface ThemeLibrary {
  themes: ThemeDocument[];
  versions: ThemeDocument[];
}
function readLibrary(): ThemeLibrary {
  try {
    const raw = localStorage.getItem(THEME_STORAGE);
    if (!raw || raw.length > 1400000) return { themes: [], versions: [] };
    const data = strictParse(raw, true);
    const list = Array.isArray(data) ? data : object(data) && Array.isArray(data.themes) ? data.themes : [];
    const versions = object(data) && Array.isArray(data.versions) ? data.versions : [];
    function valid(items: unknown[], limit: number) {
      return items.slice(0, limit).flatMap((item) => {
        try {
          return [parseTheme(JSON.stringify(item))];
        } catch {
          return [];
        }
      });
    }
    return { themes: valid(list, 20), versions: valid(versions, 200) };
  } catch {
    return { themes: [], versions: [] };
  }
}
export function loadThemes(): ThemeDocument[] {
  return readLibrary().themes;
}
export function loadThemeVersions(id: string): ThemeDocument[] {
  return readLibrary()
    .versions.filter((t) => t.id === id)
    .slice(-10)
    .reverse();
}
export function saveThemes(themes: ThemeDocument[]): void {
  if (themes.length > 20) throw new Error('Keep at most 20 custom themes. Export one before removing it.');
  const previous = readLibrary();
  let versions = previous.versions.filter((t) => themes.some((next) => next.id === t.id));
  for (const old of previous.themes) {
    const next = themes.find((t) => t.id === old.id);
    if (next && JSON.stringify(old) !== JSON.stringify(next)) versions.push(old);
  }
  versions = versions.filter((t, i) => versions.slice(i + 1).filter((v) => v.id === t.id).length < 10);
  const raw = JSON.stringify({ themes: themes.map((d) => parseTheme(JSON.stringify(d))), versions });
  if (raw.length > 1400000)
    throw new Error('Theme library is full. Export and remove a theme before saving.');
  // One atomic storage write preserves both the library and saved versions on quota failure.
  localStorage.setItem(THEME_STORAGE, raw);
  window.dispatchEvent(new Event('hc-themes-change'));
}
export function activeTheme(): ThemeDocument | null {
  try {
    const id = localStorage.getItem(ACTIVE_THEME);
    return loadThemes().find((d) => d.id === id && themeIssues(d).length === 0) ?? null;
  } catch {
    return null;
  }
}
export function activateTheme(d: ThemeDocument | null) {
  if (d && themeIssues(d).length) throw new Error('Fix contrast issues before activation.');
  if (d) localStorage.setItem(ACTIVE_THEME, d.id);
  else localStorage.removeItem(ACTIVE_THEME);
  window.dispatchEvent(new Event('hc-themes-change'));
}
export function exportTheme(d: ThemeDocument) {
  const blob = new Blob([JSON.stringify(d, null, 2) + '\n'], {
    type: 'application/vnd.homelab-control.theme+json',
  });
  const url = URL.createObjectURL(blob),
    a = document.createElement('a');
  a.href = url;
  a.download = `${d.id}.hltheme`;
  a.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}
export function newTheme(base: BaseTheme): ThemeDocument {
  return {
    schemaVersion: 1,
    id: 'local.my-theme',
    name: 'My theme',
    version: '1.0.0',
    license: 'MIT',
    base,
    colors: {},
  };
}
