// One uninterrupted real browser journey. No routes, response fixtures or state writes.
import {chromium} from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import readline from 'node:readline';
import assert from 'node:assert/strict';
const input = readline.createInterface({input: process.stdin, crlfDelay: Infinity})[Symbol.asyncIterator]();
async function receive() {
  const line = await input.next();
  assert.equal(line.done, false, 'Native journey coordinator disconnected');
  return JSON.parse(line.value);
}
const cfg = await receive();
const output = path.resolve('artifacts/v10/P21/payload/browser-journey');
fs.mkdirSync(output, {recursive: true});
const browser = await chromium.launch({headless: true, args: ['--no-sandbox']});
try {
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ignoreHTTPSErrors: true});
  const page = await context.newPage();
  const errors = []; const posts = {draft: 0, approval: 0, apply: 0};
  const ids = new Set(); const phases = new Set(); const observations = [];
  function track(current) {
    current.on('pageerror', error => errors.push(error.name));
    current.on('request', request => {
      const url = new URL(request.url());
      assert.equal(url.origin, cfg.origin);
      if (request.method() === 'POST') {
        const name = {'/install/configuration': 'draft', '/install/configuration/approve': 'approval', '/install/services': 'apply'}[url.pathname];
        if (name) posts[name]++;
      }
      if (url.pathname === '/.well-known/gojet-installation-result') {
        assert.equal(request.method(), 'GET'); assert.equal(url.search, '');
        assert.equal(request.headers().cookie, undefined);
      }
    });
    current.on('response', response => {
      if (new URL(response.url()).pathname !== '/install/services' || response.status() !== 200) return;
      observations.push(response.json().then(value => {
        if (value.result_id) { assert.match(value.result_id, /^[a-f0-9]{64}$/); ids.add(value.result_id); }
        if (value.execution?.status === 'running') phases.add(value.execution.phase);
      }));
    });
  }
  track(page);
  async function idle(current = page) {
    await current.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
  }
  async function authenticate() {
    await page.locator('#bootstrap-token').fill(cfg.token);
    const response = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/install/');
    await page.locator('#installer-login button').click();
    assert.equal((await response).status(), 200);
    await idle();
  }
  await page.goto(cfg.origin+'/install/');
  await authenticate();
  assert.equal(await page.locator('#installer-title').textContent(), 'Configure installation');
  const c = cfg.draft.configuration;
  const fields = {origin: c.origin, turnstile_secret: c.turnstile_secret};
  for (const section of ['database', 'redis', 'files', 'quotas', 'smtp', 'risk', 'auth_rate']) {
    for (const [key, value] of Object.entries(c[section])) {
      if (section !== 'files' || key !== 'types') fields[section+'-'+key] = value;
    }
  }
  for (const [key, value] of Object.entries(cfg.draft.admin)) fields['admin-'+key] = value;
  for (const [field, value] of Object.entries(fields)) await page.locator('#cfg-'+field).fill(String(value));
  await page.locator('#cfg-file-types').fill(Object.entries(c.files.types).map(([ext, mimes]) => ext+'='+mimes.join('|')).join('\n'));
  const draft = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/install/configuration');
  await page.locator('#installer-save-configuration').click();
  assert.equal((await draft).status(), 202); await idle();
  assert.equal(await page.locator('#installer-title').textContent(), 'Configuration submitted');
  assert.equal(await page.locator('#cfg-admin-password').inputValue(), '');
  assert.deepEqual(posts, {draft: 1, approval: 0, apply: 0});
  await page.screenshot({path: path.join(output, 'configuration-submitted.png'), fullPage: true});
  process.stdout.write('drafted\n');
  assert.deepEqual(await receive(), {root_approved: true});
  // Saving consumed the CSRF. Reauthenticate through the same live page before acknowledgement.
  await page.setViewportSize({width: 390, height: 844});
  await authenticate();
  const approval = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/install/configuration/approve');
  await page.locator('#installer-approve').click();
  assert.equal((await approval).status(), 200); await idle();
  assert.equal(await page.locator('#installer-title').textContent(), 'Approved plan ready');
  assert.ok((await page.locator('#preflight-summary').textContent()).startsWith('All dependency checks passed.'));
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await page.screenshot({path: path.join(output, 'approved-mobile.png'), fullPage: true});
  const mobile = await context.newPage(); track(mobile);
  await mobile.setViewportSize({width: 390, height: 844});
  await mobile.goto(cfg.origin+'/install/');
  await mobile.locator('#installer-refresh').click(); await idle(mobile);
  assert.equal(await mobile.locator('#installer-title').textContent(), 'Approved plan ready');
  await Promise.all(observations);
  assert.equal(ids.size, 1);
  assert.deepEqual(posts, {draft: 1, approval: 1, apply: 0});
  process.stdout.write('approved\n');
  assert.deepEqual(await receive(), {allow_submission: true});
  await page.setViewportSize({width: 1280, height: 900});
  const apply = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/install/services');
  await page.locator('#installer-apply').click();
  assert.equal((await apply).status(), 202); await idle();
  process.stdout.write('submitted\n');
  const pages = [{page, name: 'desktop'}, {page: mobile, name: 'mobile'}];
  const deadline = Date.now()+300000;
  while (pages.some(p => !p.completed) && Date.now() < deadline) {
    for (const item of pages.filter(p => !p.completed)) {
      await item.page.locator('#installer-refresh').click(); await idle(item.page);
      const title = await item.page.locator('#installer-title').textContent();
      assert.notEqual(title, 'Installation needs attention', 'Native execution reported a failure');
      if (title !== 'Installation completed') continue;
      assert.equal(await item.page.locator('#installer-open-admin').isVisible(), true);
      assert.equal(await item.page.locator('#installer-open-admin').getAttribute('href'), '/admin');
      assert.equal(await item.page.locator('#installer-open-site').getAttribute('href'), '/');
      assert.equal(await item.page.locator('#installer-apply').isVisible(), false);
      assert.equal(await item.page.evaluate(() => localStorage.length+sessionStorage.length), 0);
      assert.equal(await item.page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
      await item.page.screenshot({path: path.join(output, item.name+'-completed.png'), fullPage: true});
      item.completed = true;
    }
    if (pages.some(p => !p.completed)) await new Promise(resolve => setTimeout(resolve, 500));
  }
  assert.ok(pages.every(p => p.completed), 'Actual completion was not observed on the original endpoint');
  await Promise.all(observations);
  assert.deepEqual(posts, {draft: 1, approval: 1, apply: 1});
  assert.equal(ids.size, 1); assert.deepEqual(errors, []);
  process.stdout.write('completed\n');
  assert.deepEqual(await receive(), {installer_retired: true});
  const closed = await context.request.get(cfg.origin+'/install/');
  assert.equal(closed.status(), 404);
  const result = await context.request.get(cfg.origin+'/.well-known/gojet-installation-result');
  assert.equal(result.status(), 200);
  assert.deepEqual(await result.json(), {schema: 'gojet.installation-result.v1', result_id: [...ids][0], installation_complete: true});
  // Follow the actual shipped link after retirement; health checks independently bind the static bytes.
  await page.locator('#installer-open-admin').click();
  await page.waitForURL(cfg.origin+'/admin');
  assert.equal((await context.request.get(cfg.origin+'/api/public/auth/providers')).status(), 200);
  fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify({continuous_first_install: true,
    same_origin: true, draft_posts: posts.draft, approval_posts: posts.approval, apply_posts: posts.apply,
    execution_phases_observed: [...phases].sort(), desktop_mobile_completion: true,
    preapproved_state_fixture: false, network_response_fixtures: false, completion_from_actual_health: true,
    admin_link_after_retirement: true, no_browser_secret_storage: true, no_overflow: true})+'\n');
  await context.close();
} finally { await browser.close(); }
