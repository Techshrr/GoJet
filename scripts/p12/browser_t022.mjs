import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const ROOT = process.cwd();
const HEAD = process.env.GITHUB_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
const OWNER_URL = (process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174').replace(/\/$/, '');
const PLATFORM_URL = (process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081').replace(/\/$/, '');
const PRODUCER = process.env.GOJET_TEST_P12_PRODUCER ?? '/tmp/gojet-p12-browser-producer';
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
const WS = 'ws-p12-browser';
const ALT_WS = 'ws-p12-alt';
const OWNER = 'p12-browser-owner';
const OWNER_EMAIL = 'p12-browser-owner@example.test';
const VIEWER = 'p12-browser-viewer';
const VIEWER_EMAIL = 'p12-browser-viewer@example.test';
const EXPECTED_DEEP_LINK = '/app/settings/workspace';
const browserDir = `${ROOT}/artifacts/v10/P12/browser`;
const capturesDir = `${ROOT}/artifacts/v10/P12/captures`;
mkdirSync(browserDir, { recursive: true });
mkdirSync(capturesDir, { recursive: true });

const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((path) => existsSync(path));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P12 T022 browser evidence');

function assert(condition, message) { if (!condition) throw new Error(message); }
function mysql(sql) {
  return execFileSync('mysql', ['--protocol=tcp', '-h', MYSQL_HOST, '-P', MYSQL_PORT, '-u', MYSQL_USER, '--default-character-set=utf8mb4', '-N', '-B', MYSQL_DATABASE, '-e', sql], {
    encoding: 'utf8', env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
  }).trim();
}
function mysqlScalar(sql) { return mysql(sql).trim(); }
function seedBase() {
  mysql(`SET FOREIGN_KEY_CHECKS=0;
TRUNCATE TABLE workspace_audit_events;
TRUNCATE TABLE workspace_notifications;
TRUNCATE TABLE workspace_notification_state;
TRUNCATE TABLE workspace_link_tags;
TRUNCATE TABLE workspace_link_organization;
TRUNCATE TABLE workspace_folders;
TRUNCATE TABLE workspace_tags;
TRUNCATE TABLE workspace_campaigns;
TRUNCATE TABLE workspace_organizations;
TRUNCATE TABLE workspace_invitations;
TRUNCATE TABLE workspace_memberships;
TRUNCATE TABLE workspaces;
SET FOREIGN_KEY_CHECKS=1;
INSERT INTO workspaces (id,name,status,version,created_by) VALUES
('${WS}','P12 Browser Primary','active',1,'${OWNER}'),
('${ALT_WS}','P12 Browser Alternate','active',1,'${OWNER}');
INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES
('${WS}','${OWNER}','${OWNER_EMAIL}','P12 Owner','owner'),
('${WS}','${VIEWER}','${VIEWER_EMAIL}','P12 Viewer','viewer'),
('${ALT_WS}','${OWNER}','${OWNER_EMAIL}','P12 Owner','owner');
INSERT INTO workspace_organizations (workspace_id,name,description,version) VALUES
('${WS}','P12 Browser Primary','Primary organization',1),
('${ALT_WS}','P12 Browser Alternate','Alternate organization',1);
INSERT INTO workspace_notification_state (workspace_id,status,data_through_at,state_reason) VALUES
('${WS}','complete',CURRENT_TIMESTAMP(6),'current'),
('${ALT_WS}','complete',CURRENT_TIMESTAMP(6),'current');`);
}
function authHeaders() {
  return { Accept: 'application/json', 'Content-Type': 'application/json', 'X-GoJet-Test-Actor': OWNER, 'X-GoJet-Test-Email': OWNER_EMAIL, 'X-GoJet-Test-Display-Name': 'P12 Owner' };
}
async function api(path, init = {}) {
  const response = await fetch(`${PLATFORM_URL}${path}`, { ...init, headers: { ...authHeaders(), ...(init.headers ?? {}) } });
  const type = response.headers.get('content-type') ?? '';
  const body = response.status === 204 ? null : type.includes('application/json') ? await response.json() : await response.text();
  return { response, body };
}
function produceNotification({ dedupe, category = 'resources', title, summary, deepLink = '' }) {
  const args = ['--action', 'notification', '--workspace', WS, '--recipient', OWNER, '--category', category, '--event-key', `browser.${dedupe}`, '--dedupe-key', dedupe, '--title', title, '--summary', summary];
  if (deepLink) args.push('--deep-link', deepLink);
  return JSON.parse(execFileSync(PRODUCER, args, { encoding: 'utf8', env: process.env }));
}
function diagnostics() { return { console_errors: [], page_errors: [], http_errors: [], request_failures: [] }; }
function attachDiagnostics(page, report) {
  page.on('console', (entry) => { if (entry.type() === 'error') report.console_errors.push(entry.text()); });
  page.on('pageerror', (error) => report.page_errors.push(String(error)));
  page.on('response', (response) => { if (response.status() >= 400 && !response.url().endsWith('/favicon.ico')) report.http_errors.push({ status: response.status(), url: response.url() }); });
  page.on('requestfailed', (request) => report.request_failures.push({ url: request.url(), failure: request.failure() }));
}
function assertDiagnostics(report, label) {
  assert(report.console_errors.length === 0, `${label} console errors ${JSON.stringify(report.console_errors)}`);
  assert(report.page_errors.length === 0, `${label} page errors ${JSON.stringify(report.page_errors)}`);
  assert(report.request_failures.length === 0, `${label} request failures ${JSON.stringify(report.request_failures)}`);
  assert(report.http_errors.length === 0, `${label} HTTP errors ${JSON.stringify(report.http_errors)}`);
}
async function openPage(browser, path) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
  const page = await context.newPage();
  const report = diagnostics();
  attachDiagnostics(page, report);
  await page.goto(`${OWNER_URL}${path}`, { waitUntil: 'networkidle' });
  return { context, page, report };
}
async function waitState(page, selector, state) {
  await page.locator(selector).waitFor();
  await page.waitForFunction(([s, v]) => document.querySelector(s)?.getAttribute('data-state') === v, [selector, state]);
}
function writeResult(status, details, errors = []) {
  writeFileSync(`${browserDir}/P12-T022.json`, JSON.stringify({
    node: 'P12',
    case_id: 'P12-T022',
    status,
    implementation_commit: HEAD,
    generated_at: new Date().toISOString(),
    environment: {
      browser: executablePath,
      owner: OWNER_URL,
      platformapi: PLATFORM_URL,
      mysql: `${MYSQL_HOST}:${MYSQL_PORT}/${MYSQL_DATABASE}`,
      authority: 'real built Workspace + native Go platformapi + real MySQL/Redis; notification deep-link proven producer->DB->API->scoped DOM',
    },
    details,
    errors,
  }, null, 2) + '\n');
}

async function run(browser) {
  seedBase();
  const first = produceNotification({ dedupe: 'p12-browser-security', category: 'security', title: 'Security review complete', summary: 'No action required.' });
  const second = produceNotification({ dedupe: 'p12-browser-resource', title: 'Workspace settings changed', summary: 'Review Workspace settings.', deepLink: EXPECTED_DEEP_LINK });
  assert(first.inserted === true && second.inserted === true, 'notification producer did not insert fixtures');
  assert(second.notification?.deep_link === EXPECTED_DEEP_LINK, `producer deep-link mismatch ${JSON.stringify(second.notification)}`);

  const storedDeepLink = mysqlScalar(`SELECT COALESCE(deep_link,'') FROM workspace_notifications WHERE workspace_id='${WS}' AND recipient_user_id='${OWNER}' AND event_key='browser.p12-browser-resource'`);
  assert(storedDeepLink === EXPECTED_DEEP_LINK, `stored deep-link mismatch ${storedDeepLink}`);

  const listed = await api(`/api/workspaces/${WS}/notifications?category=all&limit=50`);
  assert(listed.response.status === 200, `notification API failed ${listed.response.status}`);
  const apiItem = listed.body?.items?.find((item) => item.event_key === 'browser.p12-browser-resource');
  assert(apiItem?.deep_link === EXPECTED_DEEP_LINK, `API deep-link mismatch ${JSON.stringify(apiItem)}`);

  let opened = await openPage(browser, '/app');
  await waitState(opened.page, '[data-page="workspace-overview"]', 'complete');
  const notificationButton = opened.page.getByRole('button', { name: 'Notifications, 2 unread' });
  await notificationButton.waitFor();
  await notificationButton.click();
  await opened.page.getByRole('dialog').getByText('Security review complete', { exact: true }).waitFor();
  assert(await opened.page.getByRole('dialog').getByRole('link', { name: 'View all notifications' }).count() === 1, 'notification popover View all link missing');
  assertDiagnostics(opened.report, 'T022 shell notifications');
  await opened.context.close();

  opened = await openPage(browser, '/app/notifications');
  await waitState(opened.page, '[data-page="workspace-notifications"]', 'complete');
  assert(await opened.page.getByText('2 unread', { exact: true }).count() === 1, 'initial unread count mismatch');

  const resourceCard = opened.page.getByRole('article').filter({ has: opened.page.getByRole('heading', { name: 'Workspace settings changed', exact: true }) });
  assert(await resourceCard.count() === 1, 'resource notification card missing');
  const notificationDeepLink = resourceCard.locator(`a[href="${EXPECTED_DEEP_LINK}"]`);
  assert(await notificationDeepLink.count() === 1, 'authorized notification deep-link missing from resource notification card');

  await opened.page.getByLabel('Notification category').selectOption('security');
  await waitState(opened.page, '[data-page="workspace-notifications"]', 'filtered');
  assert(await opened.page.getByRole('article').count() === 1, 'security notification filter did not narrow list');
  assert(await opened.page.getByRole('heading', { name: 'Security review complete', exact: true }).count() === 1, 'security filtered notification missing');
  await opened.page.getByLabel('Notification category').selectOption('all');
  await waitState(opened.page, '[data-page="workspace-notifications"]', 'complete');

  await opened.page.getByRole('button', { name: 'Mark read' }).first().click();
  await opened.page.getByText('1 unread', { exact: true }).waitFor();
  await opened.page.getByRole('button', { name: 'Mark all read' }).click();
  await opened.page.getByText('0 unread', { exact: true }).waitFor();
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id='${WS}' AND recipient_user_id='${OWNER}' AND read_at IS NULL`)) === 0, 'read-all did not persist recipient state');
  await opened.page.getByRole('button', { name: 'Mark unread' }).first().click();
  await opened.page.getByText('1 unread', { exact: true }).waitFor();
  await opened.page.screenshot({ path: `${capturesDir}/P12-T022-notifications.png`, fullPage: true });
  assertDiagnostics(opened.report, 'T022 notifications');
  await opened.context.close();

  return {
    producer_inserted: 2,
    producer_deep_link: second.notification.deep_link,
    stored_deep_link: storedDeepLink,
    api_deep_link: apiItem.deep_link,
    scoped_dom_deep_link: EXPECTED_DEEP_LINK,
    filter_verified: 'security',
    unread_lifecycle: [2, 1, 0, 1],
  };
}

const browser = await chromium.launch({ headless: true, executablePath, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
try {
  const details = await run(browser);
  writeResult('PASS', details, []);
  console.log(JSON.stringify({ case_id: 'P12-T022', status: 'PASS', implementation_commit: HEAD }, null, 2));
} catch (error) {
  const text = `${error?.name ?? 'Error'}: ${error?.message ?? String(error)}`;
  writeResult('FAIL', {}, [text]);
  console.error(text);
  process.exitCode = 1;
} finally {
  await browser.close();
}
