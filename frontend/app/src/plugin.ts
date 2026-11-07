// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: MIT

/*
 * Declarative read-only plugin manifest (`.hlplugin`) for the M3 plugin SDK first checkpoint.
 *
 * A plugin manifest is ONE JSON document of metadata and an allow-list of known contribution IDs.
 * There is no archive, image, font, CSS, script or binary payload, and no route, scope, broker
 * operation or helper process. Contribution IDs map to already-compiled React in the app; unknown
 * IDs are refused here and never mount UI. This is the same safety idea as `theme.ts` strictParse:
 * a dedicated scanner (not a JSON.parse reviver) rejects duplicate decoded keys, prototype-polluting
 * keys, trailing data, surrogates and oversized documents before any allow-list validation.
 *
 * This file is the single source of parser rules for both the app and the SDK: `scripts/build-plugin-sdk.mjs`
 * generates `sdk/plugin/runtime.mjs` from it, the same way the TDK shares `sdk/theme/runtime.mjs`.
 */

const ID = /^[a-z0-9]+(?:\.[a-z0-9-]+){1,4}$/;
const VERSION = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const LICENSE = /^[a-zA-Z0-9.+-]{2,40}$/;
// eslint-disable-next-line no-control-regex -- reject untrusted metadata control characters
const UNSAFE_TEXT = /[<>\u0000-\u001f\u007f\u202a-\u202e\u2066-\u2069]/;

/** Known contribution IDs (this checkpoint). Each one is mapped to already-compiled React UI. */
export const CONTRIBUTION_IDS = ['overview.sample-card'] as const;
export type ContributionId = (typeof CONTRIBUTION_IDS)[number];

export interface PluginDocument {
  schemaVersion: 1;
  id: string;
  name: string;
  version: string;
  license: string;
  contributions: ContributionId[];
}

/** Browser-local enabled-contribution store. A new key: never reuses `hc.preferences.v1`. */
export const PLUGIN_STORAGE = 'hc.plugins.v1';

/** In-tree labelled sample, compiled into the app. Mirrors `plugins/sample-readonly/manifest.hlplugin`. */
export const SAMPLE_PLUGIN: PluginDocument = Object.freeze({
  schemaVersion: 1,
  id: 'dev.homelab-control.sample-readonly',
  name: 'Read-only sample plugin',
  version: '1.0.0',
  license: 'MIT',
  contributions: ['overview.sample-card'] as ContributionId[],
});

/** Strict JSON scanner: reject duplicate decoded keys, prototype keys, excess work and surrogates. */
export function strictParse(text: string): unknown {
  if (new TextEncoder().encode(text).length > 65536) throw new Error('Plugin exceeds 64 KiB.');
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
    if (++count > 2000 || depth > 8) throw new Error('Plugin is too complex.');
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

function isObject(x: unknown): x is Record<string, unknown> {
  return !!x && typeof x === 'object' && !Array.isArray(x);
}

/** Validates a `.hlplugin` document. Unknown keys, unknown contribution IDs and payloads are refused. */
export function parsePlugin(text: string): PluginDocument {
  const d = strictParse(text);
  if (!isObject(d)) throw new Error('Expected a plugin object.');
  const allowed = ['schemaVersion', 'id', 'name', 'version', 'license', 'contributions'];
  for (const key of Object.keys(d))
    if (!allowed.includes(key))
      throw new Error(
        `Unknown plugin key: ${key}. Plugins are declarative metadata; there is no script or binary payload.`,
      );
  if (d.schemaVersion !== 1) throw new Error('Unsupported plugin schema version.');
  if (typeof d.id !== 'string' || !ID.test(d.id) || d.id.length > 100)
    throw new Error('id must be a reverse-DNS identifier.');
  if (typeof d.name !== 'string' || !d.name.trim() || d.name.length > 80 || UNSAFE_TEXT.test(d.name))
    throw new Error('name must be plain text of 1–80 characters.');
  if (typeof d.version !== 'string' || !VERSION.test(d.version))
    throw new Error('version must be major.minor.patch.');
  if (typeof d.license !== 'string' || !LICENSE.test(d.license))
    throw new Error('license must be a simple SPDX identifier.');
  if (!Array.isArray(d.contributions) || d.contributions.length === 0)
    throw new Error('contributions must be a non-empty array of known contribution IDs.');
  if (new Set(d.contributions).size !== d.contributions.length)
    throw new Error('Duplicate contribution IDs.');
  for (const id of d.contributions) {
    if (typeof id !== 'string' || !(CONTRIBUTION_IDS as readonly string[]).includes(id))
      throw new Error(`Unknown contribution ID: ${String(id)}.`);
  }
  return d as unknown as PluginDocument;
}

// --- Browser-only persistence below (not part of the SDK runtime). ---

/** Reads the enabled-contribution set, failing closed to empty on any malformed or oversized data. */
export function readEnabled(): Set<ContributionId> {
  try {
    const raw = window.localStorage.getItem(PLUGIN_STORAGE);
    if (!raw || raw.length > 65536) return new Set();
    const d = strictParse(raw);
    if (!isObject(d) || d.schemaVersion !== 1) return new Set();
    const list = Array.isArray(d.enabled) ? d.enabled : [];
    const out = new Set<ContributionId>();
    for (const id of list) {
      if (typeof id === 'string' && (CONTRIBUTION_IDS as readonly string[]).includes(id))
        out.add(id as ContributionId);
    }
    return out;
  } catch {
    return new Set();
  }
}

export function isContributionEnabled(id: ContributionId): boolean {
  return readEnabled().has(id);
}

export function setContributionEnabled(id: ContributionId, enabled: boolean): void {
  const ids = readEnabled();
  if (enabled) ids.add(id);
  else ids.delete(id);
  try {
    window.localStorage.setItem(PLUGIN_STORAGE, JSON.stringify({ schemaVersion: 1, enabled: [...ids] }));
  } catch {
    // Storage blocked or full: the toggle still applies for this page view.
  }
  window.dispatchEvent(new Event('hc-plugins-change'));
}

/** Loads a plugin (enable its contributions). The document is re-validated through the strict parser. */
export function loadPlugin(doc: PluginDocument): void {
  const d = parsePlugin(JSON.stringify(doc));
  for (const id of d.contributions) setContributionEnabled(id, true);
}

/** Unloads a plugin (disable its contributions). */
export function unloadPlugin(doc: PluginDocument): void {
  const d = parsePlugin(JSON.stringify(doc));
  for (const id of d.contributions) setContributionEnabled(id, false);
}
