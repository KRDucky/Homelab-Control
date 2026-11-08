#!/usr/bin/env node
// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: MIT
import { readFileSync, writeFileSync, statSync } from 'node:fs';
import { CONTRIBUTION_IDS, parsePlugin } from './runtime.mjs';
const [command, ...args] = process.argv.slice(2);
function readPlugin(path) {
  if (!path) throw new Error('Supply a .hlplugin file.');
  if (statSync(path).size > 65536) throw new Error('Plugin exceeds 64 KiB.');
  // Editors such as Windows Notepad add a UTF-8 byte order mark; browsers drop it when importing, so the CLI does too.
  return parsePlugin(readFileSync(path, 'utf8').replace(/^\uFEFF/, ''));
}
function write(path, plugin) {
  if (!path) throw new Error('Supply an output path.');
  // Never overwrite an author’s existing plugin accidentally.
  writeFileSync(path, JSON.stringify(plugin, null, 2) + '\n', { flag: 'wx' });
}
try {
  if (command === 'create') {
    const [path, id, name, version = '1.0.0'] = args;
    const plugin = parsePlugin(
      JSON.stringify({
        schemaVersion: 1,
        id,
        name,
        version,
        license: 'MIT',
        contributions: ['overview.sample-card'],
      }),
    );
    write(path, plugin);
    console.log(`Created ${path}. Contributions are compiled-in UI only; see sdk/plugin/README.md.`);
  } else if (command === 'validate') {
    const plugin = readPlugin(args[0]);
    if (args.includes('--json'))
      console.log(JSON.stringify({ valid: true, id: plugin.id, contributions: plugin.contributions }, null, 2));
    else
      console.log(
        `${plugin.name} (${plugin.id} ${plugin.version}) — contributions: ${plugin.contributions.join(', ') || 'none'}`,
      );
  } else if (command === 'contributions') {
    console.log(CONTRIBUTION_IDS.join('\n'));
  } else if (command === 'help' || !command || command === '--help') {
    console.log(`Homelab Control Plugin Development Kit (PDK) v1 — Node.js >=22, no dependencies

  node sdk/plugin/tdk.mjs create OUTPUT ID NAME [VERSION]
  node sdk/plugin/tdk.mjs validate FILE [--json]
  node sdk/plugin/tdk.mjs contributions

A .hlplugin is one JSON document: metadata plus an allow-list of known contribution IDs.
There is no archive, image, font, CSS, script or binary payload.`);
  } else throw new Error(`Unknown command: ${command}. Use --help.`);
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
