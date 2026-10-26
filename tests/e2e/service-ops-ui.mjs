// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
// M2-2 service-operation dialog (M2-SECURITY-ACCEPTANCE E3): preview → step-up → confirm → observed
// result, keyboard only, axe, and layout in every theme at phone and laptop widths.
// Isolated, labelled API fixtures: no request reaches a host-management backend and nothing runs.
import { readFileSync, mkdirSync, writeFileSync } from 'node:fs';
import { strict as assert } from 'node:assert';
const engines = await import(process.env.HC_PLAYWRIGHT_MODULE ?? 'playwright');
const url = process.env.HC_E2E_URL ?? 'http://127.0.0.1:5173';
const artifacts = process.env.HC_E2E_ARTIFACTS ?? 'work/service-ops-e2e';
const engine = process.env.HC_BROWSER ?? 'chromium';
mkdirSync(artifacts, { recursive: true });
const browser = await engines[engine].launch({ headless: true, ...(process.env.HC_CHROMIUM ? { executablePath: process.env.HC_CHROMIUM } : {}) });
const context = await browser.newContext();
const page = await context.newPage();
await context.addInitScript(() => { window.__hcCsp = []; document.addEventListener('securitypolicyviolation', (event) => window.__hcCsp.push(event.violatedDirective)); });
page.setDefaultTimeout(12000);
const errors = [];
page.on('pageerror', (e) => { errors.push(e.message); console.error(`Browser exception (${engine}): ${e.message}`); });

const EX = 'Example data — browser-test fixture';
const PASSWORD = 'example-fixture-password';
const units = [
  { name: 'example-web.service', description: EX, active: 'active', sub: 'running', load: 'loaded', type: 'service', unit_file_state: 'enabled' },
  { name: 'example-batch.service', description: EX, active: 'inactive', sub: 'dead', load: 'loaded', type: 'service', unit_file_state: 'disabled' },
  { name: 'ssh.service', description: EX, active: 'active', sub: 'running', load: 'loaded', type: 'service', unit_file_state: 'enabled' },
];
const observed = (active, sub) => ({ id: 'example-web.service', load_state: 'loaded', active_state: active, sub_state: sub, unit_file_state: 'enabled' });
const requests = [];
await context.route('**/api/**', async (route) => {
  const req = route.request();
  const path = new URL(req.url()).pathname;
  const json = req.postData() ? JSON.parse(req.postData()) : {};
  let body = {}, status = 200;
  if (path.endsWith('/auth/session')) body = { username: 'example-operator-fixture', csrf_token: 'test-only', scopes: ['system.read', 'services.read', 'services.operate', 'audit.read'], version: 'example' };
  else if (path.endsWith('/capabilities')) body = { broker: { status: 'available' }, capabilities: [{ id: 'services', label: 'Services', path: '/services', status: 'available' }, { id: 'audit', label: 'Audit', path: '/audit', status: 'available' }] };
  else if (path.endsWith('/services')) body = { units, source: EX, truncated: false, operate: { available: true }, protected: { 'ssh.service': { protected: true, category: 'remote', reason: 'Remote login (SSH): stopping or restarting it can lock you out of the host.' } } };
  else if (path.endsWith('/plan')) {
    requests.push(`plan ${json.action}`);
    const stop = json.action !== 'start';
    body = { unit: 'example-web.service', requested: 'example-web.service', action: json.action, risk: stop ? 'disruptive' : 'reversible', step_up_required: stop, token: 'hcp1.ZXhhbXBsZQ.example-fixture-token-not-a-real-mac-0000000', expires_at: new Date(Date.now() + 120000).toISOString(), observed: observed('active', 'running'), effect: `${EX}: ${json.action} example-web.service, which is currently active (running).`, consequences: ['The service stops now. Clients connected to it lose their connection.', 'This is a runtime action. There is no automatic rollback.'], also_affected: stop ? ['example-worker.service'] : [] };
  } else if (path.endsWith('/auth/step-up')) {
    requests.push('step-up');
    if (json.password === PASSWORD) body = { step_up_until: new Date(Date.now() + 300000).toISOString() };
    else { status = 403; body = { error: 'invalid_credentials', message: 'The password is not correct.' }; }
  } else if (path.endsWith('/apply')) {
    requests.push(`apply ${json.action}`);
    body = { operation_id: 'example0fixture0operation0id', unit: 'example-web.service', action: json.action, outcome: 'succeeded', message: `${EX}: the service is stopped. Observed state: inactive (dead).`, exit_code: 0, observed: observed('inactive', 'dead'), started_at: new Date().toISOString(), finished_at: new Date().toISOString() };
  } else { status = 503; body = { error: 'fixture_unavailable', message: 'Example fixture does not provide this resource.' }; }
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
});
const axe = readFileSync('frontend/app/node_modules/axe-core/axe.min.js', 'utf8');
const checks = [];
function check(name, ok) { assert.ok(ok, name); checks.push(name); console.log(`PASS ${name}`); }
async function audit(name) {
  await page.evaluate(axe + '\n;0');
  const violations = await page.evaluate(async () => (await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'] } })).violations.map((v) => ({ id: v.id, impact: v.impact, nodes: v.nodes.map((n) => n.target) })));
  check(`axe ${name}: ${JSON.stringify(violations)}`, violations.length === 0);
}
const dialog = () => page.getByRole('dialog');
const active = () => page.evaluate(() => ({ tag: document.activeElement?.tagName, text: document.activeElement?.textContent?.trim() ?? '', unit: document.activeElement?.getAttribute('data-unit') ?? '' }));
async function openServices() {
  await page.goto(`${url}/services`);
  await page.getByRole('table', { name: 'Units' }).waitFor();
}

try {
  await page.setViewportSize({ width: 1280, height: 900 });
  await openServices();
  check('protected unit has no controls', (await page.getByRole('button', { name: 'Stop ssh.service' }).count()) === 0);
  check('protected unit is labelled with its reason', (await page.locator('.tag', { hasText: 'Protected' }).first().textContent()).includes('lock you out'));
  check('stopped unit offers start only', (await page.getByRole('button', { name: 'Start example-batch.service' }).count()) === 1 && (await page.getByRole('button', { name: 'Stop example-batch.service' }).count()) === 0);

  // Keyboard only: open, find the password field, step-up, confirm, read the result, close.
  await page.getByRole('button', { name: 'Stop example-web.service' }).focus();
  await page.keyboard.press('Enter');
  await dialog().getByText('Preview · nothing has been changed yet').waitFor();
  check('focus starts on Close, never on the action', (await active()).text === 'Close');
  check('disruptive risk is shown as a word', await dialog().getByText('Disruptive', { exact: true }).isVisible());
  check('units stopped with it are listed', await dialog().getByText('example-worker.service').isVisible());
  for (let i = 0; i < 6 && (await active()).tag !== 'INPUT'; i++) await page.keyboard.press('Shift+Tab');
  check('password field reachable by keyboard', (await active()).tag === 'INPUT');
  await page.keyboard.type('wrong-password');
  for (let i = 0; i < 6 && !(await active()).text.startsWith('Stop'); i++) await page.keyboard.press('Tab');
  check('confirm button reachable by keyboard', (await active()).text === 'Stop example-web.service');
  await page.keyboard.press('Enter');
  await dialog().getByText('The password is not correct.').waitFor();
  check('wrong password: nothing executed', !requests.includes('apply stop'));
  check('wrong password: focus back in the password field', (await active()).tag === 'INPUT');
  await page.keyboard.type(PASSWORD);
  await page.keyboard.press('Enter');
  await dialog().getByText('Done', { exact: true }).waitFor();
  check('step-up precedes execution', requests.indexOf('step-up') < requests.indexOf('apply stop'));
  check('result shows the observed state', await dialog().getByText('inactive (dead)').first().isVisible());
  check('result links to the audit record', await dialog().getByRole('link', { name: 'view in Audit' }).isVisible());
  check('focus on Close after the result', (await active()).text === 'Close');
  await page.screenshot({ path: `${artifacts}/service-op-result.png` });
  await page.keyboard.press('Escape');
  await dialog().waitFor({ state: 'detached' });
  check('focus returns to the unit after closing', (await active()).unit === 'example-web.service');

  // Escape during preview closes without running anything.
  const applies = requests.filter((r) => r.startsWith('apply')).length;
  await page.getByRole('button', { name: 'Restart example-web.service' }).click();
  await dialog().getByText('Preview · nothing has been changed yet').waitFor();
  check('step-up is remembered for the session', (await dialog().getByRole('textbox').count()) === 0 && (await dialog().locator('input[type=password]').count()) === 0);
  await page.keyboard.press('Escape');
  await dialog().waitFor({ state: 'detached' });
  check('Escape in preview executes nothing', requests.filter((r) => r.startsWith('apply')).length === applies);

  for (const theme of ['midnight', 'daybreak', 'graphite', 'oled']) {
    for (const width of [390, 1100]) {
      await page.setViewportSize({ width, height: 900 });
      await page.evaluate((theme) => { localStorage.removeItem('hc.theme.active.v1'); localStorage.setItem('hc.preferences.v1', JSON.stringify({ schemaVersion: 1, theme, textScale: '125', font: 'system' })); }, theme);
      await openServices();
      await page.getByRole('button', { name: 'Stop example-web.service' }).click();
      await dialog().getByText('Preview · nothing has been changed yet').waitFor();
      check(`${theme}/${width}: dialog fits the viewport at 125% text`, await page.evaluate(() => { const r = document.querySelector('[role=dialog]').getBoundingClientRect(); return r.left >= 0 && r.right <= innerWidth + 1 && document.documentElement.scrollWidth <= innerWidth + 1; }));
      await audit(`${theme}/${width} service operation preview`);
      if (theme === 'midnight' && width === 390) await page.screenshot({ path: `${artifacts}/service-op-phone.png` });
      await page.keyboard.press('Escape');
      await dialog().waitFor({ state: 'detached' });
    }
  }
  check('no CSP violations', (await page.evaluate(() => window.__hcCsp)).length === 0);
  check(`no browser exceptions: ${JSON.stringify(errors)}`, errors.length === 0);
} finally {
  writeFileSync(`${artifacts}/report.json`, JSON.stringify({ browser: browser.version(), checks, errors }, null, 2));
  await browser.close();
}
