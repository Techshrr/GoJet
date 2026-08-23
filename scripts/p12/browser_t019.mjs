import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const ROOT = process.cwd();
const HEAD = process.env.GITHUB_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
const OWNER_URL = (process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174').replace(/\/$/, '');
const VIEWER_URL = (process.env.GOJET_TEST_WORKSPACE_VIEWER_URL ?? 'http://127.0.0.1:4175').replace(/\/$/, '');
const PLATFORM_URL = (process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081').replace(/\/$/, '');
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
const browserDir = `${ROOT}/artifacts/v10/P12/browser`;
const capturesDir = `${ROOT}/artifacts/v10/P12/captures`;
mkdirSync(browserDir, { recursive: true });
mkdirSync(capturesDir, { recursive: true });

const variables = JSON.parse(readFileSync(`${ROOT}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
const viewportMatch = /^(\d+)×(\d+)$/.exec(String(variables['viewport.desktop'].dimensions));
if (!viewportMatch) throw new Error('Invalid desktop viewport token');
const viewport = { width: Number(viewportMatch[1]), height: Number(viewportMatch[2]) };
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((path) => existsSync(path));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P12-T019');

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
const stateTracker = () => {
  window.__gojetP12States = [];
  const capture = () => document.querySelectorAll('[data-page][data-state]').forEach((node) => {
    const value = `${node.getAttribute('data-page')}:${node.getAttribute('data-state')}`;
    if (!window.__gojetP12States.includes(value)) window.__gojetP12States.push(value);
  });
  const start = () => { capture(); new MutationObserver(capture).observe(document.documentElement, { subtree: true, childList: true, attributes: true, attributeFilter: ['data-page', 'data-state'] }); };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start, { once: true }); else start();
};
async function openPage(browser, base, path) {
  const context = await browser.newContext({ viewport, deviceScaleFactor: 1 });
  await context.addInitScript(stateTracker);
  const page = await context.newPage();
  const report = diagnostics(); attachDiagnostics(page, report);
  await page.goto(`${base}${path}`, { waitUntil: 'networkidle' });
  return { context, page, report };
}
async function waitState(page, selector, state) {
  await page.locator(selector).waitFor();
  await page.waitForFunction(([s, v]) => document.querySelector(s)?.getAttribute('data-state') === v, [selector, state]);
}
async function screenshot(page, name) { await page.screenshot({ path: `${capturesDir}/${name}.png`, fullPage: true }); }
function writeResult(status, details, errors = []) {
  writeFileSync(`${browserDir}/P12-T019.json`, JSON.stringify({
    node: 'P12', case_id: 'P12-T019', status, implementation_commit: HEAD, generated_at: new Date().toISOString(),
    environment: { browser: executablePath, owner: OWNER_URL, viewer: VIEWER_URL, platformapi: PLATFORM_URL, mysql: `${MYSQL_HOST}:${MYSQL_PORT}/${MYSQL_DATABASE}`, desktop_viewport: viewport, authority: 'real built Workspace variants + native Go platformapi + real MySQL/Redis; no browser request interception' },
    details, errors,
  }, null, 2) + '\n');
}

const browser = await chromium.launch({ headless: true, executablePath, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
try {
  seedBase();
  let opened = await openPage(browser, OWNER_URL, '/app');
  await waitState(opened.page, '[data-page="workspace-overview"]', 'complete');
  const history = await opened.page.evaluate(() => window.__gojetP12States ?? []);
  assert(history.includes('workspace-overview:loading'), `overview loading state missing ${JSON.stringify(history)}`);
  let switcher = opened.page.getByLabel('Workspace switcher');
  await switcher.waitFor();
  assert(await switcher.locator('option').count() === 2, 'Workspace switcher did not list both memberships');
  assert(await opened.page.getByRole('heading', { name: 'P12 Browser Primary' }).count() === 1, 'primary Workspace authority missing');
  await Promise.all([opened.page.waitForNavigation({ waitUntil: 'networkidle' }), switcher.selectOption(ALT_WS)]);
  await waitState(opened.page, '[data-page="workspace-overview"]', 'complete');
  assert(await opened.page.getByRole('heading', { name: 'P12 Browser Alternate' }).count() === 1, 'Workspace switch did not change authority');
  assert(await opened.page.evaluate(() => sessionStorage.getItem('gojet.p12.active-workspace')) === ALT_WS, 'alternate Workspace selection not persisted');
  switcher = opened.page.getByLabel('Workspace switcher');
  await Promise.all([opened.page.waitForNavigation({ waitUntil: 'networkidle' }), switcher.selectOption(WS)]);
  await waitState(opened.page, '[data-page="workspace-overview"]', 'complete');
  await screenshot(opened.page, 'P12-T019-overview-switcher');
  assertDiagnostics(opened.report, 'T019 overview/switcher');
  await opened.context.close();

  opened = await openPage(browser, OWNER_URL, '/app/settings/workspace');
  await waitState(opened.page, '[data-page="workspace-settings"]', 'edit');
  await opened.page.getByLabel('Workspace name').fill('P12 Browser Renamed');
  await opened.page.getByLabel('Change reason').fill('P12 browser settings proof');
  await Promise.all([
    opened.page.waitForResponse((response) => response.url().includes(`/api/workspaces/${WS}`) && response.request().method() === 'PATCH' && response.status() === 200),
    opened.page.getByRole('button', { name: 'Save Workspace settings' }).click(),
  ]);
  await opened.page.waitForFunction(() => {
    const select = document.querySelector('select[aria-label="Workspace switcher"]');
    return select instanceof HTMLSelectElement && select.selectedOptions[0]?.textContent?.trim() === 'P12 Browser Renamed';
  });
  const selectedWorkspaceLabel = await opened.page.getByLabel('Workspace switcher').locator('option:checked').textContent();
  assert(selectedWorkspaceLabel?.trim() === 'P12 Browser Renamed', `renamed Workspace authority did not refresh: ${selectedWorkspaceLabel}`);
  assert(mysqlScalar(`SELECT CONCAT(name,'|',version) FROM workspaces WHERE id='${WS}'`) === 'P12 Browser Renamed|2', 'workspace settings did not persist/version');
  await screenshot(opened.page, 'P12-T019-workspace-settings');
  assertDiagnostics(opened.report, 'T019 settings');
  await opened.context.close();

  opened = await openPage(browser, VIEWER_URL, '/app/settings/workspace');
  await waitState(opened.page, '[data-page="workspace-settings"]', 'read-only');
  assert(await opened.page.getByRole('button', { name: 'Save Workspace settings' }).isDisabled(), 'viewer settings save was enabled');
  assertDiagnostics(opened.report, 'T019 viewer settings');
  await opened.context.close();

  writeResult('PASS', {
    workspace_switch: { primary: WS, alternate: ALT_WS },
    settings_persisted: true,
    settings_version: 2,
    settings_authority_refresh: 'selected Workspace option refreshed from server-authoritative list',
    viewer_read_only: true,
  });
  console.log(JSON.stringify({ case_id: 'P12-T019', status: 'PASS', implementation_commit: HEAD }, null, 2));
} catch (error) {
  const text = `${error?.name ?? 'Error'}: ${error?.message ?? String(error)}`;
  writeResult('FAIL', {}, [text]);
  console.error(text);
  process.exitCode = 1;
} finally {
  await browser.close();
}
