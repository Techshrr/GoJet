import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const ROOT = process.cwd();
const HEAD = process.env.GITHUB_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
const OWNER_URL = (process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174').replace(/\/$/, '');
const VIEWER_URL = (process.env.GOJET_TEST_WORKSPACE_VIEWER_URL ?? 'http://127.0.0.1:4175').replace(/\/$/, '');
const PLATFORM_URL = (process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081').replace(/\/$/, '');
const WORKSPACE = process.env.GOJET_TEST_WORKSPACE_ID ?? 'ws-p11-browser';
const ACTOR = process.env.GOJET_TEST_ACTOR_ID ?? 'p11-browser-owner';
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
const REDIS_HOST = process.env.GOJET_TEST_REDIS_HOST ?? '127.0.0.1';
const REDIS_PORT = process.env.GOJET_TEST_REDIS_PORT ?? '6379';
const browserDir = `${ROOT}/artifacts/v10/P11/browser`;
const capturesDir = `${ROOT}/artifacts/v10/P11/captures`;
mkdirSync(browserDir, { recursive: true });
mkdirSync(capturesDir, { recursive: true });

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
  if (JSON.stringify(viewports[name]) !== JSON.stringify(expected[name])) throw new Error(`P11 ${name} viewport drift`);
}
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((path) => existsSync(path));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P11 browser evidence');

function assert(condition, message) { if (!condition) throw new Error(message); }
function mysql(sql) {
  return execFileSync('mysql', ['--protocol=tcp', '-h', MYSQL_HOST, '-P', MYSQL_PORT, '-u', MYSQL_USER, '-N', '-B', MYSQL_DATABASE, '-e', sql], {
    encoding: 'utf8', env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
  }).trim();
}
function redis(...args) {
  return execFileSync('redis-cli', ['-h', REDIS_HOST, '-p', REDIS_PORT, ...args], { encoding: 'utf8' }).trim();
}
function resetBio() {
  mysql('SET FOREIGN_KEY_CHECKS=0; TRUNCATE TABLE bio_audit_events; TRUNCATE TABLE bio_child_links; TRUNCATE TABLE bio_pages; TRUNCATE TABLE bio_workspace_counters; SET FOREIGN_KEY_CHECKS=1;');
  redis('FLUSHDB');
}
function authHeaders(role = 'owner') {
  return {
    Accept: 'application/json',
    'Content-Type': 'application/json',
    'X-GoJet-Test-Actor': role === 'viewer' ? 'p11-browser-viewer' : ACTOR,
    'X-GoJet-Test-Workspace': WORKSPACE,
    'X-GoJet-Test-Workspace-Role': role,
  };
}
async function api(path, init = {}) {
  const response = await fetch(`${PLATFORM_URL}${path}`, { ...init, headers: { ...authHeaders(), ...(init.headers ?? {}) } });
  const type = response.headers.get('content-type') ?? '';
  const body = response.status === 204 ? null : type.includes('application/json') ? await response.json() : await response.text();
  return { response, body };
}
function child(label, destination_url, position, id) {
  return { ...(id ? { id } : {}), position, label, destination_url };
}
async function createBio(overrides = {}) {
  const payload = { title: 'P11 browser Bio', bio: 'P11 route-backed profile', links: [], change_reason: 'P11 browser fixture', ...overrides };
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/bio-pages`, { method: 'POST', body: JSON.stringify(payload) });
  assert(result.response.status === 201, `create Bio failed ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}
async function getBio(id) {
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/bio-pages/${id}`);
  assert(result.response.status === 200, `get Bio failed ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}
async function updateBio(item, overrides = {}) {
  const payload = { expected_version: item.version, change_reason: 'P11 browser external update', title: item.title, ...overrides };
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/bio-pages/${item.id}`, { method: 'PATCH', body: JSON.stringify(payload) });
  assert(result.response.status === 200, `update Bio failed ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}
async function transitionBio(item, action) {
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/bio-pages/${item.id}/${action}`, {
    method: 'POST', body: JSON.stringify({ expected_version: item.version, change_reason: `P11 browser ${action}` }),
  });
  assert(result.response.status === 200, `${action} Bio failed ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}
async function deleteBio(item) {
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/bio-pages/${item.id}`, {
    method: 'DELETE', body: JSON.stringify({ expected_version: item.version, change_reason: 'P11 browser delete' }),
  });
  assert(result.response.status === 204, `delete Bio failed ${result.response.status}`);
}
function seedRisk(childRecord, state) {
  assert(['allow', 'review', 'block'].includes(state), `invalid risk state ${state}`);
  const now = new Date();
  const validUntil = new Date(now.valueOf() + 30 * 60_000);
  const decision = {
    schema_version: 1,
    decision: state,
    fingerprint: childRecord.destination_fingerprint,
    checked_at: now.toISOString(),
    valid_until: validUntil.toISOString(),
    policy_version: 'p11-browser-v1',
  };
  const key = `risk:bio-child:${childRecord.id}:${childRecord.destination_fingerprint}`;
  assert(redis('SET', key, JSON.stringify(decision), 'EX', '1800') === 'OK', `failed to seed ${state}`);
  return key;
}
function diagnostics() { return { console_errors: [], page_errors: [], http_errors: [], request_failures: [] }; }
function attachDiagnostics(page, report) {
  page.on('console', (message) => { if (message.type() === 'error') report.console_errors.push(message.text()); });
  page.on('pageerror', (error) => report.page_errors.push(String(error)));
  page.on('response', (response) => { if (response.status() >= 400 && !response.url().endsWith('/favicon.ico')) report.http_errors.push({ status: response.status(), url: response.url() }); });
  page.on('requestfailed', (request) => report.request_failures.push({ url: request.url(), failure: request.failure() }));
}
function assertDiagnostics(report, label, allowedStatuses = []) {
  const httpErrors = report.http_errors.filter((entry) => !allowedStatuses.includes(entry.status));
  const consoleErrors = report.console_errors.filter((message) => {
    const match = /status of (\d{3})\b/.exec(message);
    return !match || !allowedStatuses.includes(Number(match[1]));
  });
  assert(consoleErrors.length === 0, `${label} console errors ${JSON.stringify(consoleErrors)}`);
  assert(report.page_errors.length === 0, `${label} page errors ${JSON.stringify(report.page_errors)}`);
  assert(report.request_failures.length === 0, `${label} request failures ${JSON.stringify(report.request_failures)}`);
  assert(httpErrors.length === 0, `${label} HTTP errors ${JSON.stringify(httpErrors)}`);
}
const stateTracker = () => {
  window.__gojetP11States = [];
  const capture = () => {
    document.querySelectorAll('[data-page][data-state]').forEach((node) => {
      const value = `${node.getAttribute('data-page')}:${node.getAttribute('data-state')}`;
      if (!window.__gojetP11States.includes(value)) window.__gojetP11States.push(value);
    });
  };
  const start = () => {
    capture();
    new MutationObserver(capture).observe(document.documentElement, { subtree: true, childList: true, attributes: true, attributeFilter: ['data-page', 'data-state'] });
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start, { once: true }); else start();
};
async function openPage(browser, base, path, viewport = viewports.desktop, options = {}) {
  const context = await browser.newContext({ viewport, deviceScaleFactor: 1, ...options });
  await context.addInitScript(stateTracker);
  const page = await context.newPage();
  const report = diagnostics();
  attachDiagnostics(page, report);
  await page.goto(`${base}${path}`, { waitUntil: 'networkidle' });
  return { context, page, report };
}
async function waitState(page, selector, state) {
  await page.locator(selector).waitFor();
  await page.waitForFunction(([s, v]) => document.querySelector(s)?.getAttribute('data-state') === v, [selector, state]);
}
async function stateHistory(page) { return page.evaluate(() => window.__gojetP11States ?? []); }
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
function assertLayout(value, label) {
  assert(value.root_overflow_px === 0 && value.body_overflow_px === 0, `${label} root/body overflow ${JSON.stringify(value)}`);
  assert(value.clipped.length === 0, `${label} clipped ${JSON.stringify(value.clipped)}`);
}
function writeResult(caseId, status, details, errors = []) {
  writeFileSync(`${browserDir}/${caseId}.json`, JSON.stringify({
    node: 'P11', case_id: caseId, status, implementation_commit: HEAD, generated_at: new Date().toISOString(),
    environment: {
      browser: executablePath, workspace_owner: OWNER_URL, workspace_viewer: VIEWER_URL, platformapi: PLATFORM_URL,
      mysql: `${MYSQL_HOST}:${MYSQL_PORT}/${MYSQL_DATABASE}`, redis: `${REDIS_HOST}:${REDIS_PORT}`, canonical_viewports: viewports,
      authority: 'real built owner/viewer Workspace + native Go platformapi + real MySQL/Redis; no request interception or fixture-only browser success',
    }, details, errors,
  }, null, 2) + '\n');
}
async function screenshot(page, name) {
  const path = `${capturesDir}/${name}.png`;
  await page.screenshot({ path, fullPage: true });
  return path.replace(`${ROOT}/`, '');
}

async function caseT016(browser) {
  resetBio();
  const evidence = {};
  let opened = await openPage(browser, OWNER_URL, '/app/bio');
  await waitState(opened.page, '[data-page="bio-list"]', 'empty');
  const emptyHistory = await stateHistory(opened.page);
  assert(emptyHistory.includes('bio-list:loading'), `real loading state not observed: ${JSON.stringify(emptyHistory)}`);
  evidence.loading_empty_states = emptyHistory;
  assertDiagnostics(opened.report, 'T016 empty');
  await opened.context.close();

  opened = await openPage(browser, OWNER_URL, '/app/bio');
  await opened.page.getByLabel('Page title').fill('Created from browser');
  await opened.page.getByLabel('Profile text').fill('Route-backed Bio browser creation');
  await opened.page.getByRole('button', { name: 'Create Bio page' }).click();
  await opened.page.waitForURL(/\/app\/bio\/\d+$/);
  await waitState(opened.page, '[data-page="bio-detail"]', 'draft');
  assert(await opened.page.getByText('Public preview', { exact: true }).count() === 1, 'detail public preview missing after create');
  evidence.created_url = opened.page.url();
  evidence.preview = true;
  assertDiagnostics(opened.report, 'T016 create/preview');
  await opened.context.close();

  opened = await openPage(browser, OWNER_URL, '/app/bio');
  await waitState(opened.page, '[data-page="bio-list"]', 'edit');
  evidence.edit = true;
  await opened.context.close();

  opened = await openPage(browser, VIEWER_URL, '/app/bio');
  await waitState(opened.page, '[data-page="bio-list"]', 'read-only');
  assert(await opened.page.getByRole('heading', { name: 'Create Bio page' }).count() === 0, 'read-only list exposed create form');
  evidence.read_only = true;
  await opened.context.close();

  await createBio({ title: 'Quota second' });
  opened = await openPage(browser, OWNER_URL, '/app/bio');
  await waitState(opened.page, '[data-page="bio-list"]', 'quota-reached');
  evidence.quota = true;
  await opened.context.close();

  mysql('RENAME TABLE bio_pages TO bio_pages_p11_fault');
  try {
    opened = await openPage(browser, OWNER_URL, '/app/bio');
    await waitState(opened.page, '[data-page="bio-list"]', 'error');
    evidence.error = true;
    assertDiagnostics(opened.report, 'T016 controlled error', [500, 502]);
    await opened.context.close();
  } finally {
    mysql('RENAME TABLE bio_pages_p11_fault TO bio_pages');
  }

  resetBio();
  const guarded = await createBio({ title: 'Publish error fixture', links: [child('Guarded', 'https://example.com/publish-error', 0)] });
  seedRisk(guarded.links[0], 'allow');
  opened = await openPage(browser, OWNER_URL, `/app/bio/${guarded.id}`);
  await waitState(opened.page, '[data-page="bio-detail"]', 'draft');
  assert(await opened.page.getByRole('button', { name: 'Publish Bio' }).isEnabled(), 'publish should be enabled while loaded risk authority is allow');
  seedRisk(guarded.links[0], 'review');
  await opened.page.getByRole('button', { name: 'Publish Bio' }).click();
  await waitState(opened.page, '[data-page="bio-detail"]', 'risk-blocked');
  evidence.publish_error = true;
  assertDiagnostics(opened.report, 'T016 real publish error', [409]);
  await opened.context.close();
  return evidence;
}

async function caseT017(browser) {
  resetBio();
  const draft = await createBio({ title: 'Detail Bio', bio: 'Route-backed detail profile', links: [child('Safe destination', 'https://example.com/safe', 0)] });
  seedRisk(draft.links[0], 'allow');
  const evidence = {};

  let opened = await openPage(browser, OWNER_URL, `/app/bio/${draft.id}`);
  await waitState(opened.page, '[data-page="bio-detail"]', 'draft');
  const draftHistory = await stateHistory(opened.page);
  assert(draftHistory.includes('bio-detail:loading'), `detail loading state not observed: ${JSON.stringify(draftHistory)}`);
  evidence.loading_draft_states = draftHistory;
  assert(await opened.page.getByText('Public preview', { exact: true }).count() === 1, 'public preview missing');
  await opened.page.getByRole('button', { name: 'Publish Bio' }).click();
  await waitState(opened.page, '[data-page="bio-detail"]', 'published');
  evidence.published = true;
  const publicLink = opened.page.getByRole('link', { name: 'Open public state' });
  assert(await publicLink.count() === 1, 'public state link missing after publish');
  const publicHref = await publicLink.getAttribute('href');
  evidence.public_href = publicHref;
  assertDiagnostics(opened.report, 'T017 publish');
  await opened.context.close();

  let publicResponse = await fetch(`${PLATFORM_URL}/p/${encodeURIComponent(draft.slug)}`);
  let publicHtml = await publicResponse.text();
  assert(publicResponse.status === 200, `published public page status=${publicResponse.status}`);
  assert(publicHtml.includes('href="https://example.com/safe"') && publicHtml.includes('rel="ugc nofollow"'), 'published allowed link missing required rel/navigation');
  evidence.public_preview_status = publicResponse.status;

  seedRisk(draft.links[0], 'review');
  opened = await openPage(browser, OWNER_URL, `/app/bio/${draft.id}`);
  await waitState(opened.page, '[data-page="bio-detail"]', 'published');
  assert(await opened.page.locator('.bio-child-editor[data-risk-status="review"]').count() === 1, 'review child UI state missing');
  assert(await opened.page.getByText(/published Bio has child links in review\/blocked safety state/i).count() === 1, 'published review warning missing');
  evidence.review = true;
  await opened.context.close();
  publicResponse = await fetch(`${PLATFORM_URL}/p/${encodeURIComponent(draft.slug)}`);
  publicHtml = await publicResponse.text();
  assert(publicResponse.status === 200 && !publicHtml.includes('href="https://example.com/safe"'), 'review child remained navigable');

  seedRisk(draft.links[0], 'block');
  opened = await openPage(browser, OWNER_URL, `/app/bio/${draft.id}`);
  await waitState(opened.page, '[data-page="bio-detail"]', 'published');
  assert(await opened.page.locator('.bio-child-editor[data-risk-status="blocked"]').count() === 1, 'blocked child UI state missing');
  evidence.blocked = true;
  await opened.context.close();
  publicResponse = await fetch(`${PLATFORM_URL}/p/${encodeURIComponent(draft.slug)}`);
  publicHtml = await publicResponse.text();
  assert(publicResponse.status === 200 && !publicHtml.includes('href="https://example.com/safe"'), 'blocked child remained navigable');

  const current = await getBio(draft.id);
  opened = await openPage(browser, OWNER_URL, `/app/bio/${draft.id}`);
  await waitState(opened.page, '[data-page="bio-detail"]', 'published');
  await updateBio(current, { title: 'External version' });
  await opened.page.getByLabel('Page title').fill('Stale browser edit');
  await opened.page.getByRole('button', { name: 'Save current version' }).click();
  await waitState(opened.page, '[data-page="bio-detail"]', 'conflict');
  evidence.conflict = true;
  assertDiagnostics(opened.report, 'T017 conflict', [409]);
  await opened.context.close();

  const deleted = await createBio({ title: 'Deleted detail' });
  await deleteBio(deleted);
  opened = await openPage(browser, OWNER_URL, `/app/bio/${deleted.id}`);
  await waitState(opened.page, '[data-page="bio-detail"]', 'deleted');
  evidence.deleted = true;
  assertDiagnostics(opened.report, 'T017 deleted', [410]);
  await opened.context.close();
  return evidence;
}

async function caseT018(browser) {
  resetBio();
  let item = await createBio({ title: 'Responsive public Bio', bio: 'Responsive and accessible public Bio content.', links: [child('Public destination', 'https://example.com/responsive', 0)] });
  seedRisk(item.links[0], 'allow');
  item = await transitionBio(item, 'publish');
  const captures = [];
  const layouts = [];

  for (const [name, viewport] of Object.entries(viewports)) {
    for (const [surface, base, path] of [
      ['list', OWNER_URL, '/app/bio'],
      ['detail', OWNER_URL, `/app/bio/${item.id}`],
      ['public', PLATFORM_URL, `/p/${item.slug}`],
    ]) {
      const opened = await openPage(browser, base, path, viewport);
      const value = await layout(opened.page);
      assertLayout(value, `${name} ${surface}`);
      layouts.push({ name, surface, ...value });
      captures.push(await screenshot(opened.page, `P11-T018-${name}-${surface}`));
      if (surface === 'public') {
        const anchor = opened.page.getByRole('link', { name: 'Public destination' });
        assert(await anchor.count() === 1, `${name} public outbound anchor missing`);
        const rel = (await anchor.getAttribute('rel')) ?? '';
        assert(rel.split(/\s+/).includes('ugc') && rel.split(/\s+/).includes('nofollow'), `${name} public rel=${rel}`);
      }
      assertDiagnostics(opened.report, `T018 ${name} ${surface}`);
      await opened.context.close();
    }
  }

  for (const [surface, base, path] of [
    ['detail', OWNER_URL, `/app/bio/${item.id}`],
    ['public', PLATFORM_URL, `/p/${item.slug}`],
  ]) {
    const opened = await openPage(browser, base, path, { width: 320, height: 800 });
    const value = await layout(opened.page);
    assertLayout(value, `320px ${surface}`);
    layouts.push({ name: 'reflow-320', surface, ...value });
    captures.push(await screenshot(opened.page, `P11-T018-reflow-320-${surface}`));
    await opened.context.close();
  }

  const keyboard = await openPage(browser, OWNER_URL, '/app/bio', viewports.desktop);
  const target = keyboard.page.getByLabel('Page title');
  for (let count = 0; count < 40 && !(await target.evaluate((node) => node === document.activeElement)); count += 1) await keyboard.page.keyboard.press('Tab');
  assert(await target.evaluate((node) => node === document.activeElement), 'Bio create title is not keyboard reachable');
  const focus = await target.evaluate((node) => {
    const style = getComputedStyle(node);
    return { outline: style.outlineStyle, outlineWidth: style.outlineWidth, boxShadow: style.boxShadow };
  });
  assert(focus.outline !== 'none' || focus.boxShadow !== 'none', 'keyboard focus has no visible indicator');
  await keyboard.context.close();

  const statusPage = await openPage(browser, OWNER_URL, '/app/bio');
  const statusText = await statusPage.page.locator('.bio-status').first().textContent();
  assert(Boolean(statusText?.trim()), 'Bio resource state has no visible text/non-color meaning');
  const riskText = await statusPage.page.locator('.bio-meta dd').nth(2).textContent();
  assert(riskText?.trim() === '0', `Bio risk state summary missing/non-textual: ${riskText}`);
  await statusPage.context.close();

  const reduced = await openPage(browser, OWNER_URL, `/app/bio/${item.id}`, viewports.mobile, { reducedMotion: 'reduce' });
  assert(await reduced.page.getByRole('heading', { name: 'Responsive public Bio' }).count() >= 1, 'reduced-motion detail unusable');
  await reduced.context.close();

  return { captures, layouts, keyboard_focus: focus, non_color_state_text: statusText?.trim(), non_color_risk_summary: riskText?.trim(), reduced_motion_usable: true };
}

const cases = { 'P11-T016': caseT016, 'P11-T017': caseT017, 'P11-T018': caseT018 };
async function main() {
  const index = process.argv.indexOf('--case');
  const id = index >= 0 ? process.argv[index + 1] : 'all';
  if (id !== 'all' && !cases[id]) throw new Error(`unsupported P11 browser case ${id}`);
  const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
  try {
    for (const caseId of id === 'all' ? Object.keys(cases) : [id]) {
      let details = {};
      const errors = [];
      try { details = await cases[caseId](browser); } catch (error) { errors.push(error instanceof Error ? `${error.name}: ${error.message}` : String(error)); }
      writeResult(caseId, errors.length ? 'FAIL' : 'PASS', details, errors);
      if (errors.length) throw new Error(`${caseId}: ${errors.join('; ')}`);
      console.log(`${caseId} PASS on ${HEAD}`);
    }
  } finally {
    await browser.close();
  }
}
main().catch((error) => { console.error(error); process.exitCode = 1; });
