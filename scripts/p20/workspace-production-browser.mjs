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
    // Follow the actual sidebar link under the same production session.
    await page.locator('nav[aria-label="Workspace navigation"] a[href="/app/settings/profile"]').click();
    await page.waitForURL(url => url.pathname === '/app/settings/profile');
    await page.getByRole('heading', { name: 'Profile', exact: true }).waitFor();
    await page.locator(`[data-account-state="${role === 'anonymous' ? 'session-revoked' : 'success'}"]`).waitFor();
    checks[`profile-navigation-${role}`] = true;
    await page.getByRole('link', { name: 'Danger zone', exact: true }).click();
    await page.waitForURL(url => url.pathname === '/app/settings/danger');
    await page.locator(`[data-account-state="${role === 'anonymous' ? 'session-revoked' : 'success'}"]`).waitFor();
    assert.equal(await page.getByRole('button', { name: 'Revoke current session', exact: true }).count(), role === 'anonymous' ? 0 : 1);
    checks[`danger-navigation-${role}`] = true;

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
    await page.locator('nav[aria-label="Admin navigation"] a[href="/admin/operations/jobs"]').click();
    await page.waitForURL(url => url.pathname === '/admin/operations/jobs');
    await page.getByRole('heading', { name: 'Jobs', exact: true }).waitFor();
    await page.getByText('Your administrator permission does not authorize this operation.', { exact: true }).waitFor();
    const jobsStatus = await page.evaluate(async () => (await fetch('/api/admin/operations/jobs')).status);
    assert.equal(jobsStatus, role === 'limited-admin' ? 403 : 401);
    checks[`operations-navigation-${role}`] = true;
    checks[`admin-route-${role}`] = true;
    await context.close();
  }

  {
    const context = await browser.newContext({ ignoreHTTPSErrors: true });
    await context.addCookies([{ name: '__Host-gojet_session', value: tokens.owner, url: origin, secure: true, httpOnly: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    await page.goto(`${origin}/app/settings/danger`);
    await page.getByRole('button', { name: 'Revoke current session', exact: true }).click();
    assert.equal(await page.getByRole('button', { name: 'Confirm sign out', exact: true }).isDisabled(), true);
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();
    assert.equal(await page.evaluate(async () => (await fetch('/api/me')).status), 200);
    await page.getByRole('button', { name: 'Revoke current session', exact: true }).click();
    await page.getByLabel('Type SIGN OUT', { exact: true }).fill('SIGN OUT');
    const response = page.waitForResponse(r => r.request().method() === 'DELETE' && new URL(r.url()).pathname.startsWith('/api/me/sessions/'));
    await page.getByRole('button', { name: 'Confirm sign out', exact: true }).click();
    assert.equal((await response).status(), 200);
    await page.locator('[data-account-state="session-revoked"]').waitFor();
    const revoked = await page.evaluate(async () => { const response = await fetch('/api/me'); return { status: response.status, body: await response.json() }; });
    assert.equal(revoked.status, 410);
    assert.equal(revoked.body.error.code, 'revoked_token');
    checks['danger-revocation-durable'] = true;
    await context.close();
  }
  {
    const context = await browser.newContext({ ignoreHTTPSErrors: true });
    await context.addCookies([{ name: 'gojet_admin_session', value: adminToken, url: adminOrigin, secure: true, httpOnly: true, sameSite: 'Strict' }]);
    const page = await context.newPage();
    await page.goto(`${adminOrigin}/admin/platform/mail-templates`);
    await page.locator('[data-page="admin-mail-templates"][data-state="ready"]').waitFor();
    await page.getByRole('link', { name: 'mail-test · en', exact: true }).click();
    await page.locator('[data-page="admin-mail-templates"][data-state="edit"]').waitFor();
    const current = await page.evaluate(async () => (await (await fetch('/api/admin/mail/templates')).json()).items.filter(item => item.key === 'mail-test' && item.locale === 'en').sort((a,b) => b.version-a.version)[0]);
    await page.getByLabel('Subject', { exact: true }).fill('P20 template edited');
    await page.getByRole('button', { name: 'Preview with sample values', exact: true }).click();
    await page.locator('[data-page="admin-mail-templates"][data-state="preview"]').waitFor();
    assert((await page.getByRole('region', { name: 'Template preview' }).innerText()).includes('P20 template edited'));
    const save = page.waitForResponse(r => r.request().method() === 'PATCH' && new URL(r.url()).pathname === '/api/admin/mail/templates/mail-test');
    await page.getByRole('button', { name: 'Save template', exact: true }).click();
    assert.equal((await save).status(), 200);
    await page.locator('[data-page="admin-mail-templates"][data-state="saved"]').waitFor();
    const result = await page.evaluate(async original => {
      const list = (await (await fetch('/api/admin/mail/templates')).json()).items.filter(item => item.key === 'mail-test' && item.locale === 'en');
      const session = await (await fetch('/api/admin/auth/session')).json();
      const stale = await fetch('/api/admin/mail/templates/mail-test', { method: 'PATCH', headers: {'Content-Type':'application/json','X-CSRF-Token':session.csrf_token}, body: JSON.stringify({locale:'en',expected_version:original.version,subject_template:'stale overwrite',text_template:original.text_template,html_template:original.html_template}) });
      const noCSRF = await fetch('/api/admin/mail/templates/mail-test', { method:'PATCH',headers:{'Content-Type':'application/json'},body:'{}' });
      return { list, stale:stale.status, noCSRF:noCSRF.status };
    }, current);
    assert.equal(result.stale, 409);
    assert.equal(result.noCSRF, 401);
    assert(result.list.some(item => item.version === current.version && item.subject_template === current.subject_template));
    assert(result.list.some(item => item.version === current.version+1 && item.subject_template === 'P20 template edited'));
    checks['mail-template-preview-save-conflict'] = true;
    await context.close();
    const anonymous = await browser.newContext({ignoreHTTPSErrors:true});
    const denied = await anonymous.request.get(`${adminOrigin}/api/admin/mail/templates`);
    assert.equal(denied.status(),401);
    const body = await denied.json();assert.equal(body.items,undefined);
    checks['mail-template-anonymous-denied'] = true;
    await anonymous.close();
  }
  mkdirSync('artifacts/v10/P20/runtime/t027', { recursive: true });
  writeFileSync('artifacts/v10/P20/runtime/t027/workspace-browser.json', JSON.stringify({ implementation_commit: implementationCommit, checks, production_session: true, mocked_api: false, formal_p20_t027_claim: false }) + '\n');
} finally { await browser.close(); }
