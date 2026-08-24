import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';

export const root = process.cwd();
export const evidenceRoot = `${root}/artifacts/v10/P13`;
export const browserDir = `${evidenceRoot}/browser`;
export const capturesDir = `${evidenceRoot}/captures`;
export const runtimeDir = `${evidenceRoot}/runtime`;
for (const dir of [browserDir, capturesDir, runtimeDir]) mkdirSync(dir, { recursive: true });

export const PLATFORM_URL = process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081';
export const WORKSPACE_OWNER_URL = process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174';
export const WORKSPACE_VIEWER_URL = process.env.GOJET_TEST_WORKSPACE_VIEWER_URL ?? 'http://127.0.0.1:4175';
export const WORKSPACE_UNAUTH_URL = process.env.GOJET_TEST_WORKSPACE_UNAUTH_URL ?? 'http://127.0.0.1:4176';
export const ADMIN_URL = process.env.GOJET_TEST_ADMIN_URL ?? 'http://127.0.0.1:4180';
export const ADMIN_DENIED_URL = process.env.GOJET_TEST_ADMIN_DENIED_URL ?? 'http://127.0.0.1:4181';
export const ADMIN_UNAUTH_URL = process.env.GOJET_TEST_ADMIN_UNAUTH_URL ?? 'http://127.0.0.1:4182';
export const WORKSPACE = process.env.GOJET_TEST_WORKSPACE_ID ?? 'ws-p13-browser';
export const OWNER = process.env.GOJET_TEST_ACTOR_ID ?? 'p13-browser-owner';
export const OWNER_EMAIL = process.env.GOJET_TEST_ACTOR_EMAIL ?? 'p13-browser-owner@example.test';
export const VIEWER = process.env.GOJET_TEST_VIEWER_ACTOR ?? 'p13-browser-viewer';
export const VIEWER_EMAIL = process.env.GOJET_TEST_VIEWER_EMAIL ?? 'p13-browser-viewer@example.test';
export const ADMIN = process.env.GOJET_TEST_BILLING_ADMIN_ACTOR ?? 'p13-admin';
export const ADMIN_EMAIL = process.env.GOJET_TEST_BILLING_ADMIN_EMAIL ?? 'p13-admin@example.test';
export const DENIED_ADMIN = process.env.GOJET_TEST_BILLING_DENIED_ACTOR ?? 'p13-admin-denied';
export const DENIED_ADMIN_EMAIL = process.env.GOJET_TEST_BILLING_DENIED_EMAIL ?? 'p13-admin-denied@example.test';

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
if (!executablePath) throw new Error('System Chrome/Chromium is required for P13 browser evidence');

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
export function unique(prefix) { return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`.slice(0, 60); }

export function ensureWorkspace() {
  const ws = sqlLiteral(WORKSPACE);
  mysql(`INSERT INTO workspaces (id,name,status,version,created_by) VALUES (${ws},'P13 Browser Workspace','active',1,${sqlLiteral(OWNER)}) ON DUPLICATE KEY UPDATE name=VALUES(name),status='active'`);
  for (const [actor, email, role] of [[OWNER, OWNER_EMAIL, 'owner'], [VIEWER, VIEWER_EMAIL, 'viewer']]) {
    mysql(`INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES (${ws},${sqlLiteral(actor)},${sqlLiteral(email)},${sqlLiteral(actor)},${sqlLiteral(role)}) ON DUPLICATE KEY UPDATE email=VALUES(email),display_name=VALUES(display_name),role=VALUES(role)`);
  }
}

export function resetBilling() {
  const ws = sqlLiteral(WORKSPACE);
  mysql(`
    DELETE FROM billing_audit_events;
    DELETE FROM payment_callback_events;
    DELETE FROM billing_transactions;
    DELETE FROM billing_invoices;
    DELETE FROM billing_orders;
    DELETE FROM entitlement_grants WHERE workspace_id=${ws};
    DELETE FROM custom_domain_entitlement_sources WHERE workspace_id=${ws} AND source_key LIKE 'p13:billing%';
    DELETE FROM workspace_subscriptions WHERE workspace_id=${ws};
    DELETE FROM billing_plan_entitlements;
    DELETE FROM billing_fx_rates;
    DELETE FROM billing_plans;
  `);
  ensureWorkspace();
}

export function seedPlan({ code = unique('browser-plan').replaceAll('-', '_'), name = 'P13 Browser Plan', status = 'active', currency = 'USD', amount = 1900, period = 'monthly', entitlements = [['links', 100], ['custom_domains', 2]] } = {}) {
  const insertOut = mysql(`INSERT INTO billing_plans (code,name,status,currency,amount_minor,billing_period,version) VALUES (${sqlLiteral(code)},${sqlLiteral(name)},${sqlLiteral(status)},${sqlLiteral(currency)},${Number(amount)},${sqlLiteral(period)},1); SELECT LAST_INSERT_ID();`);
  const id = Number(insertOut.split('\n').at(-1));
  for (const [capability, limit, unit = 'count'] of entitlements) {
    mysql(`INSERT INTO billing_plan_entitlements (plan_id,capability,limit_value,unit,source_version) VALUES (${id},${sqlLiteral(capability)},${Number(limit)},${sqlLiteral(unit)},1)`);
  }
  return id;
}

export function seedSubscription(planId, status = 'active', id = unique('sub')) {
  const starts = "DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 10 DAY)";
  const term = "DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 20 DAY)";
  const grace = status === 'grace' ? "DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 7 DAY)" : 'NULL';
  const cancel = status === 'canceled' ? 'UTC_TIMESTAMP(6)' : 'NULL';
  mysql(`INSERT INTO workspace_subscriptions (id,workspace_id,plan_id,status,starts_at,current_term_ends_at,grace_ends_at,cancel_at,version) VALUES (${sqlLiteral(id)},${sqlLiteral(WORKSPACE)},${Number(planId)},${sqlLiteral(status)},${starts},${term},${grace},${cancel},1)`);
  return id;
}

export function seedOrder(planId, status = 'pending', id = unique('ord')) {
  mysql(`INSERT INTO billing_orders (id,workspace_id,plan_id,kind,currency,amount_minor,status,idempotency_key_hash,created_at,updated_at) VALUES (${sqlLiteral(id)},${sqlLiteral(WORKSPACE)},${Number(planId)},'new','USD',1900,${sqlLiteral(status)},UNHEX(SHA2(${sqlLiteral(unique('key'))},256)),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`);
  mysql(`INSERT INTO billing_invoices (id,workspace_id,order_id,currency,amount_minor,status,issued_at,paid_at,refunded_at) VALUES (${sqlLiteral(`inv-${id}`)},${sqlLiteral(WORKSPACE)},${sqlLiteral(id)},'USD',1900,${sqlLiteral(status === 'paid' ? 'paid' : status === 'refunded' ? 'refunded' : 'open')},UTC_TIMESTAMP(6),${status === 'paid' || status === 'refunded' ? 'UTC_TIMESTAMP(6)' : 'NULL'},${status === 'refunded' ? 'UTC_TIMESTAMP(6)' : 'NULL'})`);
  return id;
}

export function seedPayment(planId, status, provider = 'stripe') {
  const orderStatus = status === 'pending' ? 'pending' : status;
  const orderId = seedOrder(planId, orderStatus);
  const ref = unique(`provider-${status}`);
  const insertOut = mysql(`INSERT INTO billing_transactions (workspace_id,order_id,provider,provider_transaction_id,currency,amount_minor,status,created_at,updated_at) VALUES (${sqlLiteral(WORKSPACE)},${sqlLiteral(orderId)},${sqlLiteral(provider)},${sqlLiteral(ref)},'USD',1900,${sqlLiteral(status)},UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)); SELECT LAST_INSERT_ID();`);
  return { id: Number(insertOut.split('\n').at(-1)), orderId, providerRef: ref };
}

export function seedFX(status = 'current', base = 'USD', quote = 'EUR') {
  const reason = status === 'override' ? "'browser fixture override'" : 'NULL';
  mysql(`INSERT INTO billing_fx_rates (base_currency,quote_currency,rate,source,as_of,status,override_reason) VALUES (${sqlLiteral(base)},${sqlLiteral(quote)},1.125000000000,'browser-provider',UTC_TIMESTAMP(6),${sqlLiteral(status)},${reason}) ON DUPLICATE KEY UPDATE rate=VALUES(rate),source=VALUES(source),as_of=VALUES(as_of),status=VALUES(status),override_reason=VALUES(override_reason)`);
}

function authHeaders(actor, email) {
  return { Accept: 'application/json', 'Content-Type': 'application/json', 'X-GoJet-Test-Actor': actor, 'X-GoJet-Test-Email': email, 'X-GoJet-Test-Display-Name': actor };
}
export async function api(path, { method = 'GET', actor = OWNER, email = OWNER_EMAIL, body, headers = {} } = {}) {
  const response = await fetch(`${PLATFORM_URL}${path}`, { method, headers: { ...authHeaders(actor, email), ...headers }, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await response.text();
  let data = null; try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  return { status: response.status, headers: Object.fromEntries(response.headers.entries()), data, text };
}
export async function adminApi(path, options = {}) { return api(path, { ...options, actor: ADMIN, email: ADMIN_EMAIL, headers: { 'X-Request-Correlation-ID': unique('browser-admin'), ...(options.headers ?? {}) } }); }
export async function viewerApi(path, options = {}) { return api(path, { ...options, actor: VIEWER, email: VIEWER_EMAIL }); }

export function diagnostics() { return { console_errors: [], page_errors: [], request_failures: [], http_errors: [] }; }
export function attachDiagnostics(page, report, { allowStatuses = [] } = {}) {
  page.on('console', (message) => { if (message.type() === 'error') report.console_errors.push(message.text()); });
  page.on('pageerror', (error) => report.page_errors.push(String(error)));
  page.on('requestfailed', (request) => report.request_failures.push({ url: request.url(), failure: request.failure() }));
  page.on('response', (response) => { if (response.status() >= 400 && !allowStatuses.includes(response.status()) && !response.url().endsWith('/favicon.ico')) report.http_errors.push({ status: response.status(), url: response.url() }); });
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
export function writeResult(caseId, status, details, errors = []) {
  const payload = { case_id: caseId, status, generated_at: new Date().toISOString(), implementation_commit: implementationCommit(), environment: { browser: executablePath, platformapi: PLATFORM_URL, workspace_owner: WORKSPACE_OWNER_URL, workspace_viewer: WORKSPACE_VIEWER_URL, admin: ADMIN_URL, canonical_viewports: viewports, authority: 'real MySQL/Redis + native platformapi + built Workspace/Admin surfaces; no mock server' }, details, errors };
  writeFileSync(`${browserDir}/${caseId}.json`, `${JSON.stringify(payload, null, 2)}\n`);
}
