import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const root = process.cwd();
const resultsDir = `${root}/artifacts/v10/P07/results`;
const capturesDir = `${root}/artifacts/v10/P07/captures`;
const browserDir = `${root}/artifacts/v10/P07/browser`;
const g9Dir = `${root}/artifacts/v10/gates/G9`;
for (const path of [resultsDir, capturesDir, browserDir, g9Dir]) mkdirSync(path, { recursive: true });

const WORKSPACE_URL = process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174';
const PLATFORM_URL = process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081';
const WORKSPACE = process.env.GOJET_TEST_WORKSPACE_ID ?? 'ws-p07-browser';
const ACTOR = process.env.GOJET_TEST_ACTOR_ID ?? 'p07-browser-owner';
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';

const variables = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
function parseViewport(value, name) {
  const match = /^(\d+)×(\d+)$/.exec(String(value));
  if (!match) throw new Error(`Invalid viewport ${name}: ${String(value)}`);
  return { width: Number(match[1]), height: Number(match[2]) };
}
const viewports = {
  desktop: parseViewport(variables['viewport.desktop'].dimensions, 'viewport.desktop'),
  mobile: parseViewport(variables['viewport.mobile'].dimensions, 'viewport.mobile'),
};

const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P07 browser evidence');

const G9_BUDGET = Object.freeze({
  dom_content_loaded_ms: 3000,
  load_event_ms: 5000,
  resource_bytes: 1_500_000,
  resource_count: 60,
});

function implementationCommit() {
  return execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
}
function assert(condition, message) { if (!condition) throw new Error(message); }
function sqlLiteral(value) { return `'${String(value).replaceAll("'", "''")}'`; }
function mysql(sql) {
  return execFileSync('mysql', ['--protocol=tcp', '-h', MYSQL_HOST, '-P', String(MYSQL_PORT), '-u', MYSQL_USER, '-N', '-B', MYSQL_DATABASE, '-e', sql], {
    cwd: root, encoding: 'utf8', env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
  }).trim();
}
function mysqlDate(date) { return date.toISOString().replace('T', ' ').replace('Z', ''); }
function eventID(linkId, sequence) {
  return createHash('sha256').update(`gojet.analytics.click.v1\n${WORKSPACE}\n${linkId}\n${sequence}`).digest('hex');
}

function resetWorkspace() {
  const ws = sqlLiteral(WORKSPACE);
  mysql(`
    DELETE FROM analytics_conversions WHERE workspace_id=${ws};
    DELETE FROM analytics_workspace_state WHERE workspace_id=${ws};
    DELETE FROM analytics_hourly_aggregates WHERE workspace_id=${ws};
    DELETE FROM analytics_events WHERE workspace_id=${ws};
    DELETE FROM analytics_outbox WHERE workspace_id=${ws};
    DELETE FROM link_audit_events WHERE workspace_id=${ws};
    DELETE FROM link_versions WHERE workspace_id=${ws};
    DELETE FROM links WHERE workspace_id=${ws};
  `);
}

function authHeaders() {
  return {
    Accept: 'application/json', 'Content-Type': 'application/json',
    'X-GoJet-Test-Actor': ACTOR,
    'X-GoJet-Test-Workspace': WORKSPACE,
    'X-GoJet-Test-Workspace-Role': 'owner',
    'X-GoJet-Test-Analytics-Permission': 'allow',
  };
}
async function api(path, init = {}) {
  const response = await fetch(`${PLATFORM_URL}${path}`, { ...init, headers: { ...authHeaders(), ...(init.headers ?? {}) } });
  const type = response.headers.get('content-type') ?? '';
  const body = type.includes('application/json') ? await response.json() : await response.text();
  return { response, body };
}
async function createLink(code = 'analytics-browser') {
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/links`, {
    method: 'POST',
    body: JSON.stringify({
      hostname: 'go.p07-browser.test', domain_kind: 'official', code,
      title: 'P07 browser analytics', primary_destination: 'https://example.com/p07-browser',
      redirect_status: 302, routing: [], ab: [], utm: {}, access: {},
      expires_at: null, click_limit: null, one_time: false, change_reason: 'P07 browser fixture',
    }),
  });
  assert(result.response.status === 201, `create link failed: ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}

function seedWorkspaceState(status, retentionDays = 90, reason = 'browser_fixture') {
  mysql(`INSERT INTO analytics_workspace_state (workspace_id,status,data_through_at,retention_days,state_reason)
    VALUES (${sqlLiteral(WORKSPACE)},${sqlLiteral(status)},UTC_TIMESTAMP(6),${Number(retentionDays)},${sqlLiteral(reason)})
    ON DUPLICATE KEY UPDATE status=VALUES(status),data_through_at=VALUES(data_through_at),retention_days=VALUES(retention_days),state_reason=VALUES(state_reason)`);
}
function seedEvent(linkId, sequence, minutesAgo, dimensions = {}) {
  const occurredAt = new Date(Date.now() - minutesAgo * 60_000);
  const country = dimensions.country ?? (sequence % 2 ? 'sg' : 'us');
  const device = dimensions.device ?? (sequence % 2 ? 'mobile' : 'desktop');
  const language = dimensions.language ?? 'en-sg';
  const source = dimensions.source ?? 'source.example';
  const campaign = dimensions.campaign ?? 'campaign-browser';
  const id = eventID(linkId, sequence);
  const payload = JSON.stringify({
    schema_version: 1, event_type: 'link.click', event_id: id, workspace_id: WORKSPACE,
    link_id: Number(linkId), click_sequence: sequence, occurred_at: occurredAt.toISOString(),
    dimensions: { country_code: country, device, language, source_hostname: source, campaign_id: campaign },
  });
  const bucket = new Date(occurredAt); bucket.setUTCMinutes(0, 0, 0);
  mysql(`
    INSERT INTO analytics_outbox (event_id,workspace_id,link_id,click_sequence,occurred_at,country_code,device,language,source_hostname,campaign_id,payload_json,published_at,published_stream_id,publish_attempts)
      VALUES (${sqlLiteral(id)},${sqlLiteral(WORKSPACE)},${Number(linkId)},${sequence},${sqlLiteral(mysqlDate(occurredAt))},${sqlLiteral(country)},${sqlLiteral(device)},${sqlLiteral(language)},${sqlLiteral(source)},${sqlLiteral(campaign)},${sqlLiteral(payload)},UTC_TIMESTAMP(6),${sqlLiteral(`browser-${sequence}-0`)},1);
    INSERT INTO analytics_events (event_id,workspace_id,link_id,click_sequence,occurred_at,country_code,device,language,source_hostname,campaign_id,stream_id)
      VALUES (${sqlLiteral(id)},${sqlLiteral(WORKSPACE)},${Number(linkId)},${sequence},${sqlLiteral(mysqlDate(occurredAt))},${sqlLiteral(country)},${sqlLiteral(device)},${sqlLiteral(language)},${sqlLiteral(source)},${sqlLiteral(campaign)},${sqlLiteral(`browser-${sequence}-0`)});
    INSERT INTO analytics_hourly_aggregates (workspace_id,link_id,bucket_start,country_code,device,language,source_hostname,campaign_id,clicks)
      VALUES (${sqlLiteral(WORKSPACE)},${Number(linkId)},${sqlLiteral(mysqlDate(bucket))},${sqlLiteral(country)},${sqlLiteral(device)},${sqlLiteral(language)},${sqlLiteral(source)},${sqlLiteral(campaign)},1)
      ON DUPLICATE KEY UPDATE clicks=clicks+1;
  `);
  return id;
}
async function seedSuccess() {
  resetWorkspace();
  const link = await createLink();
  seedWorkspaceState('complete', 90, 'reconciled');
  seedEvent(link.id, 1, 90);
  seedEvent(link.id, 2, 60);
  seedEvent(link.id, 3, 30, { country: 'sg', device: 'tablet', source: 'partner.example' });
  mysql(`INSERT INTO analytics_conversions (workspace_id,conversion_id,campaign_id,link_id,occurred_at)
    VALUES (${sqlLiteral(WORKSPACE)},'browser-conversion-1','campaign-browser',${Number(link.id)},DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 20 MINUTE))`);
  return link;
}

function diagnostics() { return { console_errors: [], page_errors: [], http_errors: [], request_failures: [] }; }
function attachDiagnostics(page, report) {
  page.on('console', (message) => { if (message.type() === 'error') report.console_errors.push({ text: message.text(), location: message.location() }); });
  page.on('pageerror', (error) => report.page_errors.push(String(error)));
  page.on('response', (response) => {
    if (response.status() >= 400 && !response.url().endsWith('/favicon.ico')) report.http_errors.push({ status: response.status(), url: response.url(), resourceType: response.request().resourceType() });
  });
  page.on('requestfailed', (request) => report.request_failures.push({ url: request.url(), failure: request.failure() }));
}
function assertRuntimeClean(report, label) {
  assert(report.console_errors.length === 0, `${label} console errors: ${JSON.stringify(report.console_errors)}`);
  assert(report.page_errors.length === 0, `${label} page errors: ${JSON.stringify(report.page_errors)}`);
  assert(report.request_failures.length === 0, `${label} request failures: ${JSON.stringify(report.request_failures)}`);
}
function writeResult(caseId, status, details, errors = []) {
  writeFileSync(`${resultsDir}/${caseId}.json`, `${JSON.stringify({
    case_id: caseId, status, generated_at: new Date().toISOString(), implementation_commit: implementationCommit(),
    environment: { browser: executablePath, workspace: WORKSPACE_URL, platformapi: PLATFORM_URL, mysql: `${MYSQL_HOST}:${MYSQL_PORT}/${MYSQL_DATABASE}`, canonical_viewports: viewports, authority: 'real MySQL-backed analytics API; no request interception or client-generated totals' },
    details, errors,
  }, null, 2)}\n`);
}

async function gotoAnalytics(page) {
  await page.goto(`${WORKSPACE_URL}/app/analytics`, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Analytics', exact: true }).waitFor();
}
async function assertState(page, state) {
  const report = page.locator('[data-analytics-state]');
  await report.waitFor();
  assert(await report.getAttribute('data-analytics-state') === state, `expected analytics state=${state}, got ${await report.getAttribute('data-analytics-state')}`);
}

async function caseT018(browser) {
  const report = diagnostics();
  const context = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1 });
  const page = await context.newPage();
  attachDiagnostics(page, report);
  const observed = {};

  const successLink = await seedSuccess();
  await gotoAnalytics(page);
  await assertState(page, 'success');
  await page.getByText('3', { exact: true }).first().waitFor();
  await page.getByText('1', { exact: true }).first().waitFor();
  observed.success = { clicks: 3, conversions: 1, link_id: successLink.id };
  const successCapture = 'gjv10__workspace-analytics__p07-t018-success__normal__light__en__desktop.png';
  await page.screenshot({ path: `${capturesDir}/${successCapture}`, fullPage: false });

  resetWorkspace();
  await createLink('analytics-empty');
  seedWorkspaceState('complete', 90, 'reconciled');
  await gotoAnalytics(page);
  await assertState(page, 'empty');
  await page.getByRole('heading', { name: 'No measured activity' }).waitFor();
  observed.empty = { complete_zero_distinct: true };

  const partialLink = await seedSuccess();
  seedWorkspaceState('partial', 90, 'ingestion_gap');
  await gotoAnalytics(page);
  await assertState(page, 'partial');
  await page.getByText('Analytics data is partial.', { exact: false }).waitFor();
  observed.partial = { measured_clicks_visible: 3, link_id: partialLink.id };

  await seedSuccess();
  seedWorkspaceState('stale', 90, 'worker_lag');
  await gotoAnalytics(page);
  await assertState(page, 'stale');
  await page.getByText('Analytics data is stale.', { exact: false }).waitFor();
  observed.stale = { measured_data_retained: true };

  await seedSuccess();
  seedWorkspaceState('complete', 1, 'reconciled');
  await gotoAnalytics(page);
  await assertState(page, 'retention-limited');
  await page.getByText('retained analytics window', { exact: false }).waitFor();
  observed['retention-limited'] = { earlier_history_not_presented_as_complete: true };

  await seedSuccess();
  mysql('RENAME TABLE analytics_events TO analytics_events_p07_browser_error');
  try {
    await gotoAnalytics(page);
    await page.getByText('Analytics data is unavailable.', { exact: false }).waitFor();
    await page.getByText('analytics_unavailable', { exact: false }).waitFor();
    observed.error = { persistent_error_state: true };
  } finally {
    mysql('RENAME TABLE analytics_events_p07_browser_error TO analytics_events');
  }
  const expected503 = report.http_errors.filter((entry) => entry.status === 503 && entry.url.includes('/analytics/overview'));
  assert(expected503.length >= 1, `expected analytics 503 was not observed: ${JSON.stringify(report.http_errors)}`);
  const unexpectedHttp = report.http_errors.filter((entry) => !(entry.status === 503 && entry.url.includes('/analytics/overview')));
  assert(unexpectedHttp.length === 0, `unexpected HTTP errors: ${JSON.stringify(unexpectedHttp)}`);
  assertRuntimeClean(report, 'P07-T018');
  await context.close();
  return { observed_states: observed, capture: `artifacts/v10/P07/captures/${successCapture}`, expected_http_503_count: expected503.length, diagnostics: report };
}

async function mobileLayout(page) {
  return page.evaluate(() => {
    const visible = (node) => node instanceof HTMLElement && node.offsetParent !== null;
    const clipped = [...document.querySelectorAll('main h1, main h2, main h3, main button, main a, main label, main strong')]
      .filter(visible).filter((node) => node.clientWidth > 0 && node.scrollWidth > node.clientWidth + 1)
      .map((node) => node.textContent?.trim()).filter(Boolean);
    const unnamed = [...document.querySelectorAll('a[href],button,input,select,textarea,[role="tab"]')]
      .filter(visible).filter((node) => {
        const labelledBy = node.getAttribute('aria-labelledby');
        const labelledText = labelledBy ? labelledBy.split(/\s+/).map((id) => document.getElementById(id)?.textContent ?? '').join(' ').trim() : '';
        const labelsText = 'labels' in node && node.labels ? [...node.labels].map((label) => label.textContent ?? '').join(' ').trim() : '';
        const name = node.getAttribute('aria-label') || labelledText || labelsText || node.textContent?.trim() || node.getAttribute('title') || '';
        return !name;
      }).map((node) => ({ tag: node.tagName, id: node.id, role: node.getAttribute('role') }));
    return {
      viewport: { width: innerWidth, height: innerHeight },
      root_overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
      body_overflow: document.body.scrollWidth > document.body.clientWidth,
      clipped_required_text: clipped,
      unnamed_visible_controls: unnamed,
    };
  });
}
async function performanceEvidence(page) {
  return page.evaluate(() => {
    const nav = performance.getEntriesByType('navigation')[0];
    const resources = performance.getEntriesByType('resource');
    return {
      dom_content_loaded_ms: nav ? Math.round(nav.domContentLoadedEventEnd) : null,
      load_event_ms: nav ? Math.round(nav.loadEventEnd) : null,
      resource_bytes: Math.round(resources.reduce((sum, item) => sum + (item.encodedBodySize || item.transferSize || 0), 0)),
      resource_count: resources.length,
    };
  });
}

async function caseT019(browser) {
  const link = await seedSuccess();
  const report = diagnostics();
  const context = await browser.newContext({ viewport: viewports.mobile, deviceScaleFactor: 1 });
  const page = await context.newPage();
  attachDiagnostics(page, report);
  await gotoAnalytics(page);
  await assertState(page, 'success');

  const layout = await mobileLayout(page);
  assert(layout.viewport.width === viewports.mobile.width && layout.viewport.height === viewports.mobile.height, `mobile viewport mismatch: ${JSON.stringify(layout.viewport)}`);
  assert(viewports.mobile.width === 390 && viewports.mobile.height === 844, `canonical mobile viewport is not 390x844: ${JSON.stringify(viewports.mobile)}`);
  assert(!layout.root_overflow && !layout.body_overflow, `analytics root/body overflow: ${JSON.stringify(layout)}`);
  assert(layout.clipped_required_text.length === 0, `analytics clipped required text: ${JSON.stringify(layout.clipped_required_text)}`);
  assert(layout.unnamed_visible_controls.length === 0, `analytics unnamed controls: ${JSON.stringify(layout.unnamed_visible_controls)}`);
  await page.locator('.analytics-state-label').getByText('State: success', { exact: true }).waitFor();

  const from = page.getByLabel('From', { exact: true });
  const to = page.getByLabel('To', { exact: true });
  await from.focus();
  await page.keyboard.press('Tab');
  assert(await to.evaluate((node) => node === document.activeElement), 'keyboard Tab did not move from From to To');

  const perf = await performanceEvidence(page);
  assert(perf.dom_content_loaded_ms !== null && perf.dom_content_loaded_ms <= G9_BUDGET.dom_content_loaded_ms, `DOMContentLoaded budget exceeded: ${JSON.stringify(perf)}`);
  assert(perf.load_event_ms !== null && perf.load_event_ms <= G9_BUDGET.load_event_ms, `load event budget exceeded: ${JSON.stringify(perf)}`);
  assert(perf.resource_bytes <= G9_BUDGET.resource_bytes, `resource byte budget exceeded: ${JSON.stringify(perf)}`);
  assert(perf.resource_count <= G9_BUDGET.resource_count, `resource count budget exceeded: ${JSON.stringify(perf)}`);

  await page.goto(`${WORKSPACE_URL}/app/links/${link.id}`, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'P07 browser analytics' }).waitFor();
  await page.getByRole('tab', { name: 'Analytics' }).click();
  await page.locator('[data-analytics-state="success"]').waitFor();
  await page.getByText('Measured activity', { exact: true }).waitFor();
  const detailLayout = await mobileLayout(page);
  assert(!detailLayout.root_overflow && !detailLayout.body_overflow, `Link analytics root/body overflow: ${JSON.stringify(detailLayout)}`);
  assert(detailLayout.unnamed_visible_controls.length === 0, `Link analytics unnamed controls: ${JSON.stringify(detailLayout.unnamed_visible_controls)}`);

  const capture = 'gjv10__workspace-analytics__p07-t019-mobile__normal__light__en__mobile.png';
  await page.screenshot({ path: `${capturesDir}/${capture}`, fullPage: false });
  assert(report.http_errors.length === 0, `P07-T019 HTTP errors: ${JSON.stringify(report.http_errors)}`);
  assertRuntimeClean(report, 'P07-T019');

  const g9 = {
    gate: 'G9', node: 'P07', status: 'PASS', implementation_commit: implementationCommit(),
    budget: G9_BUDGET, observed: perf, viewport: viewports.mobile,
    evidence: { root_body_overflow: false, link_detail_root_body_overflow: false, no_client_generated_totals: true },
  };
  writeFileSync(`${g9Dir}/p07-analytics.json`, `${JSON.stringify(g9, null, 2)}\n`);
  await context.close();
  return { mobile_layout: layout, link_detail_layout: detailLayout, keyboard_from_to: true, status_not_color_only: true, performance: perf, performance_budget: G9_BUDGET, g9_evidence: 'artifacts/v10/gates/G9/p07-analytics.json', capture: `artifacts/v10/P07/captures/${capture}`, diagnostics: report };
}

const requested = process.argv.includes('--case') ? process.argv[process.argv.indexOf('--case') + 1] : 'all';
const cases = requested === 'all' ? ['P07-T018', 'P07-T019'] : [requested];
for (const caseId of cases) if (!['P07-T018', 'P07-T019'].includes(caseId)) throw new Error(`Unsupported P07 browser case ${caseId}`);

const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
let failed = false;
try {
  for (const caseId of cases) {
    try {
      const details = caseId === 'P07-T018' ? await caseT018(browser) : await caseT019(browser);
      writeResult(caseId, 'PASS', details, []);
      console.log(`${caseId}: PASS`);
    } catch (error) {
      failed = true;
      writeResult(caseId, 'FAIL', {}, [String(error?.stack ?? error)]);
      console.error(`${caseId}: FAIL`, error);
    }
  }
} finally {
  await browser.close();
}
if (failed) process.exit(1);
