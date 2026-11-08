import { createHmac } from 'node:crypto';
import { chromium } from 'playwright-core';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';

const origin = process.env.P22_ADMIN_ORIGIN;
assert(origin && process.env.P22_DISPOSABLE_ADMIN_CI === '1');
const output = 'artifacts/v10/P22/admin-repair';
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const results = [];
let totpSecret = '';
function totp(secret) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  const bits = [...secret.replace(/=+$/, '').toUpperCase()].map(c => alphabet.indexOf(c).toString(2).padStart(5, '0')).join('');
  const key = Buffer.from((bits.match(/.{8}/g) || []).map(byte => parseInt(byte, 2)));
  const counter = Buffer.alloc(8); counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000)));
  const digest = createHmac('sha1', key).update(counter).digest();
  return String((digest.readUInt32BE(digest[19] & 15) & 0x7fffffff) % 1000000).padStart(6, '0');
}
try {
  for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
    const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', req => {
      assert(!Object.keys(req.headers()).some(key => key.startsWith('x-gojet-test-')), 'test identity headers used');
    });
    await page.goto(`${origin}/admin/login`);
    await page.getByLabel('Email', { exact: true }).fill(process.env.P22_ADMIN_EMAIL);
    await page.getByLabel('Password', { exact: true }).fill(process.env.P22_ADMIN_PASSWORD);
    if (totpSecret) await page.getByLabel('TOTP code', { exact: true }).fill(totp(totpSecret));
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await page.getByText('Authentication complete.', { exact: true }).waitFor();
    const routes = [
      ['/admin/operations/jobs', 'No jobs found.'], ['/admin/operations/services', 'redirectengine'],
      ['/admin/users', 'No users found.'], ['/admin/workspaces', 'No workspaces found.'],
      ['/admin/trust/destination-risk', 'No destination risk records'],
      ['/admin/trust/domain-risk', 'No domain risk records'],
      ['/admin/trust/abuse', 'No abuse reports'],
      ['/admin/tickets', 'No tickets'], ['/admin/mail', 'Dispatch settings'],
      ['/admin/commerce/plans', 'Create draft plan'], ['/admin/commerce/payments', 'No payment records'],
      ['/admin/commerce/fx', 'No FX rates'],
      ['/admin/platform/general', 'General settings have not been configured yet.'],
    ];
    // The desktop run saves settings; mobile should show the saved form instead.
    if (viewport.width < 500) routes[routes.length - 1][1] = 'Save general settings';
    for (const [path, text] of routes) {
      const failures = [];
      const responseListener = response => { if (response.url().includes('/api/') && response.status() >= 400) failures.push(`${response.status()} ${new URL(response.url()).pathname}`); };
      page.on('response', responseListener);
      await page.goto(origin + path);
      await page.getByText(text, { exact: true }).first().waitFor();
      await page.waitForLoadState('networkidle');
      if (path === '/admin/operations/services') assert.equal(await page.locator('.p17-list > li').count(), 8);
      assert.deepEqual(failures, [], `${path} API failures`);
      assert.equal(await page.getByText('Administrator authentication is required.', { exact: true }).count(), 0);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2), false, `${path} horizontal overflow`);
      await page.screenshot({ path: `${output}/${viewport.width}-${path.split('/').filter(Boolean).join('-')}.png`, fullPage: true });
      page.off('response', responseListener);
      results.push({ viewport: viewport.width, path, passed: true });
    }
    if (viewport.width > 500) {
      // Exercise real TOTP enrollment; high-risk settings keep their MFA boundary.
      await page.goto(origin + '/admin/access/administrators');
      await page.getByRole('button', { name: 'Enroll TOTP', exact: true }).click();
      await page.locator('[data-secret-once] code').waitFor();
      totpSecret = await page.locator('[data-secret-once] code').innerText();
      await page.getByLabel('Six-digit code — TOTP confirmation code', { exact: true }).fill(totp(totpSecret));
      await page.getByRole('button', { name: 'Confirm TOTP', exact: true }).click();
      await page.getByText('TOTP enabled', { exact: true }).waitFor();
      await page.goto(origin + '/admin/platform/general');
      await page.getByLabel('Site name', { exact: true }).fill('Native admin CI');
      await page.getByLabel('Public HTTPS address', { exact: true }).fill('https://example.test');
      await page.getByLabel('Support HTTPS address', { exact: true }).fill('https://example.test/support');
      await page.getByLabel('Reason for general settings change').fill('Native first-install regression');
      await page.getByRole('button', { name: 'Save general settings', exact: true }).click();
      await page.getByText('General settings saved.', { exact: true }).waitFor();
      await page.getByLabel('Reason for change', { exact: true }).fill('Keep unconfigured Google login disabled');
      await page.getByRole('button', { name: 'Save One Tap preference', exact: true }).click();
      await page.getByText('Google One Tap preference saved.', { exact: true }).waitFor();
      await page.goto(origin + '/admin/commerce/plans');
      await page.getByLabel('Code', { exact: true }).fill('native-ci');
      await page.getByLabel('Name', { exact: true }).fill('Native CI draft');
      const created = page.waitForResponse(r => r.url().endsWith('/api/admin/plans') && r.request().method() === 'POST');
      await page.getByRole('button', { name: 'Create draft', exact: true }).click();
      assert.equal((await created).status(), 201);
      await page.getByText('Native CI draft', { exact: true }).waitFor();
      await page.goto(origin + '/admin/mail');
      const checkbox = page.getByLabel('Mail delivery enabled', { exact: true });
      await checkbox.waitFor();
      await checkbox.setChecked(!(await checkbox.isChecked()));
      const saved = page.waitForResponse(r => r.url().endsWith('/api/admin/mail/settings') && r.request().method() === 'PATCH');
      await page.getByRole('button', { name: 'Save settings', exact: true }).click();
      assert.equal((await saved).status(), 200);
      // No test mail is queued and no SMTP worker or payment provider runs here.
    }
    assert.deepEqual(errors, [], 'browser runtime errors');
    await context.close();
  }
  await writeFile(`${output}/browser.json`, JSON.stringify({ passed: true, native_cookie_auth: true, external_mail_sent: false, checks: results }, null, 2));
  console.log(JSON.stringify({ native_browser_passed: true, pages: results.length, widths: [1440, 390] }));
} finally { await browser.close(); }
