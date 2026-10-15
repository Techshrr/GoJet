import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

// UI protocol fixture only: never presented as a live provider exchange.
const origin = process.env.GOJET_TEST_SITE_URL ?? 'http://127.0.0.1:4184';
const executablePath = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].find((p) => p && existsSync(p));
if (!executablePath) throw new Error('Browser unavailable');
const checks = {};
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] });
const requireCheck = (name, condition) => { checks[name] = Boolean(condition); if (!condition) throw new Error(name); };
try {
  for (const scenario of ['login', 'registration', 'binding']) {
    const context = await browser.newContext();
    const page = await context.newPage();
    const calls = { callback: 0, handoff: 0, complete: 0, me: 0 };
    const pageErrors = [];
    page.on('pageerror', () => pageErrors.push('pageerror'));
    await page.route('**/api/**', async (route) => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      let response;
      if (path === '/api/public/auth/providers') response = { providers: [], google_one_tap_enabled: false };
      else if (path === '/api/public/auth/google/callback') {
        calls.callback++;
        response = scenario === 'binding' ? { status: 'binding_required' } : { status: 'handoff_ready', handoff_code: 'fixture-handoff', expires_at: '2099-01-01T00:00:00Z' };
      } else if (path === '/api/public/auth/handoff') {
        calls.handoff++;
        response = scenario === 'registration' ? { status: 'registration_required', registration_code: 'fixture-registration' } : { status: 'authenticated' };
      } else if (path === '/api/me') {
        calls.me++;
        response = { csrf_token: 'fixture-session-csrf' };
      } else if (path === '/api/me/connected-accounts/google/complete') {
        calls.complete++;
        requireCheck('binding_csrf_header', request.headers()['x-csrf-token'] === 'fixture-session-csrf');
        requireCheck('binding_post', request.method() === 'POST');
        const body = request.postDataJSON();
        requireCheck('binding_callback_input', body.state === 'fixture-state' && body.code === 'fixture-code');
        response = { status: 'connected' };
      } else throw new Error('Unexpected callback API route');
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(response) });
    });
    await page.route('**/app', (route) => route.fulfill({ contentType: 'text/html', body: '<main>Signed in</main>' }));
    await page.route('**/app/settings/connected-accounts', (route) => route.fulfill({ contentType: 'text/html', body: '<main>Connected</main>' }));
    await page.goto(`${origin}/oauth/google/callback?state=fixture-state&code=fixture-code`);
    if (scenario === 'registration') {
      await page.getByRole('link', { name: 'Continue registration' }).waitFor();
      requireCheck('registration_continuation', await page.getByRole('link', { name: 'Continue registration' }).getAttribute('href') === '/social-registration?code=fixture-registration');
    } else {
      await page.waitForURL(`${origin}${scenario === 'binding' ? '/app/settings/connected-accounts' : '/app'}`);
    }
    requireCheck(`${scenario}_callback_once`, calls.callback === 1);
    requireCheck(`${scenario}_correct_exchange`, scenario === 'binding' ? calls.me === 1 && calls.complete === 1 && calls.handoff === 0 : calls.handoff === 1 && calls.complete === 0);
    requireCheck(`${scenario}_credentials_removed`, !page.url().includes('fixture-state') && !page.url().includes('fixture-code'));
    const storage = await page.evaluate(() => Object.keys(localStorage).length + Object.keys(sessionStorage).length);
    requireCheck(`${scenario}_no_browser_storage`, storage === 0);
    requireCheck(`${scenario}_no_page_error`, pageErrors.length === 0);
    await context.close();
  }
} finally {
  await browser.close();
  const dir = 'artifacts/v10/P20/runtime/t025';
  mkdirSync(dir, { recursive: true });
  writeFileSync(`${dir}/oauth-callback-browser.json`, JSON.stringify({ source_sha: process.env.GITHUB_SHA, checks, formal: false, external_provider_exchange_verified: false, scope: 'Browser UI with intercepted protocol responses' }, null, 2));
}
