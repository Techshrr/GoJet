import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const root = process.cwd();
const resultsDir = `${root}/artifacts/v10/P05/results`;
const browserDir = `${root}/artifacts/v10/P05/browser`;
const capturesDir = `${root}/artifacts/v10/P05/captures`;
mkdirSync(resultsDir, { recursive: true });
mkdirSync(browserDir, { recursive: true });
mkdirSync(capturesDir, { recursive: true });

const WORKSPACE_URL = process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174';
const PLATFORM_URL = process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081';
const REDIRECT_URL = process.env.GOJET_TEST_REDIRECT_URL ?? 'http://127.0.0.1:18080';
const REDIS_HOST = process.env.GOJET_TEST_REDIS_HOST ?? '127.0.0.1';
const REDIS_PORT = process.env.GOJET_TEST_REDIS_PORT ?? '6379';
const WORKSPACE = process.env.GOJET_TEST_WORKSPACE_ID ?? 'ws-p05-browser';
const ACTOR = process.env.GOJET_TEST_ACTOR_ID ?? 'p05-browser-owner';
const HOSTNAME = process.env.GOJET_TEST_SHORT_HOST ?? '127.0.0.1';

const variables = JSON.parse(
  readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8'),
).tokens.composite;

function parseViewport(value, tokenName) {
  const match = /^(\d+)×(\d+)$/.exec(String(value));
  if (!match) throw new Error(`Invalid canonical viewport ${tokenName}: ${String(value)}`);
  return { width: Number(match[1]), height: Number(match[2]) };
}

const viewports = {
  desktop: parseViewport(variables['viewport.desktop'].dimensions, 'viewport.desktop'),
  mobile: parseViewport(variables['viewport.mobile'].dimensions, 'viewport.mobile'),
};

const chromeCandidates = [
  process.env.CHROME_BIN,
  '/usr/bin/google-chrome',
  '/usr/bin/google-chrome-stable',
  '/usr/bin/chromium',
].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P05 browser evidence');

function implementationCommit() {
  return execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
}

function isoNow() {
  return new Date().toISOString();
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
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
  const response = await fetch(`${PLATFORM_URL}${path}`, {
    ...init,
    headers: { ...authHeaders(), ...(init.headers ?? {}) },
  });
  const type = response.headers.get('content-type') ?? '';
  const body = type.includes('application/json') ? await response.json() : await response.text();
  return { response, body };
}

function createPayload(code, destination, title = code) {
  return {
    hostname: HOSTNAME,
    domain_kind: 'official',
    code,
    title,
    primary_destination: destination,
    redirect_status: 302,
    routing: [],
    ab: [],
    utm: {},
    access: {},
    expires_at: null,
    click_limit: null,
    one_time: false,
    change_reason: `browser fixture ${code}`,
  };
}

async function ensureLink(code, destination, title = code) {
  const list = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/links?q=${encodeURIComponent(code)}&limit=100&offset=0`);
  assert(list.response.ok, `fixture list failed: ${list.response.status}`);
  const existing = Array.isArray(list.body?.items) ? list.body.items.find((item) => item.code === code) : undefined;
  if (existing) return existing;
  const created = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/links`, {
    method: 'POST',
    body: JSON.stringify(createPayload(code, destination, title)),
  });
  assert(created.response.status === 201, `fixture create ${code} failed: ${created.response.status} ${JSON.stringify(created.body)}`);
  return created.body;
}

function redis(...args) {
  return execFileSync('redis-cli', ['-h', REDIS_HOST, '-p', String(REDIS_PORT), '--raw', ...args], {
    cwd: root,
    encoding: 'utf8',
  }).trim();
}

function riskKey(link) {
  return `risk:link:${link.id}:${link.risk_fingerprint}`;
}

function setRisk(link, decision) {
  const checkedAt = new Date(Date.now() - 1000);
  const validUntil = new Date(Date.now() + 5 * 60 * 1000);
  const payload = JSON.stringify({
    schema_version: 1,
    decision,
    fingerprint: link.risk_fingerprint,
    checked_at: checkedAt.toISOString(),
    valid_until: validUntil.toISOString(),
    policy_version: 'p05-browser-v1',
  });
  redis('SET', riskKey(link), payload, 'EX', '300');
}

function clearRisk(link) {
  redis('DEL', riskKey(link));
}

function diagnostics() {
  return { console_errors: [], page_errors: [], http_errors: [], request_failures: [] };
}

function attachDiagnostics(page, report) {
  page.on('console', (message) => {
    if (message.type() === 'error') report.console_errors.push({ text: message.text(), location: message.location() });
  });
  page.on('pageerror', (error) => report.page_errors.push(String(error)));
  page.on('response', (response) => {
    if (response.status() >= 400 && !response.url().endsWith('/favicon.ico')) {
      report.http_errors.push({ status: response.status(), url: response.url(), resourceType: response.request().resourceType() });
    }
  });
  page.on('requestfailed', (request) => report.request_failures.push({ url: request.url(), failure: request.failure() }));
}

function assertCleanDiagnostics(report, label) {
  assert(report.console_errors.length === 0, `${label} console errors: ${JSON.stringify(report.console_errors)}`);
  assert(report.page_errors.length === 0, `${label} page errors: ${JSON.stringify(report.page_errors)}`);
  assert(report.request_failures.length === 0, `${label} request failures: ${JSON.stringify(report.request_failures)}`);
  assert(report.http_errors.length === 0, `${label} HTTP errors: ${JSON.stringify(report.http_errors)}`);
}

function writeResult(caseId, status, details, errors = []) {
  const payload = {
    case_id: caseId,
    status,
    generated_at: isoNow(),
    implementation_commit: implementationCommit(),
    environment: {
      browser: executablePath,
      workspace: WORKSPACE_URL,
      platformapi: PLATFORM_URL,
      redirectengine: REDIRECT_URL,
      redis: `${REDIS_HOST}:${REDIS_PORT}`,
      canonical_viewports: viewports,
    },
    details,
    errors,
  };
  writeFileSync(`${resultsDir}/${caseId}.json`, `${JSON.stringify(payload, null, 2)}\n`);
}

async function caseT017(browser) {
  const report = diagnostics();
  const context = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1 });
  const page = await context.newPage();
  attachDiagnostics(page, report);

  await page.goto(`${WORKSPACE_URL}/app/links`, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Links', exact: true }).waitFor();
  await page.getByRole('link', { name: 'Create link' }).first().click();
  await page.waitForURL((url) => url.pathname === '/app/links/new');

  await page.locator('#link-destination').fill('https://example.com/browser-flow');
  await page.locator('#link-hostname').fill(HOSTNAME);
  await page.locator('#link-code').fill('browser-flow');
  await page.locator('#link-title').fill('Browser flow');
  await page.locator('#link-change-reason').fill('Create browser flow');
  const createResponse = page.waitForResponse((response) => response.request().method() === 'POST' && /\/api\/workspaces\/[^/]+\/links$/.test(new URL(response.url()).pathname));
  await page.getByRole('button', { name: 'Create link' }).click();
  const created = await createResponse;
  assert(created.status() === 201, `UI create returned ${created.status()}`);
  await page.waitForURL((url) => /^\/app\/links\/\d+$/.test(url.pathname));
  const createdId = Number(new URL(page.url()).pathname.split('/').at(-1));
  assert(Number.isSafeInteger(createdId) && createdId > 0, `invalid created link id: ${createdId}`);

  await page.getByText(/No current destination-risk decision exists/).waitFor();
  await page.locator('#detail-title').fill('Browser flow updated');
  await page.locator('#detail-reason').fill('Browser detail edit');
  const patchResponse = page.waitForResponse((response) => response.request().method() === 'PATCH' && new URL(response.url()).pathname.endsWith(`/links/${createdId}`));
  await page.getByRole('button', { name: 'Save changes' }).click();
  const patched = await patchResponse;
  assert(patched.status() === 200, `UI update returned ${patched.status()}`);
  await page.getByText(/No current destination-risk decision exists/).waitFor();

  await page.getByRole('tab', { name: 'History' }).click();
  await page.getByText('Version 2', { exact: true }).waitFor();
  await page.getByText('Version 1', { exact: true }).waitFor();

  await page.getByRole('link', { name: 'Back to links' }).click();
  await page.waitForURL((url) => url.pathname === '/app/links');
  await page.locator('#links-search').fill('browser-flow');
  await page.getByRole('link', { name: `${HOSTNAME}/browser-flow` }).waitFor();

  await page.reload({ waitUntil: 'networkidle' });
  await page.getByRole('link', { name: `${HOSTNAME}/browser-flow` }).waitFor();
  const capture = 'gjv10__workspace-links__p05-browser-flow__normal__light__en__desktop.png';
  await page.screenshot({ path: `${capturesDir}/${capture}`, fullPage: false });
  assertCleanDiagnostics(report, 'P05-T017');
  await context.close();

  return {
    created_link_id: createdId,
    create_status: created.status(),
    update_status: patched.status(),
    history_versions_verified: [1, 2],
    reload_persisted: true,
    capture: `artifacts/v10/P05/captures/${capture}`,
    diagnostics: report,
  };
}

async function caseT018(browser) {
  const fixture = await ensureLink('mobile-a11y', 'https://example.com/mobile-a11y', 'Mobile accessibility fixture');
  const report = diagnostics();
  const context = await browser.newContext({ viewport: viewports.mobile, deviceScaleFactor: 1 });
  const page = await context.newPage();
  attachDiagnostics(page, report);

  await page.goto(`${WORKSPACE_URL}/app/links`, { waitUntil: 'networkidle' });
  const layout = await page.evaluate(() => {
    const clipped = [...document.querySelectorAll('main h1, main h2, main button, main a, main label')]
      .filter((node) => node instanceof HTMLElement && node.offsetParent !== null && node.clientWidth > 0 && node.scrollWidth > node.clientWidth + 1)
      .map((node) => node.textContent?.trim()).filter(Boolean);
    const unnamed = [...document.querySelectorAll('a[href],button,input,select,textarea,[role="tab"]')]
      .filter((node) => node instanceof HTMLElement && node.offsetParent !== null)
      .filter((node) => {
        const labelledBy = node.getAttribute('aria-labelledby');
        const labelledText = labelledBy ? labelledBy.split(/\s+/).map((id) => document.getElementById(id)?.textContent ?? '').join(' ').trim() : '';
        const labelsText = 'labels' in node && node.labels ? [...node.labels].map((label) => label.textContent ?? '').join(' ').trim() : '';
        const name = node.getAttribute('aria-label') || labelledText || labelsText || node.textContent?.trim() || node.getAttribute('title') || '';
        return !name;
      })
      .map((node) => ({ tag: node.tagName, id: node.id, role: node.getAttribute('role') }));
    return {
      viewport: { width: window.innerWidth, height: window.innerHeight },
      root_overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
      body_overflow: document.body.scrollWidth > document.body.clientWidth,
      clipped_required_text: clipped,
      unnamed_visible_controls: unnamed,
    };
  });
  assert(layout.viewport.width === viewports.mobile.width && layout.viewport.height === viewports.mobile.height, `mobile viewport mismatch: ${JSON.stringify(layout.viewport)}`);
  assert(!layout.root_overflow && !layout.body_overflow, `mobile root overflow detected: ${JSON.stringify(layout)}`);
  assert(layout.clipped_required_text.length === 0, `required text clipped: ${JSON.stringify(layout.clipped_required_text)}`);
  assert(layout.unnamed_visible_controls.length === 0, `unnamed visible controls: ${JSON.stringify(layout.unnamed_visible_controls)}`);

  await page.goto(`${WORKSPACE_URL}/app/links/${fixture.id}`, { waitUntil: 'networkidle' });
  const overviewTab = page.getByRole('tab', { name: 'Overview' });
  await overviewTab.focus();
  await page.keyboard.press('ArrowRight');
  const focusedTab = await page.evaluate(() => document.activeElement?.textContent?.trim() ?? '');
  assert(focusedTab === 'Analytics', `tab keyboard focus did not advance: ${focusedTab}`);

  const capture = 'gjv10__workspace-links__p05-mobile-a11y__normal__light__en__mobile.png';
  await page.screenshot({ path: `${capturesDir}/${capture}`, fullPage: false });
  assertCleanDiagnostics(report, 'P05-T018');
  await context.close();

  return {
    fixture_link_id: fixture.id,
    layout,
    keyboard_tab_focus_after_arrow_right: focusedTab,
    capture: `artifacts/v10/P05/captures/${capture}`,
    diagnostics: report,
  };
}

async function caseT019(browser) {
  const review = await ensureLink('browser-review', 'https://unsafe.example/review', 'Review safety fixture');
  const blocked = await ensureLink('browser-blocked', 'https://unsafe.example/blocked', 'Blocked safety fixture');
  const missing = await ensureLink('browser-missing', 'https://unsafe.example/missing', 'Missing safety fixture');
  setRisk(review, 'review');
  setRisk(blocked, 'block');
  clearRisk(missing);

  const report = diagnostics();
  const context = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1 });
  const page = await context.newPage();
  attachDiagnostics(page, report);
  const scenarios = [
    { name: 'review', link: review, heading: 'Link under review' },
    { name: 'blocked', link: blocked, heading: 'Link blocked' },
    { name: 'missing', link: missing, heading: 'Link temporarily unavailable' },
  ];
  const outcomes = [];

  for (const scenario of scenarios) {
    const response = await page.goto(`${REDIRECT_URL}/${scenario.link.code}`, { waitUntil: 'networkidle' });
    assert(response, `no navigation response for ${scenario.name}`);
    assert(response.status() === 200, `${scenario.name} safety status ${response.status()}`);
    await page.getByRole('heading', { name: scenario.heading }).waitFor();
    const body = await page.textContent('body');
    const hrefs = await page.locator('a[href]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('href')));
    assert(!body.includes(scenario.link.primary_destination), `${scenario.name} leaked destination in body`);
    assert(hrefs.length === 0, `${scenario.name} exposes bypass links: ${JSON.stringify(hrefs)}`);
    assert(!response.headers().location, `${scenario.name} exposed Location header`);
    assert(response.headers()['x-robots-tag'] === 'noindex, nofollow', `${scenario.name} missing X-Robots-Tag`);
    assert(response.headers()['cache-control'] === 'no-store', `${scenario.name} missing no-store`);
    assert(response.headers()['referrer-policy'] === 'no-referrer', `${scenario.name} missing no-referrer`);
    assert((response.headers()['content-security-policy'] ?? '').includes("default-src 'none'"), `${scenario.name} missing fail-closed CSP`);
    const capture = `gjv10__redirect-safety__p05-${scenario.name}__blocked__light__en__desktop.png`;
    await page.screenshot({ path: `${capturesDir}/${capture}`, fullPage: false });
    outcomes.push({
      state: scenario.name,
      link_id: scenario.link.id,
      status: response.status(),
      final_url: page.url(),
      destination_absent: true,
      bypass_links: hrefs,
      capture: `artifacts/v10/P05/captures/${capture}`,
    });
  }

  assertCleanDiagnostics(report, 'P05-T019');
  await context.close();
  return { outcomes, diagnostics: report };
}

const CASES = {
  'P05-T017': caseT017,
  'P05-T018': caseT018,
  'P05-T019': caseT019,
};

const caseIndex = process.argv.indexOf('--case');
const requested = caseIndex >= 0 ? process.argv[caseIndex + 1] : undefined;
if (!requested || (requested !== 'all' && !Object.hasOwn(CASES, requested))) {
  throw new Error(`Usage: node scripts/p05/browser.mjs --case ${Object.keys(CASES).join('|')}|all`);
}

const selected = requested === 'all' ? Object.keys(CASES) : [requested];
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] });
const summary = { generated_at: isoNow(), implementation_commit: implementationCommit(), cases: [] };
let allPassed = true;

for (const caseId of selected) {
  try {
    const details = await CASES[caseId](browser);
    writeResult(caseId, 'PASS', details, []);
    summary.cases.push({ case_id: caseId, status: 'PASS' });
    console.log(`${caseId}: PASS`);
  } catch (error) {
    const message = `${error?.name ?? 'Error'}: ${error?.message ?? String(error)}`;
    writeResult(caseId, 'FAIL', {}, [message]);
    summary.cases.push({ case_id: caseId, status: 'FAIL', error: message });
    allPassed = false;
    console.error(`${caseId}: FAIL\n  - ${message}`);
  }
}

await browser.close();
writeFileSync(`${browserDir}/browser-summary.json`, `${JSON.stringify(summary, null, 2)}\n`);
process.exitCode = allPassed ? 0 : 1;
