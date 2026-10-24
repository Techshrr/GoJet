// Actual shipped browser UI submits the sealed plan; Python verifies subsequent native apply.
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));
const browser = await chromium.launch({headless: true, args: ['--no-sandbox']});
try {
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ignoreHTTPSErrors: true});
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('console', msg => { if (msg.type() === 'error' && !msg.text().startsWith('Failed to load resource:')) errors.push(msg.text()); });
  await page.goto(cfg.origin + '/install/');
  await page.locator('#bootstrap-token').fill(cfg.token);
  const login = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/install/');
  await page.locator('#installer-login button').click();
  const authenticated = await login;
  assert.equal(authenticated.status(), 200);
  const auth = await authenticated.json();
  assert.equal(auth.step, 'services');
  await page.waitForFunction(() => document.querySelector('#preflight-summary').textContent.startsWith('All dependency checks passed.'));
  const submission = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/install/services');
  await page.locator('#installer-apply').click();
  const queued = await submission;
  assert.equal(queued.status(), 202);
  assert.deepEqual(await queued.json(), {step: 'services', status: 'checking'});
  await page.waitForFunction(() => document.querySelector('#installer-apply').hidden || document.querySelector('#installer-apply').disabled);
  assert.equal(await page.locator('#bootstrap-token').inputValue(), '');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  assert.deepEqual(errors, []);
  const cookie = (await context.cookies()).find(c => c.name === '__Secure-gojet_install');
  assert.ok(cookie?.secure && cookie.httpOnly && cookie.sameSite === 'Strict');
  const output = path.resolve('artifacts/v10/P21/payload/browser-apply');
  fs.mkdirSync(output, {recursive: true});
  await page.screenshot({path: path.join(output, 'submitted-desktop.png'), fullPage: true});
  fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify({real_browser_submission: true, secure_cookie: true, no_overflow: true, console_errors: 0, native_execution_verified_here: false})+'\n');
  // Private pipe back to the parent negative tests; never persisted as CI evidence.
  process.stdout.write(JSON.stringify({session: cookie.value, csrf: auth.csrf}));
  await context.close();
} finally { await browser.close(); }
