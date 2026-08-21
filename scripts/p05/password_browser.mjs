import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const root = process.cwd();
const resultsDir = `${root}/artifacts/v10/P05/results`;
const capturesDir = `${root}/artifacts/v10/P05/captures`;
mkdirSync(resultsDir, { recursive: true });
mkdirSync(capturesDir, { recursive: true });

const WORKSPACE_URL = process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174';
const PLATFORM_URL = process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081';
const REDIRECT_URL = process.env.GOJET_TEST_REDIRECT_URL ?? 'http://127.0.0.1:18080';
const REDIS_HOST = process.env.GOJET_TEST_REDIS_HOST ?? '127.0.0.1';
const REDIS_PORT = process.env.GOJET_TEST_REDIS_PORT ?? '6379';
const WORKSPACE = process.env.GOJET_TEST_WORKSPACE_ID ?? 'ws-p05-browser';
const ACTOR = process.env.GOJET_TEST_ACTOR_ID ?? 'p05-browser-owner';
const HOSTNAME = process.env.GOJET_TEST_SHORT_HOST ?? '127.0.0.1';

const CODE = 'browser-password';
const PASSWORD_ONE = 'Browser-P05-Password-2026!';
const PASSWORD_TWO = 'Browser-P05-Replaced-2026!';
const DESTINATION = `${WORKSPACE_URL}/app/links`;

const chromeCandidates = [
  process.env.CHROME_BIN,
  '/usr/bin/google-chrome',
  '/usr/bin/google-chrome-stable',
  '/usr/bin/chromium',
].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P05 password browser evidence');

function exactHead() {
  return execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
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

async function api(path) {
  const response = await fetch(`${PLATFORM_URL}${path}`, { headers: authHeaders() });
  const raw = await response.text();
  let body = raw;
  try { body = raw ? JSON.parse(raw) : null; } catch { /* keep text */ }
  return { response, raw, body };
}

function redis(...args) {
  return execFileSync('redis-cli', ['-h', REDIS_HOST, '-p', String(REDIS_PORT), '--raw', ...args], {
    cwd: root,
    encoding: 'utf8',
  }).trim();
}

function setRisk(link, decision) {
  const payload = JSON.stringify({
    schema_version: 1,
    decision,
    fingerprint: link.risk_fingerprint,
    checked_at: new Date(Date.now() - 1000).toISOString(),
    valid_until: new Date(Date.now() + 5 * 60 * 1000).toISOString(),
    policy_version: 'p05-password-browser-v1',
  });
  redis('SET', `risk:link:${link.id}:${link.risk_fingerprint}`, payload, 'EX', '300');
}

function readCase(caseId) {
  const path = `${resultsDir}/${caseId}.json`;
  assert(existsSync(path), `baseline ${caseId} evidence is missing`);
  return { path, data: JSON.parse(readFileSync(path, 'utf8')) };
}

function augmentCase(caseId, detailKey, details, error = null) {
  const { path, data } = readCase(caseId);
  data.implementation_commit = exactHead();
  data.details = data.details ?? {};
  data.details[detailKey] = details ?? {};
  data.errors = Array.isArray(data.errors) ? data.errors.filter((item) => !String(item).startsWith(`${detailKey}:`)) : [];
  if (error) {
    data.errors.push(`${detailKey}: ${error}`);
    data.status = 'FAIL';
  }
  writeFileSync(path, `${JSON.stringify(data, null, 2)}\n`);
}

async function fetchLink(linkId) {
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/links/${linkId}`);
  assert(result.response.status === 200, `GET link ${linkId} returned ${result.response.status}: ${result.raw}`);
  assert(!result.raw.includes('password_hash'), 'Workspace API exposed password_hash');
  assert(!result.raw.includes(PASSWORD_ONE) && !result.raw.includes(PASSWORD_TWO), 'Workspace API exposed plaintext password');
  return result.body;
}

async function run() {
  const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] });
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
  const page = await context.newPage();
  const expectedHttpErrors = [];
  const expectedConsoleErrors = [];
  const unexpectedConsoleErrors = [];
  const pageErrors = [];
  const requestFailures = [];

  page.on('console', (message) => {
    if (message.type() !== 'error') return;
    const entry = { text: message.text(), location: message.location() };
    const expected401Text = 'Failed to load resource: the server responded with a status of 401 (Unauthorized)';
    if (entry.text === expected401Text && entry.location?.url === `${REDIRECT_URL}/${CODE}`) {
      expectedConsoleErrors.push(entry);
      return;
    }
    unexpectedConsoleErrors.push(entry);
  });
  page.on('pageerror', (error) => pageErrors.push(String(error)));
  page.on('requestfailed', (request) => requestFailures.push({ url: request.url(), failure: request.failure() }));
  page.on('response', (response) => {
    if (response.status() >= 400 && response.status() !== 401 && !response.url().endsWith('/favicon.ico')) {
      expectedHttpErrors.push({ status: response.status(), url: response.url() });
    }
  });

  try {
    await page.goto(`${WORKSPACE_URL}/app/links/new`, { waitUntil: 'networkidle' });
    await page.getByRole('heading', { name: 'Create link' }).waitFor();
    await page.locator('#link-destination').fill(DESTINATION);
    await page.locator('#link-hostname').fill(HOSTNAME);
    await page.locator('#link-code').fill(CODE);
    await page.locator('#link-title').fill('Browser password contract');
    await page.locator('#link-password').fill(PASSWORD_ONE);
    await page.locator('#link-change-reason').fill('Create password-protected browser link');

    const createResponsePromise = page.waitForResponse((response) =>
      response.request().method() === 'POST' && /\/api\/workspaces\/[^/]+\/links$/.test(new URL(response.url()).pathname));
    await page.getByRole('button', { name: 'Create link' }).click();
    const createResponse = await createResponsePromise;
    assert(createResponse.status() === 201, `Workspace password create returned ${createResponse.status()}`);
    await page.waitForURL((url) => /^\/app\/links\/\d+$/.test(url.pathname));
    const linkId = Number(new URL(page.url()).pathname.split('/').at(-1));
    assert(Number.isSafeInteger(linkId) && linkId > 0, `invalid password link id ${linkId}`);

    let link = await fetchLink(linkId);
    assert(link.access?.password_protected === true, 'created link is not password protected');

    await page.getByRole('tab', { name: 'Access' }).click();
    await page.getByText(/Password protection is currently\s+enabled/i).waitFor();
    assert((await page.locator('#detail-password').inputValue()) === '', 'Workspace echoed password into Detail input');
    assert(!(await page.textContent('body')).includes(PASSWORD_ONE), 'Workspace DOM exposed plaintext password');

    await page.locator('#detail-password').fill(PASSWORD_TWO);
    await page.locator('#detail-reason').fill('Replace browser password');
    const replaceResponsePromise = page.waitForResponse((response) =>
      response.request().method() === 'PATCH' && new URL(response.url()).pathname.endsWith(`/links/${linkId}`));
    await page.getByRole('button', { name: 'Save changes' }).click();
    const replaceResponse = await replaceResponsePromise;
    assert(replaceResponse.status() === 200, `Workspace password replace returned ${replaceResponse.status()}`);
    await page.getByText(/Password protection is currently\s+enabled/i).waitFor();
    await page.waitForFunction(() => document.querySelector('#detail-password')?.value === '');
    assert(!(await page.textContent('body')).includes(PASSWORD_TWO), 'Workspace DOM retained replacement password');

    link = await fetchLink(linkId);
    assert(link.version === 2, `password replacement expected version 2, got ${link.version}`);
    assert(link.access?.password_protected === true, 'replacement disabled password protection unexpectedly');
    setRisk(link, 'allow');

    const workspaceCapture = 'gjv10__workspace-links__p05-password-access__protected__light__en__desktop.png';
    await page.screenshot({ path: `${capturesDir}/${workspaceCapture}`, fullPage: false });

    const challengeResponse = await page.goto(`${REDIRECT_URL}/${CODE}`, { waitUntil: 'networkidle' });
    assert(challengeResponse?.status() === 200, `password challenge returned ${challengeResponse?.status()}`);
    await page.getByRole('heading', { name: 'Password required' }).waitFor();
    assert(await page.getByRole('textbox', { name: 'Password', exact: true }).isVisible(), 'password challenge input is not accessible by label');
    const challengeBody = await page.textContent('body');
    assert(!challengeBody.includes(DESTINATION), 'password challenge leaked destination');
    assert((await page.locator('a[href]').count()) === 0, 'password challenge exposes bypass links');
    assert(!challengeResponse.headers().location, 'password challenge exposed Location header');
    assert(challengeResponse.headers()['cache-control'] === 'no-store', 'password challenge missing no-store');
    assert(challengeResponse.headers()['referrer-policy'] === 'no-referrer', 'password challenge missing no-referrer');
    assert(challengeResponse.headers()['x-robots-tag'] === 'noindex, nofollow', 'password challenge missing noindex');
    const challengeCsp = challengeResponse.headers()['content-security-policy'] ?? '';
    assert(challengeCsp.includes("default-src 'none'") && challengeCsp.includes("form-action 'self' http: https:"), `password challenge CSP invalid: ${challengeCsp}`);

    const challengeCapture = 'gjv10__redirect-access__p05-password-challenge__protected__light__en__desktop.png';
    await page.screenshot({ path: `${capturesDir}/${challengeCapture}`, fullPage: false });

    await page.getByRole('textbox', { name: 'Password', exact: true }).fill(PASSWORD_ONE);
    const wrongResponsePromise = page.waitForResponse((response) =>
      response.request().method() === 'POST' && new URL(response.url()).pathname === `/${CODE}`);
    await page.getByRole('button', { name: 'Continue' }).click();
    const wrongResponse = await wrongResponsePromise;
    assert(wrongResponse.status() === 401, `old password returned ${wrongResponse.status()} instead of 401`);
    await page.getByRole('alert').getByText(/not accepted/i).waitFor();
    const wrongBody = await page.textContent('body');
    assert(!wrongBody.includes(DESTINATION), 'wrong-password response leaked destination');
    assert(!wrongResponse.headers().location, 'wrong-password response exposed Location header');

    await page.getByRole('textbox', { name: 'Password', exact: true }).fill(PASSWORD_TWO);
    const destinationRequestPromise = page.waitForRequest((request) =>
      request.isNavigationRequest() && request.url() === DESTINATION);
    await page.getByRole('button', { name: 'Continue' }).click();
    const destinationRequest = await destinationRequestPromise;
    await page.waitForURL((url) => url.origin === new URL(WORKSPACE_URL).origin && url.pathname === '/app/links');
    const passwordPostRequest = destinationRequest.redirectedFrom();
    assert(passwordPostRequest, 'successful destination navigation has no redirect source request');
    assert(passwordPostRequest.method() === 'POST', `password redirect source method was ${passwordPostRequest.method()}`);
    assert(new URL(passwordPostRequest.url()).pathname === `/${CODE}`, `password redirect source URL was ${passwordPostRequest.url()}`);
    const correctResponse = await passwordPostRequest.response();
    assert(correctResponse, 'password POST redirect response is unavailable from redirect chain');
    assert(correctResponse.status() === 302, `replacement password returned ${correctResponse.status()} instead of 302`);
    assert(correctResponse.headers().location === DESTINATION, `password redirect Location mismatch: ${correctResponse.headers().location}`);

    await page.goto(`${WORKSPACE_URL}/app/links/${linkId}`, { waitUntil: 'networkidle' });
    await page.getByRole('tab', { name: 'Access' }).click();
    await page.getByText(/Password protection is currently\s+enabled/i).waitFor();
    await page.getByLabel('Remove password protection').check();
    await page.locator('#detail-reason').fill('Clear browser password');
    const clearResponsePromise = page.waitForResponse((response) =>
      response.request().method() === 'PATCH' && new URL(response.url()).pathname.endsWith(`/links/${linkId}`));
    await page.getByRole('button', { name: 'Save changes' }).click();
    const clearResponse = await clearResponsePromise;
    assert(clearResponse.status() === 200, `Workspace password clear returned ${clearResponse.status()}`);
    await page.getByText(/Password protection is currently\s+disabled/i).waitFor();

    const cleared = await fetchLink(linkId);
    assert(cleared.version === 3, `password clear expected version 3, got ${cleared.version}`);
    assert(cleared.access?.password_protected === false, 'password clear did not disable protection');
    assert(cleared.risk_fingerprint === link.risk_fingerprint, 'password-only mutations changed destination fingerprint');

    const noChallengeResponse = await page.goto(`${REDIRECT_URL}/${CODE}`, { waitUntil: 'domcontentloaded' });
    assert(noChallengeResponse, 'no navigation response after password clear');
    // Playwright returns the final document response after following the redirect;
    // final URL proves the public GET no longer rendered the password challenge.
    await page.waitForURL((url) => url.origin === new URL(WORKSPACE_URL).origin && url.pathname === '/app/links');

    assert(expectedConsoleErrors.length === 1, `expected exactly one wrong-password 401 console diagnostic: ${JSON.stringify(expectedConsoleErrors)}`);
    assert(unexpectedConsoleErrors.length === 0, `password browser console errors: ${JSON.stringify(unexpectedConsoleErrors)}`);
    assert(pageErrors.length === 0, `password browser page errors: ${JSON.stringify(pageErrors)}`);
    assert(requestFailures.length === 0, `password browser request failures: ${JSON.stringify(requestFailures)}`);
    assert(expectedHttpErrors.length === 0, `password browser unexpected HTTP errors: ${JSON.stringify(expectedHttpErrors)}`);

    const t017Details = {
      link_id: linkId,
      create_status: createResponse.status(),
      replace_status: replaceResponse.status(),
      clear_status: clearResponse.status(),
      versions_verified: [1, 2, 3],
      password_plaintext_echoed: false,
      password_protected_after_create: true,
      password_protected_after_replace: true,
      password_protected_after_clear: false,
      password_mutations_preserved_fingerprint: true,
      capture: `artifacts/v10/P05/captures/${workspaceCapture}`,
    };
    const t019Details = {
      link_id: linkId,
      challenge_status: challengeResponse.status(),
      old_password_status: wrongResponse.status(),
      correct_password_status: correctResponse.status(),
      destination_absent_from_challenge: true,
      destination_absent_from_wrong_password: true,
      bypass_links: 0,
      no_store: true,
      no_referrer: true,
      noindex: true,
      csp_form_action_self: true,
      csp_form_action_http_https: true,
      expected_wrong_password_console_401_observed: expectedConsoleErrors.length === 1,
      correct_password_location: correctResponse.headers().location,
      clear_removed_public_challenge: true,
      capture: `artifacts/v10/P05/captures/${challengeCapture}`,
    };

    augmentCase('P05-T017', 'password_workspace_flow', t017Details);
    augmentCase('P05-T019', 'password_public_gate', t019Details);
    console.log('P05 password browser evidence: PASS');
  } catch (error) {
    const message = `${error?.name ?? 'Error'}: ${error?.message ?? String(error)}`;
    for (const [caseId, key] of [['P05-T017', 'password_workspace_flow'], ['P05-T019', 'password_public_gate']]) {
      try { augmentCase(caseId, key, {}, message); } catch { /* baseline evidence may itself be missing */ }
    }
    console.error(`P05 password browser evidence: FAIL\n  - ${message}`);
    process.exitCode = 1;
  } finally {
    await context.close();
    await browser.close();
  }
}

await run();
