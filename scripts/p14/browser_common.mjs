import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';

export const root = process.cwd();
export const evidenceRoot = `${root}/artifacts/v10/P14`;
export const browserDir = `${evidenceRoot}/browser`;
export const capturesDir = `${evidenceRoot}/captures`;
export const runtimeDir = `${evidenceRoot}/runtime/browser`;
for (const dir of [browserDir, capturesDir, runtimeDir]) mkdirSync(dir, { recursive: true });

export const PLATFORM_URL = process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081';
export const OWNER_URL = process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174';
export const NO_TURNSTILE_URL = process.env.GOJET_TEST_WORKSPACE_NO_TURNSTILE_URL ?? 'http://127.0.0.1:4175';
export const BAD_TURNSTILE_URL = process.env.GOJET_TEST_WORKSPACE_BAD_TURNSTILE_URL ?? 'http://127.0.0.1:4176';
export const FOREIGN_URL = process.env.GOJET_TEST_WORKSPACE_FOREIGN_URL ?? 'http://127.0.0.1:4177';
export const SITE_URL = process.env.GOJET_TEST_SITE_URL ?? 'http://127.0.0.1:4180';
export const SITE_BAD_TURNSTILE_URL = process.env.GOJET_TEST_SITE_BAD_TURNSTILE_URL ?? 'http://127.0.0.1:4181';
export const ADMIN_URL = process.env.GOJET_TEST_ADMIN_URL ?? 'http://127.0.0.1:4182';
export const ADMIN_DENIED_URL = process.env.GOJET_TEST_ADMIN_DENIED_URL ?? 'http://127.0.0.1:4183';
export const WORKSPACE = process.env.GOJET_TEST_WORKSPACE_ID ?? 'ws-p14-browser';
export const OWNER = process.env.GOJET_TEST_ACTOR_ID ?? 'p14-browser-owner';
const OWNER_EMAIL = process.env.GOJET_TEST_ACTOR_EMAIL ?? 'p14-browser-owner@example.test';
const ADMIN = process.env.GOJET_TEST_SUPPORT_TICKETS_ADMIN_ACTOR ?? 'p14-browser-ticket-admin';
const ADMIN_EMAIL = process.env.GOJET_TEST_SUPPORT_TICKETS_ADMIN_EMAIL ?? 'p14-browser-admin@example.test';
const MAIL_ADMIN = process.env.GOJET_TEST_SUPPORT_MAIL_ADMIN_ACTOR ?? 'p14-browser-mail-admin';
const MAIL_ADMIN_EMAIL = process.env.GOJET_TEST_SUPPORT_MAIL_ADMIN_EMAIL ?? 'p14-browser-mail-admin@example.test';

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
if (!executablePath) throw new Error('System Chrome/Chromium is required for P14 browser evidence');

export function implementationCommit() {
  return process.env.GITHUB_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
}
export function assert(condition, message) { if (!condition) throw new Error(message); }
export function sqlLiteral(value) { return `'${String(value).replaceAll('\\', '\\\\').replaceAll("'", "''")}'`; }
export function mysql(sql) {
  return execFileSync('mysql', ['--protocol=tcp', '-h', MYSQL_HOST, '-P', String(MYSQL_PORT), '-u', MYSQL_USER, '-N', '-B', MYSQL_DATABASE, '-e', sql], {
    cwd: root, encoding: 'utf8', env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
  }).trim();
}
export function mysqlScalar(sql) { const out = mysql(sql); return out ? out.split('\n')[0] : ''; }
export function unique(prefix) { return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 9)}`.slice(0, 60); }

export function ensureWorkspace() {
  const ws = sqlLiteral(WORKSPACE);
  mysql(`INSERT INTO workspaces (id,name,status,version,created_by) VALUES (${ws},'P14 Browser Workspace','active',1,${sqlLiteral(OWNER)}) ON DUPLICATE KEY UPDATE name=VALUES(name),status='active'`);
  mysql(`INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES (${ws},${sqlLiteral(OWNER)},${sqlLiteral(OWNER_EMAIL)},'P14 Browser Owner','owner') ON DUPLICATE KEY UPDATE email=VALUES(email),display_name=VALUES(display_name),role='owner'`);
}

function authHeaders(actor, email) {
  return { Accept: 'application/json', 'Content-Type': 'application/json', 'X-GoJet-Test-Actor': actor, 'X-GoJet-Test-Email': email, 'X-GoJet-Test-Display-Name': actor };
}
export async function api(path, { method = 'GET', actor = OWNER, email = OWNER_EMAIL, body, headers = {} } = {}) {
  const response = await fetch(`${PLATFORM_URL}${path}`, {
    method,
    headers: { ...authHeaders(actor, email), ...headers },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  return { status: response.status, headers: Object.fromEntries(response.headers.entries()), data, text };
}
export async function ownerApi(path, options = {}) { return api(path, options); }
export async function adminApi(path, options = {}) {
  return api(path, { ...options, actor: ADMIN, email: ADMIN_EMAIL, headers: { 'Idempotency-Key': unique('browser-admin'), ...(options.headers ?? {}) } });
}
export async function mailAdminApi(path, options = {}) {
  return api(path, { ...options, actor: MAIL_ADMIN, email: MAIL_ADMIN_EMAIL, headers: { 'Idempotency-Key': unique('browser-mail-admin'), ...(options.headers ?? {}) } });
}

export function diagnostics() { return { console_errors: [], page_errors: [], request_failures: [], http_errors: [] }; }
export function attachDiagnostics(page, report, { allowStatuses = [] } = {}) {
  page.on('console', (message) => { const text = message.text(); if (message.type() === 'error' && text !== 'Failed to load resource: net::ERR_FAILED' && text !== 'Failed to load resource: net::ERR_INTERNET_DISCONNECTED' && !allowStatuses.some((status) => text.startsWith(`Failed to load resource: the server responded with a status of ${status} `))) report.console_errors.push(text); });
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
export async function tabToAccessibleName(page, name, maxTabs = 40) {
  for (let i = 0; i < maxTabs; i += 1) {
    await page.keyboard.press('Tab');
    const current = await page.evaluate(() => {
      const el = document.activeElement;
      return el ? { text: (el.textContent ?? '').trim(), aria: el.getAttribute('aria-label') ?? '', tag: el.tagName } : null;
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
    environment: {
      browser: executablePath,
      platformapi: PLATFORM_URL,
      workspace_surfaces: [OWNER_URL, NO_TURNSTILE_URL, BAD_TURNSTILE_URL, FOREIGN_URL],
      p14_t023_surfaces: [SITE_URL, SITE_BAD_TURNSTILE_URL, ADMIN_URL, ADMIN_DENIED_URL],
      canonical_viewports: viewports,
      authority: 'real MySQL 8.x + real Redis + native Go platformapi + built Website/Workspace/Admin; browser interception delays/aborts transport only and never fabricates API success',
    },
    details,
    errors,
  };
  writeFileSync(`${browserDir}/${caseId}.json`, `${JSON.stringify(payload, null, 2)}\n`);
}
