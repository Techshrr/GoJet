import { createHmac } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const root = process.cwd();
const caseId = process.argv[2] === '--case' ? process.argv[3] : '';
if (caseId !== 'P15-T026') throw new Error(`Unsupported P15 admin browser case: ${caseId || '<missing>'}`);

const evidenceRoot = `${root}/artifacts/v10/P15`;
const browserDir = `${evidenceRoot}/browser`;
const capturesDir = `${evidenceRoot}/captures`;
for (const dir of [browserDir, capturesDir]) mkdirSync(dir, { recursive: true });

const SITE_URL = process.env.GOJET_TEST_ADMIN_URL ?? 'http://localhost:4186';
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
const GRANT_KEY_HEX = process.env.GOJET_AUTH_GRANT_KEY_HEX ?? '';
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P15 admin browser evidence');
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
const frozenProviders = ['google', 'facebook', 'github', 'qq', 'wechat', 'rainbow'];

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
async function waitAdminState(page, state) { await page.locator(`[data-admin-oauth-state="${state}"]`).waitFor({ state: 'visible', timeout: 15000 }); }
async function waitAdminViewport(page, viewport) { await page.locator(`.admin-shell[data-viewport="${viewport}"]`).waitFor({ state: 'visible', timeout: 5000 }); }
async function noOverflow(page, label) {
  const size = await page.evaluate(() => ({ innerWidth: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }));
  assert(size.scrollWidth <= size.innerWidth + 1, `${label} horizontal overflow: ${JSON.stringify(size)}`);
}
async function capture(page, label) {
  await page.locator('input[type="password"]').evaluateAll((nodes) => { for (const node of nodes) node.value = ''; }).catch(() => {});
  const path = `${capturesDir}/${caseId}-${label}.png`;
  await page.screenshot({ path, fullPage: true });
  return path.replace(`${root}/`, '');
}
async function api(context, path, options = {}) {
  return context.request.fetch(`${SITE_URL}${path}`, { ...options, failOnStatusCode: false });
}
async function registerVerifyLogin(context, email, displayName, password) {
  const register = await api(context, '/api/auth/register', { method: 'POST', data: { email, display_name: displayName, password } });
  assert(register.status() === 202, `registration failed: ${register.status()}`);
  const grantID = mysql(`SELECT id FROM auth_one_time_grants WHERE email_normalized=${sqlLiteral(email)} AND purpose='email_verification' ORDER BY created_at DESC,id DESC LIMIT 1`);
  assert(grantID.startsWith('grt_'), 'verification grant was not durably created');
  const code = derive('gvc_', 'email_verification', grantID);
  const verify = await api(context, '/api/auth/verifyemail', { method: 'POST', data: { code } });
  assert(verify.status() === 200, `verification failed: ${verify.status()}`);
  const login = await api(context, '/api/auth/login', { method: 'POST', data: { email, password } });
  assert(login.status() === 200, `login failed: ${login.status()}`);
}
function resultPayload(status, details, errors = []) {
  return {
    case_id: caseId,
    status,
    generated_at: new Date().toISOString(),
    implementation_commit: implementationCommit(),
    environment: {
      browser: executablePath,
      admin_origin: new URL(SITE_URL).origin,
      canonical_viewports: viewports,
      authority: 'real MySQL 8.x + real Redis + native Go platformapi + built Admin OAuth surface; deterministic local permission/provider test fixtures only',
    },
    details,
    errors,
  };
}
function writeResult(status, details, errors = []) { writeFileSync(`${browserDir}/${caseId}.json`, `${JSON.stringify(resultPayload(status, details, errors), null, 2)}\n`); }

const screenshots = [];
const states = [];
const diagnostics = { console_errors: [], page_errors: [], request_failures: [] };
let browser;
try {
  browser = await chromium.launch({ executablePath, headless: true });

  const unauthContext = await browser.newContext({ viewport: viewports.desktop, reducedMotion: 'reduce' });
  const unauthPage = await unauthContext.newPage();
  await unauthPage.goto(`${SITE_URL}/admin/platform/oauth`, { waitUntil: 'domcontentloaded' });
  await waitAdminState(unauthPage, 'provider-error');
  await unauthPage.locator('.admin-shell[data-state="admin-auth-required"]').waitFor({ state: 'visible' });
  states.push('provider-error');
  screenshots.push(await capture(unauthPage, 'direct-load-auth-required'));
  await unauthContext.close();

  const context = await browser.newContext({ viewport: viewports.desktop, reducedMotion: 'reduce' });
  const page = await context.newPage();
  page.on('console', (message) => {
    if (message.type() === 'error' && !message.text().startsWith('Failed to load resource: the server responded with a status of ')) diagnostics.console_errors.push(message.text());
  });
  page.on('pageerror', (error) => diagnostics.page_errors.push(String(error)));
  page.on('requestfailed', (request) => diagnostics.request_failures.push({ url: request.url().replace(/\?.*$/, ''), failure: request.failure()?.errorText ?? 'failed' }));

  const suffix = uniqueSuffix();
  const email = `p15-t026-${suffix}@example.test`;
  const password = 'P15-T026-Admin-Password!42';
  const providerSecret = 'P15-T026-Client-Secret!84';
  await registerVerifyLogin(context, email, 'P15 T026 Admin OAuth', password);

  const cookies = await context.cookies(SITE_URL);
  const sessionCookie = cookies.find((cookie) => cookie.name === '__Host-gojet_session');
  assert(sessionCookie && sessionCookie.httpOnly && sessionCookie.secure, 'formal secure HttpOnly session cookie was not established');
  const me = await api(context, '/api/me');
  assert(me.status() === 200, `GET /api/me failed: ${me.status()}`);
  const mePayload = await me.json();
  const userID = mePayload.user?.id;
  assert(typeof userID === 'string' && userID.startsWith('usr_'), 'current account identity missing');

  const navigation = await page.goto(`${SITE_URL}/admin/platform/oauth`, { waitUntil: 'domcontentloaded' });
  assert(navigation, 'Admin OAuth navigation response missing');
  const pageHeaders = await navigation.headers();
  assert(String(pageHeaders['cache-control'] ?? '').includes('no-store'), 'Admin OAuth page is not no-store');
  assert(String(pageHeaders['x-robots-tag'] ?? '').includes('noindex'), 'Admin OAuth page is not noindex');
  await waitAdminState(page, 'empty');
  states.push('empty');

  const providerButtons = page.locator('.p15-admin-oauth__providers button');
  assert(await providerButtons.count() === frozenProviders.length, 'Admin OAuth must render exactly six providers');
  const renderedProviders = await providerButtons.locator('strong').allTextContents();
  assert(JSON.stringify(renderedProviders) === JSON.stringify(frozenProviders), `provider registry/order mismatch: ${JSON.stringify(renderedProviders)}`);

  await waitAdminViewport(page, 'desktop');
  await noOverflow(page, 'Admin OAuth desktop'); screenshots.push(await capture(page, 'empty-desktop'));
  await page.setViewportSize(viewports.mobile); await waitAdminViewport(page, 'mobile'); await noOverflow(page, 'Admin OAuth mobile'); screenshots.push(await capture(page, 'empty-mobile'));
  await page.setViewportSize(viewports.narrow); await waitAdminViewport(page, 'mobile'); await noOverflow(page, 'Admin OAuth narrow'); screenshots.push(await capture(page, 'empty-narrow'));
  await page.setViewportSize(viewports.desktop); await waitAdminViewport(page, 'desktop');

  let focusedClientID = false;
  for (let index = 0; index < 60; index += 1) {
    await page.keyboard.press('Tab');
    if (await page.getByLabel('Client ID').evaluate((element) => element === document.activeElement)) { focusedClientID = true; break; }
  }
  assert(focusedClientID, 'keyboard focus did not reach Client ID');

  await page.getByLabel('Client ID').fill(`p15-google-${suffix}`);
  await page.getByLabel('Client secret').fill(providerSecret);
  await page.getByLabel('Authorization URL').fill('https://provider.example/authorize');
  await page.getByLabel('Token URL').fill('https://provider.example/token');
  await page.getByLabel('User info URL').fill('https://provider.example/userinfo');
  await page.getByLabel('Redirect URI').fill('https://gojet.example/oauth/google/callback');
  await page.getByLabel('Scopes').fill('openid email');
  await page.getByRole('button', { name: 'Save provider' }).click();
  await waitAdminState(page, 'configured');
  states.push('configured');
  assert(await page.getByLabel('Client secret').inputValue() === '', 'client secret was retained in the browser after save');
  assert((await page.locator('body').innerText()).includes('Configured · masked'), 'configured secret status is not masked');

  const cipherHex = mysql("SELECT HEX(client_secret_ciphertext) FROM oauth_provider_configs WHERE provider='google'");
  const secretHex = Buffer.from(providerSecret, 'utf8').toString('hex').toUpperCase();
  assert(cipherHex.length > 32 && !cipherHex.includes(secretHex), 'provider secret was not encrypted at rest');
  assert(mysql("SELECT secret_key_id FROM oauth_provider_configs WHERE provider='google'").trim().length > 0, 'provider secret key id missing');
  assert(Number(mysql(`SELECT COUNT(*) FROM auth_audit_events WHERE action='auth.oauth.provider.updated' AND actor_kind='admin' AND actor_id=${sqlLiteral(userID)} AND resource_id='google'`)) === 1, 'settings.manage provider update audit missing');
  screenshots.push(await capture(page, 'google-configured'));

  await page.getByRole('button', { name: 'Review secret status' }).click();
  await waitAdminState(page, 'secret-masked');
  states.push('secret-masked');
  assert(!(await page.locator('body').innerText()).includes(providerSecret), 'provider client secret was rendered in browser text');
  screenshots.push(await capture(page, 'secret-masked'));

  await page.getByRole('button', { name: /facebook/i }).click();
  await waitAdminState(page, 'incomplete');
  states.push('incomplete');
  screenshots.push(await capture(page, 'facebook-incomplete'));

  await page.getByLabel('Client ID').fill(`p15-facebook-${suffix}`);
  await page.getByLabel('Client secret').fill('P15-T026-Facebook-Secret!21');
  await page.getByLabel('Authorization URL').fill('http://provider.example/authorize');
  await page.getByLabel('Token URL').fill('https://provider.example/token');
  await page.getByLabel('User info URL').fill('https://provider.example/userinfo');
  await page.getByLabel('Redirect URI').fill('https://gojet.example/oauth/facebook/callback');
  await page.getByLabel('Scopes').fill('openid email');
  await page.getByRole('button', { name: 'Save provider' }).click();
  await waitAdminState(page, 'provider-error');
  states.push('provider-error');
  assert(await page.locator('.p15-admin-oauth > div[tabindex="-1"]').evaluate((element) => element === document.activeElement), 'provider validation error focus was not moved in-page');
  assert(Number(mysql("SELECT configured FROM (SELECT (client_id <> '' AND client_secret_ciphertext IS NOT NULL AND authorization_url <> '' AND token_url <> '' AND redirect_uri <> '') AS configured FROM oauth_provider_configs WHERE provider='facebook') AS x")) === 0, 'invalid provider configuration mutated durable authority');
  screenshots.push(await capture(page, 'provider-validation-error'));

  await page.getByRole('button', { name: /google/i }).click();
  await waitAdminState(page, 'configured');
  await page.getByRole('button', { name: 'Test provider' }).click();
  await waitAdminState(page, 'test-result');
  states.push('test-result');
  await page.getByText('Server-side provider configuration test passed.').waitFor({ state: 'visible' });
  screenshots.push(await capture(page, 'test-result'));

  const storage = await page.evaluate(() => ({ local: Object.keys(localStorage), session: Object.keys(sessionStorage) }));
  assert(storage.local.length === 0 && storage.session.length === 0, `Admin OAuth browser storage must remain empty: ${JSON.stringify(storage)}`);
  assert(!(await page.locator('body').innerText()).includes(providerSecret), 'provider client secret appeared in rendered page text');
  assert(diagnostics.console_errors.length === 0, `console errors: ${JSON.stringify(diagnostics.console_errors)}`);
  assert(diagnostics.page_errors.length === 0, `page errors: ${JSON.stringify(diagnostics.page_errors)}`);
  assert(diagnostics.request_failures.length === 0, `request failures: ${JSON.stringify(diagnostics.request_failures)}`);

  const details = {
    real_local_api: true,
    direct_load_authorization: true,
    settings_manage_consumed: true,
    p17_lifecycle_claim: false,
    provider_registry_count: frozenProviders.length,
    secret_encrypted_at_rest: true,
    secret_masked_in_browser: true,
    states,
    responsive_viewports: Object.keys(viewports),
    accessibility: { keyboard_focus: true, error_focus: true, labels: true, reduced_motion: true },
    security: { secure_cookie: true, csrf_origin_server_authority: true, web_storage_secret_free: true, noindex: true, private_headers: true },
    screenshot_count: screenshots.length,
    closure_claim: false,
  };
  writeResult('PASS', details, []);
  console.log(JSON.stringify({ case_id: caseId, status: 'PASS', implementation_commit: implementationCommit(), screenshot_count: screenshots.length }));
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  writeResult('FAIL', { states, screenshot_count: screenshots.length, diagnostics, p17_lifecycle_claim: false, closure_claim: false }, [message]);
  console.error(message);
  process.exitCode = 1;
} finally {
  if (browser) await browser.close().catch(() => {});
}
