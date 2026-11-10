// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: MIT
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { CONTRIBUTION_IDS, parsePlugin } from './runtime.mjs';
const dir = mkdtempSync(join(tmpdir(), 'hc-pdk-'));
const cli = new URL('./tdk.mjs', import.meta.url);
const run = (...args) => spawnSync(process.execPath, [cli.pathname, ...args], { encoding: 'utf8' });

const body = '"schemaVersion":1,"id":"example.readonly","name":"Example","version":"1.0.0","license":"MIT","contributions":["overview.sample-card"]';
const valid = { schemaVersion: 1, id: 'example.readonly', name: 'Example', version: '1.0.0', license: 'MIT', contributions: ['overview.sample-card'] };

test('standalone create, validate and overwrite protection', () => {
  const file = join(dir, 'example.hlplugin');
  assert.equal(run('create', file, 'example.readonly', 'Example').status, 0);
  assert.equal(run('create', file, 'example.readonly', 'Example').status, 1);
  assert.equal(run('validate', file).status, 0);
  assert.equal(run('validate', file, '--json').status, 0);
});

// The imported Extensions sample is validated by scripts/build-extensions.mjs --check.
test('the SDK example is a valid fixture', () => {
  const example = new URL('./examples/sample-readonly.hlplugin', import.meta.url).pathname;
  const doc = parsePlugin(readFileSync(example, 'utf8'));
  assert.deepEqual({ ...parsePlugin(JSON.stringify(doc)) }, { ...doc });
  assert.equal(run('validate', example).status, 0);
});

test('runtime.container declarations are validated strictly', () => {
  const example = new URL('./examples/container-identity.hlplugin', import.meta.url).pathname;
  assert.equal(run('validate', example).status, 0);
  const rt = { supportedPlatforms: ['docker'], deploymentMethods: ['compose'], identity: { configurable: true, method: 'runtime-user', umask: { method: 'none' }, supplementalGroups: true }, privileged: false, capabilities: [], volumes: [] };
  const doc = (container) => JSON.stringify({ ...valid, runtime: { container } });
  assert.doesNotThrow(() => parsePlugin(doc(rt)));
  for (const bad of [
    { ...rt, script: 'x' },
    { ...rt, supportedPlatforms: ['lxc'] },
    { ...rt, supportedPlatforms: ['kubernetes'] },
    { ...rt, identity: { ...rt.identity, uidVariable: 'PUID' } },
    { ...rt, identity: { ...rt.identity, configurable: false } },
    { ...rt, identity: { ...rt.identity, method: 'fixed', configurable: false, fixedUid: 0, fixedGid: 0 } },
    { ...rt, identity: { ...rt.identity, umask: { method: 'none', variable: 'UMASK' } } },
    { ...rt, capabilities: ['CAP_NET_ADMIN'] },
    { ...rt, volumes: [{ name: 'x', target: '/a/../b', access: 'rw' }] },
  ])
    assert.throws(() => parsePlugin(doc(bad)), JSON.stringify(bad));
  assert.throws(() => parsePlugin(JSON.stringify({ ...valid, runtime: { container: rt, extra: 1 } })));
});

test('malicious and malformed inputs fail closed in the SDK runtime and CLI', () => {
  const file = join(dir, 'invalid.hlplugin');
  const hostile = [
    // duplicate decoded key
    `{${body},"schemaVersion":1}`,
    // prototype-polluting keys
    `{${body},"__proto__":{"polluted":true}}`,
    `{${body},"constructor":{}}`,
    `{${body},"prototype":{}}`,
    // embedded script/code strings
    `{"schemaVersion":1,"id":"a.b","name":"<script>alert(1)</script>","version":"1.0.0","license":"MIT","contributions":["overview.sample-card"]}`,
    `{${body},"script":"require('child_process')"}`,
    `{${body},"code":"eval(1)"}`,
    // archive/binary payloads (extra fields are refused)
    `{${body},"payload":"UEsDBAoAAAAA"}`,
    `{${body},"files":[{"path":"../../etc"}]}`,
    // unknown contribution IDs
    `{"schemaVersion":1,"id":"a.b","name":"A","version":"1.0.0","license":"MIT","contributions":["unknown.card"]}`,
    // empty or duplicate contributions, wrong type
    `{"schemaVersion":1,"id":"a.b","name":"A","version":"1.0.0","license":"MIT","contributions":[]}`,
    `{"schemaVersion":1,"id":"a.b","name":"A","version":"1.0.0","license":"MIT","contributions":["overview.sample-card","overview.sample-card"]}`,
    `{"schemaVersion":1,"id":"a.b","name":"A","version":"1.0.0","license":"MIT","contributions":"overview.sample-card"}`,
    // extra fields and bad metadata
    `{${body},"extra":"field"}`,
    `{"schemaVersion":2,"id":"a.b","name":"A","version":"1.0.0","license":"MIT","contributions":["overview.sample-card"]}`,
    `{"schemaVersion":1,"id":"a.b","name":"A","version":"1.0","license":"MIT","contributions":["overview.sample-card"]}`,
    // trailing data and oversized document
    `{${body}} trailing`,
    `{${body}}${' '.repeat(65536)}`,
    // malformed grammar
    '{',
  ];
  for (const raw of hostile) {
    writeFileSync(file, raw);
    assert.equal(run('validate', file).status, 1, `CLI accepted: ${raw.slice(0, 60)}`);
    assert.throws(() => parsePlugin(raw), undefined, `runtime accepted: ${raw.slice(0, 60)}`);
  }
});

test('a UTF-8 byte order mark is ignored, as in the app importer', () => {
  const file = join(dir, 'bom.hlplugin');
  writeFileSync(file, '\uFEFF' + JSON.stringify(valid));
  assert.equal(run('validate', file).status, 0);
});

test('contributions allow-list is closed to the one known ID', () => {
  assert.deepEqual(CONTRIBUTION_IDS, ['overview.sample-card']);
});
