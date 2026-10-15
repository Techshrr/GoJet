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
  let submissions = 0;
  await page.route('**/api/public/contact', async (route) => {
    submissions++;
    assert('token_sent_to_server', route.request().postDataJSON().turnstile_token === 'fixture-contact-once');
    await route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: { code: 'turnstile_rejected' } }) });
  });
  await page.goto(`${origin}/contact`);
  await page.getByText('Verification fixture', { exact: true }).waitFor();
  await page.getByLabel('Name', { exact: true }).fill('Fixture');
  await page.getByLabel('Email', { exact: true }).fill('fixture@example.test');
  await page.getByLabel('Subject', { exact: true }).fill('Fixture subject');
  await page.getByLabel('Message', { exact: true }).fill('Fixture message');
  const submit = page.getByRole('button', { name: 'Send message', exact: true });
  assert('missing_token_disables_submit', await submit.isDisabled());
  await page.evaluate(() => window.__challengeFixture.options.callback('fixture-contact-once'));
  await submit.click();
  await page.locator('[data-page="contact"][data-state="Turnstile-error"]').waitFor();
  assert('server_denial_not_shown_as_success', submissions === 1);
  assert('token_consumed_before_retry', await submit.isDisabled());
  assert('widget_reset_after_attempt', await page.evaluate(() => window.__challengeFixture.resets === 1));
  await page.evaluate(() => { window.__challengeFixture.options.callback('fixture-expired'); window.__challengeFixture.options['expired-callback'](); });
  assert('expiry_blocks_submit', await submit.isDisabled());
  assert('token_not_in_storage', await page.evaluate(() => !JSON.stringify({ ...localStorage, ...sessionStorage }).includes('fixture-contact-once')));
} finally {
  await browser.close();
  const dir = 'artifacts/v10/P20/runtime/t025';
  mkdirSync(dir, { recursive: true });
  writeFileSync(`${dir}/support-turnstile-browser.json`, JSON.stringify({ source_sha: process.env.GITHUB_SHA, checks, formal: false, external_turnstile_verification: false, scope: 'Built production contact page with explicit widget and API protocol fixtures' }, null, 2));
}
