import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';
const origin = 'http://127.0.0.1:4187';
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
  let sessionReads = 0;
  await page.route('**/api/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    let body;
    if (path === '/api/me') { sessionReads++; body = { user: { id: 'fixture-user', email: 'fixture@example.test', display_name: 'Fixture' }, csrf_token: `csrf-${sessionReads}` }; }
    else if (path === '/api/workspaces') body = { items: [{ id: 'ws-one', name: 'First workspace', status: 'active' }, { id: 'ws-two', name: 'Second workspace', status: 'active' }] };
    else if (path === '/api/support/tickets' && request.method() === 'POST') {
      submissions++;
      const headers = request.headers();
      assert('fresh_csrf_attached', sessionReads >= 2 && headers['x-csrf-token'] === `csrf-${sessionReads}`);
      assert('idempotency_header_preserved', Boolean(headers['idempotency-key']));
      assert('no_fixture_identity_headers', !Object.keys(headers).some((key) => key.startsWith('x-gojet-test-')));
      assert('selected_workspace_submitted', request.postDataJSON().workspace_id === 'ws-two');
      assert('widget_token_submitted', request.postDataJSON().turnstile_token === 'fixture-support-once');
      await route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: { code: 'turnstile_rejected' } }) });
      return;
    } else throw new Error('Unexpected support request');
    await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
  });
  await page.goto(`${origin}/app/support/new`);
  await page.getByLabel('Support workspace').waitFor();
  assert('multiple_workspaces_require_selection', await page.getByLabel('Support workspace').inputValue() === '');
  await page.getByLabel('Support workspace').selectOption('ws-two');
  await page.getByLabel('Support subject').fill('Fixture subject');
  await page.getByLabel('Support message').fill('Fixture message');
  await page.getByText('Verification fixture', { exact: true }).waitFor();
  const submit = page.getByRole('button', { name: 'Submit ticket', exact: true });
  assert('missing_widget_token_blocks_submit', await submit.isDisabled());
  await page.evaluate(() => window.__challengeFixture.options.callback('fixture-support-once'));
  await submit.click();
  await page.locator('[data-page="support-new"][data-state="Turnstile-error"]').waitFor();
  assert('server_denial_not_success', submissions === 1);
  assert('consumed_token_blocks_retry', await submit.isDisabled());
  assert('widget_reset', await page.evaluate(() => window.__challengeFixture.resets === 1));
} finally {
  await browser.close();
  const dir = 'artifacts/v10/P20/runtime/t025';
  mkdirSync(dir, { recursive: true });
  writeFileSync(`${dir}/support-session-browser.json`, JSON.stringify({ source_sha: process.env.GITHUB_SHA, checks, formal: false, external_turnstile_verification: false, scope: 'Built production workspace support page with explicit widget and API protocol fixtures' }, null, 2));
}
