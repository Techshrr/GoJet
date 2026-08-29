import { execFileSync } from 'node:child_process';
import { createHmac } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';

export const root = process.cwd();
export const evidenceRoot = `${root}/artifacts/v10/P17`;
export const browserDir = `${evidenceRoot}/browser`;
export const capturesDir = `${evidenceRoot}/captures`;
export const runtimeDir = `${evidenceRoot}/runtime/browser`;
for (const dir of [browserDir, capturesDir, runtimeDir]) mkdirSync(dir, { recursive: true });

export const ADMIN_URL = process.env.GOJET_TEST_ADMIN_URL ?? 'http://localhost:4182';
export const WORKSPACE_URL = process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4180';
const fixturePath = process.env.GOJET_P17_BROWSER_FIXTURE ?? '/tmp/p17-browser-fixture.json';
export const fixture = JSON.parse(readFileSync(fixturePath, 'utf8'));

const variables = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
function parseViewport(value, tokenName) {
  const match = /^(\d+)×(\d+)$/.exec(String(value));
  if (!match) throw new Error(`invalid canonical viewport ${tokenName}: ${String(value)}`);
  return { width: Number(match[1]), height: Number(match[2]) };
}
export const viewports = {
  desktop: parseViewport(variables['viewport.desktop'].dimensions, 'viewport.desktop'),
  tablet: parseViewport(variables['viewport.tablet'].dimensions, 'viewport.tablet'),
  mobile: parseViewport(variables['viewport.mobile'].dimensions, 'viewport.mobile'),
  narrow: { width: 320, height: 800 },
};

const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
export const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P17 browser evidence');

export function implementationCommit() {
  return process.env.GITHUB_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
}

export function assert(condition, message) {
  if (!condition) throw new Error(message);
}

export function diagnostics() {
  return { console_errors: [], page_errors: [], request_failures: [], http_errors: [] };
}

export function attachDiagnostics(page, report, { allowStatuses = [] } = {}) {
  page.on('console', (message) => {
    const text = message.text();
    if (message.type() === 'error' && !allowStatuses.some((status) => text.includes(`status of ${status}`))) report.console_errors.push(text);
  });
  page.on('pageerror', (error) => report.page_errors.push(String(error)));
  page.on('requestfailed', (request) => report.request_failures.push({ url: request.url(), failure: request.failure() }));
  page.on('response', (response) => {
    if (response.status() >= 400 && !allowStatuses.includes(response.status()) && !response.url().endsWith('/favicon.ico')) {
      report.http_errors.push({ status: response.status(), url: response.url() });
    }
  });
}

export function assertCleanDiagnostics(report, label) {
  assert(report.console_errors.length === 0, `${label} console errors: ${JSON.stringify(report.console_errors)}`);
  assert(report.page_errors.length === 0, `${label} page errors: ${JSON.stringify(report.page_errors)}`);
  assert(report.request_failures.length === 0, `${label} request failures: ${JSON.stringify(report.request_failures)}`);
  assert(report.http_errors.length === 0, `${label} HTTP errors: ${JSON.stringify(report.http_errors)}`);
}

export async function screenshot(page, caseId, label) {
  const path = `${capturesDir}/${caseId}-${label}.png`;
  await page.screenshot({ path, fullPage: true });
  return path.replace(`${root}/`, '');
}

export async function assertNoOverflow(page, label) {
  const value = await page.evaluate(() => ({ innerWidth: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }));
  assert(value.scrollWidth <= value.innerWidth + 1, `${label} horizontal overflow ${JSON.stringify(value)}`);
  return value;
}

export async function waitState(page, pageName, state) {
  const locator = page.locator(`[data-page="${pageName}"]`);
  await locator.waitFor({ state: 'visible' });
  try {
    await page.waitForFunction(
      ({ pageName, state }) => document.querySelector(`[data-page="${pageName}"]`)?.getAttribute('data-state') === state,
      { pageName, state },
    );
  } catch (error) {
    const actual = await locator.getAttribute('data-state').catch(() => null);
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`waitState ${pageName} expected=${state} actual=${actual}; ${message}`);
  }
}

export async function adminLogin(page, email = fixture.root_email, password = fixture.root_password, totpCode = '') {
  await page.goto(`${ADMIN_URL}/admin/login`);
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  if (totpCode) await page.getByLabel('TOTP code').fill(totpCode);
  await page.getByRole('button', { name: 'Sign in' }).click();
}

export async function workspaceActor(page, actor = fixture.owner_actor) {
  const handler = async (route) => {
    const request = route.request();
    await route.continue({
      headers: {
        ...request.headers(),
        'x-gojet-test-actor': actor,
        'x-gojet-test-email': `${actor}@p17.test`,
      },
    });
  };
  await page.route('**/api/workspaces/**', handler);
  return () => page.unroute('**/api/workspaces/**', handler);
}

export function mysql(sql) {
  const host = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
  const port = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
  const user = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
  const database = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
  return execFileSync(
    'mysql',
    ['--protocol=tcp', '-h', host, '-P', port, '-u', user, '-N', '-B', database, '-e', sql],
    { cwd: root, encoding: 'utf8', env: { ...process.env, MYSQL_PWD: process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root' } },
  ).trim();
}

export function mysqlScalar(sql) {
  const output = mysql(sql);
  return output ? output.split('\n')[0] : '';
}

export function versions() {
  const redis = execFileSync('redis-cli', ['-h', '127.0.0.1', '-p', '6379', 'INFO', 'server'], { encoding: 'utf8' });
  return {
    mysql: mysql('SELECT VERSION()'),
    redis: redis.split('\n').find((line) => line.startsWith('redis_version:'))?.split(':')[1]?.trim() || '',
  };
}

function base32Decode(value) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const char of value.toUpperCase().replaceAll('=', '')) {
    const index = alphabet.indexOf(char);
    if (index < 0) throw new Error('invalid base32');
    bits += index.toString(2).padStart(5, '0');
  }
  const bytes = [];
  for (let index = 0; index + 8 <= bits.length; index += 8) bytes.push(parseInt(bits.slice(index, index + 8), 2));
  return Buffer.from(bytes);
}

export function totp(secret, at = Date.now()) {
  const counter = Math.floor(at / 1000 / 30);
  const message = Buffer.alloc(8);
  message.writeBigUInt64BE(BigInt(counter));
  const digest = createHmac('sha1', base32Decode(secret)).update(message).digest();
  const offset = digest[digest.length - 1] & 0x0f;
  const binary = ((digest[offset] & 0x7f) << 24)
    | ((digest[offset + 1] & 0xff) << 16)
    | ((digest[offset + 2] & 0xff) << 8)
    | (digest[offset + 3] & 0xff);
  return String(binary % 1000000).padStart(6, '0');
}

export async function tabToAccessibleName(page, name, maxTabs = 80) {
  for (let index = 0; index < maxTabs; index += 1) {
    await page.keyboard.press('Tab');
    const current = await page.evaluate(() => {
      const element = document.activeElement;
      return element ? {
        text: (element.textContent ?? '').trim(),
        aria: element.getAttribute('aria-label') ?? '',
        tag: element.tagName,
      } : null;
    });
    if (current && (current.text === name || current.aria === name)) return current;
  }
  throw new Error(`Keyboard focus never reached ${name}`);
}

export async function reducedMotionEvidence(browser, url) {
  const context = await browser.newContext({ viewport: viewports.mobile, reducedMotion: 'reduce' });
  const page = await context.newPage();
  await page.goto(url);
  const result = await page.evaluate(() => ({
    matches: matchMedia('(prefers-reduced-motion: reduce)').matches,
    animation: getComputedStyle(document.querySelector('main') || document.body).animationDuration,
  }));
  await context.close();
  assert(result.matches, 'reduced-motion media query not active');
  return result;
}

export function writeResult(caseId, status, checks, captures, details = {}, errors = []) {
  const runtimeVersions = versions();
  const payload = {
    node: 'P17',
    case: caseId,
    status,
    exact_head: implementationCommit(),
    contract_authority: '30174f40df28678360f644b8fed79736906b0ea0',
    environment: {
      browser: executablePath,
      mysql_version: runtimeVersions.mysql,
      redis_version: runtimeVersions.redis,
      admin_surface: ADMIN_URL,
      workspace_surface: WORKSPACE_URL,
      canonical_viewports: viewports,
      authority: 'real MySQL 8.x + real Redis 7.x + native Go platformapi + built Admin/Workspace Vite surfaces; browser routing may add test identity headers or inject transport failure but never fabricates API success',
    },
    checks,
    captures,
    details,
    evidence_policy: {
      raw_admin_password_present: false,
      raw_totp_secret_present: false,
      raw_api_key_secret_present: false,
      raw_webhook_secret_present: false,
      raw_session_present: false,
      dsn_present: false,
    },
    errors,
  };
  writeFileSync(`${browserDir}/${caseId}.json`, `${JSON.stringify(payload, null, 2)}\n`);
}
