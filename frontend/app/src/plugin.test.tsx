// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, describe, expect, it } from 'vitest';
import { act, cleanup, render, screen } from '@testing-library/react';
import {
  CONTRIBUTION_IDS,
  PLUGIN_STORAGE,
  isContributionEnabled,
  loadPlugin,
  parsePlugin,
  readEnabled,
  setContributionEnabled,
  strictParse,
  unloadPlugin,
} from './plugin';
import { PluginContributions } from './plugin-ui';
import { EXTENSION_COMPONENTS, EXTENSION_PLUGINS } from './extensions.generated';

// The sample now comes from the pinned Extensions import (extensions/plugins/sample-readonly).
const found = EXTENSION_PLUGINS.find((p) => p.id === 'dev.homelab-control.sample-readonly');
if (!found) throw new Error('sample plugin missing from the Extensions import');
const SAMPLE_PLUGIN = found;

afterEach(() => {
  cleanup();
  localStorage.clear();
});

const valid = JSON.stringify(SAMPLE_PLUGIN);

describe('plugin manifest runtime.container', () => {
  const rt = {
    supportedPlatforms: ['docker', 'podman', 'kubernetes'],
    deploymentMethods: ['compose', 'kubernetes-manifest'],
    identity: {
      configurable: true,
      method: 'env',
      uidVariable: 'PUID',
      gidVariable: 'PGID',
      umask: { method: 'env', variable: 'UMASK' },
      supplementalGroups: true,
    },
    privileged: false,
    capabilities: [],
    volumes: [{ name: 'config', target: '/config', access: 'rw' }],
  };
  const doc = (container: unknown) => JSON.stringify({ ...SAMPLE_PLUGIN, runtime: { container } });
  it('accepts a declared env identity and fixed identities', () => {
    expect(parsePlugin(doc(rt)).runtime?.container.identity.method).toBe('env');
    const fixed = {
      ...rt,
      identity: {
        configurable: false,
        method: 'fixed',
        fixedUid: 65532,
        fixedGid: 65532,
        umask: { method: 'none' },
        supplementalGroups: false,
      },
    };
    expect(parsePlugin(doc(fixed)).runtime?.container.identity.fixedUid).toBe(65532);
    // Manifests without runtime keep working unchanged.
    expect(parsePlugin(valid).runtime).toBeUndefined();
  });
  it('refuses inconsistent or unsafe declarations', () => {
    for (const bad of [
      { ...rt, payload: 'x' },
      { ...rt, deploymentMethods: ['compose'] },
      { ...rt, identity: { ...rt.identity, gidVariable: 'PUID' } },
      { ...rt, identity: { ...rt.identity, uidVariable: 'puid' } },
      { ...rt, identity: { ...rt.identity, method: 'magic' } },
      { ...rt, identity: { ...rt.identity, fixedUid: 1 } },
      { ...rt, identity: { ...rt.identity, umask: { method: 'env' } } },
      { ...rt, privileged: 'no' },
      { ...rt, volumes: [{ name: 'config', target: 'config', access: 'rw' }] },
      { ...rt, volumes: [rt.volumes[0], rt.volumes[0]] },
    ])
      expect(() => parsePlugin(doc(bad)), JSON.stringify(bad)).toThrow();
    expect(() =>
      parsePlugin(JSON.stringify({ ...SAMPLE_PLUGIN, runtime: { container: rt, hooks: [] } })),
    ).toThrow();
  });
});

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
  it('round trips the imported sample and exposes exactly one contribution ID', () => {
    expect(parsePlugin(valid)).toEqual(SAMPLE_PLUGIN);
    expect(CONTRIBUTION_IDS).toEqual(['overview.sample-card']);
  });
  it('imports exactly the sample and maps every known contribution ID to a component', () => {
    expect(EXTENSION_PLUGINS.map((p) => p.id)).toEqual(['dev.homelab-control.sample-readonly']);
    expect(Object.keys(EXTENSION_COMPONENTS)).toEqual([...CONTRIBUTION_IDS]);
    expect(Object.isFrozen(EXTENSION_PLUGINS) && Object.isFrozen(EXTENSION_COMPONENTS)).toBe(true);
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
