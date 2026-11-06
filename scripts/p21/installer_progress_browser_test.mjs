// Read-only browser observation of the actual native timer, after adversarial fixtures are removed.
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';

const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));
const output = path.resolve('artifacts/v10/P21/payload/browser-progress');
fs.mkdirSync(output, {recursive: true});
const browser = await chromium.launch({headless: true, args: ['--no-sandbox']});
try {
  const context = await browser.newContext({ignoreHTTPSErrors: true});
  const errors = [];
  let applyPosts = 0;
  const phases = new Map([
    ['Starting installation', 'starting'], ['Initializing data', 'data'],
    ['Starting services', 'services'], ['Publishing the site', 'site'],
    ['Checking installation health', 'health'],
  ]);
  const pages = [];
  for (const [name, viewport] of [['desktop', {width: 1280, height: 900}], ['mobile', {width: 390, height: 844}]]) {
    const page = await context.newPage();
    await page.setViewportSize(viewport);
    page.on('pageerror', e => errors.push(e.message));
    page.on('console', msg => {
      if (msg.type() === 'error' && !msg.text().startsWith('Failed to load resource:')) errors.push(msg.text());
    });
    page.on('request', request => {
      if (request.method() === 'POST' && new URL(request.url()).pathname === '/install/services') applyPosts++;
    });
    await page.goto(cfg.origin + '/install/');
    if (name === 'desktop') {
      await page.locator('#bootstrap-token').fill(cfg.token);
      const response = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/install/');
      await page.locator('#installer-login button').click();
      const authenticated = await response;
      assert.equal(authenticated.status(), 200);
      const state = await authenticated.json();
      assert.equal(state.step, 'services');
      assert.equal(state.status, 'checking');
    }
    await page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
    await page.locator('#installer-refresh').click();
    await page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
    assert.equal(await page.locator('#installer-title').textContent(), 'Installation requested');
    assert.equal(await page.locator('#installer-apply').isVisible(), false);
    assert.equal(await page.locator('#installer-login').isVisible(), false);
    pages.push({page, name, observed: new Set(), closed: false});
  }
  // Only this bounded marker crosses the pipe; the parent may now enable the real root timer.
  process.stdout.write('ready\n');
  const deadline = Date.now() + 480000;
  while (pages.some(item => !item.closed) && Date.now() < deadline) {
    for (const item of pages.filter(item => !item.closed)) {
      const {page, name, observed} = item;
      const response = page.waitForResponse(r => r.request().method() === 'GET' && new URL(r.url()).pathname === '/install/');
      await page.locator('#installer-refresh').click();
      const status = (await response).status();
      assert.ok([200, 404].includes(status), 'Unexpected native observer HTTP status');
      await page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
      const title = await page.locator('#installer-title').textContent();
      assert.equal(await page.locator('#installer-apply').isVisible(), false);
      assert.equal(await page.locator('#installer-login').isVisible(), false);
      assert.equal(await page.locator('#bootstrap-token').inputValue(), '');
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
      if (phases.has(title)) {
        if (!observed.size) await page.screenshot({path: path.join(output, name + '-running.png'), fullPage: true});
        observed.add(phases.get(title));
      }
      if (title === 'Installation result unconfirmed') {
        // The lock can appear between the two GETs made by Refresh status.
        assert.equal(await page.locator('#installer-refresh').isVisible(), true);
        assert.equal(await page.locator('#installer-login').isVisible(), false);
        assert.ok(observed.size > 0, 'No actual worker phase observed');
        assert.match(await page.locator('#installer-message').textContent(), /completion has not yet been confirmed/);
        await page.screenshot({path: path.join(output, name + '-closed.png'), fullPage: true});
        item.closed = true;
      }
    }
    if (pages.some(item => !item.closed)) await new Promise(resolve => setTimeout(resolve, 500));
  }
  assert.ok(pages.every(item => item.closed), 'Native installer did not close in browser');
  assert.equal(applyPosts, 0, 'Observation must never resubmit installation');
  assert.deepEqual(errors, []);
  fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify({
    actual_worker_observed: true, desktop_mobile_closure: true, apply_posts: applyPosts,
    console_errors: 0, no_overflow: true,
    observed_phases: Object.fromEntries(pages.map(item => [item.name, [...item.observed]])),
    all_phases_observed_claim: false, full_interactive_configuration_verified: false,
  }) + '\n');
  await context.close();
} finally { await browser.close(); }
