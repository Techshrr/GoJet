// Real same-origin handover and completion; network fixtures below test refusals only.
import {chromium} from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));
const output = path.resolve('artifacts/v10/P21/payload/browser-result');
fs.mkdirSync(output, {recursive: true});
const browser = await chromium.launch({headless: true, args: ['--no-sandbox']});
try {
  const context = await browser.newContext({ignoreHTTPSErrors: true});
  const pages = []; const errors = []; let applyPosts = 0;
  for (const [name, viewport] of [['desktop', {width: 1280, height: 900}], ['mobile', {width: 390, height: 844}]]) {
    const page = await context.newPage(); await page.setViewportSize(viewport);
    page.on('pageerror', e => errors.push(e.message));
    page.on('console', msg => { if (msg.type() === 'error' && !msg.text().startsWith('Failed to load resource:')) errors.push(msg.text()); });
    page.on('request', request => {
      const url = new URL(request.url());
      assert.equal(url.origin, cfg.origin);
      if (request.method() === 'POST' && url.pathname === '/install/services') applyPosts++;
      if (url.pathname === '/.well-known/gojet-installation-result') {
        assert.equal(request.method(), 'GET'); assert.equal(url.search, '');
        assert.equal(request.headers().cookie, undefined);
      }
    });
    await page.goto(cfg.origin+'/install/');
    if (name === 'desktop') {
      await page.locator('#bootstrap-token').fill(cfg.token);
      await page.locator('#installer-login button').click();
    }
    await page.locator('#installer-refresh').click();
    await page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
    // Explicit readiness-state fixture; no simulated completion is accepted.
    assert.equal(await page.locator('#installer-title').textContent(), 'Approved plan ready');
    const closed = '**/install/'; const status = '**/.well-known/gojet-installation-result';
    await page.route(closed, route => route.fulfill({status: 404, body: 'not found'}));
    for (const response of [
      {status: 404, body: 'not found'},
      {status: 200, contentType: 'application/json', body: JSON.stringify({schema: 'gojet.installation-result.v1', result_id: '0'.repeat(64), installation_complete: true})},
      {status: 200, contentType: 'application/json', body: JSON.stringify({schema: 'gojet.installation-result.v1', result_id: cfg.result_id, installation_complete: false})},
      {status: 200, contentType: 'application/json', body: '{'},
      {status: 200, contentType: 'application/json', body: ' '.repeat(1025)},
      {status: 302, headers: {location: '/admin'}, body: ''},
    ]) {
      await page.route(status, route => route.fulfill(response));
      await page.locator('#installer-refresh').click();
      await page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
      assert.equal(await page.locator('#installer-title').textContent(), 'Installation result unconfirmed');
      assert.equal(await page.locator('#installer-open-admin').isVisible(), false);
      assert.equal(await page.locator('#installer-apply').isVisible(), false);
      await page.unroute(status);
    }
    await page.route(status, route => route.abort());
    await page.locator('#installer-refresh').click();
    await page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
    assert.equal(await page.locator('#installer-title').textContent(), 'Installation result unconfirmed');
    await page.unroute(status); await page.unroute(closed);
    await page.screenshot({path: path.join(output, name+'-unconfirmed.png'), fullPage: true});
    pages.push({page, name, completed: false});
  }
  process.stdout.write('ready\n');
  const deadline = Date.now()+300000;
  while (pages.some(p => !p.completed) && Date.now() < deadline) {
    for (const item of pages.filter(p => !p.completed)) {
      await item.page.locator('#installer-refresh').click();
      await item.page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
      if (await item.page.locator('#installer-title').textContent() === 'Installation completed') {
        assert.equal(await item.page.locator('#installer-open-admin').getAttribute('href'), '/admin');
        assert.equal(await item.page.locator('#installer-open-site').getAttribute('href'), '/');
        assert.equal(await item.page.locator('#installer-open-admin').isVisible(), true);
        assert.equal(await item.page.locator('#installer-login').isVisible(), false);
        assert.equal(await item.page.locator('#installer-apply').isVisible(), false);
        assert.equal(await item.page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
        assert.equal(await item.page.evaluate(() => localStorage.length+sessionStorage.length), 0);
        await item.page.screenshot({path: path.join(output, item.name+'-completed.png'), fullPage: true});
        item.completed = true;
      }
    }
    if (pages.some(p => !p.completed)) await new Promise(r => setTimeout(r, 500));
  }
  assert.ok(pages.every(p => p.completed), 'Real matching completion receipt was not observed');
  assert.equal(applyPosts, 0); assert.deepEqual(errors, []);
  fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify({same_origin_completion_observed: true,
    desktop_mobile_verified: true, invalid_results_refused: true, apply_posts: 0, console_errors: 0,
    no_overflow: true, preapproved_state_fixture: true, completion_from_actual_health: true})+'\n');
  await context.close();
} finally { await browser.close(); }
