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
if (caseId !== 'P15-T024') throw new Error(`Unsupported P15 browser case: ${caseId || '<missing>'}`);

const SITE_URL = process.env.GOJET_TEST_SITE_URL ?? 'http://127.0.0.1:4184';
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
const GRANT_KEY_HEX = process.env.GOJET_AUTH_GRANT_KEY_HEX ?? '';
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P15 browser evidence');
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
async function waitState(page, state) { await page.locator(`[data-auth-state="${state}"]`).waitFor({ state: 'visible', timeout: 15000 }); }
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
function resultPayload(status, details, errors = []) {
  return {
    case_id: caseId,
    status,
    generated_at: new Date().toISOString(),
    implementation_commit: implementationCommit(),
    environment: {
      browser: executablePath,
      site: SITE_URL,
      canonical_viewports: viewports,
      authority: 'real MySQL 8.x + real Redis + native Go platformapi + built Website Auth shell; no fabricated API success',
    },
    details,
    errors,
  };
}
function writeResult(status, details, errors = []) { writeFileSync(`${browserDir}/${caseId}.json`, `${JSON.stringify(resultPayload(status, details, errors), null, 2)}\n`); }

const screenshots = [];
const states = { login: [], register: [], verify: [], forgot: [], reset: [], oauth: [], social: [] };
const diagnostics = { console_errors: [], page_errors: [], request_failures: [] };

try {
  const browser = await chromium.launch({ executablePath, headless: true });
  const context = await browser.newContext({ viewport: viewports.desktop });
  const page = await context.newPage();
  page.on('console', (message) => { if (message.type() === 'error' && !message.text().startsWith('Failed to load resource: the server responded with a status of ')) diagnostics.console_errors.push(message.text()); });
  page.on('pageerror', (error) => diagnostics.page_errors.push(String(error)));
  page.on('requestfailed', (request) => diagnostics.request_failures.push({ url: request.url().replace(/\?.*$/, ''), failure: request.failure()?.errorText ?? 'failed' }));

  await page.goto(`${SITE_URL}/login`, { waitUntil: 'networkidle' });
  await waitState(page, 'input'); states.login.push('input');
  assert((await page.locator('.p15-auth__providers a').count()) === 6, 'frozen OAuth provider registry must render all six providers');
  assert((await page.locator('meta[name="robots"]').getAttribute('content')) === 'noindex,nofollow', 'Auth routes must be noindex');
  await noOverflow(page, 'login desktop'); screenshots.push(await capture(page, 'login-input-desktop'));

  await page.setViewportSize(viewports.mobile); await noOverflow(page, 'login mobile'); screenshots.push(await capture(page, 'login-input-mobile'));
  await page.setViewportSize(viewports.narrow); await noOverflow(page, 'login narrow'); screenshots.push(await capture(page, 'login-input-narrow'));
  await page.setViewportSize(viewports.desktop);

  let focusedEmail = false;
  for (let index = 0; index < 12; index += 1) {
    await page.keyboard.press('Tab');
    if (await page.locator('#login-email').evaluate((el) => el === document.activeElement)) { focusedEmail = true; break; }
  }
  assert(focusedEmail, 'keyboard focus did not reach login email field');

  await page.getByLabel('Email').fill('missing-account@example.test');
  await page.getByLabel('Password').fill('P15-Browser-Missing!42');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await waitState(page, 'invalid'); states.login.push('invalid');
  assert(await page.locator('[role="alert"]').evaluate((el) => el === document.activeElement), 'invalid login error must receive focus');
  screenshots.push(await capture(page, 'login-invalid'));

  const suffix = uniqueSuffix();
  const email = `p15-browser-${suffix}@example.test`;
  const password = 'P15-Browser-Password!42';
  const replacementPassword = 'P15-Browser-New-Password!42';
  await page.goto(`${SITE_URL}/register`, { waitUntil: 'networkidle' }); states.register.push('input'); screenshots.push(await capture(page, 'register-input'));
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Display name').fill('P15 Browser User');
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Create account' }).click();
  await waitState(page, 'code-sent'); states.register.push('code-sent'); screenshots.push(await capture(page, 'register-code-sent'));

  const verificationGrantID = mysql(`SELECT id FROM auth_one_time_grants WHERE email_normalized=${sqlLiteral(email)} AND purpose='email_verification' ORDER BY created_at DESC,id DESC LIMIT 1`);
  assert(verificationGrantID.startsWith('grt_'), 'verification grant was not durably created');
  const verificationCode = derive('gvc_', 'email_verification', verificationGrantID);

  await page.goto(`${SITE_URL}/verify-email?code=${encodeURIComponent(verificationCode)}`, { waitUntil: 'networkidle' });
  await waitState(page, 'verifying'); states.verify.push('verifying');
  await page.getByRole('button', { name: 'Verify email' }).click();
  await waitState(page, 'success'); states.verify.push('success');
  assert(!(await page.locator('body').innerText()).includes(verificationCode), 'consumed verification code remained visible');
  screenshots.push(await capture(page, 'verify-success'));

  await page.goto(`${SITE_URL}/verify-email?code=${encodeURIComponent(verificationCode)}`, { waitUntil: 'networkidle' });
  await page.getByRole('button', { name: 'Verify email' }).click();
  await waitState(page, 'reused-token'); states.verify.push('reused-token'); screenshots.push(await capture(page, 'verify-reused'));

  await page.goto(`${SITE_URL}/login`, { waitUntil: 'networkidle' });
  await page.getByLabel('Email').fill(email); await page.getByLabel('Password').fill(password);
  const loginResponsePromise = page.waitForResponse((response) => response.url().includes('/api/auth/login') && response.request().method() === 'POST');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const loginResponse = await loginResponsePromise;
  await waitState(page, 'success'); states.login.push('success');
  const headers = await loginResponse.allHeaders();
  assert((headers['cache-control'] ?? '').includes('no-store'), 'auth API Cache-Control must be no-store');
  assert((headers['x-robots-tag'] ?? '').includes('noindex'), 'auth API X-Robots-Tag must be noindex');
  assert((headers['content-security-policy'] ?? '').includes("default-src 'self'"), 'auth API CSP missing');
  assert(headers['x-content-type-options'] === 'nosniff', 'auth API nosniff missing');
  assert(headers['referrer-policy'] === 'no-referrer', 'auth API Referrer-Policy missing');
  const setCookie = await loginResponse.headerValue('set-cookie');
  assert(Boolean(setCookie?.includes('__Host-gojet_session=') && setCookie.includes('HttpOnly') && setCookie.includes('Secure') && setCookie.includes('SameSite=Lax')), 'secure session cookie contract missing');
  screenshots.push(await capture(page, 'login-success'));

  await page.goto(`${SITE_URL}/forgot-password`, { waitUntil: 'networkidle' }); states.forgot.push('input');
  await page.getByLabel('Email').fill(email); await page.getByRole('button', { name: 'Send reset instructions' }).click();
  await waitState(page, 'submitted-neutral'); states.forgot.push('submitted-neutral'); screenshots.push(await capture(page, 'forgot-neutral'));

  const resetGrantID = mysql(`SELECT id FROM auth_one_time_grants WHERE email_normalized=${sqlLiteral(email)} AND purpose='password_reset' ORDER BY created_at DESC,id DESC LIMIT 1`);
  assert(resetGrantID.startsWith('grt_'), 'password reset grant was not durably created');
  const resetToken = derive('grp_', 'password_reset', resetGrantID);
  await page.goto(`${SITE_URL}/reset-password?token=${encodeURIComponent(resetToken)}`, { waitUntil: 'networkidle' }); states.reset.push('input');
  await page.getByLabel('New password').fill(replacementPassword); await page.getByRole('button', { name: 'Reset password' }).click();
  await waitState(page, 'success'); states.reset.push('success'); assert(!(await page.locator('body').innerText()).includes(resetToken), 'reset token became visible'); screenshots.push(await capture(page, 'reset-success'));

  await page.goto(`${SITE_URL}/reset-password?token=grp_invalid_browser_authority`, { waitUntil: 'networkidle' });
  await page.getByLabel('New password').fill(replacementPassword); await page.getByRole('button', { name: 'Reset password' }).click();
  await waitState(page, 'invalid-token'); states.reset.push('invalid-token'); screenshots.push(await capture(page, 'reset-invalid'));

  await page.goto(`${SITE_URL}/oauth/google/callback?state=bad-state&code=bad-code`, { waitUntil: 'networkidle' }); states.oauth.push('processing');
  await waitState(page, 'state-error'); states.oauth.push('state-error'); assert(!(await page.locator('body').innerText()).includes('bad-code'), 'raw OAuth callback code became visible'); screenshots.push(await capture(page, 'oauth-state-error'));

  await page.goto(`${SITE_URL}/social-registration?code=gsr_invalid_browser_authority`, { waitUntil: 'networkidle' }); states.social.push('loading-handoff');
  await waitState(page, 'expired-handoff'); states.social.push('expired-handoff'); screenshots.push(await capture(page, 'social-expired-handoff'));

  const storage = await page.evaluate(() => ({ local: Object.keys(localStorage), session: Object.keys(sessionStorage) }));
  assert(storage.local.length === 0 && storage.session.length === 0, `Auth browser storage must remain empty: ${JSON.stringify(storage)}`);
  assert(diagnostics.console_errors.length === 0, `console errors: ${JSON.stringify(diagnostics.console_errors)}`);
  assert(diagnostics.page_errors.length === 0, `page errors: ${JSON.stringify(diagnostics.page_errors)}`);
  assert(diagnostics.request_failures.length === 0, `request failures: ${JSON.stringify(diagnostics.request_failures)}`);

  const details = {
    frozen_route_authority: true,
    real_local_api: true,
    provider_registry_count: 6,
    states,
    responsive_viewports: Object.keys(viewports),
    accessibility: { keyboard_focus: true, error_focus: true, labels: true },
    security: { noindex: true, private_headers: true, secure_cookie: true, web_storage_secret_free: true, raw_callback_not_rendered: true },
    screenshot_count: screenshots.length,
    closure_claim: false,
  };
  writeResult('PASS', details, []);
  await browser.close();
  console.log(JSON.stringify({ case_id: caseId, status: 'PASS', implementation_commit: implementationCommit(), screenshot_count: screenshots.length }));
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  writeResult('FAIL', { states, screenshot_count: screenshots.length, diagnostics, closure_claim: false }, [message]);
  console.error(message);
  process.exitCode = 1;
}
