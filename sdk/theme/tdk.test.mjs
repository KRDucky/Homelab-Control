// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: MIT
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { BASES, newTheme, parseTheme, themeIssues } from './runtime.mjs';
const dir=mkdtempSync(join(tmpdir(), 'hc-tdk-'));
const cli=new URL('./tdk.mjs',import.meta.url);
const run=(...args)=>spawnSync(process.execPath,[cli.pathname,...args],{encoding:'utf8'});
test('standalone create, validate, flatten and overwrite protection',()=>{
 const file=join(dir,'ocean.hltheme'), flat=join(dir,'flat.hltheme');
 assert.equal(run('create',file,'local.ocean','Ocean','midnight').status,0);
 assert.equal(run('create',file,'local.ocean','Ocean').status,1);
 assert.equal(run('validate',file).status,0);
 assert.equal(run('flatten',file,flat).status,0);
 assert.equal(Object.keys(parseTheme(readFileSync(flat,'utf8')).colors).length,57);
});
test('malicious inputs fail closed in the SDK runtime and CLI',()=>{
 const file=join(dir,'invalid.hltheme');
 for(const raw of ['{"schemaVersion":1,"schemaVersion":1}',JSON.stringify({...newTheme('midnight'),colors:{'text-primary':'url(https://example.org)'}}), JSON.stringify({...newTheme('midnight'),navigation:{}})]) {
  writeFileSync(file,raw);assert.equal(run('validate',file).status,1);assert.throws(()=>parseTheme(raw));
 }
 writeFileSync(file,JSON.stringify({...newTheme('midnight'),colors:{'text-primary':'#111111','surface-raised':'#111111'}}));
 const result=run('validate',file,'--json');assert.equal(result.status,1);assert.ok(JSON.parse(result.stdout).issues.length);
});
test('all base palettes and shipped starter themes pass',()=>{
 for(const base of BASES){assert.deepEqual(themeIssues(newTheme(base)),[]);execFileSync(process.execPath,[cli.pathname,'validate',new URL(`./examples/${base}.hltheme`,import.meta.url).pathname]);}
});
test('a UTF-8 byte order mark is ignored, as in the app importer',()=>{
 const file=join(dir,'bom.hltheme');
 writeFileSync(file,'\uFEFF'+JSON.stringify(newTheme('midnight')));
 assert.equal(run('validate',file).status,0);
});
