import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const root = process.cwd();
const resultsDir = `${root}/artifacts/v10/P06/results`;
const capturesDir = `${root}/artifacts/v10/P06/captures`;
const browserDir = `${root}/artifacts/v10/P06/browser`;
mkdirSync(resultsDir, { recursive: true });
mkdirSync(capturesDir, { recursive: true });
mkdirSync(browserDir, { recursive: true });

const WORKSPACE_URL = process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174';
const PLATFORM_URL = process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081';
const WORKSPACE = process.env.GOJET_TEST_WORKSPACE_ID ?? 'ws-p06-browser';
const ACTOR = process.env.GOJET_TEST_ACTOR_ID ?? 'p06-browser-owner';
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';

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
if (!executablePath) throw new Error('System Chrome/Chromium is required for P06 browser evidence');

function implementationCommit() {
  return execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function sqlLiteral(value) {
  return `'${String(value).replaceAll("'", "''")}'`;
}

function mysql(sql) {
  return execFileSync('mysql', [
    '--protocol=tcp', '-h', MYSQL_HOST, '-P', String(MYSQL_PORT), '-u', MYSQL_USER,
    '-N', '-B', MYSQL_DATABASE, '-e', sql,
  ], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
  }).trim();
}

function resetWorkspace() {
  const ws = sqlLiteral(WORKSPACE);
  mysql(`
    DELETE FROM link_audit_events WHERE workspace_id=${ws};
    DELETE FROM link_versions WHERE workspace_id=${ws};
    DELETE FROM links WHERE workspace_id=${ws};
    DELETE FROM custom_domain_revalidations WHERE workspace_id=${ws};
    DELETE FROM custom_domain_audit_events WHERE workspace_id=${ws};
    DELETE FROM custom_domains WHERE workspace_id=${ws};
    DELETE FROM custom_domain_usage WHERE workspace_id=${ws};
    DELETE FROM custom_domain_entitlement_requests WHERE workspace_id=${ws};
    DELETE FROM custom_domain_entitlement_sources WHERE workspace_id=${ws};
  `);
}

function seedActive(limit = 5) {
  mysql(`INSERT INTO custom_domain_entitlement_sources
    (workspace_id,source,source_key,status,domain_limit,starts_at,decision_reason)
    VALUES (${sqlLiteral(WORKSPACE)},'plan','business-browser','active',${Number(limit)},DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 30 DAY),'browser active entitlement')`);
}

function seedRequested() {
  mysql(`INSERT INTO custom_domain_entitlement_requests
    (workspace_id,support_ticket_id,requested_domain_limit,status,submitted_at)
    VALUES (${sqlLiteral(WORKSPACE)},'P06-BROWSER-REQUEST',3,'requested',UTC_TIMESTAMP(6))`);
}

function seedGrace() {
  mysql(`INSERT INTO custom_domain_entitlement_sources
    (workspace_id,source,source_key,status,domain_limit,starts_at,expires_at,degraded_at,grace_until,decision_reason)
    VALUES (${sqlLiteral(WORKSPACE)},'plan','business-browser','active',5,DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 30 DAY),DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 DAY),DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 DAY),DATE_ADD(DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 DAY),INTERVAL 7 DAY),'normal browser downgrade')`);
}

function seedSecurity(status) {
  mysql(`INSERT INTO custom_domain_entitlement_sources
    (workspace_id,source,source_key,status,domain_limit,starts_at,decision_reason,security_category)
    VALUES (${sqlLiteral(WORKSPACE)},'plan','business-browser',${sqlLiteral(status)},5,DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 30 DAY),${sqlLiteral(`browser ${status}`)},${sqlLiteral(status === 'revoked' ? 'security' : 'abuse')})`);
}

function seedExpired() {
  mysql(`INSERT INTO custom_domain_entitlement_sources
    (workspace_id,source,source_key,status,domain_limit,starts_at,expires_at,decision_reason)
    VALUES (${sqlLiteral(WORKSPACE)},'plan','business-browser','expired',5,DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 30 DAY),DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 DAY),'browser expired entitlement')`);
}

function authHeaders(role = 'owner') {
  return {
    Accept: 'application/json',
    'Content-Type': 'application/json',
    'X-GoJet-Test-Actor': ACTOR,
    'X-GoJet-Test-Workspace': WORKSPACE,
    'X-GoJet-Test-Workspace-Role': role,
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

async function createDomain(hostname, reason = 'P06 browser fixture') {
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/domains`, {
    method: 'POST', body: JSON.stringify({ hostname, change_reason: reason }),
  });
  assert(result.response.status === 201, `create domain ${hostname} failed: ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}

function seedDomainReady(domainId) {
  mysql(`UPDATE custom_domains SET
    routing_state='enabled', ownership_status='verified', ownership_verified_at=UTC_TIMESTAMP(6),
    ingress_dns_status='valid', ingress_dns_checked_at=UTC_TIMESTAMP(6),
    https_status='active', https_checked_at=UTC_TIMESTAMP(6),
    risk_status='allow', risk_checked_at=UTC_TIMESTAMP(6), risk_policy_version='p06-browser-v1', risk_evidence_ref='risk:p06-browser'
    WHERE workspace_id=${sqlLiteral(WORKSPACE)} AND id=${Number(domainId)}`);
}

function seedDomainProblems(domainId) {
  mysql(`UPDATE custom_domains SET
    routing_state='pending', ownership_status='verified', ownership_verified_at=UTC_TIMESTAMP(6),
    ingress_dns_status='invalid', ingress_dns_checked_at=UTC_TIMESTAMP(6),
    https_status='error', https_checked_at=UTC_TIMESTAMP(6),
    risk_status='review', risk_checked_at=UTC_TIMESTAMP(6), risk_policy_version='p06-browser-v1', risk_evidence_ref='risk:p06-browser-problem'
    WHERE workspace_id=${sqlLiteral(WORKSPACE)} AND id=${Number(domainId)}`);
}

function seedRevalidations(domainId) {
  const axes = [
    ['entitlement', 'pass'], ['ownership', 'pass'], ['ingress_dns', 'pass'], ['https', 'pass'], ['risk', 'pass'],
  ];
  for (const [axis, result] of axes) {
    mysql(`INSERT INTO custom_domain_revalidations
      (domain_id,workspace_id,axis,result,policy_version,checked_at,next_due_at,evidence_ref,correlation_id,metadata_json)
      VALUES (${Number(domainId)},${sqlLiteral(WORKSPACE)},${sqlLiteral(axis)},${sqlLiteral(result)},'p06-browser-v1',UTC_TIMESTAMP(6),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 HOUR),'private-browser-evidence','corr-p06-browser',JSON_OBJECT('fixture','browser'))`);
  }
}

async function createAssignedLink(hostname, code = 'domain-resource') {
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/links`, {
    method: 'POST',
    body: JSON.stringify({
      hostname,
      domain_kind: 'custom',
      code,
      title: 'P06 assigned resource',
      primary_destination: 'https://example.com/p06-browser-resource',
      redirect_status: 302,
      routing: [], ab: [], utm: {}, access: {},
      expires_at: null, click_limit: null, one_time: false,
      change_reason: 'P06 browser assigned resource fixture',
    }),
  });
  assert(result.response.status === 201, `assigned Link create failed: ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
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
    generated_at: new Date().toISOString(),
    implementation_commit: implementationCommit(),
    environment: {
      browser: executablePath,
      workspace: WORKSPACE_URL,
      platformapi: PLATFORM_URL,
      mysql: `${MYSQL_HOST}:${MYSQL_PORT}/${MYSQL_DATABASE}`,
      canonical_viewports: viewports,
      authority: 'real MySQL-backed Workspace Domains API; browser never supplies entitlement or trust state',
    },
    details,
    errors,
  };
  writeFileSync(`${resultsDir}/${caseId}.json`, `${JSON.stringify(payload, null, 2)}\n`);
}

async function caseT021(browser) {
  const report = diagnostics();
  const context = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1 });
  const page = await context.newPage();
  attachDiagnostics(page, report);
  const observed = {};

  const scenarios = [
    { name: 'locked', seed: () => {} },
    { name: 'requested', seed: seedRequested },
    { name: 'grace', seed: seedGrace },
    { name: 'suspended', seed: () => seedSecurity('suspended') },
    { name: 'expired', seed: seedExpired },
    { name: 'revoked', seed: () => seedSecurity('revoked') },
  ];

  for (const scenario of scenarios) {
    resetWorkspace();
    scenario.seed();
    await page.goto(`${WORKSPACE_URL}/app/domains`, { waitUntil: 'networkidle' });
    await page.getByRole('heading', { name: 'Custom domains' }).waitFor();
    const authority = page.locator('.domains-entitlement');
    await authority.getByText(scenario.name, { exact: true }).waitFor();
    assert(await page.getByRole('link', { name: 'Add domain' }).count() === 0, `${scenario.name} exposed forbidden Add domain`);

    await page.goto(`${WORKSPACE_URL}/app/domains/new`, { waitUntil: 'networkidle' });
    await page.getByRole('heading', { name: 'Add domain unavailable' }).waitFor();
    assert(await page.locator('[data-wizard-mounted="true"]').count() === 0, `${scenario.name} deep link mounted wizard`);
    assert(await page.locator('#domain-hostname').count() === 0, `${scenario.name} deep link mounted/prefilled hostname input`);
    observed[scenario.name] = { add_control: false, deep_link_wizard_mounted: false };
  }

  resetWorkspace();
  seedActive(5);
  await page.goto(`${WORKSPACE_URL}/app/domains`, { waitUntil: 'networkidle' });
  await page.locator('.domains-entitlement').getByText('active', { exact: true }).waitFor();
  await page.getByRole('link', { name: 'Add domain' }).waitFor();
  await page.goto(`${WORKSPACE_URL}/app/domains/new`, { waitUntil: 'networkidle' });
  await page.locator('[data-wizard-mounted="true"]').waitFor();
  await page.getByRole('button', { name: 'Continue to hostname' }).click();
  const activeHostname = page.locator('#domain-hostname');
  await activeHostname.waitFor();
  assert(await activeHostname.inputValue() === '', 'active wizard hostname was unexpectedly prefilled');
  observed.active = { add_control: true, wizard_mounted: true, hostname_prefilled: false };

  resetWorkspace();
  seedActive(5);
  await createDomain('partial-t021.example.com', 'P06 T021 partial-axis fixture');
  await page.goto(`${WORKSPACE_URL}/app/domains`, { waitUntil: 'networkidle' });
  await page.locator('.domains-entitlement').getByText('partial-axis', { exact: true }).waitFor();
  await page.getByText('partial-t021.example.com', { exact: true }).waitFor();
  await page.getByText('Ownership').first().waitFor();
  await page.getByText('pending', { exact: true }).first().waitFor();
  observed['partial-axis'] = { persisted_domain: true, independent_pending_axes_visible: true, add_control: await page.getByRole('link', { name: 'Add domain' }).count() === 1 };

  const capture = 'gjv10__workspace-domains__p06-t021-entitlement__normal__light__en__desktop.png';
  await page.screenshot({ path: `${capturesDir}/${capture}`, fullPage: false });
  assertCleanDiagnostics(report, 'P06-T021');
  await context.close();
  return { observed_states: observed, capture: `artifacts/v10/P06/captures/${capture}`, diagnostics: report };
}

async function caseT022(browser) {
  resetWorkspace();
  seedActive(5);
  const report = diagnostics();
  const context = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1 });
  const page = await context.newPage();
  attachDiagnostics(page, report);

  await page.goto(`${WORKSPACE_URL}/app/domains/new`, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Entitlement' }).waitFor();
  await page.getByRole('button', { name: 'Continue to hostname' }).click();
  await page.getByRole('heading', { name: 'Hostname' }).waitFor();
  await page.locator('#domain-hostname').fill('WIZARD-T022.EXAMPLE.COM.');
  await page.locator('#domain-change-reason').fill('P06 T022 browser wizard');
  const createResponsePromise = page.waitForResponse((response) => response.request().method() === 'POST' && /\/api\/workspaces\/[^/]+\/domains$/.test(new URL(response.url()).pathname));
  await page.getByRole('button', { name: 'Create domain and continue' }).click();
  const createResponse = await createResponsePromise;
  assert(createResponse.status() === 201, `wizard create status=${createResponse.status()}`);
  const created = await createResponse.json();
  const domainId = Number(created.domain.id);
  const hostname = String(created.domain.hostname_ascii);
  const oneTimeSecret = String(created.ownership_txt_value);
  assert(hostname === 'wizard-t022.example.com', `hostname not canonicalized: ${hostname}`);
  assert(oneTimeSecret.startsWith('gojet-verification='), 'ownership TXT value missing from one-time create response');
  await page.getByRole('heading', { name: 'TXT ownership' }).waitFor();
  await page.getByText(created.ownership_txt_name, { exact: true }).waitFor();
  await page.getByText(oneTimeSecret, { exact: true }).waitFor();

  const safeDetailBefore = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/domains/${domainId}`);
  assert(safeDetailBefore.response.status === 200, `safe detail status=${safeDetailBefore.response.status}`);
  assert(!JSON.stringify(safeDetailBefore.body).includes(oneTimeSecret), 'safe detail leaked one-time ownership secret');
  assert(!JSON.stringify(safeDetailBefore.body).includes('risk_evidence_ref'), 'safe detail exposed risk evidence field');

  seedDomainReady(domainId);
  seedRevalidations(domainId);
  const assignedLink = await createAssignedLink(hostname, 'assigned-t022');

  await page.getByRole('button', { name: 'Refresh status' }).click();
  await page.getByText('verified', { exact: true }).waitFor();
  await page.getByRole('button', { name: 'Continue to DNS' }).click();
  await page.getByRole('heading', { name: 'Ingress DNS' }).waitFor();
  await page.getByText('valid', { exact: true }).waitFor();
  await page.getByRole('button', { name: 'Continue to HTTPS' }).click();
  await page.getByRole('heading', { name: 'HTTPS' }).waitFor();
  await page.getByText('active', { exact: true }).waitFor();
  await page.getByRole('button', { name: 'Continue to risk' }).click();
  await page.getByRole('heading', { name: 'Domain risk' }).waitFor();
  await page.getByText('allow', { exact: true }).waitFor();
  await page.getByRole('button', { name: 'Continue to Ready' }).click();
  await page.getByRole('heading', { name: 'Ready' }).waitFor();
  await page.getByText('All current mutation authorities are ready for new Link assignment.').waitFor();

  const wizardCapture = 'gjv10__workspace-domains__p06-t022-wizard-ready__normal__light__en__desktop.png';
  await page.screenshot({ path: `${capturesDir}/${wizardCapture}`, fullPage: false });
  await page.getByRole('link', { name: 'Open domain detail' }).click();
  await page.waitForURL((url) => url.pathname === `/app/domains/${domainId}`);
  await page.getByRole('heading', { name: hostname }).waitFor();
  assert(!(await page.locator('body').innerText()).includes(oneTimeSecret), 'domain detail DOM leaked one-time ownership secret');

  const expectedTabs = ['Overview', 'Entitlement', 'Ownership', 'DNS', 'HTTPS', 'Risk', 'Assigned resources', 'Revalidation', 'Settings'];
  for (const tab of expectedTabs) {
    await page.getByRole('tab', { name: tab }).click();
    await page.getByRole('heading', { name: tab, exact: true }).waitFor();
  }
  await page.getByRole('tab', { name: 'Assigned resources' }).click();
  await page.getByText('assigned-t022', { exact: false }).waitFor();
  await page.getByRole('tab', { name: 'Revalidation' }).click();
  await page.getByRole('table', { name: 'Custom domain revalidation history' }).waitFor();
  await page.getByText('p06-browser-v1', { exact: true }).first().waitFor();

  const detailCapture = 'gjv10__workspace-domains__p06-t022-detail__normal__light__en__desktop.png';
  await page.screenshot({ path: `${capturesDir}/${detailCapture}`, fullPage: false });
  assertCleanDiagnostics(report, 'P06-T022');
  await context.close();
  return {
    domain_id: domainId,
    canonical_hostname: hostname,
    seven_steps_verified: [...Array(7)].map((_, index) => index + 1),
    one_time_txt_exposed_only_on_create: true,
    safe_detail_secret_absent: true,
    detail_tabs_verified: expectedTabs,
    assigned_link_id: assignedLink.id,
    revalidation_axes_persisted: 5,
    captures: [`artifacts/v10/P06/captures/${wizardCapture}`, `artifacts/v10/P06/captures/${detailCapture}`],
    diagnostics: report,
  };
}

async function mobileLayout(page) {
  return page.evaluate(() => {
    const visible = (node) => node instanceof HTMLElement && node.offsetParent !== null;
    const clipped = [...document.querySelectorAll('main h1, main h2, main h3, main button, main a, main label, main strong')]
      .filter(visible)
      .filter((node) => node.clientWidth > 0 && node.scrollWidth > node.clientWidth + 1)
      .map((node) => node.textContent?.trim()).filter(Boolean);
    const unnamed = [...document.querySelectorAll('a[href],button,input,select,textarea,[role="tab"]')]
      .filter(visible)
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
}

async function caseT023(browser) {
  resetWorkspace();
  seedActive(5);
  const created = await createDomain('mobile-t023.example.com', 'P06 T023 persistent problem fixture');
  const domainId = Number(created.domain.id);
  seedDomainProblems(domainId);

  const report = diagnostics();
  const context = await browser.newContext({ viewport: viewports.mobile, deviceScaleFactor: 1 });
  const page = await context.newPage();
  attachDiagnostics(page, report);

  await page.goto(`${WORKSPACE_URL}/app/domains`, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Custom domains' }).waitFor();
  const listLayout = await mobileLayout(page);
  assert(listLayout.viewport.width === viewports.mobile.width && listLayout.viewport.height === viewports.mobile.height, `mobile viewport mismatch: ${JSON.stringify(listLayout.viewport)}`);
  assert(!listLayout.root_overflow && !listLayout.body_overflow, `domains list root/body overflow: ${JSON.stringify(listLayout)}`);
  assert(listLayout.clipped_required_text.length === 0, `domains list clipped required text: ${JSON.stringify(listLayout.clipped_required_text)}`);
  assert(listLayout.unnamed_visible_controls.length === 0, `domains list unnamed controls: ${JSON.stringify(listLayout.unnamed_visible_controls)}`);
  const statusTexts = await page.locator('.domains-state:visible,.domains-axis:visible strong').allTextContents();
  assert(statusTexts.length > 0 && statusTexts.every((value) => value.trim().length > 0), `status is color-only or empty: ${JSON.stringify(statusTexts)}`);

  await page.goto(`${WORKSPACE_URL}/app/domains/${domainId}`, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'mobile-t023.example.com' }).waitFor();
  await page.getByText('Ingress DNS is invalid. Current CNAME authority must be valid.').waitFor();
  await page.getByText('HTTPS is error. A current successful TLS/hostname check is required.').waitFor();
  await page.getByText('Domain risk is review. Only a current allow decision can satisfy this axis.').waitFor();
  const detailLayout = await mobileLayout(page);
  assert(!detailLayout.root_overflow && !detailLayout.body_overflow, `domain detail root/body overflow: ${JSON.stringify(detailLayout)}`);
  assert(detailLayout.clipped_required_text.length === 0, `domain detail clipped required text: ${JSON.stringify(detailLayout.clipped_required_text)}`);
  assert(detailLayout.unnamed_visible_controls.length === 0, `domain detail unnamed controls: ${JSON.stringify(detailLayout.unnamed_visible_controls)}`);

  const overviewTab = page.getByRole('tab', { name: 'Overview' });
  await overviewTab.focus();
  await page.keyboard.press('ArrowRight');
  const focused = await page.evaluate(() => document.activeElement?.textContent?.trim() ?? '');
  assert(focused === 'Entitlement', `domain tab keyboard focus did not advance: ${focused}`);
  const selectedTabs = await page.locator('[role="tab"][aria-selected="true"]').count();
  assert(selectedTabs === 1, `name-role-value contract has ${selectedTabs} selected tabs`);

  await page.reload({ waitUntil: 'networkidle' });
  await page.getByText('Ingress DNS is invalid. Current CNAME authority must be valid.').waitFor();
  await page.getByText('HTTPS is error. A current successful TLS/hostname check is required.').waitFor();
  await page.getByText('Domain risk is review. Only a current allow decision can satisfy this axis.').waitFor();

  await page.goto(`${WORKSPACE_URL}/app/domains/new`, { waitUntil: 'networkidle' });
  await page.locator('[data-wizard-mounted="true"]').waitFor();
  const wizardLayout = await mobileLayout(page);
  assert(!wizardLayout.root_overflow && !wizardLayout.body_overflow, `mobile wizard root/body overflow: ${JSON.stringify(wizardLayout)}`);
  assert(wizardLayout.clipped_required_text.length === 0, `mobile wizard clipped required text: ${JSON.stringify(wizardLayout.clipped_required_text)}`);
  await page.getByRole('button', { name: 'Continue to hostname' }).click();
  const hostnameInput = page.locator('#domain-hostname');
  await hostnameInput.waitFor();
  assert(await hostnameInput.inputValue() === '', 'mobile active wizard unexpectedly prefilled hostname');

  const capture = 'gjv10__workspace-domains__p06-t023-mobile-problems__normal__light__en__mobile.png';
  await page.screenshot({ path: `${capturesDir}/${capture}`, fullPage: false });
  assertCleanDiagnostics(report, 'P06-T023');
  await context.close();
  return {
    list_layout: listLayout,
    detail_layout: detailLayout,
    wizard_layout: wizardLayout,
    keyboard_tab_focus_after_arrow_right: focused,
    exactly_one_selected_tab: true,
    status_not_color_only: true,
    persistent_problem_states_after_reload: ['ingress_dns_invalid', 'https_error', 'risk_review'],
    active_wizard_hostname_prefilled: false,
    capture: `artifacts/v10/P06/captures/${capture}`,
    diagnostics: report,
  };
}

const requested = process.argv.includes('--case') ? process.argv[process.argv.indexOf('--case') + 1] : 'all';
const cases = requested === 'all' ? ['P06-T021', 'P06-T022', 'P06-T023'] : [requested];
for (const caseId of cases) {
  if (!['P06-T021', 'P06-T022', 'P06-T023'].includes(caseId)) throw new Error(`Unsupported P06 browser case ${caseId}`);
}

const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
let failed = false;
try {
  for (const caseId of cases) {
    try {
      const details = caseId === 'P06-T021' ? await caseT021(browser) : caseId === 'P06-T022' ? await caseT022(browser) : await caseT023(browser);
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
