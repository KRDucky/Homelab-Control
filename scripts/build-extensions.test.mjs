// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Negative and positive tests for scripts/build-extensions.mjs. Each case copies the pinned extensions/ tree into
// a temporary directory, applies one change, and checks that generation fails closed (or succeeds unchanged).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { generate } from './build-extensions.mjs';

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const outFile = join(repo, 'frontend/app/src/extensions.generated.ts');
const sample = 'plugins/sample-readonly';
const card = `${sample}/ui/SampleOverviewCard.tsx`;

function copy(edit) {
  const dir = mkdtempSync(join(tmpdir(), 'hc-ext-'));
  cpSync(join(repo, 'extensions'), dir, { recursive: true });
  edit?.(dir);
  return dir;
}
const write = (dir, rel, text) => {
  mkdirSync(dirname(join(dir, rel)), { recursive: true });
  writeFileSync(join(dir, rel), text);
};
const read = (dir, rel) => readFileSync(join(dir, rel), 'utf8');
async function rejects(edit, pattern) {
  const dir = copy(edit);
  try {
    await assert.rejects(generate({ extensionsDir: dir, outFile }), pattern);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

test('the pinned import generates exactly the checked-in registry', async () => {
  assert.equal(await generate({ outFile }), readFileSync(outFile, 'utf8'));
});

test('--check fails on a drifted generated file and passes on the real one', () => {
  const run = () => spawnSync(process.execPath, [join(repo, 'scripts/build-extensions.mjs'), '--check'], { encoding: 'utf8' });
  assert.equal(run().status, 0);
  const original = readFileSync(outFile, 'utf8');
  try {
    writeFileSync(outFile, original.replace("'Read-only sample plugin'", "'Tampered name'"));
    const r = run();
    assert.equal(r.status, 1);
    assert.match(r.stderr, /is stale/);
  } finally {
    writeFileSync(outFile, original);
  }
});

test('unknown contribution IDs and manifest payloads are refused', async () => {
  const manifest = (dir) => JSON.parse(read(dir, `${sample}/manifest.hlplugin`));
  await rejects((d) => write(d, `${sample}/manifest.hlplugin`, JSON.stringify({ ...manifest(d), contributions: ['evil.card'] })), /Unknown contribution ID: evil\.card/);
  await rejects((d) => write(d, `${sample}/manifest.hlplugin`, JSON.stringify({ ...manifest(d), script: 'x' })), /Unknown plugin key: script/);
  await rejects((d) => write(d, `${sample}/manifest.hlplugin`, '{"schemaVersion":1,"schemaVersion":1}'), /Duplicate key/);
  await rejects((d) => rmSync(join(d, `${sample}/manifest.hlplugin`)), /has no manifest/);
});

test('extra file kinds, package manifests and symbolic links are refused', async () => {
  for (const rel of ['package.json', 'go.mod', `${sample}/index.js`, `${sample}/ui/helper.ts`, `${sample}/ui/style.css`, 'scripts/run.sh', `${sample}/ui/lowercase.tsx`])
    await rejects((d) => write(d, rel, 'x'), /unsupported file under extensions/);
  await rejects((d) => symlinkSync('/etc/passwd', join(d, `${sample}/README-link.md`)), /symbolic links/);
});

test('a second plugin cannot claim an existing contribution and orphan components are refused', async () => {
  await rejects((d) => cpSync(join(d, sample), join(d, 'plugins/copy'), { recursive: true }), /duplicate plugin id|claimed by both/);
  await rejects((d) => write(d, `${sample}/ui/ExtraCard.tsx`, 'export function ExtraCard() { return null; }\n'), /not referenced/);
  await rejects((d) => rmSync(join(d, card)), /needs plugins\/sample-readonly\/ui\/SampleOverviewCard\.tsx/);
});

test('components are limited to static presentational React', async () => {
  const body = (d, from, to) => write(d, card, read(d, card).replace(from, to));
  const cases = [
    [(d) => body(d, 'return (', 'fetch("/api/v1/host/overview");\n  return ('), /'fetch' is not allowed/],
    [(d) => body(d, 'return (', 'eval("1");\n  return ('), /'eval' is not allowed/],
    [(d) => body(d, 'return (', 'void import("./x");\n  return ('), /dynamic import\(\)/],
    [(d) => body(d, 'return (', 'localStorage.clear();\n  return ('), /'localStorage' is not allowed/],
    [(d) => body(d, 'return (', 'window.location.href = "/";\n  return ('), /'window' is not allowed/],
    [(d) => body(d, '<p>', '<p dangerouslySetInnerHTML={{ __html: "<b>x</b>" }}>'), /dangerouslySetInnerHTML/],
    [(d) => body(d, "import type { ReactNode } from 'react';", "import { useEffect, type ReactNode } from 'react';"), /only `import type/],
    [(d) => body(d, "import type { ReactNode } from 'react';", "import type { ReactNode } from 'react';\nimport x from 'left-pad';"), /only `import type/],
    [(d) => body(d, 'export function SampleOverviewCard', 'export default function SampleOverviewCard'), /one `export function SampleOverviewCard`|only type imports/],
    [(d) => write(d, card, read(d, card) + '\nconst leaked = 1;\n'), /only type imports/],
    [(d) => body(d, 'return (', 'return ((('), /does not parse/],
    // Reflective routes to Function (Codex review of 9a322fb3).
    [(d) => body(d, 'return (', "[].filter.constructor('return window')();\n  return ("), /'constructor' is not allowed/],
    [(d) => body(d, 'return (', "(async () => {}).constructor('return 1');\n  return ("), /'constructor' is not allowed/],
    [(d) => body(d, 'return (', "const f = [].filter;\n  f['constructor']('return 1')();\n  return ("), /computed member access/],
    [(d) => body(d, 'return (', "const k = 'con' + 'structor';\n  ({} as Record<string, unknown>)[k];\n  return ("), /computed member access/],
    [(d) => body(d, 'return (', "const { ['con' + 'structor']: C } = [].filter;\n  void C;\n  return ("), /computed property names/],
    [(d) => body(d, 'return (', "const { 'constructor': C } = [].filter;\n  void C;\n  return ("), /string 'constructor'/],
    [(d) => body(d, 'return (', "const { constructor: C } = [].filter;\n  void C;\n  return ("), /'constructor' is not allowed/],
    [(d) => body(d, 'return (', "Reflect.get([].filter, 'x');\n  return ("), /'Reflect' is not allowed/],
    [(d) => body(d, 'return (', "Object.getPrototypeOf([].filter);\n  return ("), /'getPrototypeOf' is not allowed/],
    [(d) => body(d, 'return (', "[].filter.call([], () => true);\n  return ("), /'call' is not allowed/],
    [(d) => body(d, 'return (', "void `constructor`;\n  return ("), /string 'constructor'/],
  ];
  for (const [edit, pattern] of cases) await rejects(edit, pattern);
});
