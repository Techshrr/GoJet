import { execFileSync, spawn } from 'node:child_process';
import { existsSync, mkdirSync, openSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const ROOT = process.cwd();
const HEAD = process.env.GITHUB_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
const OWNER_URL = (process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174').replace(/\/$/, '');
const VIEWER_URL = (process.env.GOJET_TEST_WORKSPACE_VIEWER_URL ?? 'http://127.0.0.1:4175').replace(/\/$/, '');
const INVITEE_URL = (process.env.GOJET_TEST_WORKSPACE_INVITEE_URL ?? 'http://127.0.0.1:4176').replace(/\/$/, '');
const PLATFORM_URL = (process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081').replace(/\/$/, '');
const PLATFORM_BINARY = process.env.GOJET_TEST_PLATFORM_BINARY ?? '/tmp/gojet-p12-browser-platformapi';
const PLATFORM_PID_FILE = process.env.GOJET_TEST_PLATFORM_PID_FILE ?? `${ROOT}/artifacts/v10/P12/runtime/browser-platformapi.pid`;
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
const INVITEE = 'p12-browser-invitee';
const INVITEE_EMAIL = 'p12-browser-invitee@example.test';
const browserDir = `${ROOT}/artifacts/v10/P12/browser`;
const capturesDir = `${ROOT}/artifacts/v10/P12/captures`;
const runtimeDir = `${ROOT}/artifacts/v10/P12/runtime`;
mkdirSync(browserDir, { recursive: true });
mkdirSync(capturesDir, { recursive: true });
mkdirSync(runtimeDir, { recursive: true });

const variables = JSON.parse(readFileSync(`${ROOT}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
function parseViewport(value, name) {
  const match = /^(\d+)×(\d+)$/.exec(String(value));
  if (!match) throw new Error(`Invalid ${name}: ${value}`);
  return { width: Number(match[1]), height: Number(match[2]) };
}
const viewports = {
  desktop: parseViewport(variables['viewport.desktop'].dimensions, 'desktop viewport'),
  tablet: parseViewport(variables['viewport.tablet'].dimensions, 'tablet viewport'),
  mobile: parseViewport(variables['viewport.mobile'].dimensions, 'mobile viewport'),
};
const expected = { desktop: { width: 1440, height: 900 }, tablet: { width: 1024, height: 768 }, mobile: { width: 390, height: 844 } };
for (const name of Object.keys(expected)) {
  if (JSON.stringify(viewports[name]) !== JSON.stringify(expected[name])) throw new Error(`P12 ${name} viewport drift`);
}
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((path) => existsSync(path));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P12 browser evidence');

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
function authHeaders(actor = OWNER, email = OWNER_EMAIL) {
  return { Accept: 'application/json', 'Content-Type': 'application/json', 'X-GoJet-Test-Actor': actor, 'X-GoJet-Test-Email': email, 'X-GoJet-Test-Display-Name': actor };
}
async function api(path, init = {}, actor = OWNER, email = OWNER_EMAIL) {
  const response = await fetch(`${PLATFORM_URL}${path}`, { ...init, headers: { ...authHeaders(actor, email), ...(init.headers ?? {}) } });
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
function assertDiagnostics(report, label, allowedStatuses = []) {
  const httpErrors = report.http_errors.filter((entry) => !allowedStatuses.includes(entry.status));
  const consoleErrors = report.console_errors.filter((entry) => {
    const match = /status of (\d{3})\b/.exec(entry);
    return !match || !allowedStatuses.includes(Number(match[1]));
  });
  assert(consoleErrors.length === 0, `${label} console errors ${JSON.stringify(consoleErrors)}`);
  assert(report.page_errors.length === 0, `${label} page errors ${JSON.stringify(report.page_errors)}`);
  assert(report.request_failures.length === 0, `${label} request failures ${JSON.stringify(report.request_failures)}`);
  assert(httpErrors.length === 0, `${label} HTTP errors ${JSON.stringify(httpErrors)}`);
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
async function openPage(browser, base, path, viewport = viewports.desktop) {
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
async function states(page) { return page.evaluate(() => window.__gojetP12States ?? []); }
async function screenshot(page, name) { const path = `${capturesDir}/${name}.png`; await page.screenshot({ path, fullPage: true }); return path.replace(`${ROOT}/`, ''); }
async function layout(page) {
  return page.evaluate(() => ({
    viewport: { width: innerWidth, height: innerHeight },
    root_overflow_px: Math.max(0, document.documentElement.scrollWidth - document.documentElement.clientWidth),
    body_overflow_px: Math.max(0, document.body.scrollWidth - document.body.clientWidth),
    clipped: [...document.querySelectorAll('main h1,main h2,main h3,main button,main a,main label,main dd,main code')]
      .filter((node) => node instanceof HTMLElement && node.offsetParent !== null && node.clientWidth > 0 && node.scrollWidth > node.clientWidth + 1)
      .map((node) => ({ tag: node.tagName, text: node.textContent?.trim().slice(0, 80), clientWidth: node.clientWidth, scrollWidth: node.scrollWidth })),
  }));
}
function assertLayout(value, label) { assert(value.root_overflow_px === 0 && value.body_overflow_px === 0, `${label} overflow ${JSON.stringify(value)}`); assert(value.clipped.length === 0, `${label} clipped ${JSON.stringify(value.clipped)}`); }
async function accessibility(page) {
  return page.evaluate(() => {
    const interactive = [...document.querySelectorAll('button,a,input,select,textarea')].filter((node) => node instanceof HTMLElement && node.offsetParent !== null);
    const unlabeled = interactive.filter((node) => {
      const text = node.textContent?.trim() ?? '';
      const aria = node.getAttribute('aria-label')?.trim() ?? '';
      const labelled = node.getAttribute('aria-labelledby')?.trim() ?? '';
      const labels = 'labels' in node && node.labels ? node.labels.length : 0;
      return !text && !aria && !labelled && !labels;
    }).map((node) => node.outerHTML.slice(0, 180));
    const headings = [...document.querySelectorAll('main h1')].filter((node) => node instanceof HTMLElement && node.offsetParent !== null).length;
    return { unlabeled, headings, has_workspace_nav: Boolean(document.querySelector('nav[aria-label="Workspace navigation"]')) };
  });
}
function writeResult(caseId, status, details, errors = []) {
  writeFileSync(`${browserDir}/${caseId}.json`, JSON.stringify({
    node: 'P12', case_id: caseId, status, implementation_commit: HEAD, generated_at: new Date().toISOString(),
    environment: { browser: executablePath, owner: OWNER_URL, viewer: VIEWER_URL, invitee: INVITEE_URL, platformapi: PLATFORM_URL, mysql: `${MYSQL_HOST}:${MYSQL_PORT}/${MYSQL_DATABASE}`, canonical_viewports: viewports, authority: 'real built Workspace variants + native Go platformapi + real MySQL/Redis; no browser request interception' },
    details, errors,
  }, null, 2) + '\n');
}
async function waitHealth() {
  for (let i = 0; i < 50; i += 1) {
    try { const response = await fetch(`${PLATFORM_URL}/healthz`); if (response.ok) return; } catch { /* unavailable */ }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error('platformapi did not become healthy');
}
async function stopPlatform() {
  const pid = Number(readFileSync(PLATFORM_PID_FILE, 'utf8').trim());
  if (Number.isFinite(pid) && pid > 1) {
    try { process.kill(pid, 'SIGTERM'); } catch { /* already stopped */ }
  }
  for (let i = 0; i < 30; i += 1) {
    try { const response = await fetch(`${PLATFORM_URL}/healthz`); if (!response.ok) return; } catch { return; }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error('platformapi did not stop');
}
async function startPlatform() {
  const log = openSync(`${runtimeDir}/browser-platformapi-restart.log`, 'a');
  const child = spawn(PLATFORM_BINARY, [], { detached: true, stdio: ['ignore', log, log], env: process.env });
  child.unref();
  writeFileSync(PLATFORM_PID_FILE, `${child.pid}\n`);
  await waitHealth();
}

async function caseT019(browser) {
  seedBase();
  const evidence = {};
  let opened = await openPage(browser, OWNER_URL, '/app');
  await waitState(opened.page, '[data-page="workspace-overview"]', 'complete');
  const history = await states(opened.page);
  assert(history.includes('workspace-overview:loading'), `overview loading state missing ${JSON.stringify(history)}`);
  const switcher = opened.page.getByLabel('Workspace switcher');
  await switcher.waitFor();
  assert(await switcher.locator('option').count() === 2, 'Workspace switcher did not list both memberships');
  assert(await opened.page.getByRole('heading', { name: 'P12 Browser Primary' }).count() === 1, 'primary Workspace authority missing');
  await Promise.all([opened.page.waitForNavigation({ waitUntil: 'networkidle' }), switcher.selectOption(ALT_WS)]);
  await waitState(opened.page, '[data-page="workspace-overview"]', 'complete');
  assert(await opened.page.getByRole('heading', { name: 'P12 Browser Alternate' }).count() === 1, 'Workspace switch did not change authority');
  const alternateSelected = await opened.page.evaluate(() => sessionStorage.getItem('gojet.p12.active-workspace'));
  assert(alternateSelected === ALT_WS, `alternate selection not persisted ${alternateSelected}`);
  await Promise.all([opened.page.waitForNavigation({ waitUntil: 'networkidle' }), opened.page.getByLabel('Workspace switcher').selectOption(WS)]);
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
  await opened.page.getByText('P12 Browser Renamed', { exact: true }).first().waitFor();
  assert(mysqlScalar(`SELECT CONCAT(name,'|',version) FROM workspaces WHERE id='${WS}'`) === 'P12 Browser Renamed|2', 'workspace settings did not persist/version');
  await screenshot(opened.page, 'P12-T019-workspace-settings');
  assertDiagnostics(opened.report, 'T019 settings');
  await opened.context.close();

  opened = await openPage(browser, VIEWER_URL, '/app/settings/workspace');
  await waitState(opened.page, '[data-page="workspace-settings"]', 'read-only');
  assert(await opened.page.getByRole('button', { name: 'Save Workspace settings' }).isDisabled(), 'viewer settings save was enabled');
  assertDiagnostics(opened.report, 'T019 viewer settings');
  await opened.context.close();
  evidence.workspace_switch = { primary: WS, alternate: ALT_WS };
  evidence.settings_persisted = true;
  evidence.viewer_read_only = true;
  return evidence;
}

async function caseT020(browser) {
  seedBase();
  const evidence = {};
  let opened = await openPage(browser, OWNER_URL, '/app/members');
  await waitState(opened.page, '[data-page="workspace-members"]', 'manage');
  await opened.page.getByLabel('Email').fill(INVITEE_EMAIL);
  await opened.page.getByLabel('Invitation role').selectOption('member');
  await opened.page.getByRole('button', { name: 'Create invitation' }).click();
  const code = opened.page.locator('.p12-invite-link code');
  await code.waitFor();
  const invitationPath = (await code.textContent())?.trim() ?? '';
  assert(invitationPath.startsWith('/invite/'), 'one-time invitation link missing');
  const token = invitationPath.slice('/invite/'.length);
  assert(token.length > 20, 'invitation token unexpectedly short');
  const persisted = Number(mysqlScalar(`SELECT COUNT(*) FROM workspace_invitations WHERE token_hash='${token.replaceAll("'", "''")}'`));
  assert(persisted === 0, 'raw invitation token persisted');
  assertDiagnostics(opened.report, 'T020 invitation create');
  await opened.context.close();

  opened = await openPage(browser, INVITEE_URL, invitationPath);
  await waitState(opened.page, '[data-page="invitation"]', 'pending');
  assert(await opened.page.getByText('P12 Browser Primary', { exact: true }).count() === 1, 'safe invitation Workspace name missing');
  assert(await opened.page.getByText('Yes', { exact: true }).count() === 1, 'account-match state missing');
  assert(!(await opened.page.locator('body').innerText()).includes(INVITEE_EMAIL), 'safe invitation inspection leaked invited email');
  await opened.page.getByRole('button', { name: 'Accept invitation' }).click();
  await waitState(opened.page, '[data-page="invitation"]', 'accepted');
  assert(mysqlScalar(`SELECT role FROM workspace_memberships WHERE workspace_id='${WS}' AND user_id='${INVITEE}'`) === 'member', 'accepted invitation did not create membership');
  await screenshot(opened.page, 'P12-T020-invitation-accepted');
  assertDiagnostics(opened.report, 'T020 invitation accept');
  await opened.context.close();

  const mismatch = await api(`/api/workspaces/${WS}/invitations`, { method: 'POST', body: JSON.stringify({ email: 'different-account@example.test', role: 'viewer', expires_at: new Date(Date.now() + 3600_000).toISOString(), reason: 'T020 mismatch' }) });
  assert(mismatch.response.status === 201, `mismatch invitation create failed ${mismatch.response.status}`);
  const mismatchToken = mismatch.body.token;
  opened = await openPage(browser, INVITEE_URL, `/invite/${encodeURIComponent(mismatchToken)}`);
  await waitState(opened.page, '[data-page="invitation"]', 'pending');
  assert(await opened.page.getByText('No', { exact: true }).count() === 1, 'account mismatch not shown');
  assert(await opened.page.getByRole('button', { name: 'Accept invitation' }).count() === 0, 'account mismatch exposed accept action');
  assert(!(await opened.page.locator('body').innerText()).includes('different-account@example.test'), 'mismatch inspection leaked invited email');
  assertDiagnostics(opened.report, 'T020 mismatch');
  await opened.context.close();
  evidence.accepted_role = 'member';
  evidence.safe_fields = ['workspace_name', 'role', 'status', 'expires_at', 'account_match'];
  evidence.account_mismatch_fail_closed = true;
  evidence.raw_token_persisted = false;
  return evidence;
}

async function caseT021(browser) {
  seedBase();
  const evidence = {};
  let opened = await openPage(browser, OWNER_URL, '/app/organization');
  await waitState(opened.page, '[data-page="workspace-organization"]', 'edit');
  await opened.page.getByLabel('Organization name').fill('P12 Organization Updated');
  await opened.page.getByLabel('Organization description').fill('Browser-governed organization metadata');
  await opened.page.getByRole('button', { name: 'Save organization' }).click();
  await opened.page.waitForFunction(() => document.querySelector('[data-page="workspace-organization"]')?.getAttribute('data-state') === 'edit');
  assert(mysqlScalar(`SELECT CONCAT(name,'|',version) FROM workspace_organizations WHERE workspace_id='${WS}'`) === 'P12 Organization Updated|2', 'organization update not persisted');
  assertDiagnostics(opened.report, 'T021 organization');
  await opened.context.close();

  opened = await openPage(browser, OWNER_URL, '/app/campaigns');
  await waitState(opened.page, '[data-page="workspace-campaigns"]', 'manage');
  await opened.page.getByLabel('Campaign name').fill('P12 Browser Campaign');
  await opened.page.getByRole('button', { name: 'Create campaign' }).click();
  await opened.page.getByRole('heading', { name: 'P12 Browser Campaign' }).waitFor();
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM workspace_campaigns WHERE workspace_id='${WS}' AND name='P12 Browser Campaign'`)) === 1, 'campaign not persisted');
  await opened.page.getByRole('button', { name: 'Archive' }).click();
  await opened.page.getByText('archived', { exact: true }).waitFor();
  assertDiagnostics(opened.report, 'T021 campaigns');
  await opened.context.close();

  opened = await openPage(browser, OWNER_URL, '/app/tags');
  await waitState(opened.page, '[data-page="workspace-tags"]', 'manage');
  await opened.page.getByLabel('Tag name').fill('重要标签');
  await opened.page.getByRole('button', { name: 'Create tag' }).click();
  await opened.page.getByText('重要标签', { exact: true }).waitFor();
  await opened.page.getByLabel('Folder name').fill('客户资料');
  await opened.page.getByRole('button', { name: 'Create folder' }).click();
  await opened.page.getByText('客户资料', { exact: true }).waitFor();
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM workspace_tags WHERE workspace_id='${WS}' AND name='重要标签'`)) === 1, 'tag not persisted');
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM workspace_folders WHERE workspace_id='${WS}' AND name='客户资料'`)) === 1, 'folder not persisted');
  assert(await opened.page.locator('a[href="/app/folders"]').count() === 0, 'forbidden /app/folders navigation link exists');
  await screenshot(opened.page, 'P12-T021-tags-folders');
  assertDiagnostics(opened.report, 'T021 tags/folders');
  await opened.context.close();

  const routerSource = readFileSync(`${ROOT}/frontend/apps/workspace/src/router.tsx`, 'utf8');
  assert(!routerSource.includes("path: '/app/folders'"), 'router introduced forbidden /app/folders route');
  opened = await openPage(browser, OWNER_URL, '/app/folders');
  await opened.page.waitForTimeout(300);
  assert(await opened.page.locator('[data-page="workspace-folders"]').count() === 0, 'direct /app/folders rendered a P12 folders page');
  await opened.context.close();
  evidence.organization_version = 2;
  evidence.campaign_created_and_archived = true;
  evidence.unicode_tag_and_folder = true;
  evidence.app_folders_route_absent = true;
  return evidence;
}

async function caseT022(browser) {
  seedBase();
  const first = produceNotification({ dedupe: 'p12-browser-security', category: 'security', title: 'Security review complete', summary: 'No action required.' });
  const second = produceNotification({ dedupe: 'p12-browser-resource', title: 'Workspace settings changed', summary: 'Review Workspace settings.', deepLink: '/app/settings/workspace' });
  assert(first.inserted === true && second.inserted === true, 'notification producer did not insert fixtures');
  const evidence = {};
  let opened = await openPage(browser, OWNER_URL, '/app');
  await waitState(opened.page, '[data-page="workspace-overview"]', 'complete');
  const notificationButton = opened.page.getByRole('button', { name: 'Notifications, 2 unread' });
  await notificationButton.waitFor();
  await notificationButton.click();
  await opened.page.getByRole('dialog').getByText('Security review complete', { exact: true }).waitFor();
  assertDiagnostics(opened.report, 'T022 shell notifications');
  await opened.context.close();

  opened = await openPage(browser, OWNER_URL, '/app/notifications');
  await waitState(opened.page, '[data-page="workspace-notifications"]', 'complete');
  assert(await opened.page.getByText('2 unread', { exact: true }).count() === 1, 'initial unread count mismatch');
  const settingsLink = opened.page.locator('a[href="/app/settings/workspace"]');
  assert(await settingsLink.count() === 1, 'authorized notification deep-link missing');
  await opened.page.getByRole('button', { name: 'Mark read' }).first().click();
  await opened.page.getByText('1 unread', { exact: true }).waitFor();
  await opened.page.getByRole('button', { name: 'Mark all read' }).click();
  await opened.page.getByText('0 unread', { exact: true }).waitFor();
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id='${WS}' AND recipient_user_id='${OWNER}' AND read_at IS NULL`)) === 0, 'read-all did not persist recipient state');
  await opened.page.getByRole('button', { name: 'Mark unread' }).first().click();
  await opened.page.getByText('1 unread', { exact: true }).waitFor();
  await screenshot(opened.page, 'P12-T022-notifications');
  assertDiagnostics(opened.report, 'T022 notifications');
  await opened.context.close();
  evidence.producer_inserted = 2;
  evidence.unread_lifecycle = [2, 1, 0, 1];
  evidence.authorized_deep_link = '/app/settings/workspace';
  return evidence;
}

async function caseT023(browser) {
  seedBase();
  const evidence = { layouts: {}, screenshots: [] };
  let opened = await openPage(browser, VIEWER_URL, '/app/campaigns');
  await waitState(opened.page, '[data-page="workspace-campaigns"]', 'read-only');
  assert(await opened.page.getByRole('button', { name: 'Create campaign' }).count() === 0, 'viewer exposed campaign mutation');
  assertDiagnostics(opened.report, 'T023 viewer read-only');
  await opened.context.close();

  for (const [name, viewport] of Object.entries(viewports)) {
    opened = await openPage(browser, OWNER_URL, '/app/tags', viewport);
    await waitState(opened.page, '[data-page="workspace-tags"]', 'manage');
    const value = await layout(opened.page); assertLayout(value, `T023 ${name}`);
    const a11y = await accessibility(opened.page);
    assert(a11y.unlabeled.length === 0, `T023 ${name} unlabeled controls ${JSON.stringify(a11y.unlabeled)}`);
    assert(a11y.headings === 1 && a11y.has_workspace_nav, `T023 ${name} landmark/heading mismatch ${JSON.stringify(a11y)}`);
    evidence.layouts[name] = value;
    evidence.screenshots.push(await screenshot(opened.page, `P12-T023-${name}`));
    assertDiagnostics(opened.report, `T023 ${name}`);
    await opened.context.close();
  }

  await stopPlatform();
  try {
    opened = await openPage(browser, OWNER_URL, '/app');
    await waitState(opened.page, '[data-page="workspace-overview"]', 'error');
    await opened.page.waitForFunction(() => document.querySelector('[data-shell="workspace"]')?.getAttribute('data-state') === 'api-offline');
    assert(await opened.page.getByText('API is offline. Local navigation remains available.').count() === 1, 'offline shell warning missing');
    evidence.offline = { page_state: 'error', shell_state: 'api-offline', request_failures: opened.report.request_failures.length, http_errors: opened.report.http_errors.length };
    await screenshot(opened.page, 'P12-T023-api-offline');
    await opened.context.close();
  } finally {
    await startPlatform();
  }
  const health = await fetch(`${PLATFORM_URL}/healthz`);
  assert(health.ok, 'native API did not recover after offline evidence');
  evidence.viewer_read_only = true;
  evidence.accessibility = 'no visible unlabeled interactive controls; one main h1; Workspace navigation landmark present';
  evidence.native_api_recovered = true;
  return evidence;
}

const cases = { 'P12-T019': caseT019, 'P12-T020': caseT020, 'P12-T021': caseT021, 'P12-T022': caseT022, 'P12-T023': caseT023 };
async function main() {
  const index = process.argv.indexOf('--case');
  const caseId = index >= 0 ? process.argv[index + 1] : '';
  if (!cases[caseId]) throw new Error('case must be P12-T019..P12-T023');
  const browser = await chromium.launch({ headless: true, executablePath, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
  try {
    const details = await cases[caseId](browser);
    writeResult(caseId, 'PASS', details, []);
    console.log(JSON.stringify({ case_id: caseId, status: 'PASS', implementation_commit: HEAD }, null, 2));
  } catch (error) {
    const text = `${error?.name ?? 'Error'}: ${error?.message ?? String(error)}`;
    writeResult(caseId, 'FAIL', {}, [text]);
    console.error(text);
    process.exitCode = 1;
  } finally {
    await browser.close();
  }
}
await main();
