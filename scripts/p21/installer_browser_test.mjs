// Real Chromium -> native TLS/Nginx/FPM; prepared-plan fixture, no privileged apply claim.
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));
const output = path.resolve('artifacts/v10/P21/native-web');
fs.mkdirSync(output, {recursive: true});
const browser = await chromium.launch({headless: true, args: ['--no-sandbox']});
try {
  for (const [label, viewport] of [['desktop', {width: 1280, height: 900}], ['mobile', {width: 390, height: 844}]]) {
    // Explicit test fixture only. Root approval/seal and actual execution are covered by payload CI.
    if (fs.existsSync(cfg.completion)) fs.unlinkSync(cfg.completion);
    const stateFile = path.join(cfg.state, 'state.json');
    const state = JSON.parse(fs.readFileSync(stateFile));
    Object.assign(state, {step: 4, status: 'ready', session: null, csrf: null, expires: 0, attempts: 0, retry_at: 0});
    fs.writeFileSync(stateFile, JSON.stringify(state));
    const approval = path.join(cfg.state, 'apply-approval.json');
    fs.writeFileSync(approval, JSON.stringify({schema: 'gojet.web-apply.v1', seal_sha256: 'a'.repeat(64)}), {mode: 0o600});
    fs.chownSync(approval, cfg.uid, cfg.gid);
    const request = path.join(cfg.state, 'apply-request.json');
    if (fs.existsSync(request)) fs.unlinkSync(request);
    // Fixture certificate trust is verified by the native Python TLS tests; browser uses a self-signed fixture.
    const context = await browser.newContext({viewport, ignoreHTTPSErrors: true});
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    page.on('console', msg => { if (msg.type() === 'error' && !msg.text().startsWith('Failed to load resource:')) errors.push(msg.text()); });
    let submissions = 0;
    page.on('request', req => { if (req.method() === 'POST' && req.url().endsWith('/install/services')) submissions++; });
    const first = await page.goto(cfg.origin+'/install/');
    assert.equal(first.status(), 401);
    assert.match(first.headers()['content-security-policy'], /script-src 'sha256-/);
    assert.equal(await page.locator('#installer-login').getAttribute('method'), 'post');
    assert.equal(await page.locator('#installer-login').getAttribute('action'), '/install/');
    await page.locator('#bootstrap-token').waitFor();
    await page.keyboard.press('Tab');
    assert.equal(await page.locator('#bootstrap-token').evaluate(el => el === document.activeElement), true);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await page.screenshot({path: path.join(output, label+'-login.png'), fullPage: true});
    await page.getByLabel('Installation access token', {exact: true}).fill('wrong-token-with-at-least-thirty-two-characters');
    await page.getByRole('button', {name: 'Authenticate', exact: true}).click();
    await page.waitForFunction(() => document.querySelector('#installer-message').textContent.startsWith('Authentication required.'));
    assert.equal(fs.existsSync(request), false);
    await page.getByLabel('Installation access token', {exact: true}).fill(cfg.token);
    await page.getByRole('button', {name: 'Authenticate', exact: true}).click();
    await page.getByRole('button', {name: 'Submit approved installation', exact: true}).waitFor();
    assert.equal(await page.locator('#bootstrap-token').inputValue(), '');
    assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    // Real click sends one request; immediate repeated activation cannot duplicate it.
    await page.getByRole('button', {name: 'Submit approved installation', exact: true}).click();
    await page.waitForFunction(() => document.querySelector('#installer-message').textContent.startsWith('Installation request submitted.'));
    assert.equal(submissions, 1);
    assert.equal(JSON.parse(fs.readFileSync(request)).seal_sha256, 'a'.repeat(64));
    await page.getByRole('button', {name: 'Refresh status', exact: true}).click();
    await page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
    assert.equal(submissions, 1);
    await page.screenshot({path: path.join(output, label+'-queued.png'), fullPage: true});
    await context.setOffline(true);
    await page.getByRole('button', {name: 'Refresh status', exact: true}).click();
    await page.waitForFunction(() => document.querySelector('#installer-message').textContent.startsWith('The server could not be reached.'));
    await context.setOffline(false);
    fs.writeFileSync(cfg.completion, '', {mode: 0o600});
    await page.getByRole('button', {name: 'Refresh status', exact: true}).click();
    await page.waitForFunction(() => document.querySelector('#installer-message').textContent.startsWith('The installer is closed or unavailable.'));
    assert.equal(await page.locator('#installer-apply').isVisible(), false);
    assert.equal(submissions, 1);
    assert.deepEqual(errors, []);
    await context.close();
    // With scripts disabled, the browser must never put the token into a GET URL.
    if (label === 'desktop') {
      fs.unlinkSync(cfg.completion);
      const noJS = await browser.newContext({javaScriptEnabled: false, ignoreHTTPSErrors: true});
      const fallback = await noJS.newPage();
      await fallback.goto(cfg.origin+'/install/');
      await fallback.getByLabel('Installation access token', {exact: true}).fill('non-secret-no-js-fixture-token-0123456789');
      const response = fallback.waitForResponse(r => r.request().method() === 'POST');
      await fallback.getByRole('button', {name: 'Authenticate', exact: true}).click();
      assert.equal((await response).status(), 415);
      assert.equal(new URL(fallback.url()).search, '');
      await noJS.close();
    }
  }
} finally { await browser.close(); }
console.log(JSON.stringify({desktop_mobile_browser: true, real_tls_fpm_browser_requests: true,
  keyboard_access: true, no_horizontal_overflow: true, authentication_and_apply_submission: true,
  no_secret_browser_storage: true, no_script_post_only_verified: true, offline_recovery: true, post_lock_closed: true,
  unexpected_console_errors: 0, approved_plan_is_fixture: true, privileged_apply_verified: false}));
