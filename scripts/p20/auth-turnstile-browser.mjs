import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';
const origin = process.env.GOJET_TEST_SITE_URL ?? 'http://127.0.0.1:4184';
const executablePath = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].find((p) => p && existsSync(p));
if (!executablePath) throw new Error('Browser unavailable');
const checks = {};
const assert = (name, value) => { checks[name] = Boolean(value); if (!value) throw new Error(name); };
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] });
try {
  const page = await browser.newPage();
  await page.addInitScript(() => {
    window.__challengeFixture = { options: null, resets: 0 };
    window.turnstile = {
      render: (host, options) => { window.__challengeFixture.options = options; host.textContent = 'Verification fixture'; return 'fixture-widget'; },
      reset: () => { window.__challengeFixture.resets++; }, remove: () => {},
    };
  });
  let logins = 0;
  await page.route('**/api/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/api/public/auth/providers') {
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ providers: [], turnstile_required: true, turnstile_available: true, turnstile_site_key: 'fixture-site' }) });
    } else if (path === '/api/auth/login') {
      logins++;
      assert('token_sent_in_header', request.headers()['x-turnstile-token'] === 'fixture-once');
      await route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: { code: 'invalid_credentials', message: 'Sign in failed.' } }) });
    } else throw new Error('Unexpected authentication request');
  });
  await page.goto(`${origin}/login`);
  await page.getByText('Verification fixture', { exact: true }).waitFor();
  await page.locator('#login-email').fill('fixture@example.test');
  await page.locator('#login-password').fill('fixture-password');
  const submit = page.locator('form').getByRole('button', { name: 'Sign in', exact: true });
  await submit.click();
  await page.getByText('Complete verification before submitting.', { exact: true }).waitFor();
  assert('missing_token_does_not_submit', logins === 0);
  await page.evaluate(() => window.__challengeFixture.options.callback('fixture-once'));
  await submit.click();
  await page.getByText('Sign in failed.', { exact: true }).waitFor();
  assert('verification_consumed_once', logins === 1);
  assert('widget_reset_after_request', await page.evaluate(() => window.__challengeFixture.resets === 1));
  await submit.click();
  await page.getByText('Complete verification before submitting.', { exact: true }).waitFor();
  assert('consumed_token_not_reused', logins === 1);
  await page.evaluate(() => { window.__challengeFixture.options.callback('fixture-expired'); window.__challengeFixture.options['expired-callback'](); });
  await submit.click();
  assert('expired_token_not_submitted', logins === 1);
  assert('token_not_stored', await page.evaluate(() => !JSON.stringify({ ...localStorage, ...sessionStorage }).includes('fixture-once')));
} finally {
  await browser.close();
  const dir = 'artifacts/v10/P20/runtime/t025';
  mkdirSync(dir, { recursive: true });
  writeFileSync(`${dir}/auth-turnstile-browser.json`, JSON.stringify({ source_sha: process.env.GITHUB_SHA, checks, formal: false, external_turnstile_verification: false, scope: 'Built authentication page with explicit widget and API protocol fixtures' }, null, 2));
}
