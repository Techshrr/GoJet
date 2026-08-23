import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const ROOT = process.cwd();
const OWNER_URL = (process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174').replace(/\/$/, '');
const VIEWER_URL = (process.env.GOJET_TEST_WORKSPACE_VIEWER_URL ?? 'http://127.0.0.1:4175').replace(/\/$/, '');
const INVITEE_URL = (process.env.GOJET_TEST_WORKSPACE_INVITEE_URL ?? 'http://127.0.0.1:4176').replace(/\/$/, '');
const UNAUTH_URL = (process.env.GOJET_TEST_WORKSPACE_UNAUTH_URL ?? 'http://127.0.0.1:4177').replace(/\/$/, '');
const PLATFORM_URL = (process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081').replace(/\/$/, '');
const PRODUCER = process.env.GOJET_TEST_P12_PRODUCER ?? '/tmp/gojet-p12-browser-producer';
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
const WS = 'ws-p12-browser';
const FOREIGN_WS = 'ws-p12-browser-foreign';
const OWNER = 'p12-browser-owner';
const OWNER_EMAIL = 'p12-browser-owner@example.test';
const INVITEE_EMAIL = 'p12-browser-invitee@example.test';
const resultDir = `${ROOT}/artifacts/v10/P12/browser`;
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((path) => existsSync(path));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P12 browser contract completion');

function assert(condition, message) { if (!condition) throw new Error(message); }
function mysql(sql) {
  return execFileSync('mysql', ['--protocol=tcp', '-h', MYSQL_HOST, '-P', MYSQL_PORT, '-u', MYSQL_USER, '--default-character-set=utf8mb4', '-N', '-B', MYSQL_DATABASE, '-e', sql], {
    encoding: 'utf8', env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
  }).trim();
}
function authHeaders() { return { Accept: 'application/json', 'Content-Type': 'application/json', 'X-GoJet-Test-Actor': OWNER, 'X-GoJet-Test-Email': OWNER_EMAIL, 'X-GoJet-Test-Display-Name': 'P12 Owner' }; }
async function api(method, path, body) {
  const response = await fetch(`${PLATFORM_URL}${path}`, { method, headers: authHeaders(), ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
  const type = response.headers.get('content-type') ?? '';
  const value = response.status === 204 ? null : type.includes('application/json') ? await response.json() : await response.text();
  return { response, value };
}
async function createInvite(email = INVITEE_EMAIL) {
  const created = await api('POST', `/api/workspaces/${WS}/invitations`, { email, role: 'member', expires_at: new Date(Date.now() + 3600_000).toISOString(), reason: 'P12 browser contract state' });
  assert(created.response.status === 201, `invitation create failed ${created.response.status} ${JSON.stringify(created.value)}`);
  return created.value;
}
function producer(...args) { return JSON.parse(execFileSync(PRODUCER, args, { encoding: 'utf8', env: process.env })); }
async function openPage(browser, base, path, viewport = { width: 1440, height: 900 }, contextOptions = {}) {
  const context = await browser.newContext({ viewport, deviceScaleFactor: 1, ...contextOptions });
  const page = await context.newPage();
  await page.goto(`${base}${path}`, { waitUntil: 'networkidle' });
  return { context, page };
}
async function waitState(page, selector, state) {
  await page.locator(selector).waitFor();
  await page.waitForFunction(([s, v]) => document.querySelector(s)?.getAttribute('data-state') === v, [selector, state]);
}
async function layout(page) {
  return page.evaluate(() => ({
    root_overflow_px: Math.max(0, document.documentElement.scrollWidth - document.documentElement.clientWidth),
    body_overflow_px: Math.max(0, document.body.scrollWidth - document.body.clientWidth),
    clipped: [...document.querySelectorAll('main h1,main h2,main h3,main button,main a,main label,main dd,main code')]
      .filter((node) => node instanceof HTMLElement && node.offsetParent !== null && node.clientWidth > 0 && node.scrollWidth > node.clientWidth + 1)
      .map((node) => ({ tag: node.tagName, text: node.textContent?.trim().slice(0, 80), clientWidth: node.clientWidth, scrollWidth: node.scrollWidth })),
  }));
}
function mergeResult(caseId, completion) {
  const path = `${resultDir}/${caseId}.json`;
  const data = JSON.parse(readFileSync(path, 'utf8'));
  assert(data.status === 'PASS' && Array.isArray(data.errors) && data.errors.length === 0, `${caseId} base evidence is not PASS`);
  data.details = { ...(data.details ?? {}), frozen_contract_completion: completion };
  writeFileSync(path, JSON.stringify(data, null, 2) + '\n');
}
function failResult(caseId, error) {
  const path = `${resultDir}/${caseId}.json`;
  let data = { node: 'P12', case_id: caseId, status: 'FAIL', errors: [] };
  try { data = JSON.parse(readFileSync(path, 'utf8')); } catch { /* base did not create result */ }
  data.status = 'FAIL';
  data.errors = [...(Array.isArray(data.errors) ? data.errors : []), `${error?.name ?? 'Error'}: ${error?.message ?? String(error)}`];
  writeFileSync(path, JSON.stringify(data, null, 2) + '\n');
}

async function completeT019(browser) {
  mysql(`INSERT IGNORE INTO workspaces (id,name,status,version,created_by) VALUES ('${FOREIGN_WS}','P12 Browser Foreign','active',1,'foreign-user');
INSERT IGNORE INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES ('${FOREIGN_WS}','foreign-user','foreign-user@example.test','Foreign User','owner');
INSERT IGNORE INTO workspace_organizations (workspace_id,name,description,version) VALUES ('${FOREIGN_WS}','P12 Browser Foreign','Foreign authority',1);
INSERT IGNORE INTO workspace_notification_state (workspace_id,status,data_through_at,state_reason) VALUES ('${FOREIGN_WS}','complete',CURRENT_TIMESTAMP(6),'current');`);
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
  await context.addInitScript((workspaceId) => sessionStorage.setItem('gojet.p12.active-workspace', workspaceId), FOREIGN_WS);
  const page = await context.newPage();
  await page.goto(`${OWNER_URL}/app`, { waitUntil: 'networkidle' });
  await waitState(page, '[data-page="workspace-overview"]', 'error');
  await page.waitForFunction(() => document.querySelector('[data-shell="workspace"]')?.getAttribute('data-state') === 'api-offline');
  assert(!(await page.locator('body').innerText()).includes('P12 Browser Foreign'), 'unauthorized Workspace switch leaked foreign name');
  const options = await page.getByLabel('Workspace switcher').locator('option').allTextContents();
  assert(!options.includes('P12 Browser Foreign'), `foreign Workspace appeared in switcher ${JSON.stringify(options)}`);
  await context.close();
  return { unauthorized_switch: 'forced client selection denied without foreign metadata', visible_switcher_memberships_only: true };
}

async function completeT020(browser) {
  let opened = await openPage(browser, OWNER_URL, '/app/members');
  await waitState(opened.page, '[data-page="workspace-members"]', 'manage');
  const ownerCard = opened.page.locator('[data-member-role="owner"]').first();
  await ownerCard.getByRole('button', { name: 'Remove' }).click();
  await waitState(opened.page, '[data-page="workspace-members"]', 'last-owner-protected');
  assert(Number(mysql(`SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id='${WS}' AND role='owner'`)) === 1, 'last-owner browser action removed owner');
  await opened.context.close();

  const rejected = await createInvite();
  opened = await openPage(browser, INVITEE_URL, `/invite/${encodeURIComponent(rejected.token)}`);
  await waitState(opened.page, '[data-page="invitation"]', 'pending');
  await opened.page.getByRole('button', { name: 'Reject invitation' }).click();
  await waitState(opened.page, '[data-page="invitation"]', 'rejected');
  await opened.context.close();

  const expired = await createInvite();
  mysql(`UPDATE workspace_invitations SET created_at=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 2 HOUR), expires_at=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE id=${Number(expired.invitation.id)}`);
  opened = await openPage(browser, INVITEE_URL, `/invite/${encodeURIComponent(expired.token)}`);
  await waitState(opened.page, '[data-page="invitation"]', 'expired');
  assert(await opened.page.getByRole('button', { name: 'Accept invitation' }).count() === 0, 'expired invitation exposed accept');
  await opened.context.close();

  const revoked = await createInvite();
  const revokedResponse = await api('DELETE', `/api/workspaces/${WS}/invitations/${revoked.invitation.id}`);
  assert(revokedResponse.response.status === 204, `revoke failed ${revokedResponse.response.status}`);
  opened = await openPage(browser, INVITEE_URL, `/invite/${encodeURIComponent(revoked.token)}`);
  await waitState(opened.page, '[data-page="invitation"]', 'revoked');
  assert(await opened.page.getByRole('button', { name: 'Accept invitation' }).count() === 0, 'revoked invitation exposed accept');
  await opened.context.close();

  const unauth = await createInvite();
  opened = await openPage(browser, UNAUTH_URL, `/invite/${encodeURIComponent(unauth.token)}`);
  await waitState(opened.page, '[data-page="invitation"]', 'authentication-required');
  const body = await opened.page.locator('body').innerText();
  assert(!body.includes('P12 Browser Primary') && !body.includes(INVITEE_EMAIL), 'unauthenticated invitation route disclosed invitation metadata');
  await opened.context.close();
  return { last_owner_protected: true, invite_states_added: ['rejected', 'expired', 'revoked', 'unauthenticated'], raw_tokens_recorded_in_evidence: false };
}

async function completeT021(browser) {
  let opened = await openPage(browser, VIEWER_URL, '/app/organization');
  await waitState(opened.page, '[data-page="workspace-organization"]', 'read-only');
  assert(await opened.page.getByRole('button', { name: 'Save organization' }).isDisabled(), 'viewer organization save enabled');
  await opened.context.close();
  opened = await openPage(browser, VIEWER_URL, '/app/tags');
  await waitState(opened.page, '[data-page="workspace-tags"]', 'read-only');
  assert(await opened.page.getByRole('button', { name: 'Create tag' }).count() === 0, 'viewer exposed create tag');
  assert(await opened.page.getByRole('button', { name: 'Create folder' }).count() === 0, 'viewer exposed create folder');
  await opened.context.close();
  return { viewer_organization_read_only: true, viewer_tag_folder_read_only: true, folder_route_remains_absent: true };
}

async function completeT022(browser) {
  let opened = await openPage(browser, OWNER_URL, '/app');
  await waitState(opened.page, '[data-page="workspace-overview"]', 'complete');
  const bell = opened.page.getByRole('button', { name: /Notifications, \d+ unread/ });
  await bell.click();
  const dialog = opened.page.getByRole('dialog');
  await dialog.getByRole('link', { name: 'View all notifications' }).waitFor();
  await opened.context.close();

  opened = await openPage(browser, OWNER_URL, '/app/notifications');
  await waitState(opened.page, '[data-page="workspace-notifications"]', 'complete');
  await opened.page.getByLabel('Notification category').selectOption('security');
  await waitState(opened.page, '[data-page="workspace-notifications"]', 'filtered');
  const categories = await opened.page.locator('[data-notification-category]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('data-notification-category')));
  assert(categories.length > 0 && categories.every((value) => value === 'security'), `notification filter returned ${JSON.stringify(categories)}`);
  await opened.context.close();

  producer('--action', 'notification-state', '--workspace', WS, '--state', 'partial', '--reason', 'browser_partial');
  opened = await openPage(browser, OWNER_URL, '/app/notifications');
  await waitState(opened.page, '[data-page="workspace-notifications"]', 'partial');
  assert((await opened.page.locator('body').innerText()).includes('browser_partial'), 'partial notification state reason missing');
  await opened.context.close();

  producer('--action', 'notification-state', '--workspace', WS, '--state', 'stale', '--reason', 'browser_stale');
  opened = await openPage(browser, OWNER_URL, '/app/notifications');
  await waitState(opened.page, '[data-page="workspace-notifications"]', 'stale');
  assert((await opened.page.locator('body').innerText()).includes('browser_stale'), 'stale notification state reason missing');
  await opened.context.close();

  mysql('RENAME TABLE workspace_notifications TO workspace_notifications_p12_browser_fault');
  try {
    opened = await openPage(browser, OWNER_URL, '/app/notifications');
    await waitState(opened.page, '[data-page="workspace-notifications"]', 'error');
    await opened.context.close();
  } finally {
    mysql('RENAME TABLE workspace_notifications_p12_browser_fault TO workspace_notifications');
    producer('--action', 'notification-state', '--workspace', WS, '--state', 'complete', '--reason', 'current');
  }
  return { shell_badge_and_popover: true, view_all: true, category_filter: 'security', explicit_states: ['partial', 'stale', 'error'], read_actions_and_deep_link: 'covered by base browser evidence' };
}

async function completeT023(browser) {
  let opened = await openPage(browser, OWNER_URL, '/app/tags', { width: 320, height: 800 });
  await waitState(opened.page, '[data-page="workspace-tags"]', 'manage');
  const narrowLayout = await layout(opened.page);
  assert(narrowLayout.root_overflow_px === 0 && narrowLayout.body_overflow_px === 0 && narrowLayout.clipped.length === 0, `320px overflow/clipping ${JSON.stringify(narrowLayout)}`);
  const bell = opened.page.getByRole('button', { name: /Notifications/ });
  await bell.click();
  const sheet = opened.page.getByRole('dialog');
  const box = await sheet.boundingBox();
  assert(box && box.width >= 318 && box.height >= 760, `mobile notification sheet not full-height ${JSON.stringify(box)}`);
  await opened.page.keyboard.press('Escape');
  await opened.context.close();

  opened = await openPage(browser, OWNER_URL, '/app');
  await waitState(opened.page, '[data-page="workspace-overview"]', 'complete');
  const command = opened.page.getByRole('button', { name: 'Command' });
  await command.focus();
  await command.click();
  assert(await opened.page.locator('dialog[open]').count() === 1, 'command overlay count is not one');
  await opened.page.keyboard.press('Escape');
  await opened.page.waitForFunction(() => document.querySelectorAll('dialog[open]').length === 0);
  assert(await command.evaluate((node) => document.activeElement === node), 'Esc did not return focus to Command trigger');
  const notificationButton = opened.page.getByRole('button', { name: /Notifications/ });
  await notificationButton.click();
  assert(await opened.page.locator('dialog[open]').count() === 1, 'notification overlay count is not one');
  await command.evaluate((node) => node.click());
  await opened.page.getByRole('heading', { name: 'Command palette' }).waitFor();
  assert(await opened.page.locator('dialog[open]').count() === 1, 'overlay stacking occurred after switching overlay authority');
  await opened.page.keyboard.press('Escape');
  await opened.context.close();

  opened = await openPage(browser, OWNER_URL, '/app', { width: 390, height: 844 }, { reducedMotion: 'reduce' });
  await waitState(opened.page, '[data-page="workspace-overview"]', 'complete');
  const reduced = await opened.page.evaluate(() => ({ matches: matchMedia('(prefers-reduced-motion: reduce)').matches, transition: getComputedStyle(document.querySelector('.gj-button')).transitionDuration }));
  assert(reduced.matches, `reduced-motion media query not active ${JSON.stringify(reduced)}`);
  const reducedLayout = await layout(opened.page);
  assert(reducedLayout.root_overflow_px === 0 && reducedLayout.body_overflow_px === 0, `reduced-motion layout overflow ${JSON.stringify(reducedLayout)}`);
  await opened.context.close();
  return { width_320: narrowLayout, mobile_full_height_sheet: true, esc_focus_return: true, overlay_stack_max: 1, reduced_motion: reduced };
}

const completions = { 'P12-T019': completeT019, 'P12-T020': completeT020, 'P12-T021': completeT021, 'P12-T022': completeT022, 'P12-T023': completeT023 };
const index = process.argv.indexOf('--case');
const caseId = index >= 0 ? process.argv[index + 1] : '';
if (!completions[caseId]) throw new Error('case must be P12-T019..P12-T023');
const browser = await chromium.launch({ headless: true, executablePath, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
try {
  const completion = await completions[caseId](browser);
  mergeResult(caseId, completion);
  console.log(JSON.stringify({ case_id: caseId, frozen_contract_completion: 'PASS' }, null, 2));
} catch (error) {
  failResult(caseId, error);
  console.error(`${error?.name ?? 'Error'}: ${error?.message ?? String(error)}`);
  process.exitCode = 1;
} finally {
  await browser.close();
}
