import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';

export const root = process.cwd();
export const evidenceRoot = `${root}/artifacts/v10/P16`;
export const browserDir = `${evidenceRoot}/browser`;
export const capturesDir = `${evidenceRoot}/captures`;
export const runtimeDir = `${evidenceRoot}/runtime/browser`;
for (const dir of [browserDir, capturesDir, runtimeDir]) mkdirSync(dir, { recursive: true });

export const PLATFORM_URL = process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081';
export const SITE_URL = process.env.GOJET_TEST_SITE_URL ?? 'http://127.0.0.1:4180';
export const SITE_BAD_TURNSTILE_URL = process.env.GOJET_TEST_SITE_BAD_TURNSTILE_URL ?? 'http://127.0.0.1:4181';
export const ADMIN_URL = process.env.GOJET_TEST_ADMIN_URL ?? 'http://127.0.0.1:4182';

const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';

const variables = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
function parseViewport(value, tokenName) {
  const match = /^(\d+)×(\d+)$/.exec(String(value));
  if (!match) throw new Error(`Invalid canonical viewport ${tokenName}: ${String(value)}`);
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
if (!executablePath) throw new Error('System Chrome/Chromium is required for P16 browser evidence');

export function implementationCommit() {
  return process.env.GITHUB_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
}

export function assert(condition, message) {
  if (!condition) throw new Error(message);
}

export function sqlLiteral(value) {
  return `'${String(value).replaceAll('\\', '\\\\').replaceAll("'", "''")}'`;
}

export function mysql(sql) {
  return execFileSync('mysql', ['--protocol=tcp', '-h', MYSQL_HOST, '-P', String(MYSQL_PORT), '-u', MYSQL_USER, '-N', '-B', MYSQL_DATABASE, '-e', sql], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
  }).trim();
}

export function mysqlScalar(sql) {
  const output = mysql(sql);
  return output ? output.split('\n')[0] : '';
}

function fixture(mode) {
  const output = execFileSync('go', ['run', './scripts/p16/browserfixture', '--mode', mode], {
    cwd: root,
    encoding: 'utf8',
    env: process.env,
  });
  return JSON.parse(output);
}

export function createBrowserSessions() {
  return fixture('sessions');
}

export function seedAdminFixture() {
  return fixture('seed-admin');
}

export function seedPublicFixture() {
  return fixture('seed-public');
}

export async function addSessionCookie(context, sessions, kind) {
  const value = kind === 'security' ? sessions.security_session : kind === 'domain' ? sessions.domain_session : sessions.denied_session;
  assert(value, `missing ${kind} browser session`);
  await context.addCookies([{
    name: sessions.cookie_name,
    value,
    url: ADMIN_URL,
    httpOnly: true,
    secure: true,
    sameSite: 'Lax',
  }]);
}

export function diagnostics() {
  return { console_errors: [], page_errors: [], request_failures: [], http_errors: [] };
}

export function attachDiagnostics(page, report, { allowStatuses = [] } = {}) {
  page.on('console', (message) => {
    const text = message.text();
    if (message.type() === 'error' && !allowStatuses.some((status) => text.startsWith(`Failed to load resource: the server responded with a status of ${status} `))) {
      report.console_errors.push(text);
    }
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

export async function assertNoHorizontalOverflow(page, label) {
  const values = await page.evaluate(() => ({ innerWidth: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }));
  assert(values.scrollWidth <= values.innerWidth + 1, `${label} horizontal overflow ${JSON.stringify(values)}`);
  return values;
}

export async function expectState(page, pageName, state) {
  const locator = page.locator(`[data-page="${pageName}"]`);
  await locator.waitFor({ state: 'visible' });
  await page.waitForFunction(({ pageName, state }) => document.querySelector(`[data-page="${pageName}"]`)?.getAttribute('data-state') === state, { pageName, state });
  return state;
}

export async function delayedAPIRoute(page, matcher) {
  let release;
  let seenResolve;
  let handledResolve;
  const gate = new Promise((resolve) => { release = resolve; });
  const seen = new Promise((resolve) => { seenResolve = resolve; });
  const handled = new Promise((resolve) => { handledResolve = resolve; });
  let matched = false;
  const handler = async (route) => {
    if (!matched && matcher(route.request())) {
      matched = true;
      seenResolve();
      await gate;
      await route.continue();
      handledResolve();
      return;
    }
    await route.continue();
  };
  await page.route('**/api/**', handler);
  return {
    seen,
    release: () => release(),
    dispose: async () => {
      await handled;
      await page.unroute('**/api/**', handler);
    },
  };
}

export async function tabToAccessibleName(page, name, maxTabs = 60) {
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

export function writeResult(caseId, status, details, errors = []) {
  const payload = {
    case_id: caseId,
    status,
    generated_at: new Date().toISOString(),
    implementation_commit: implementationCommit(),
    contract_authority: '43c5d4d7e1833c593ceacb48016abac6e3133893',
    environment: {
      browser: executablePath,
      platformapi: PLATFORM_URL,
      site_surfaces: [SITE_URL, SITE_BAD_TURNSTILE_URL],
      admin_surface: ADMIN_URL,
      canonical_viewports: viewports,
      authority: 'real MySQL 8.x + real Redis 7.x + native Go platformapi + built Website/Admin; browser interception delays transport only and never fabricates API success',
    },
    details,
    errors,
  };
  writeFileSync(`${browserDir}/${caseId}.json`, `${JSON.stringify(payload, null, 2)}\n`);
}
