// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
// Isolated, labelled API fixtures. No requests reach a host-management backend.
import { readFileSync, mkdirSync, writeFileSync } from 'node:fs';
import { strict as assert } from 'node:assert';
const engines = await import(process.env.HC_PLAYWRIGHT_MODULE ?? 'playwright');
const url = process.env.HC_E2E_URL ?? 'http://127.0.0.1:5173';
const artifacts = process.env.HC_E2E_ARTIFACTS ?? 'work/hcis-ui-e2e';
mkdirSync(artifacts, { recursive: true });
const browser = await engines[process.env.HC_BROWSER ?? 'chromium'].launch({ headless: true, ...(process.env.HC_CHROMIUM ? { executablePath: process.env.HC_CHROMIUM } : {}) });
const context = await browser.newContext();
const page = await context.newPage();
await context.addInitScript(() => { window.__hcCsp = []; document.addEventListener('securitypolicyviolation', (event) => window.__hcCsp.push(event.violatedDirective)); });
page.setDefaultTimeout(12000);
const errors = [];
page.on('pageerror', (e) => errors.push(e.message));
const units = Array.from({ length: 230 }, (_, i) => ({ name: `example-${String(i).padStart(3, '0')}.service`, description: 'Example data — browser-test fixture', active: i % 3 ? 'active' : 'failed', sub: i % 3 ? 'running' : 'failed', load: 'loaded', type: 'service', unit_file_state: 'enabled' }));
await context.route('**/api/**', (route) => {
  const path = new URL(route.request().url()).pathname;
  let body = {}, status = 200;
  if (path.endsWith('/auth/session')) body = { username: 'example-browser-fixture', csrf_token: 'test-only', scopes: ['system.read', 'services.read'], version: 'example' };
  else if (path.endsWith('/capabilities')) body = { broker: { status: 'available' }, capabilities: [{ id: 'services', label: 'Services', path: '/services', status: 'available' }, { id: 'audit', label: 'Audit', path: '/audit', status: 'forbidden', reason: 'Example: permission not granted' }] };
  else if (path.endsWith('/services')) body = { units, source: 'Example data — browser-test fixture', truncated: false };
  else { status = 503; body = { error: 'fixture_unavailable', message: 'Example fixture does not provide this resource.' }; }
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
});
const axe = readFileSync('frontend/app/node_modules/axe-core/axe.min.js', 'utf8');
const checks = [];
function check(name, ok) { assert.ok(ok, name); checks.push(name); console.log(`PASS ${name}`); }
async function noOverflow(name) { check(name, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); }
async function audit(name) {
  await page.evaluate(axe + '\n;0');
  const violations = await page.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'] } })).violations.map((v) => ({ id: v.id, impact: v.impact, nodes: v.nodes.map((n) => n.target) })));
  check(`axe ${name}: ${JSON.stringify(violations)}`, violations.length === 0);
}
try {
  await page.goto(`${url}/services`);
  await page.getByRole('table', { name: 'Units' }).waitFor();
  const response = await context.request.get(url);
  check('production CSP keeps unsafe-inline disabled', !response.headers()['content-security-policy']?.includes("'unsafe-inline'"));
  check('pagination bounds live service rows', await page.locator('.hc-data-table tbody tr').count() === 100);
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  check('pagination advances', (await page.getByRole('rowheader').first().textContent()).includes('100'));
  await page.getByRole('button', { name: 'Search pages' }).click();
  await page.getByRole('combobox', { name: 'Search permitted pages' }).fill('theme');
  check('palette filters permitted pages', await page.getByRole('dialog').getByRole('option').count() === 1);
  await page.getByRole('combobox', { name: 'Search permitted pages' }).press('Enter');
  await page.getByRole('heading', { name: 'Themes & Theme Studio' }).waitFor();
  check('palette navigates', page.url().endsWith('/appearance/themes'));
  await page.getByRole('button', { name: 'Save draft' }).click();
  check('browser-local save', (await page.evaluate(() => JSON.parse(localStorage.getItem('hc.themes.v1')).themes)).length === 1);
  await page.getByRole('button', { name: 'Preview on application' }).click();
  await page.getByRole('button', { name: 'Keep theme', exact: true }).click();
  check('no CSP errors while applying custom theme', (await page.evaluate(() => window.__hcCsp)).length === 0);
  check('confirmed activation persists', await page.evaluate(() => localStorage.getItem('hc.theme.active.v1')) === 'local.my-theme');
  await page.getByRole('button', { name: 'Use built-in appearance' }).click();
  await page.getByRole('button', { name: 'Preview on application' }).click();
  await page.keyboard.press('Escape');
  check('Escape reverts trial', await page.evaluate(() => localStorage.getItem('hc.theme.active.v1')) === null);
  await page.getByRole('button', { name: 'Preview on application' }).click();
  await page.getByRole('region', { name: 'Confirm theme preview' }).waitFor({ state: 'hidden', timeout: 17000 });
  check('timed preview automatically reverts', await page.evaluate(() => localStorage.getItem('hc.theme.active.v1')) === null);
  await page.getByRole('button', { name: 'Advanced JSON' }).click();
  await page.getByRole('textbox', { name: 'Theme JSON' }).fill('{"schemaVersion":1,"schemaVersion":2}');
  await page.getByRole('button', { name: 'Validate and apply JSON' }).click();
  check('hostile import explains duplicate keys', await page.getByText('Duplicate key: schemaVersion.').isVisible());
  await page.getByRole('button', { name: 'Form editor' }).click();
  for (const theme of ['midnight', 'daybreak', 'graphite', 'oled']) {
    for (const width of [390, 560, 768, 1100, 1920, 3440]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.evaluate((theme) => { localStorage.removeItem('hc.theme.active.v1'); localStorage.setItem('hc.preferences.v1', JSON.stringify({ schemaVersion: 1, theme, textScale: '125', font: 'system' })); }, theme);
      await page.reload();
      await page.getByRole('heading', { name: 'Themes & Theme Studio' }).waitFor();
      await noOverflow(`${theme}/${width}: Studio fits at 125% text`);
      if (width === 390 || width === 1100) await audit(`${theme}/${width} studio`);
      if (theme === 'midnight' && width === 390) await page.screenshot({ path: `${artifacts}/studio-phone.png`, fullPage: true });
      if (theme === 'midnight' && width === 1920) await page.screenshot({ path: `${artifacts}/studio-desktop.png`, fullPage: true });
      await page.goto(`${url}/services`);
      await page.getByRole('table', { name: 'Units' }).waitFor();
      await noOverflow(`${theme}/${width}: Services fits at 125% text`);
      if (width === 390 || width === 1100) await audit(`${theme}/${width} services`);
      await page.goto(`${url}/appearance/themes`);
    }
  }
  check('no browser exceptions', errors.length === 0);
} finally {
  writeFileSync(`${artifacts}/report.json`, JSON.stringify({ browser: browser.version(), checks, errors }, null, 2));
  await browser.close();
}
