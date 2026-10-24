// Real browser acknowledgement of the approval produced by the shipped root command.
import {chromium} from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));
const browser = await chromium.launch({headless: true, args: ['--no-sandbox']});
try {
  const context = await browser.newContext({viewport: {width: 390, height: 844}, ignoreHTTPSErrors: true});
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.name));
  let approvals = 0, applies = 0;
  page.on('request', request => {
    if (request.method() !== 'POST') return;
    const route = new URL(request.url()).pathname;
    if (route === '/install/configuration/approve') approvals++;
    if (route === '/install/services') applies++;
  });
  await page.goto(cfg.origin+'/install/');
  await page.locator('#bootstrap-token').fill(cfg.token);
  await page.locator('#installer-login button').click();
  await page.waitForFunction(() => !document.querySelector('#installer-approve').disabled);
  assert.equal(await page.locator('#installer-apply').isVisible(), false);
  const response = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/install/configuration/approve');
  await page.locator('#installer-approve').evaluate(button => { button.click(); button.click(); });
  assert.equal((await response).status(), 200);
  await page.waitForFunction(() => document.querySelector('#installer-title').textContent === 'Approved plan ready' && !document.querySelector('#installer-refresh').disabled);
  assert.equal(approvals, 1); assert.equal(applies, 0);
  assert.equal(await page.locator('#installer-apply').isEnabled(), true);
  assert.equal(await page.locator('#installer-approve').isVisible(), false);
  assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  const output = path.resolve('artifacts/v10/P21/payload/browser-approval');
  fs.mkdirSync(output, {recursive: true});
  await page.screenshot({path: path.join(output, 'confirmed-mobile.png'), fullPage: true});
  await page.reload();
  await page.locator('#installer-refresh').click();
  await page.waitForFunction(() => document.querySelector('#installer-title').textContent === 'Approved plan ready' && !document.querySelector('#installer-refresh').disabled);
  assert.equal(await page.locator('#installer-apply').isDisabled(), true);
  assert.equal(await page.locator('#installer-login').isVisible(), true);
  assert.equal(approvals, 1); assert.equal(applies, 0);
  assert.deepEqual(errors, []);
  fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify({real_root_approval_browser_acknowledgement: true,
    approval_posts: approvals, apply_posts: applies, refresh_requires_authentication_to_apply: true,
    no_browser_secret_storage: true, native_execution_verified_here: false})+'\n');
  await context.close();
} finally { await browser.close(); }
