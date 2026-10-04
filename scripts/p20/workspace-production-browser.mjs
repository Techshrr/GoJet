import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';
const implementationCommit = execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
assert.match(implementationCommit, /^[0-9a-f]{40}$/);
const { origin, tokens, workspace, adminOrigin, adminToken } = JSON.parse(process.env.P20_BROWSER_HANDOFF);
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
  for (const role of ['anonymous', 'owner', 'admin', 'member', 'viewer', 'limited-admin']) {
    const context = await browser.newContext({ ignoreHTTPSErrors: true, extraHTTPHeaders: { 'X-Role': 'admin', 'X-GoJet-Test-Admin-Permissions': 'admins.manage' } });
    if (tokens[role]) await context.addCookies([{ name: '__Host-gojet_session', value: tokens[role], url: adminOrigin, secure: true, httpOnly: true, sameSite: 'Lax' }]);
    if (role === 'limited-admin') await context.addCookies([{ name: 'gojet_admin_session', value: adminToken, url: adminOrigin, secure: true, httpOnly: true, sameSite: 'Strict' }]);
    await context.addInitScript(() => localStorage.setItem('role', 'admin'));
    const page = await context.newPage();
    await page.goto(`${adminOrigin}/admin/access/roles`, { waitUntil: 'domcontentloaded' });
    await page.getByText('Your administrator permission does not authorize this operation.', { exact: true }).waitFor();
    if (role === 'limited-admin') await page.locator('[data-page="admin-roles"][data-state="permission-denied"]').waitFor();
    const result = await page.evaluate(async () => {
      const response = await fetch('/api/admin/roles');
      return { status: response.status, body: await response.json() };
    });
    assert.equal(result.status, role === 'limited-admin' ? 403 : 401);
    assert.equal(result.body.items, undefined);
    assert(!(await page.locator('body').innerText()).includes('browser-limited@p20.test'));
    checks[`admin-route-${role}`] = true;
    await context.close();
  }
  mkdirSync('artifacts/v10/P20/runtime/t027', { recursive: true });
  writeFileSync('artifacts/v10/P20/runtime/t027/workspace-browser.json', JSON.stringify({ implementation_commit: implementationCommit, checks, production_session: true, mocked_api: false, formal_p20_t027_claim: false }) + '\n');
} finally { await browser.close(); }
