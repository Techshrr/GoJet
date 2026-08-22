import { execFileSync, spawn } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const root = process.cwd();
const resultsDir = `${root}/artifacts/v10/P08/results`;
const capturesDir = `${root}/artifacts/v10/P08/captures`;
for (const path of [resultsDir, capturesDir]) mkdirSync(path, { recursive: true });

const WORKSPACE_URL = process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174';
const PLATFORM_URL = process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081';
const WORKSPACE = process.env.GOJET_TEST_WORKSPACE_ID ?? 'ws-p08-browser';
const ACTOR = process.env.GOJET_TEST_ACTOR_ID ?? 'p08-browser-owner';
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
const REDIS_HOST = process.env.GOJET_TEST_REDIS_HOST ?? '127.0.0.1';
const REDIS_PORT = process.env.GOJET_TEST_REDIS_PORT ?? '6379';

const variables = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
function parseViewport(value, name) {
  const match = /^(\d+)×(\d+)$/.exec(String(value));
  if (!match) throw new Error(`Invalid viewport ${name}: ${String(value)}`);
  return { width: Number(match[1]), height: Number(match[2]) };
}
const viewports = {
  desktop: parseViewport(variables['viewport.desktop'].dimensions, 'viewport.desktop'),
  tablet: parseViewport(variables['viewport.tablet'].dimensions, 'viewport.tablet'),
  mobile: parseViewport(variables['viewport.mobile'].dimensions, 'viewport.mobile'),
};
const expectedViewports = {
  desktop: { width: 1440, height: 900 },
  tablet: { width: 1024, height: 768 },
  mobile: { width: 390, height: 844 },
};
for (const name of Object.keys(expectedViewports)) {
  const actual = viewports[name];
  const expected = expectedViewports[name];
  if (actual.width !== expected.width || actual.height !== expected.height) {
    throw new Error(`P08 canonical ${name} viewport drift: ${JSON.stringify(actual)}`);
  }
}

const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P08 browser evidence');

function assert(condition, message) { if (!condition) throw new Error(message); }
function sleep(ms) { return new Promise((resolve) => setTimeout(resolve, ms)); }
function implementationCommit() { return execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim(); }
function sqlLiteral(value) { return `'${String(value).replaceAll("'", "''")}'`; }
function mysqlArgs(sql) { return ['--protocol=tcp', '-h', MYSQL_HOST, '-P', String(MYSQL_PORT), '-u', MYSQL_USER, '-N', '-B', MYSQL_DATABASE, '-e', sql]; }
function mysql(sql) {
  return execFileSync('mysql', mysqlArgs(sql), { cwd: root, encoding: 'utf8', env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD } }).trim();
}
function redis(...args) {
  return execFileSync('redis-cli', ['-h', REDIS_HOST, '-p', String(REDIS_PORT), '--raw', ...args], { cwd: root, encoding: 'utf8' }).trim();
}
function holdQRWriteLock(seconds = 3) {
  const child = spawn('mysql', mysqlArgs(`LOCK TABLES qr_codes WRITE; DO SLEEP(${Number(seconds)}); UNLOCK TABLES;`), {
    cwd: root,
    env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
    stdio: ['ignore', 'ignore', 'pipe'],
  });
  let stderr = '';
  child.stderr.on('data', (chunk) => { stderr += chunk.toString(); });
  const done = new Promise((resolve, reject) => {
    child.on('error', reject);
    child.on('exit', (code) => code === 0 ? resolve() : reject(new Error(`QR write lock exited ${code}: ${stderr}`)));
  });
  return { done };
}

function resetWorkspace() {
  const ws = sqlLiteral(WORKSPACE);
  mysql(`
    DELETE FROM qr_audit_events WHERE workspace_id=${ws};
    DELETE FROM qr_codes WHERE workspace_id=${ws};
    DELETE FROM qr_workspace_counters WHERE workspace_id=${ws};
    DELETE FROM link_audit_events WHERE workspace_id=${ws};
    DELETE FROM link_versions WHERE workspace_id=${ws};
    DELETE FROM links WHERE workspace_id=${ws};
  `);
  redis('FLUSHDB');
}
function authHeaders() {
  return {
    Accept: 'application/json',
    'Content-Type': 'application/json',
    'X-GoJet-Test-Actor': ACTOR,
    'X-GoJet-Test-Workspace': WORKSPACE,
    'X-GoJet-Test-Workspace-Role': 'owner',
  };
}
async function api(path, init = {}) {
  const response = await fetch(`${PLATFORM_URL}${path}`, { ...init, headers: { ...authHeaders(), ...(init.headers ?? {}) } });
  const type = response.headers.get('content-type') ?? '';
  let body = null;
  if (response.status !== 204) body = type.includes('application/json') ? await response.json() : await response.text();
  return { response, body };
}
async function createLink(code, destination = `https://destination.example/${code}`) {
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/links`, {
    method: 'POST',
    body: JSON.stringify({
      hostname: 'go.p08-browser.test', domain_kind: 'official', code,
      title: `P08 browser ${code}`, primary_destination: destination,
      redirect_status: 302, routing: [], ab: [], utm: {}, access: {},
      expires_at: null, click_limit: null, one_time: false, change_reason: 'P08 browser fixture',
    }),
  });
  assert(result.response.status === 201, `create Link failed: ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}
function riskKey(link) { return `risk:link:${link.id}:${link.risk_fingerprint}`; }
function setRisk(link, state, { stale = false, malformed = false } = {}) {
  const key = riskKey(link);
  redis('DEL', key);
  if (malformed) { redis('SET', key, '{not-json', 'EX', '300'); return; }
  const now = Date.now();
  const checked = new Date(now - (stale ? 600_000 : 1_000));
  const validUntil = new Date(stale ? now - 1_000 : now + 300_000);
  redis('SET', key, JSON.stringify({
    schema_version: 1,
    decision: state,
    fingerprint: link.risk_fingerprint,
    checked_at: checked.toISOString(),
    valid_until: validUntil.toISOString(),
    policy_version: 'p08-browser-v1',
  }), 'EX', '300');
}
async function createQR(link, label = `P08 QR ${link.code}`) {
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/qr-codes`, {
    method: 'POST',
    body: JSON.stringify({ source_link_id: link.id, label, change_reason: 'P08 browser fixture' }),
  });
  assert(result.response.status === 201, `create QR failed: ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}
async function seedReady(code = 'browser-ready', label = 'Browser ready QR') {
  resetWorkspace();
  const link = await createLink(code);
  setRisk(link, 'allow');
  const qr = await createQR(link, label);
  return { link, qr };
}

function diagnostics() { return { console_errors: [], page_errors: [], http_errors: [], request_failures: [] }; }
function attachDiagnostics(page, report) {
  page.on('console', (message) => { if (message.type() === 'error') report.console_errors.push({ text: message.text(), location: message.location() }); });
  page.on('pageerror', (error) => report.page_errors.push(String(error));
  page.on('response', (response) => {
    if (response.status() >= 400 && !response.url().endsWith('/favicon.ico')) report.http_errors.push({ status: response.status(), url: response.url(), resourceType: response.request().resourceType() });
  });
  page.on('requestfailed', (request) => report.request_failures.push({ url: request.url(), failure: request.failure() });
}
function allowedMatch(entry, rules) {
  return rules.some((rule) => entry.url.includes(rule.includes) && (rule.status === undefined || entry.status === rule.status));
}
function assertDiagnostics(report, label, { allowedHttp = [], allowedFailures = [] } = {}) {
  const unexpectedHttp = report.http_errors.filter((entry) => !allowedMatch(entry, allowedHttp));
  const unexpectedConsole = report.console_errors.filter((entry) => {
    const url = entry.location?.url ?? '';
    return !allowedHttp.some((rule) => url.includes(rule.includes) && (rule.status === undefined || String(entry.text).includes(String(rule.status))));
  });
  const unexpectedFailures = report.request_failures.filter((entry) => !allowedFailures.some((part) => entry.url.includes(part)));
  assert(unexpectedHttp.length === 0, `${label} unexpected HTTP errors: ${JSON.stringify(unexpectedHttp)}`);
  assert(unexpectedConsole.length === 0, `${label} unexpected console errors: ${JSON.stringify(unexpectedConsole)}`);
  assert(report.page_errors.length === 0, `${label} page errors: ${JSON.stringify(report.page_errors)}`);
  assert(unexpectedFailures.length === 0, `${label} request failures: ${JSON.stringify(unexpectedFailures)}`);
}
function writeResult(caseId, status, details, errors = []) {
  writeFileSync(`${resultsDir}/${caseId}.json`, `${JSON.stringify({
    node: 'P08',
    case_id: caseId,
    status,
    generated_at: new Date().toISOString(),
    implementation_commit: implementationCommit(),
    environment: {
      browser: executablePath,
      workspace: WORKSPACE_URL,
      platformapi: PLATFORM_URL,
      mysql: `${MYSQL_HOST}:${MYSQL_PORT}/${MYSQL_DATABASE}`,
      redis: `${REDIS_HOST}:${REDIS_PORT}`,
      canonical_viewports: viewports,
      authority: 'real built Workspace + native Go platformapi + real MySQL/Redis; no request interception or static browser fixture',
    },
    details,
    errors,
  }, null, 2)}\n`);
}
async function newPage(browser, viewport, options = {}) {
  const context = await browser.newContext({ viewport, deviceScaleFactor: 1, acceptDownloads: true, ...options });
  const page = await context.newPage();
  return { context, page };
}
async function goto(page, path, waitUntil = 'networkidle') { await page.goto(`${WORKSPACE_URL}${path}`, { waitUntil }); }
async function waitPath(page, pattern) {
  await page.waitForFunction((source) => new RegExp(source).test(location.pathname), pattern.source);
}
async function waitDetailState(page, expected) {
  const section = page.locator('[data-page="qr-detail"]');
  await section.waitFor();
  await page.waitForFunction((state) => document.querySelector('[data-page="qr-detail"]')?.getAttribute('data-state') === state, expected);
  assert(await section.getAttribute('data-state') === expected, `detail state expected ${expected}`);
}
async function ensureCreateOpen(page) {
  const source = page.getByLabel('Source Link', { exact: true });
  if (await source.isVisible()) return;
  const trigger = page.getByRole('button', { name: 'Create QR', exact: true });
  await trigger.waitFor();
  await trigger.click();
  await page.getByRole('heading', { name: 'Create QR', exact: true }).waitFor();
  await source.waitFor();
}
function isCollectionPost(response) {
  const url = new URL(response.url());
  return response.request().method() === 'POST' && url.pathname === `/api/workspaces/${WORKSPACE}/qr-codes`;
}
async function createThroughUI(page, link, label) {
  await ensureCreateOpen(page);
  await page.getByLabel('Source Link', { exact: true }).selectOption(String(link.id));
  await page.getByLabel('Label', { exact: true }).fill(label);
  const submit = page.getByRole('button', { name: 'Create QR', exact: true });
  const responsePromise = page.waitForResponse(isCollectionPost);
  await submit.click();
  const response = await responsePromise;
  assert(response.status() === 201, `UI create returned ${response.status()}`);
  await waitPath(page, /^\/app\/qr\/\d+$/);
  await waitDetailState(page, 'ready');
  return Number(new URL(page.url()).pathname.split('/').at(-1));
}
async function layoutEvidence(page) {
  return page.evaluate(() => {
    const visible = (node) => node instanceof HTMLElement && node.offsetParent !== null;
    const clipped = [...document.querySelectorAll('main h1, main h2, main h3, main button, main a, main label, main strong, main dd, main code')]
      .filter(visible)
      .filter((node) => node.clientWidth > 0 && node.scrollWidth > node.clientWidth + 1)
      .map((node) => ({ tag: node.tagName, text: node.textContent?.trim().slice(0, 120) ?? '', clientWidth: node.clientWidth, scrollWidth: node.scrollWidth }));
    const unnamed = [...document.querySelectorAll('main button, main a[href], main input, main select')]
      .filter(visible)
      .filter((node) => {
        const aria = node.getAttribute('aria-label')?.trim();
        const labelled = node.getAttribute('aria-labelledby')?.trim();
        const text = node.textContent?.trim();
        const id = node.id;
        const label = id ? document.querySelector(`label[for="${CSS.escape(id)}"]`)?.textContent?.trim() : '';
        return !(aria || labelled || text || label);
      })
      .map((node) => ({ tag: node.tagName, id: node.id }));
    return {
      viewport: { width: innerWidth, height: innerHeight },
      root_overflow_px: Math.max(0, document.documentElement.scrollWidth - document.documentElement.clientWidth),
      body_overflow_px: Math.max(0, document.body.scrollWidth - document.body.clientWidth),
      clipped_required_controls_or_text: clipped,
      unnamed_visible_controls: unnamed,
    };
  });
}
function assertLayout(layout, label) {
  assert(layout.root_overflow_px === 0, `${label} root overflow: ${JSON.stringify(layout)}`);
  assert(layout.body_overflow_px === 0, `${label} body overflow: ${JSON.stringify(layout)}`);
  assert(layout.clipped_required_controls_or_text.length === 0, `${label} clipped required content: ${JSON.stringify(layout.clipped_required_controls_or_text)}`);
  assert(layout.unnamed_visible_controls.length === 0, `${label} unnamed controls: ${JSON.stringify(layout.unnamed_visible_controls)}`);
}

async function caseT011(browser) {
  const observed = {};

  resetWorkspace();
  await createLink('t011-loading');
  {
    const { context, page } = await newPage(browser, viewports.desktop);
    const lock = holdQRWriteLock(3);
    await sleep(250);
    await goto(page, '/app/qr', 'domcontentloaded');
    await page.getByRole('status').filter({ hasText: 'Loading QR resources…' }).waitFor();
    observed.loading = { visible: true, route: new URL(page.url()).pathname };
    await lock.done;
    await page.getByRole('heading', { name: 'No QR codes yet', exact: true }).waitFor();
    await context.close();
  }

  resetWorkspace();
  const createLinkFixture = await createLink('t011-create');
  setRisk(createLinkFixture, 'allow');
  {
    const { context, page } = await newPage(browser, viewports.desktop);
    await goto(page, '/app/qr');
    await page.getByRole('heading', { name: 'No QR codes yet', exact: true }).waitFor();
    observed.empty = { visible: true };
    await ensureCreateOpen(page);
    observed.create_form = { source_picker: true, raw_destination_input_absent: await page.locator('input[type="url"]').count() === 0 };
    await page.getByLabel('Source Link', { exact: true }).selectOption(String(createLinkFixture.id));
    await page.getByLabel('Label', { exact: true }).fill('T011 created through UI');
    const lock = holdQRWriteLock(3);
    await sleep(250);
    const requestPromise = page.waitForRequest((request) => request.method() === 'POST' && new URL(request.url()).pathname === `/api/workspaces/${WORKSPACE}/qr-codes`);
    const responsePromise = page.waitForResponse(isCollectionPost);
    await page.getByRole('button', { name: 'Create QR', exact: true }).click();
    await requestPromise;
    assert(new URL(page.url()).pathname === '/app/qr', `fabricated success/navigation before server confirmation: ${page.url()}`);
    assert(mysql(`SELECT COUNT(*) FROM qr_codes WHERE workspace_id=${sqlLiteral(WORKSPACE)}`) === '0', 'QR row appeared before locked server transaction completed');
    await lock.done;
    const response = await responsePromise;
    assert(response.status() === 201, `T011 server-confirmed create status=${response.status()}`);
    await waitPath(page, /^\/app\/qr\/\d+$/);
    await waitDetailState(page, 'ready');
    observed.create_confirmed = {
      server_confirmation_status: response.status(),
      server_row_count: Number(mysql(`SELECT COUNT(*) FROM qr_codes WHERE workspace_id=${sqlLiteral(WORKSPACE)}`)),
      detail_ready: true,
    };
    await context.close();
  }

  resetWorkspace();
  const reviewLink = await createLink('t011-review');
  setRisk(reviewLink, 'review');
  {
    const { context, page } = await newPage(browser, viewports.desktop);
    await goto(page, '/app/qr');
    await ensureCreateOpen(page);
    await page.getByLabel('Source Link', { exact: true }).selectOption(String(reviewLink.id));
    const responsePromise = page.waitForResponse(isCollectionPost);
    await page.getByRole('button', { name: 'Create QR', exact: true }).click();
    const response = await responsePromise;
    assert(response.status() === 409, `T011 review create status=${response.status()}`);
    await page.getByRole('status').filter({ hasText: 'under review' }).waitFor();
    assert(mysql(`SELECT COUNT(*) FROM qr_codes WHERE workspace_id=${sqlLiteral(WORKSPACE)}`) === '0', 'risk-denied create persisted QR');
    observed.risk_denied = { message: 'under review', response_status: response.status(), row_count: 0 };
    await context.close();
  }

  resetWorkspace();
  const quotaLink = await createLink('t011-quota');
  setRisk(quotaLink, 'allow');
  await createQR(quotaLink, 'quota one');
  await createQR(quotaLink, 'quota two');
  {
    const { context, page } = await newPage(browser, viewports.desktop);
    await goto(page, '/app/qr');
    await page.getByRole('status').filter({ hasText: 'quota reached' }).waitFor();
    const createButton = page.getByRole('button', { name: 'Create QR', exact: true });
    assert(await createButton.isDisabled(), 'quota-reached create button must be disabled');
    observed.quota_reached = { used: 2, limit: 2, create_disabled: true };
    await context.close();
  }

  resetWorkspace();
  {
    const report = diagnostics();
    mysql('RENAME TABLE qr_codes TO qr_codes_p08_browser_error');
    try {
      const { context, page } = await newPage(browser, viewports.desktop);
      attachDiagnostics(page, report);
      await goto(page, '/app/qr', 'domcontentloaded');
      await page.getByRole('alert').filter({ hasText: 'QR resources could not be loaded' }).waitFor();
      observed.error = { visible: true };
      await page.waitForTimeout(150);
      await context.close();
    } finally {
      mysql('RENAME TABLE qr_codes_p08_browser_error TO qr_codes');
    }
    const allowedHttp = [{ status: 500, includes: `/api/workspaces/${WORKSPACE}/qr-codes` }];
    assert(report.http_errors.some((entry) => allowedMatch(entry, allowedHttp)), `T011 expected real QR API 500 not observed: ${JSON.stringify(report.http_errors)}`);
    assertDiagnostics(report, 'T011', { allowedHttp });
  }

  return { observed_states: observed, route_backed: true, server_confirmation_boundary: true, request_interception: false };
}

async function caseT012(browser) {
  const observed = {};
  const fixture = await seedReady('t012-ready', 'T012 ready QR');
  const report = diagnostics();
  const { context, page } = await newPage(browser, viewports.desktop);
  attachDiagnostics(page, report);

  const lock = holdQRWriteLock(3);
  await sleep(250);
  await goto(page, `/app/qr/${fixture.qr.id}`, 'domcontentloaded');
  await waitDetailState(page, 'loading');
  await page.getByRole('status').filter({ hasText: 'Loading QR resource…' }).waitFor();
  observed.loading = true;
  await lock.done;
  await waitDetailState(page, 'ready');
  await page.getByRole('img', { name: /QR code for https:\/\/go\.p08-browser\.test\/t012-ready/ }).waitFor();
  await page.getByText('SHA-256', { exact: true }).waitFor();
  observed.ready = { preview: true, state_label: await page.locator('.qr-state').first().textContent() };

  const downloadPromise = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download PNG', exact: true }).click();
  const download = await downloadPromise;
  assert(download.suggestedFilename() === `gojet-qr-${fixture.qr.id}.png`, `unexpected PNG filename ${download.suggestedFilename()}`);
  observed.download = { filename: download.suggestedFilename() };

  await goto(page, `/app/links/${fixture.link.id}`);
  await page.getByRole('tab', { name: 'QR', exact: true }).click();
  await page.getByRole('heading', { name: 'QR resources', exact: true }).waitFor();
  await page.getByText('T012 ready QR', { exact: true }).waitFor();
  assert(await page.getByText('QR is owned by P08', { exact: false }).count() === 0, 'P05 QR placeholder still visible');
  observed.link_detail = { same_qr_visible: true, placeholder_absent: true };

  setRisk(fixture.link, 'review');
  await goto(page, `/app/qr/${fixture.qr.id}`);
  await waitDetailState(page, 'source-link-review');
  await page.getByRole('status').filter({ hasText: 'under safety review' }).waitFor();
  assert(await page.getByRole('button', { name: 'Download PNG', exact: true }).isDisabled(), 'review download must be disabled');
  observed.review = { state: 'source-link-review', download_disabled: true };

  setRisk(fixture.link, 'block');
  await goto(page, `/app/qr/${fixture.qr.id}`);
  await waitDetailState(page, 'source-link-block');
  await page.getByRole('alert').filter({ hasText: 'not currently eligible for QR distribution' }).waitFor();
  observed.block = { state: 'source-link-block' };

  setRisk(fixture.link, 'allow');
  await goto(page, `/app/qr/${fixture.qr.id}`);
  await waitDetailState(page, 'ready');
  const deleteResponsePromise = page.waitForResponse((response) => response.request().method() === 'DELETE' && new URL(response.url()).pathname === `/api/workspaces/${WORKSPACE}/qr-codes/${fixture.qr.id}`);
  await page.getByRole('button', { name: 'Delete QR', exact: true }).click();
  const deleteResponse = await deleteResponsePromise;
  assert(deleteResponse.status() === 204, `T012 delete status=${deleteResponse.status()}`);
  await waitPath(page, /^\/app\/qr$/);
  assert(mysql(`SELECT deleted_at IS NOT NULL FROM qr_codes WHERE id=${Number(fixture.qr.id)}`) === '1', 'browser delete did not persist');
  observed.delete_action = { persisted: true, response_status: deleteResponse.status(), navigation: '/app/qr' };

  await goto(page, `/app/qr/${fixture.qr.id}`, 'domcontentloaded');
  await waitDetailState(page, 'deleted');
  await page.getByRole('status').filter({ hasText: 'was deleted' }).waitFor();
  observed.deleted = { state: 'deleted' };
  await page.waitForTimeout(150);

  const errorFixture = await seedReady('t012-error', 'T012 error QR');
  mysql('RENAME TABLE qr_codes TO qr_codes_p08_browser_error');
  try {
    await goto(page, `/app/qr/${errorFixture.qr.id}`, 'domcontentloaded');
    await waitDetailState(page, 'error');
    await page.getByRole('alert').filter({ hasText: 'could not be loaded' }).waitFor();
    observed.error = { state: 'error' };
    await page.waitForTimeout(250);
  } finally {
    mysql('RENAME TABLE qr_codes_p08_browser_error TO qr_codes');
  }

  const allowedHttp = [
    { status: 410, includes: `/api/workspaces/${WORKSPACE}/qr-codes/${fixture.qr.id}` },
    { status: 500, includes: `/api/workspaces/${WORKSPACE}/qr-codes/${errorFixture.qr.id}` },
  ];
  assert(report.http_errors.some((entry) => entry.status === 410), 'T012 deleted 410 was not observed');
  assert(report.http_errors.some((entry) => entry.status === 500), 'T012 controlled error 500 was not observed');
  assertDiagnostics(report, 'T012', {
    allowedHttp,
    allowedFailures: [`/api/workspaces/${WORKSPACE}/qr-codes/${fixture.qr.id}`],
  });

  const capture = 'gjv10__workspace-qr-detail__p08-t012-ready__normal__light__en__desktop.png';
  setRisk(errorFixture.link, 'allow');
  await goto(page, `/app/qr/${errorFixture.qr.id}`);
  await waitDetailState(page, 'ready');
  await page.screenshot({ path: `${capturesDir}/${capture}`, fullPage: false });
  await context.close();
  return {
    observed_states: observed,
    real_preview_download_delete: true,
    link_detail_same_authority: true,
    expected_error_responses: { deleted_410: true, controlled_500: true },
    capture: `artifacts/v10/P08/captures/${capture}`,
    diagnostics: report,
  };
}

async function caseT013(browser) {
  const perViewport = {};
  for (const [name, viewport] of Object.entries(viewports)) {
    resetWorkspace();
    const link = await createLink(`t013-${name}`);
    setRisk(link, 'allow');
    const report = diagnostics();
    const { context, page } = await newPage(browser, viewport);
    attachDiagnostics(page, report);

    await goto(page, '/app/qr');
    await page.getByRole('heading', { name: 'No QR codes yet', exact: true }).waitFor();
    await ensureCreateOpen(page);
    const listCreateLayout = await layoutEvidence(page);
    assertLayout(listCreateLayout, `T013 ${name} list/create`);

    const qrId = await createThroughUI(page, link, `T013 ${name} QR`);
    await page.getByRole('img', { name: /QR code for/ }).waitFor();
    const detailLayout = await layoutEvidence(page);
    assertLayout(detailLayout, `T013 ${name} detail/preview`);

    const downloadPromise = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Download PNG', exact: true }).click();
    const download = await downloadPromise;
    assert(download.suggestedFilename() === `gojet-qr-${qrId}.png`, `T013 ${name} download filename ${download.suggestedFilename()}`);

    setRisk(link, 'review');
    await goto(page, `/app/qr/${qrId}`);
    await waitDetailState(page, 'source-link-review');
    const deniedLayout = await layoutEvidence(page);
    assertLayout(deniedLayout, `T013 ${name} risk-denied`);

    setRisk(link, 'allow');
    await goto(page, `/app/qr/${qrId}`);
    await waitDetailState(page, 'ready');
    const deleteResponsePromise = page.waitForResponse((response) => response.request().method() === 'DELETE' && new URL(response.url()).pathname === `/api/workspaces/${WORKSPACE}/qr-codes/${qrId}`);
    await page.getByRole('button', { name: 'Delete QR', exact: true }).click();
    const deleteResponse = await deleteResponsePromise;
    assert(deleteResponse.status() === 204, `T013 ${name} delete status=${deleteResponse.status()}`);
    await waitPath(page, /^\/app\/qr$/);
    const afterDeleteLayout = await layoutEvidence(page);
    assertLayout(afterDeleteLayout, `T013 ${name} post-delete list`);

    const capture = `gjv10__workspace-qr__p08-t013-${name}__normal__light__en__${name}.png`;
    await page.screenshot({ path: `${capturesDir}/${capture}`, fullPage: false });
    assertDiagnostics(report, `T013 ${name}`);
    perViewport[name] = {
      viewport,
      list_create: listCreateLayout,
      detail_preview: detailLayout,
      risk_denied: deniedLayout,
      post_delete: afterDeleteLayout,
      download_filename: download.suggestedFilename(),
      capture: `artifacts/v10/P08/captures/${capture}`,
    };
    await context.close();
  }
  return {
    canonical_viewports: perViewport,
    root_body_overflow_zero: true,
    clipped_required_content: false,
    shared_workspace_responsive_system: true,
  };
}

async function tabUntil(page, locator, max = 30) {
  for (let count = 1; count <= max; count += 1) {
    await page.keyboard.press('Tab');
    if (await locator.evaluate((node) => node === document.activeElement)) return count;
  }
  throw new Error(`target was not keyboard reachable within ${max} Tab presses`);
}
async function focusEvidence(locator) {
  return locator.evaluate((node) => {
    const style = getComputedStyle(node);
    return { outline: style.outline, outline_width: style.outlineWidth, box_shadow: style.boxShadow, active: node === document.activeElement };
  });
}

async function caseT014(browser) {
  resetWorkspace();
  const link = await createLink('t014-keyboard');
  setRisk(link, 'allow');
  const report = diagnostics();
  const { context, page } = await newPage(browser, { width: 320, height: 800 }, { reducedMotion: 'reduce' });
  attachDiagnostics(page, report);

  await goto(page, '/app/qr');
  const headerCreate = page.getByRole('button', { name: 'Create QR', exact: true });
  const tabsToCreate = await tabUntil(page, headerCreate, 40);
  const focus = await focusEvidence(headerCreate);
  assert(focus.active, 'Create QR is not active after keyboard traversal');
  assert(focus.outline_width !== '0px' || focus.box_shadow !== 'none', `Create QR has no visible focus treatment: ${JSON.stringify(focus)}`);
  await headerCreate.press('Enter');
  await page.getByRole('heading', { name: 'Create QR', exact: true }).waitFor();

  const source = page.getByLabel('Source Link', { exact: true });
  const tabsToSource = await tabUntil(page, source, 20);
  assert(await source.getAttribute('required') !== null, 'Source Link required semantics missing');
  await source.press('ArrowDown');
  await source.press('Enter');
  assert(await source.inputValue() === String(link.id), `keyboard source selection failed: ${await source.inputValue()}`);

  const label = page.getByLabel('Label', { exact: true });
  const tabsToLabel = await tabUntil(page, label, 10);
  await page.keyboard.type('Keyboard QR');
  const submit = page.getByRole('button', { name: 'Create QR', exact: true });
  const tabsToSubmit = await tabUntil(page, submit, 10);
  const createResponsePromise = page.waitForResponse(isCollectionPost);
  await submit.press('Enter');
  const createResponse = await createResponsePromise;
  assert(createResponse.status() === 201, `T014 keyboard create status=${createResponse.status()}`);
  await waitPath(page, /^\/app\/qr\/\d+$/);
  await waitDetailState(page, 'ready');
  const qrId = Number(new URL(page.url()).pathname.split('/').at(-1));

  const reflow = await layoutEvidence(page);
  assertLayout(reflow, 'T014 320px reflow');
  const reduced = await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches);
  assert(reduced, 'reduced-motion media query is not active');
  await page.getByRole('img', { name: /QR code for/ }).waitFor();
  await page.getByRole('button', { name: 'Download PNG', exact: true }).waitFor();

  setRisk(link, 'review');
  await goto(page, `/app/qr/${qrId}`);
  await waitDetailState(page, 'source-link-review');
  const reviewMessage = page.getByRole('status').filter({ hasText: 'under safety review' });
  await reviewMessage.waitFor();
  await page.getByText('Source under review', { exact: true }).waitFor();
  const reviewRole = await reviewMessage.getAttribute('role');
  assert(reviewRole === 'status', `review message role=${reviewRole}`);

  setRisk(link, 'block');
  await goto(page, `/app/qr/${qrId}`);
  await waitDetailState(page, 'source-link-block');
  const blockMessage = page.getByRole('alert').filter({ hasText: 'not currently eligible for QR distribution' });
  await blockMessage.waitFor();
  await page.getByText('Source blocked', { exact: true }).waitFor();
  const blockRole = await blockMessage.getAttribute('role');
  assert(blockRole === 'alert', `block message role=${blockRole}`);

  const deniedStatus = page.getByRole('status').filter({ hasText: 'QR artifact distribution is unavailable' });
  await deniedStatus.waitFor();
  const nonColor = {
    review_text: 'Source under review',
    block_text: 'Source blocked',
    review_role: reviewRole,
    block_role: blockRole,
    denied_artifact_status: await deniedStatus.textContent(),
  };
  assertDiagnostics(report, 'T014');

  const capture = 'gjv10__workspace-qr__p08-t014-keyboard-reflow__reduced__light__en__mobile.png';
  await page.screenshot({ path: `${capturesDir}/${capture}`, fullPage: false });
  await context.close();
  return {
    keyboard: {
      tabs_to_header_create: tabsToCreate,
      tabs_to_source: tabsToSource,
      tabs_to_label: tabsToLabel,
      tabs_to_submit: tabsToSubmit,
      focus,
      server_confirmation_status: createResponse.status(),
    },
    accessible_names_roles_values: true,
    required_source: true,
    status_and_alert_roles: true,
    non_color_safety_meaning: nonColor,
    reduced_motion: reduced,
    reflow_320: reflow,
    capture: `artifacts/v10/P08/captures/${capture}`,
    diagnostics: report,
  };
}

const requested = process.argv.includes('--case') ? process.argv[process.argv.indexOf('--case') + 1] : 'all';
const supported = ['P08-T011', 'P08-T012', 'P08-T013', 'P08-T014'];
const cases = requested === 'all' ? supported : [requested];
for (const caseId of cases) if (!supported.includes(caseId)) throw new Error(`Unsupported P08 browser case ${caseId}`);

const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
let failed = false;
try {
  for (const caseId of cases) {
    try {
      const details = caseId === 'P08-T011' ? await caseT011(browser)
        : caseId === 'P08-T012' ? await caseT012(browser)
          : caseId === 'P08-T013' ? await caseT013(browser)
            : await caseT014(browser);
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
