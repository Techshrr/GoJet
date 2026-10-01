import assert from 'node:assert/strict';
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';
const { origin, tokens, workspace } = JSON.parse(process.env.P20_BROWSER_HANDOFF);
delete process.env.P20_BROWSER_HANDOFF;
const executablePath = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].find(p => p && existsSync(p));
assert(executablePath, 'Chrome unavailable');
const browser = await chromium.launch({ executablePath, args: ['--no-sandbox'] });
const checks = {};
try {
  for (const role of ['owner', 'admin', 'member', 'viewer', 'anonymous']) {
    const context = await browser.newContext({ ignoreHTTPSErrors: true });
    if (tokens[role]) await context.addCookies([{ name: '__Host-gojet_session', value: tokens[role], url: origin, secure: true, httpOnly: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    let fixtureHeader = false;
    page.on('request', request => { if (Object.keys(request.headers()).some(k => k.startsWith('x-gojet-test-'))) fixtureHeader = true; });
    await page.goto(`${origin}/app/settings/workspace`, { waitUntil: 'domcontentloaded' });
    if (role === 'anonymous') {
      await page.waitForResponse(r => new URL(r.url()).pathname === '/api/me' && r.status() === 401).catch(async () => {
        assert.equal(await page.evaluate(async () => (await fetch('/api/me')).status), 401);
      });
      assert.equal(await page.getByLabel('Workspace name', { exact: true }).count(), 0);
      assert(!(await page.locator('body').innerText()).includes('P20 browser admin saved'));
    } else {
      const allowed = role === 'owner' || role === 'admin';
      await page.locator(`[data-page="workspace-settings"][data-state="${allowed ? 'edit' : 'read-only'}"]`).waitFor();
      const field = page.getByLabel('Workspace name', { exact: true });
      assert.equal(await field.isDisabled(), !allowed);
      if (allowed) {
        await field.fill(`P20 browser ${role} saved`);
        const response = page.waitForResponse(r => r.request().method() === 'PATCH' && new URL(r.url()).pathname === `/api/workspaces/${workspace}`);
        await page.getByRole('button', { name: 'Save Workspace settings', exact: true }).click();
        assert.equal((await response).status(), 200);
      } else {
        const status = await page.evaluate(async (id) => {
          const current = await (await fetch('/api/me')).json();
          return (await fetch(`/api/workspaces/${id}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': current.csrf_token }, body: JSON.stringify({ name: 'unauthorized', version: 1, reason: 'RBAC browser probe' }) })).status;
        }, workspace);
        assert.equal(status, 403);
      }
    }
    assert.equal(fixtureHeader, false);
    checks[role] = true;
    await context.close();
  }
  mkdirSync('artifacts/v10/P20/runtime/t027', { recursive: true });
  writeFileSync('artifacts/v10/P20/runtime/t027/workspace-browser.json', JSON.stringify({ implementation_commit: process.env.GITHUB_SHA, checks, production_session: true, mocked_api: false, formal_p20_t027_claim: false }) + '\n');
} finally { await browser.close(); }
