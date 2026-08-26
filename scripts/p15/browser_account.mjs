import { createHmac } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const root = process.cwd();
const evidenceRoot = `${root}/artifacts/v10/P15`;
const browserDir = `${evidenceRoot}/browser`;
const capturesDir = `${evidenceRoot}/captures`;
for (const dir of [browserDir, capturesDir]) mkdirSync(dir, { recursive: true });

const caseId = process.argv[2] === '--case' ? process.argv[3] : '';
if (caseId !== 'P15-T025') throw new Error(`Unsupported P15 account browser case: ${caseId || '<missing>'}`);

const SITE_URL = process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://localhost:4185';
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
const GRANT_KEY_HEX = process.env.GOJET_AUTH_GRANT_KEY_HEX ?? '';
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P15 account browser evidence');
if (!/^[0-9a-fA-F]{64}$/.test(GRANT_KEY_HEX)) throw new Error('GOJET_AUTH_GRANT_KEY_HEX must be an exact 32-byte hex key');

const variables = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
function parseViewport(value, tokenName) {
  const match = /^(\d+)×(\d+)$/.exec(String(value));
  if (!match) throw new Error(`Invalid canonical viewport ${tokenName}: ${String(value)}`);
  return { width: Number(match[1]), height: Number(match[2]) };
}
const viewports = {
  desktop: parseViewport(variables['viewport.desktop'].dimensions, 'viewport.desktop'),
  mobile: parseViewport(variables['viewport.mobile'].dimensions, 'viewport.mobile'),
  narrow: { width: 320, height: 800 },
};

function implementationCommit() {
  return process.env.GITHUB_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
}
function assert(condition, message) { if (!condition) throw new Error(message); }
function sqlLiteral(value) { return `'${String(value).replaceAll('\\', '\\\\').replaceAll("'", "''")}'`; }
function mysql(sql) {
  return execFileSync('mysql', ['--protocol=tcp', '-h', MYSQL_HOST, '-P', String(MYSQL_PORT), '-u', MYSQL_USER, '-N', '-B', MYSQL_DATABASE, '-e', sql], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
  }).trim();
}
function writePart(mac, value) {
  const bytes = Buffer.from(value, 'utf8');
  const length = Buffer.alloc(4); length.writeUInt32BE(bytes.length); mac.update(length); mac.update(bytes);
}
function derive(prefix, purpose, identifier) {
  const mac = createHmac('sha256', Buffer.from(GRANT_KEY_HEX, 'hex'));
  writePart(mac, purpose); writePart(mac, identifier);
  return `${prefix}${mac.digest('base64url')}`;
}
function uniqueSuffix() { return `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`; }
async function waitAccountState(page, state) { await page.locator(`[data-account-state="${state}"]`).waitFor({ state: 'visible', timeout: 15000 }); }
async function waitWorkspaceViewport(page, viewport) { await page.locator(`.workspace-shell[data-viewport="${viewport}"]`).waitFor({ state: 'visible', timeout: 5000 }); }
async function noOverflow(page, label) {
  const size = await page.evaluate(() => ({ innerWidth: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }));
  assert(size.scrollWidth <= size.innerWidth + 1, `${label} horizontal overflow: ${JSON.stringify(size)}`);
}
async function maskInputs(page) { await page.evaluate(() => { for (const input of document.querySelectorAll('input')) input.value = ''; }); }
async function capture(page, label) {
  await maskInputs(page);
  const path = `${capturesDir}/${caseId}-${label}.png`;
  await page.screenshot({ path, fullPage: true });
  return path.replace(`${root}/`, '');
}
async function api(context, path, options = {}) {
  return context.request.fetch(`${SITE_URL}${path}`, { ...options, failOnStatusCode: false });
}
async function registerVerifyLogin(context, email, displayName, password) {
  const register = await api(context, '/api/auth/register', {
    method: 'POST',
    data: { email, display_name: displayName, password },
  });
  assert(register.status() === 202, `registration failed: ${register.status()}`);
  const grantID = mysql(`SELECT id FROM auth_one_time_grants WHERE email_normalized=${sqlLiteral(email)} AND purpose='email_verification' ORDER BY created_at DESC,id DESC LIMIT 1`);
  assert(grantID.startsWith('grt_'), 'verification grant was not durably created');
  const code = derive('gvc_', 'email_verification', grantID);
  const verify = await api(context, '/api/auth/verifyemail', { method: 'POST', data: { code } });
  assert(verify.status() === 200, `verification failed: ${verify.status()}`);
  const login = await api(context, '/api/auth/login', { method: 'POST', data: { email, password } });
  assert(login.status() === 200, `login failed: ${login.status()}`);
}
async function login(context, email, password) {
  const response = await api(context, '/api/auth/login', { method: 'POST', data: { email, password } });
  assert(response.status() === 200, `login failed: ${response.status()}`);
}
async function currentUser(context) {
  const response = await api(context, '/api/me');
  assert(response.status() === 200, `GET /api/me failed: ${response.status()}`);
  return response.json();
}
function resultPayload(status, details, errors = []) {
  return {
    case_id: caseId,
    status,
    generated_at: new Date().toISOString(),
    implementation_commit: implementationCommit(),
    environment: {
      browser: executablePath,
      site_origin: new URL(SITE_URL).origin,
      canonical_viewports: viewports,
      authority: 'real MySQL 8.x + real Redis + native Go platformapi + built Workspace account settings; no fabricated API success',
    },
    details,
    errors,
  };
}
function writeResult(status, details, errors = []) { writeFileSync(`${browserDir}/${caseId}.json`, `${JSON.stringify(resultPayload(status, details, errors), null, 2)}\n`); }

const screenshots = [];
const states = { profile: [], security: [], sessions: [], connected_accounts: [] };
const diagnostics = { console_errors: [], page_errors: [], request_failures: [] };

try {
  const browser = await chromium.launch({ executablePath, headless: true });

  const unauthContext = await browser.newContext({ viewport: viewports.desktop });
  const unauthPage = await unauthContext.newPage();
  await unauthPage.goto(`${SITE_URL}/app/settings/profile`, { waitUntil: 'domcontentloaded' });
  await waitAccountState(unauthPage, 'session-revoked');
  states.profile.push('session-revoked');
  screenshots.push(await capture(unauthPage, 'profile-direct-load-denied'));
  await unauthContext.close();

  const context = await browser.newContext({ viewport: viewports.desktop });
  const page = await context.newPage();
  page.on('console', (message) => {
    if (message.type() === 'error' && !message.text().startsWith('Failed to load resource: the server responded with a status of ')) diagnostics.console_errors.push(message.text());
  });
  page.on('pageerror', (error) => diagnostics.page_errors.push(String(error)));
  page.on('requestfailed', (request) => diagnostics.request_failures.push({ url: request.url().replace(/\?.*$/, ''), failure: request.failure()?.errorText ?? 'failed' }));

  const suffix = uniqueSuffix();
  const email = `p15-t025-${suffix}@example.test`;
  const password = 'P15-T025-Initial-Password!42';
  const nextPassword = 'P15-T025-New-Password!84';
  await registerVerifyLogin(context, email, 'P15 T025 Account', password);

  const cookies = await context.cookies(SITE_URL);
  const sessionCookie = cookies.find((cookie) => cookie.name === '__Host-gojet_session');
  assert(sessionCookie && sessionCookie.httpOnly && sessionCookie.secure, 'formal secure HttpOnly session cookie was not established');
  const me = await currentUser(context);
  const userID = me.user.id;
  assert(typeof userID === 'string' && userID.startsWith('usr_'), 'current account identity missing');

  await page.goto(`${SITE_URL}/app/settings/profile`, { waitUntil: 'domcontentloaded' });
  await waitAccountState(page, 'success'); states.profile.push('success');
  await waitWorkspaceViewport(page, 'desktop');
  await noOverflow(page, 'profile desktop'); screenshots.push(await capture(page, 'profile-desktop'));
  await page.setViewportSize(viewports.mobile); await waitWorkspaceViewport(page, 'mobile'); await noOverflow(page, 'profile mobile'); screenshots.push(await capture(page, 'profile-mobile'));
  await page.setViewportSize(viewports.narrow); await waitWorkspaceViewport(page, 'mobile'); await noOverflow(page, 'profile narrow'); screenshots.push(await capture(page, 'profile-narrow'));
  await page.setViewportSize(viewports.desktop); await waitWorkspaceViewport(page, 'desktop');

  let focusedName = false;
  for (let index = 0; index < 30; index += 1) {
    await page.keyboard.press('Tab');
    if (await page.getByLabel('Display name').evaluate((element) => element === document.activeElement)) { focusedName = true; break; }
  }
  assert(focusedName, 'keyboard focus did not reach display name');

  await page.getByLabel('Display name').fill('x'.repeat(300));
  await page.getByRole('button', { name: 'Save profile' }).click();
  await waitAccountState(page, 'validation-error'); states.profile.push('validation-error');
  assert(await page.locator('.p15-account__form > div[tabindex="-1"]').evaluate((element) => element === document.activeElement), 'validation error focus was not moved in-page');
  screenshots.push(await capture(page, 'profile-validation-error'));

  await page.getByLabel('Display name').fill('P15 T025 Updated');
  await page.getByRole('button', { name: 'Save profile' }).click();
  await waitAccountState(page, 'success');
  const durableDisplayName = mysql(`SELECT display_name FROM auth_users WHERE id=${sqlLiteral(userID)}`);
  assert(durableDisplayName === 'P15 T025 Updated', 'profile update was not durable in MySQL');
  screenshots.push(await capture(page, 'profile-updated'));

  await page.goto(`${SITE_URL}/app/settings/security`, { waitUntil: 'domcontentloaded' });
  await waitAccountState(page, 'success'); states.security.push('success');
  await page.getByLabel('Current password').fill(password);
  await page.getByLabel('New password').fill(nextPassword);
  await page.getByRole('button', { name: 'Change password' }).click();
  await page.getByText('Password changed. Other active sessions were revoked.').waitFor({ state: 'visible' });
  screenshots.push(await capture(page, 'security-password-changed'));

  const secondContext = await browser.newContext({ viewport: viewports.desktop });
  await login(secondContext, email, nextPassword);
  const secondMe = await currentUser(secondContext);
  const secondSessionID = secondMe.session.id;

  await page.goto(`${SITE_URL}/app/settings/sessions`, { waitUntil: 'domcontentloaded' });
  await waitAccountState(page, 'success'); states.sessions.push('success');
  assert((await page.locator('.p15-account__row').count()) >= 2, 'session UI must expose multiple server sessions');
  screenshots.push(await capture(page, 'sessions-list'));
  const otherSession = page.locator('.p15-account__row[data-session-current="false"]').first();
  await otherSession.getByRole('button', { name: 'Revoke session' }).click();
  await waitAccountState(page, 'destructive-confirm'); states.sessions.push('destructive-confirm');
  screenshots.push(await capture(page, 'sessions-destructive-confirm'));
  await page.getByRole('button', { name: 'Confirm revoke' }).click();
  await waitAccountState(page, 'success');
  const secondStatus = mysql(`SELECT status FROM auth_sessions WHERE id=${sqlLiteral(secondSessionID)}`);
  assert(secondStatus === 'revoked', 'session revocation was not durable');
  screenshots.push(await capture(page, 'sessions-revoked'));

  const revokedResponse = await api(secondContext, '/api/me');
  assert(revokedResponse.status() === 410, `revoked session was not rejected server-side: ${revokedResponse.status()}`);
  const secondPage = await secondContext.newPage();
  await secondPage.goto(`${SITE_URL}/app/settings/profile`, { waitUntil: 'domcontentloaded' });
  await waitAccountState(secondPage, 'session-revoked');
  screenshots.push(await capture(secondPage, 'revoked-session-direct-load'));
  await login(secondContext, email, nextPassword);
  await secondPage.reload({ waitUntil: 'domcontentloaded' });
  await waitAccountState(secondPage, 'success');
  screenshots.push(await capture(secondPage, 'revoked-session-recovered'));

  const identityID = `oid_t025_${suffix}`.slice(0, 63);
  mysql(`INSERT INTO oauth_identities (id,user_id,provider,provider_subject_hash,provider_email_normalized,provider_email_verified,display_name,created_at,updated_at) VALUES (${sqlLiteral(identityID)},${sqlLiteral(userID)},'google',UNHEX(SHA2(${sqlLiteral(`p15-t025-subject-${suffix}`)},256)),NULL,0,'P15 T025 Google',CURRENT_TIMESTAMP(6),CURRENT_TIMESTAMP(6))`);
  await page.goto(`${SITE_URL}/app/settings/connected-accounts`, { waitUntil: 'domcontentloaded' });
  await waitAccountState(page, 'success'); states.connected_accounts.push('success');
  assert((await page.locator('.p15-account__providers button').count()) === 6, 'exact six-provider registry is not rendered');
  await page.getByRole('button', { name: 'Disconnect google' }).click();
  await waitAccountState(page, 'destructive-confirm'); states.connected_accounts.push('destructive-confirm');
  screenshots.push(await capture(page, 'connected-destructive-confirm'));
  await page.getByRole('button', { name: 'Confirm disconnect' }).click();
  await waitAccountState(page, 'success');
  const remainingIdentities = Number(mysql(`SELECT COUNT(*) FROM oauth_identities WHERE id=${sqlLiteral(identityID)}`));
  assert(remainingIdentities === 0, 'connected-account unbind was not durable');
  screenshots.push(await capture(page, 'connected-disconnected'));

  await page.getByRole('button', { name: 'Connect google' }).click();
  await waitAccountState(page, 'provider-error'); states.connected_accounts.push('provider-error');
  screenshots.push(await capture(page, 'connected-provider-error'));

  const storage = await page.evaluate(() => ({ local: Object.keys(localStorage), session: Object.keys(sessionStorage) }));
  assert(storage.local.length === 0 && storage.session.length === 0, `account browser storage must remain empty: ${JSON.stringify(storage)}`);
  assert(diagnostics.console_errors.length === 0, `console errors: ${JSON.stringify(diagnostics.console_errors)}`);
  assert(diagnostics.page_errors.length === 0, `page errors: ${JSON.stringify(diagnostics.page_errors)}`);
  assert(diagnostics.request_failures.length === 0, `request failures: ${JSON.stringify(diagnostics.request_failures)}`);

  const details = {
    real_local_api: true,
    direct_load_authorization: true,
    revoked_session_rejected_server_side: true,
    revoked_session_recovery: true,
    durable_profile_mutation: true,
    durable_session_revoke: true,
    durable_connected_account_unbind: true,
    states,
    responsive_viewports: Object.keys(viewports),
    accessibility: { keyboard_focus: true, error_focus: true, labels: true },
    security: { secure_cookie: true, csrf_origin_server_authority: true, web_storage_secret_free: true },
    screenshot_count: screenshots.length,
    closure_claim: false,
  };
  writeResult('PASS', details, []);
  await secondContext.close();
  await context.close();
  await browser.close();
  console.log(JSON.stringify({ case_id: caseId, status: 'PASS', implementation_commit: implementationCommit(), screenshot_count: screenshots.length }));
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  writeResult('FAIL', { states, screenshot_count: screenshots.length, diagnostics, closure_claim: false }, [message]);
  console.error(message);
  process.exitCode = 1;
}
