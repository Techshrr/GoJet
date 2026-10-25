// Real UI -> TLS/FPM draft submission. No state writes or fabricated approval records.
import {chromium} from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));
const browser = await chromium.launch({headless: true, args: ['--no-sandbox']});
try {
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ignoreHTTPSErrors: true});
  if (cfg.old_session) {
    const denied = await context.request.get(cfg.origin+'/install/', {
      headers: {Accept: 'application/json', Cookie: '__Secure-gojet_install='+cfg.old_session}
    });
    assert.equal(denied.status(), 401);
  }
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.name));
  let drafts = 0, applies = 0;
  page.on('request', request => {
    if (request.method() !== 'POST') return;
    if (new URL(request.url()).pathname === '/install/configuration') drafts++;
    if (new URL(request.url()).pathname === '/install/services') applies++;
  });
  await page.goto(cfg.origin+'/install/');
  await page.locator('#bootstrap-token').fill(cfg.token);
  await page.locator('#installer-login button').click();
  await page.waitForFunction(() => !document.querySelector('#installer-save-configuration').disabled);
  const c = cfg.draft.configuration;
  const fields = {origin: c.origin, turnstile_secret: c.turnstile_secret};
  for (const section of ['database', 'redis', 'files', 'quotas', 'smtp', 'risk', 'auth_rate']) {
    for (const [key, value] of Object.entries(c[section])) {
      if (section === 'files' && key === 'types') continue;
      fields[section+'-'+key] = value;
    }
  }
  for (const [key, value] of Object.entries(cfg.draft.admin)) fields['admin-'+key] = value;
  for (const [field, value] of Object.entries(fields)) await page.locator('#cfg-'+field).fill(String(value));
  await page.locator('#cfg-file-types').fill(Object.entries(c.files.types).map(([ext, mimes]) => ext+'='+mimes.join('|')).join('\n'));
  const response = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/install/configuration');
  await page.locator('#installer-save-configuration').click();
  assert.equal((await response).status(), 202);
  await page.waitForFunction(() => document.querySelector('#installer-title').textContent === 'Configuration submitted');
  assert.equal(drafts, 1); assert.equal(applies, 0);
  assert.equal(await page.locator('#cfg-admin-password').inputValue(), '');
  assert.equal(await page.locator('#installer-configuration').isVisible(), false);
  assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
  assert.deepEqual(errors, []);
  const evidence = cfg.evidence_name ?? 'browser-configuration';
  assert.ok(['browser-configuration', 'browser-replacement'].includes(evidence));
  const output = path.resolve('artifacts/v10/P21/payload', evidence);
  fs.mkdirSync(output, {recursive: true});
  await page.screenshot({path: path.join(output, 'saved-desktop.png'), fullPage: true});
  fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify({real_browser_configuration: true,
    draft_posts: drafts, apply_posts: applies, no_browser_secret_storage: true,
    previous_session_rejected: Boolean(cfg.old_session), root_preparation_verified_here: false})+'\n');
  await context.close();
} finally { await browser.close(); }
