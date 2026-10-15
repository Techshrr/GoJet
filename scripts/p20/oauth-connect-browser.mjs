import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const origin = process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://localhost:4185';
const executablePath = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].find((p) => p && existsSync(p));
if (!executablePath) throw new Error('Browser unavailable');
const checks = {};
const assert = (name, value) => { checks[name] = Boolean(value); if (!value) throw new Error(name); };
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] });
try {
  for (const provider of ['google', 'rainbow', 'x', 'linkedin']) {
    const context = await browser.newContext();
    const page = await context.newPage();
    let starts = 0;
    await page.route('**/api/**', async (route) => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      let body;
      if (path === '/api/me') body = { csrf_token: 'fixture-csrf', user: { display_name: 'Fixture' } };
      else if (path === '/api/public/auth/providers') body = { providers: ['google', 'rainbow', 'x', 'linkedin'].map((provider) => ({ provider, enabled: true })) };
      else if (path === '/api/me/connected-accounts') body = { accounts: [] };
      else if (path === `/api/me/connected-accounts/${provider}/start`) {
        starts++;
        assert(`${provider}_csrf_post`, request.method() === 'POST' && request.headers()['x-csrf-token'] === 'fixture-csrf');
        body = { authorization_url: `https://provider.example/authorize?provider=${provider}` };
      } else throw new Error('Unexpected connection API route');
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
    });
    await page.route('https://provider.example/**', (route) => route.fulfill({ contentType: 'text/html', body: '<main>Provider authorization fixture</main>' }));
    await page.goto(`${origin}/app/settings/connected-accounts`);
    await page.locator('[data-account-state="success"]').waitFor();
    await page.getByRole('button', { name: `Connect ${provider}`, exact: true }).click();
    await page.waitForURL(`https://provider.example/authorize?provider=${provider}`);
    assert(`${provider}_navigates_once`, starts === 1);
    await context.close();
  }
} finally {
  await browser.close();
  const dir = 'artifacts/v10/P20/runtime/t025';
  mkdirSync(dir, { recursive: true });
  writeFileSync(`${dir}/oauth-connect-browser.json`, JSON.stringify({ source_sha: process.env.GITHUB_SHA, checks, formal: false, external_provider_exchange_verified: false, scope: 'Built workspace navigation with intercepted protocol fixtures' }, null, 2));
}
