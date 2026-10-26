#!/usr/bin/env node
// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: MIT
import { readFileSync, writeFileSync, statSync } from 'node:fs';
import { BASES, COLOR_KEYS, parseTheme, themeIssues, newTheme, resolvedColors } from './runtime.mjs';
const [command, ...args] = process.argv.slice(2);
function readTheme(path) {
  if (!path) throw new Error('Supply a .hltheme file.');
  if (statSync(path).size > 65536) throw new Error('Theme exceeds 64 KiB.');
  // Editors such as Windows Notepad add a UTF-8 byte order mark; browsers drop it when importing, so the CLI does too.
  return parseTheme(readFileSync(path, 'utf8').replace(/^\uFEFF/, ''));
}
function write(path, theme) {
  if (!path) throw new Error('Supply an output path.');
  // Never overwrite an author’s existing theme accidentally.
  writeFileSync(path, JSON.stringify(theme, null, 2) + '\n', { flag: 'wx' });
}
try {
  if (command === 'create') {
    const [path, id, name, base = 'midnight'] = args;
    const theme = parseTheme(JSON.stringify({ ...newTheme(base), id, name }));
    write(path, theme); console.log(`Created ${path}. Import it into Appearance → Themes & Theme Studio.`);
  } else if (command === 'validate') {
    const theme = readTheme(args[0]), issues = themeIssues(theme);
    if (args.includes('--json')) console.log(JSON.stringify({ valid: !issues.length, id: theme.id, issues }, null, 2));
    else { console.log(`${theme.name} (${theme.id} ${theme.version}) — base: ${theme.base}`); for (const issue of issues) console.error(issue.message); console.log(issues.length ? `${issues.length} issues block activation.` : 'Key contrast checks pass. Validate keyboard, responsive layout and real content in the app before sharing.'); }
    if (issues.length) process.exitCode = 1;
  } else if (command === 'flatten') {
    const theme = readTheme(args[0]);
    const issues = themeIssues(theme);
    if (issues.length) throw new Error(issues.map((i) => i.message).join('\n'));
    write(args[1], { ...theme, colors: resolvedColors(theme) }); console.log(`Exported ${args[1]}.`);
  } else if (command === 'tokens') {
    console.log(COLOR_KEYS.join('\n'));
  } else if (command === 'help' || !command || command === '--help') {
    console.log(`Homelab Control Theme Development Kit (TDK) v1 — Node.js >=22, no dependencies\n\n  node sdk/theme/tdk.mjs create OUTPUT ID NAME [BASE]\n  node sdk/theme/tdk.mjs validate FILE [--json]\n  node sdk/theme/tdk.mjs flatten INPUT OUTPUT\n  node sdk/theme/tdk.mjs tokens\n\nBases: ${BASES.join(', ')}\nA .hltheme is one JSON document. Fonts, density, navigation and motion are user preferences.`);
  } else throw new Error(`Unknown command: ${command}. Use --help.`);
} catch (error) {
  console.error(error.message); process.exitCode = 1;
}
