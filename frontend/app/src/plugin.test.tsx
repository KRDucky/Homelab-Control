// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, describe, expect, it } from 'vitest';
import { act, cleanup, render, screen } from '@testing-library/react';
import {
  CONTRIBUTION_IDS,
  PLUGIN_STORAGE,
  SAMPLE_PLUGIN,
  isContributionEnabled,
  loadPlugin,
  parsePlugin,
  readEnabled,
  setContributionEnabled,
  strictParse,
  unloadPlugin,
} from './plugin';
import { PluginContributions } from './plugin-ui';

afterEach(() => {
  cleanup();
  localStorage.clear();
});

const valid = JSON.stringify(SAMPLE_PLUGIN);

describe('plugin manifest input boundary', () => {
  it('rejects duplicate keys, polluted keys and invalid grammar', () => {
    for (const s of [
      '{"schemaVersion":1,"schemaVersion":1}',
      '{"__proto__":{}}',
      '{"a":{"constructor":1}}',
      '{"a":{"prototype":1}}',
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
  });
  it('refuses script/code/archive payloads, unknown contributions and extra fields', () => {
    const base = SAMPLE_PLUGIN;
    for (const field of ['script', 'code', 'payload', 'files', 'routes', 'scopes', 'binary'])
      expect(() => parsePlugin(JSON.stringify({ ...base, [field]: {} })), field).toThrow();
    expect(() => parsePlugin(JSON.stringify({ ...base, contributions: ['unknown.card'] }))).toThrow();
    expect(() => parsePlugin(JSON.stringify({ ...base, contributions: [] }))).toThrow();
    expect(() =>
      parsePlugin(
        JSON.stringify({ ...base, contributions: ['overview.sample-card', 'overview.sample-card'] }),
      ),
    ).toThrow();
    expect(() => parsePlugin(JSON.stringify({ ...base, name: '<script>alert(1)</script>' }))).toThrow();
    expect(() => parsePlugin(JSON.stringify({ ...base, extra: 'field' }))).toThrow();
    expect(() => parsePlugin(JSON.stringify({ ...base, schemaVersion: 2 }))).toThrow();
  });
  it('round trips the in-tree sample and exposes exactly one contribution ID', () => {
    expect(parsePlugin(valid)).toEqual(SAMPLE_PLUGIN);
    expect(CONTRIBUTION_IDS).toEqual(['overview.sample-card']);
  });
});

describe('browser-local load/unload', () => {
  it('is hidden until loaded, then unload removes it', () => {
    const { unmount } = render(<PluginContributions slot="overview" />);
    expect(screen.queryByText('Read-only sample plugin')).toBeNull();
    unmount();

    act(() => loadPlugin(SAMPLE_PLUGIN));
    render(<PluginContributions slot="overview" />);
    expect(screen.getByText('Read-only sample plugin')).toBeTruthy();
    expect(screen.getByText(/Sample plugin · example only/)).toBeTruthy();

    act(() => unloadPlugin(SAMPLE_PLUGIN));
    render(<PluginContributions slot="overview" />);
    expect(screen.queryByText('Read-only sample plugin')).toBeNull();
  });

  it('fails closed on tampered storage and never mounts unknown UI', () => {
    localStorage.setItem(
      PLUGIN_STORAGE,
      JSON.stringify({ schemaVersion: 1, enabled: ['overview.sample-card', 'evil.card', 42, null] }),
    );
    expect(readEnabled()).toEqual(new Set(['overview.sample-card']));
    render(<PluginContributions slot="overview" />);
    expect(screen.getByText('Read-only sample plugin')).toBeTruthy();
    // Unknown / non-string entries never mount UI.
    expect(screen.queryByText(/evil\.card/)).toBeNull();
  });

  it('uses its own storage key and leaves theme/preferences storage untouched', () => {
    expect(PLUGIN_STORAGE).toBe('hc.plugins.v1');
    act(() => setContributionEnabled('overview.sample-card', true));
    expect(isContributionEnabled('overview.sample-card')).toBe(true);
    expect(localStorage.getItem('hc.preferences.v1')).toBeNull();
    expect(localStorage.getItem('hc.themes.v1')).toBeNull();
  });
});
