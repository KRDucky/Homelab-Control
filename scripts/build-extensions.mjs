// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Builds frontend/app/src/extensions.generated.ts from the pinned extensions/ subtree (extensions.lock).
//
//   node scripts/build-extensions.mjs           regenerate
//   node scripts/build-extensions.mjs --check   fail if the checked-in file is stale or extensions/ breaks policy
//
// Core keeps the trust decisions: the contribution-ID allow-list (CONTRIBUTION_IDS in plugin.ts, through the
// generated SDK runtime) and the core-owned ID → component table below. Extensions content is validated with the
// same strict manifest parser the app uses, limited to a closed set of file kinds, and every component is checked
// against a static-only source policy. The output uses static imports only; nothing is loaded at runtime.
import ts from '../frontend/app/node_modules/typescript/lib/typescript.js';
import * as prettier from '../frontend/app/node_modules/prettier/index.mjs';
import { CONTRIBUTION_IDS, parsePlugin } from '../sdk/plugin/runtime.mjs';
import { lstatSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..');

/** Core-owned: the exported component that renders each known contribution ID. A new ID needs a core PR. */
export const CONTRIBUTION_COMPONENTS = Object.freeze({ 'overview.sample-card': 'SampleOverviewCard' });

const ROOT_FILES = new Set(['README.md', 'CONTRIBUTING.md', 'LICENSE.md', 'REUSE.toml']);
const LICENSE_FILE = /^LICENSES\/[A-Za-z0-9.+-]{1,64}\.txt$/;
const PLUGIN_FILE = /^plugins\/([a-z0-9][a-z0-9-]{0,62})\/(manifest\.hlplugin|README\.md|ui\/([A-Z][A-Za-z0-9]{0,63})\.tsx)$/;
const MAX_FILE = 256 * 1024;

// Identifiers a compiled-in contribution may not reference, even as a property name: no code evaluation, network,
// storage, timers, workers, or host/DOM globals. Contributions are static presentational React.
const DENIED = new Set([
  'eval', 'Function', 'require', 'process', 'fetch', 'XMLHttpRequest', 'WebSocket', 'EventSource', 'Worker',
  'SharedWorker', 'importScripts', 'sendBeacon', 'window', 'globalThis', 'self', 'document', 'navigator', 'location',
  'localStorage', 'sessionStorage', 'indexedDB', 'caches', 'cookieStore', 'cookie', 'setTimeout', 'setInterval',
  'queueMicrotask', 'postMessage', 'innerHTML', 'outerHTML', 'insertAdjacentHTML', 'dangerouslySetInnerHTML',
  'srcDoc', 'srcdoc',
  // Reflective routes back to Function, e.g. [].filter.constructor('return window')().
  'constructor', 'prototype', '__proto__', 'Reflect', 'Proxy', 'getPrototypeOf', 'setPrototypeOf',
  'getOwnPropertyDescriptor', 'getOwnPropertyDescriptors', 'defineProperty', 'defineProperties', 'apply', 'call', 'bind',
]);

function fail(message) {
  throw new Error(`build-extensions: ${message}`);
}

/** Lists every file under dir (relative, '/'-separated). Symlinks, devices and oversized files are refused. */
function listFiles(dir) {
  const out = [];
  const walk = (abs) => {
    for (const name of readdirSync(abs).sort()) {
      const path = join(abs, name);
      const rel = relative(dir, path).split(sep).join('/');
      const st = lstatSync(path);
      if (st.isDirectory()) walk(path);
      else if (st.isFile()) {
        if (st.size > MAX_FILE) fail(`${rel} exceeds ${MAX_FILE} bytes.`);
        out.push(rel);
      } else fail(`${rel} is not a regular file or directory (symbolic links are not allowed).`);
    }
  };
  walk(dir);
  return out;
}

/** Static-only source policy for one component file. Returns nothing; throws on the first violation. */
export function checkComponent(rel, source, exportName) {
  const file = ts.createSourceFile(rel, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  if (file.parseDiagnostics.length > 0) fail(`${rel} does not parse as TSX.`);
  let exported = 0;
  for (const st of file.statements) {
    if (ts.isImportDeclaration(st)) {
      const from = ts.isStringLiteral(st.moduleSpecifier) ? st.moduleSpecifier.text : '';
      if (from !== 'react' || !st.importClause?.isTypeOnly)
        fail(`${rel}: only \`import type … from 'react'\` is allowed (found import from '${from}').`);
      continue;
    }
    const isExport = ts.canHaveModifiers(st) && ts.getModifiers(st)?.some((m) => m.kind === ts.SyntaxKind.ExportKeyword);
    if (ts.isFunctionDeclaration(st) && isExport && st.name?.text === exportName && !st.asteriskToken &&
      !ts.getModifiers(st)?.some((m) => m.kind === ts.SyntaxKind.AsyncKeyword || m.kind === ts.SyntaxKind.DefaultKeyword)) {
      exported++;
      continue;
    }
    fail(`${rel}: only type imports from 'react' and one \`export function ${exportName}\` are allowed at top level.`);
  }
  if (exported !== 1) fail(`${rel}: expected exactly one \`export function ${exportName}\`.`);
  const visit = (node) => {
    if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword)
      fail(`${rel}: dynamic import() is not allowed.`);
    if (ts.isIdentifier(node) && DENIED.has(node.text)) fail(`${rel}: '${node.text}' is not allowed in a contribution.`);
    if (ts.isJsxAttribute(node) && DENIED.has(node.name.getText(file)))
      fail(`${rel}: JSX attribute '${node.name.getText(file)}' is not allowed.`);
    if (ts.isTaggedTemplateExpression(node)) fail(`${rel}: tagged templates are not allowed.`);
    // Names can also be built from strings: x['constructor'], { ['con' + 'structor']: C } = x,
    // { 'constructor': C } = x. Computed access and computed keys are refused outright, and no string
    // literal may spell a denied name.
    if (ts.isElementAccessExpression(node)) fail(`${rel}: computed member access (x[...]) is not allowed.`);
    if (ts.isComputedPropertyName(node)) fail(`${rel}: computed property names are not allowed.`);
    if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && DENIED.has(node.text))
      fail(`${rel}: the string '${node.text}' is not allowed in a contribution.`);
    ts.forEachChild(node, visit);
  };
  visit(file);
}

const quote = (s) => (s.includes("'") && !s.includes('"') ? JSON.stringify(s) : `'${s.replace(/\\/g, '\\\\').replace(/'/g, "\\'")}'`);

/** Validates extensionsDir and returns the generated TypeScript (formatted) for outFile. */
export async function generate({ extensionsDir = join(repo, 'extensions'), outFile = join(repo, 'frontend/app/src/extensions.generated.ts') } = {}) {
  const tableIds = Object.keys(CONTRIBUTION_COMPONENTS).sort();
  if (JSON.stringify(tableIds) !== JSON.stringify([...CONTRIBUTION_IDS].sort()))
    fail('CONTRIBUTION_COMPONENTS must list exactly the CONTRIBUTION_IDS in frontend/app/src/plugin.ts.');
  if (new Set(Object.values(CONTRIBUTION_COMPONENTS)).size !== tableIds.length) fail('component names must be unique.');

  const plugins = new Map(); // dir -> { manifest, components: Set }
  for (const rel of listFiles(extensionsDir)) {
    if (ROOT_FILES.has(rel) || LICENSE_FILE.test(rel)) continue;
    const m = PLUGIN_FILE.exec(rel);
    if (!m) fail(`unsupported file under extensions/: ${rel}. Only manifests, READMEs, licence metadata and ui/<Component>.tsx are imported.`);
    const entry = plugins.get(m[1]) ?? { manifest: null, components: new Set() };
    plugins.set(m[1], entry);
    if (m[2] === 'manifest.hlplugin') {
      try {
        entry.manifest = parsePlugin(readFileSync(join(extensionsDir, rel), 'utf8'));
      } catch (e) {
        fail(`${rel}: ${e.message}`);
      }
    } else if (m[3]) entry.components.add(m[3]);
  }

  const claimed = new Map(); // contribution ID -> plugin dir
  const ids = new Set();
  const imports = [];
  for (const [dir, { manifest, components }] of [...plugins].sort(([a], [b]) => (a < b ? -1 : 1))) {
    if (!manifest) fail(`plugins/${dir}/ has no manifest.hlplugin.`);
    if (ids.has(manifest.id)) fail(`duplicate plugin id ${manifest.id}.`);
    ids.add(manifest.id);
    const used = new Set();
    for (const id of manifest.contributions) {
      if (claimed.has(id)) fail(`contribution ${id} is claimed by both plugins/${claimed.get(id)} and plugins/${dir}.`);
      claimed.set(id, dir);
      const name = CONTRIBUTION_COMPONENTS[id];
      const rel = `plugins/${dir}/ui/${name}.tsx`;
      if (!components.has(name)) fail(`${id} needs ${rel}.`);
      checkComponent(rel, readFileSync(join(extensionsDir, rel), 'utf8'), name);
      used.add(name);
      let spec = relative(dirname(outFile), join(extensionsDir, 'plugins', dir, 'ui', name)).split(sep).join('/');
      if (!spec.startsWith('.')) spec = `./${spec}`;
      imports.push({ id, name, spec });
    }
    for (const name of components)
      if (!used.has(name)) fail(`plugins/${dir}/ui/${name}.tsx is not referenced by the manifest's contributions.`);
  }
  for (const id of CONTRIBUTION_IDS) if (!claimed.has(id)) fail(`no imported plugin provides contribution ${id}.`);

  const manifests = [...plugins].sort(([a], [b]) => (a < b ? -1 : 1)).map(([, { manifest: d }]) =>
    `Object.freeze({ schemaVersion: 1, id: ${quote(d.id)}, name: ${quote(d.name)}, version: ${quote(d.version)}, ` +
    `license: ${quote(d.license)}, contributions: [${d.contributions.map(quote).join(', ')}] as ContributionId[] })`);
  const text = [
    // Split so that REUSE does not read these template strings as this script's own licence tags.
    '// SPDX-' + 'FileCopyrightText: 2026 The Homelab Control Authors',
    '//',
    '// SPDX-' + 'License-Identifier: GPL-3.0-or-later',
    '',
    '// Generated by scripts/build-extensions.mjs from the pinned extensions/ subtree (extensions.lock). Do not edit.',
    '// Manifests were validated with the core plugin parser at build time; components are static imports only.',
    '',
    "import type { ReactNode } from 'react';",
    "import type { ContributionId, PluginDocument } from './plugin';",
    ...imports.map(({ name, spec }) => `import { ${name} } from ${quote(spec)};`),
    '',
    '/** Plugin manifests imported from extensions/plugins/, in directory order. */',
    `export const EXTENSION_PLUGINS: readonly PluginDocument[] = Object.freeze([${manifests.join(', ')}]);`,
    '',
    '/** Contribution ID → imported component (the ID → name table is core-owned in scripts/build-extensions.mjs). */',
    'export const EXTENSION_COMPONENTS: Readonly<Record<ContributionId, () => ReactNode>> = Object.freeze({',
    ...imports.map(({ id, name }) => `  ${quote(id)}: ${name},`),
    '});',
    '',
  ].join('\n');
  const config = (await prettier.resolveConfig(outFile)) ?? {};
  return prettier.format(text, { ...config, filepath: outFile });
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const check = process.argv.includes('--check');
  const outFile = join(repo, 'frontend/app/src/extensions.generated.ts');
  try {
    const text = await generate({ outFile });
    if (check) {
      let current = '';
      try {
        current = readFileSync(outFile, 'utf8');
      } catch {
        // missing file is drift
      }
      if (current !== text) fail(`${relative(repo, outFile)} is stale. Run node scripts/build-extensions.mjs.`);
      console.log('Extensions registry matches the pinned extensions/ import.');
    } else {
      writeFileSync(outFile, text);
      console.log(`Generated ${relative(repo, outFile)}.`);
    }
  } catch (e) {
    console.error(e.message);
    process.exit(1);
  }
}
