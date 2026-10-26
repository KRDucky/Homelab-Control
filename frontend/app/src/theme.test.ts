// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { beforeEach, describe, expect, it } from 'vitest';
import {
  ACTIVE_THEME,
  BASES,
  COLOR_KEYS,
  activeTheme,
  applyTheme,
  newTheme,
  parseTheme,
  resolvedColors,
  saveThemes,
  loadThemes,
  loadThemeVersions,
  strictParse,
  themeIssues,
} from './theme';

beforeEach(() => {
  localStorage.clear();
  applyTheme(null);
});
describe('theme input boundary', () => {
  it('rejects duplicate decoded keys, polluted keys and invalid grammar', () => {
    for (const s of [
      '{"x":1,"x":2}',
      '{"x":1,"\\u0078":2}',
      '{"__proto__":{}}',
      '{"a":{"constructor":1}}',
      '{"x":01}',
      '{"x":1e999}',
      '{"x":"\\ud800"}',
      '{"x":1,}',
      '[1,]',
      'true false',
    ])
      expect(() => strictParse(s), s).toThrow();
  });
  it('enforces byte, nesting and item budgets', () => {
    expect(() => strictParse(' '.repeat(65537))).toThrow();
    expect(() => strictParse('['.repeat(10) + '0' + ']'.repeat(10))).toThrow();
    expect(() => strictParse(JSON.stringify(Array(2001).fill(0)))).toThrow();
    expect(strictParse('{"ok":[true,null,"😀"]}')).toEqual({ ok: [true, null, '😀'] });
  });
  it('rejects code, preference fields, markup, bidi, old drafts and alias overrides', () => {
    const d = newTheme('midnight');
    for (const field of ['density', 'navigation', 'typography', 'motion', 'extends', 'modes'])
      expect(() => parseTheme(JSON.stringify({ ...d, [field]: {} }))).toThrow();
    for (const value of ['url(https://example.org)', 'var(--x)', 'red', '#123', 'expression(x)'])
      expect(() => parseTheme(JSON.stringify({ ...d, colors: { 'text-primary': value } }))).toThrow();
    for (const name of ['<script>', 'x\u202eexe', ''])
      expect(() => parseTheme(JSON.stringify({ ...d, name }))).toThrow();
    expect(() => parseTheme(JSON.stringify({ ...d, colors: { ink: '#ffffff' } }))).toThrow();
  });
  it('round trips all built-in bases and reports their current contrast', () => {
    for (const base of BASES) {
      const d = parseTheme(JSON.stringify(newTheme(base)));
      expect(d.base).toBe(base);
      expect(COLOR_KEYS.length).toBe(57);
      expect(themeIssues(d), base).toEqual([]);
    }
  });
  it('blocks unreadable and transparent data surfaces', () => {
    const d = { ...newTheme('midnight'), colors: { 'text-primary': '#111111', 'surface-raised': '#111111' } };
    expect(themeIssues(d).length).toBeGreaterThan(0);
    expect(
      themeIssues({ ...d, colors: { 'surface-raised': '#11111100' } }).some((i) =>
        i.message.includes('opaque'),
      ),
    ).toBe(true);
  });
  it('persists documents but fails closed on tampered active data', () => {
    const d = newTheme('midnight');
    saveThemes([d]);
    expect(loadThemes()).toEqual([d]);
    localStorage.setItem(ACTIVE_THEME, d.id);
    expect(activeTheme()?.id).toBe(d.id);
    saveThemes([{ ...d, colors: { 'text-primary': '#111111', 'surface-raised': '#111111' } }]);
    expect(activeTheme()).toBeNull();
  });
  it('only sets semantic colour properties and removes them on reset', () => {
    const d = newTheme('midnight');
    applyTheme(d);
    expect(document.documentElement.style.getPropertyValue('--hc-color-surface-base')).toBe(
      resolvedColors(d)['surface-base'],
    );
    expect(document.documentElement.style.getPropertyValue('--hc-color-ink')).toBe('');
    expect(document.documentElement.style.getPropertyValue('--hc-font-ui')).toBe('');
    applyTheme(null);
    expect(document.documentElement.style.getPropertyValue('--hc-color-surface-base')).toBe('');
  });
});

it('keeps ten recoverable revisions ', () => {
  const d = newTheme('midnight');
  saveThemes([d]);
  for (let n = 1; n <= 12; n++) saveThemes([{ ...d, version: `1.0.${n}` }]);
  expect(loadThemeVersions(d.id)).toHaveLength(10);
  expect(loadThemeVersions(d.id)[0]?.version).toBe('1.0.11');
  expect(loadThemes()[0]?.version).toBe('1.0.12');
});
