// Real Chromium -> native TLS/Nginx/FPM; prepared-plan fixture, no privileged apply claim.
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import {createHash} from 'node:crypto';
import path from 'node:path';
import assert from 'node:assert/strict';
const cfg = JSON.parse(fs.readFileSync(0, 'utf8'));
const output = path.resolve('artifacts/v10/P21/native-web');
fs.mkdirSync(output, {recursive: true});
const browser = await chromium.launch({headless: true, args: ['--no-sandbox']});
try {
  for (const [label, viewport] of [['desktop', {width: 1280, height: 900}], ['mobile', {width: 390, height: 844}]]) {
    // Exercise actual HTTP adoption with a root-owned fixture record, never a real plan approval claim.
    if (fs.existsSync(cfg.completion)) fs.unlinkSync(cfg.completion);
    const draftFile = path.join(cfg.state, 'configuration-draft.json');
    const rootApproval = path.join(path.dirname(cfg.completion), 'configuration-approval.json');
    const localApproval = path.join(cfg.state, 'apply-approval.json');
    const applyRequest = path.join(cfg.state, 'apply-request.json');
    for (const file of [rootApproval, localApproval, applyRequest]) if (fs.existsSync(file)) fs.unlinkSync(file);
    const pendingFile = path.join(cfg.state, 'state.json');
    const pending = JSON.parse(fs.readFileSync(pendingFile));
    Object.assign(pending, {step: 0, status: 'ready', session: null, csrf: null, expires: 0, attempts: 0, retry_at: 0});
    fs.writeFileSync(pendingFile, JSON.stringify(pending));
    const adoptionContext = await browser.newContext({viewport, ignoreHTTPSErrors: true});
    const adoptionPage = await adoptionContext.newPage();
    const adoptionErrors = [];
    adoptionPage.on('pageerror', error => adoptionErrors.push(error.message));
    let acknowledgements = 0, prematureApply = 0, configurationPosts = 0;
    adoptionPage.on('request', req => {
      if (req.method() !== 'POST') return;
      if (req.url().endsWith('/install/configuration')) configurationPosts++;
      if (req.url().endsWith('/install/configuration/approve')) acknowledgements++;
      if (req.url().endsWith('/install/services')) prematureApply++;
    });
    await adoptionPage.goto(cfg.origin+'/install/');
    async function authenticateApproval(control = '#installer-approve') {
      await adoptionPage.getByLabel('Installation access token', {exact: true}).fill(cfg.token);
      await adoptionPage.getByRole('button', {name: 'Authenticate', exact: true}).click();
      await adoptionPage.waitForFunction(selector => !document.querySelector(selector).disabled, control);
    }
    await authenticateApproval('#installer-save-configuration');
    const values = {'origin': 'https://installer.example.test', 'database-name': 'gojet_browser', 'database-user': 'gojet_browser',
      'database-password': 'private-database-fixture', 'smtp-host': 'smtp.example.test',
      'smtp-from': 'mail@example.test', 'risk-name': 'browser-fixture',
      'risk-endpoint': 'https://risk.example.test/check', 'risk-policy_version': 'v1',
      'turnstile_secret': 'private-browser-fixture', 'admin-email': 'admin@example.test',
      'admin-display_name': 'Administrator', 'admin-password': 'private-browser-password'};
    for (const [field, value] of Object.entries(values)) await adoptionPage.locator('#cfg-'+field).fill(value);
    await adoptionPage.locator('#cfg-origin').fill(cfg.origin);
    await adoptionPage.locator('#installer-save-configuration').click();
    assert.match(await adoptionPage.locator('#installer-message').textContent(), /Use the final DNS hostname/);
    assert.equal(configurationPosts, 0);
    await adoptionPage.locator('#cfg-origin').fill(values.origin);
    await adoptionPage.locator('#cfg-file-types').fill('pdf=application/pdf\npdf=application/pdf');
    await adoptionPage.locator('#installer-save-configuration').click();
    // Validation is synchronous. Report field identifiers, never credential values.
    const rejectedFields = await adoptionPage.locator('#installer-configuration input:invalid, #installer-configuration textarea:invalid').evaluateAll(nodes => nodes.map(node => node.id));
    assert.deepEqual(rejectedFields, ['cfg-file-types'], 'Unexpected fields blocked configuration validation');
    assert.match(await adoptionPage.locator('#installer-message').textContent(), /^Use a unique extension=/);
    assert.equal(configurationPosts, 0);
    assert.equal(fs.existsSync(draftFile), false);
    await adoptionPage.locator('#cfg-file-types').fill('pdf=application/pdf');
    assert.equal(await adoptionPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await adoptionPage.screenshot({path: path.join(output, label+'-configuration-form.png'), fullPage: true});
    await adoptionPage.locator('#installer-save-configuration').evaluate(el => { el.click(); el.click(); });
    await adoptionPage.waitForFunction(() => document.querySelector('#installer-title').textContent === 'Configuration submitted');
    assert.equal(configurationPosts, 1);
    const savedDraft = JSON.parse(fs.readFileSync(draftFile));
    assert.equal(savedDraft.configuration.database.user, values['database-user']);
    assert.equal(savedDraft.admin.password, values['admin-password']);
    assert.deepEqual(savedDraft.configuration.files.types, {pdf: ['application/pdf']});
    assert.equal(Object.hasOwn(savedDraft.configuration, 'secrets'), false);
    assert.equal(Object.hasOwn(savedDraft.configuration.files, 'root'), false);
    assert.equal(await adoptionPage.locator('#cfg-admin-password').inputValue(), '');
    assert.equal(await adoptionPage.locator('#installer-configuration').isVisible(), false);
    await authenticateApproval();
    assert.equal(await adoptionPage.locator('#installer-title').textContent(), 'Configuration submitted');
    assert.equal(await adoptionPage.locator('#installer-apply').isVisible(), false);
    const frozenBefore = fs.readFileSync(draftFile);
    await adoptionPage.locator('#installer-approve').click();
    await adoptionPage.waitForFunction(() => document.querySelector('#installer-message').textContent.startsWith('Approval could not be confirmed.'));
    assert.equal(await adoptionPage.locator('#installer-approve').isDisabled(), true);
    assert.equal(fs.existsSync(localApproval), false);
    assert.equal(fs.existsSync(applyRequest), false);
    fs.writeFileSync(rootApproval, JSON.stringify({schema: 1,
      state_sha256: createHash('sha256').update(cfg.state).digest('hex'),
      draft_sha256: createHash('sha256').update(frozenBefore).digest('hex'), seal_sha256: 'a'.repeat(64)}), {mode: 0o640});
    fs.chownSync(rootApproval, 0, cfg.gid);
    await authenticateApproval();
    await adoptionPage.screenshot({path: path.join(output, label+'-configuration-pending.png'), fullPage: true});
    await adoptionPage.locator('#installer-approve').evaluate(el => { el.click(); el.click(); });
    await adoptionPage.waitForFunction(() => document.querySelector('#installer-title').textContent === 'Approved plan ready');
    await adoptionPage.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
    assert.equal(acknowledgements, 2); // One rejected attempt and one successful acknowledgement.
    assert.equal(prematureApply, 0);
    assert.equal(fs.existsSync(applyRequest), false);
    assert.equal(JSON.parse(fs.readFileSync(pendingFile)).step, 4);
    assert.equal(fs.statSync(localApproval).uid, cfg.uid);
    assert.deepEqual(fs.readFileSync(draftFile), frozenBefore);
    assert.equal(await adoptionPage.locator('#installer-approve').isVisible(), false);
    assert.equal(await adoptionPage.locator('#installer-apply').isEnabled(), true);
    assert.equal(await adoptionPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    assert.equal(await adoptionPage.evaluate(() => localStorage.length + sessionStorage.length), 0);
    assert.equal((await adoptionPage.content()).includes('private-browser-password'), false);
    await adoptionPage.screenshot({path: path.join(output, label+'-approval-confirmed.png'), fullPage: true});
    assert.deepEqual(adoptionErrors, []);
    await adoptionContext.close();
    fs.unlinkSync(rootApproval); fs.unlinkSync(draftFile);
    // Explicit test fixture only. Root approval/seal and actual execution are covered by payload CI.
    if (fs.existsSync(cfg.completion)) fs.unlinkSync(cfg.completion);
    const stateFile = path.join(cfg.state, 'state.json');
    const state = JSON.parse(fs.readFileSync(stateFile));
    Object.assign(state, {step: 4, status: 'ready', session: null, csrf: null, expires: 0, attempts: 0, retry_at: 0});
    fs.writeFileSync(stateFile, JSON.stringify(state));
    const approval = path.join(cfg.state, 'apply-approval.json');
    fs.writeFileSync(approval, JSON.stringify({schema: 'gojet.web-apply.v1', seal_sha256: 'a'.repeat(64)}), {mode: 0o600});
    fs.chownSync(approval, cfg.uid, cfg.gid);
    const request = path.join(cfg.state, 'apply-request.json');
    if (fs.existsSync(request)) fs.unlinkSync(request);
    const snapshot = path.join(path.dirname(cfg.completion), 'preflight.json');
    if (fs.existsSync(snapshot)) fs.unlinkSync(snapshot);
    const progress = path.join(path.dirname(cfg.completion), 'apply-progress.json');
    if (fs.existsSync(progress)) fs.unlinkSync(progress);
    function executionFixture(phase, status = 'running', age = 0) {
      fs.writeFileSync(progress, JSON.stringify({schema: 1,
        state_sha256: createHash('sha256').update(cfg.state).digest('hex'),
        updated_at: Math.floor(Date.now() / 1000) - age, phase, status}), {mode: 0o644});
    }
    const checks = {package: true, systemd: true, nginx: true, php: true, mysql: true, redis: true, storage: true, clamav: true};
    function publishFixture(changes = {}, age = 0) {
      // Root-owned snapshot fixture: browser rendering/HTTP read only, not a native probe claim.
      fs.writeFileSync(snapshot, JSON.stringify({schema: 1,
        state_sha256: createHash('sha256').update(cfg.state).digest('hex'),
        checked_at: Math.floor(Date.now() / 1000) - age, checks: {...checks, ...changes}}), {mode: 0o644});
    }
    // Fixture certificate trust is verified by the native Python TLS tests; browser uses a self-signed fixture.
    const context = await browser.newContext({viewport, ignoreHTTPSErrors: true});
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    page.on('console', msg => { if (msg.type() === 'error' && !msg.text().startsWith('Failed to load resource:')) errors.push(msg.text()); });
    let submissions = 0;
    page.on('request', req => { if (req.method() === 'POST' && req.url().endsWith('/install/services')) submissions++; });
    const first = await page.goto(cfg.origin+'/install/');
    assert.equal(first.status(), 401);
    assert.match(first.headers()['content-security-policy'], /script-src 'sha256-/);
    assert.equal(await page.locator('#installer-login').getAttribute('method'), 'post');
    assert.equal(await page.locator('#installer-login').getAttribute('action'), '/install/');
    await page.locator('#bootstrap-token').waitFor();
    await page.keyboard.press('Tab');
    assert.equal(await page.locator('#bootstrap-token').evaluate(el => el === document.activeElement), true);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await page.screenshot({path: path.join(output, label+'-login.png'), fullPage: true});
    await page.getByLabel('Installation access token', {exact: true}).fill('wrong-token-with-at-least-thirty-two-characters');
    await page.getByRole('button', {name: 'Authenticate', exact: true}).click();
    await page.waitForFunction(() => document.querySelector('#installer-message').textContent.startsWith('Authentication required.'));
    assert.equal(fs.existsSync(request), false);
    await page.getByLabel('Installation access token', {exact: true}).fill(cfg.token);
    await page.getByRole('button', {name: 'Authenticate', exact: true}).click();
    await page.getByRole('button', {name: 'Submit approved installation', exact: true}).waitFor();
    assert.equal(await page.locator('#bootstrap-token').inputValue(), '');
    assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    await page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
    assert.match(await page.locator('#preflight-summary').textContent(), /^Checks unavailable/);
    const durableBefore = fs.readFileSync(stateFile, 'utf8');
    async function refreshChecks() {
      await page.getByRole('button', {name: 'Refresh status', exact: true}).click();
      await page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
    }
    publishFixture(); await refreshChecks();
    assert.match(await page.locator('#preflight-summary').textContent(), /^All dependency checks passed/);
    assert.equal(await page.locator('#preflight-checks dd').count(), 8);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await page.screenshot({path: path.join(output, label+'-preflight-ready.png'), fullPage: true});
    publishFixture({redis: false}); await refreshChecks();
    assert.match(await page.locator('#preflight-summary').textContent(), /^Some dependency checks failed/);
    assert.equal(await page.locator('#preflight-checks dd', {hasText: 'Failed'}).count(), 1);
    await page.screenshot({path: path.join(output, label+'-preflight-failed.png'), fullPage: true});
    publishFixture({}, 121); await refreshChecks();
    assert.match(await page.locator('#preflight-summary').textContent(), /^Checks expired/);
    assert.equal(await page.locator('#preflight-checks').isVisible(), false);
    // A page left open must discard green without waiting for another HTTP request.
    publishFixture({}, 118); await refreshChecks();
    await page.waitForFunction(() => document.querySelector('#preflight-summary').textContent.startsWith('Checks expired.'), null, {timeout: 10000});
    assert.equal(await page.locator('#preflight-checks').isVisible(), false);
    assert.equal(fs.readFileSync(stateFile, 'utf8'), durableBefore);
    assert.equal(fs.existsSync(request), false);
    publishFixture(); await refreshChecks();
    // Real click sends one request; immediate repeated activation cannot duplicate it.
    await page.getByRole('button', {name: 'Submit approved installation', exact: true}).click();
    await page.waitForFunction(() => document.querySelector('#installer-message').textContent.startsWith('Installation request submitted.'));
    assert.equal(submissions, 1);
    assert.equal(JSON.parse(fs.readFileSync(request)).seal_sha256, 'a'.repeat(64));
    await page.getByRole('button', {name: 'Refresh status', exact: true}).click();
    await page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
    assert.equal(submissions, 1);
    await page.screenshot({path: path.join(output, label+'-queued.png'), fullPage: true});
    for (const [phase, heading] of [['starting', 'Starting installation'], ['data', 'Initializing data'],
      ['services', 'Starting services'], ['site', 'Publishing the site'], ['health', 'Checking installation health']]) {
      executionFixture(phase); await refreshChecks();
      assert.equal(await page.locator('#installer-title').textContent(), heading);
      assert.equal(await page.locator('#installer-apply').isVisible(), false);
    }
    await page.screenshot({path: path.join(output, label+'-execution-running.png'), fullPage: true});
    executionFixture('services', 'failed'); await refreshChecks();
    assert.equal(await page.locator('#installer-title').textContent(), 'Installation needs attention');
    assert.match(await page.locator('#installer-message').textContent(), /Do not submit again/);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await page.screenshot({path: path.join(output, label+'-execution-failed.png'), fullPage: true});
    executionFixture('data', 'running', 121); await refreshChecks();
    assert.equal(await page.locator('#installer-title').textContent(), 'Execution status unconfirmed');
    // Preflight expiry must not cancel the separate execution expiry timer.
    publishFixture({}, 119); executionFixture('data', 'running', 117); await refreshChecks();
    await page.waitForFunction(() => document.querySelector('#installer-title').textContent === 'Execution status unconfirmed', null, {timeout: 10000});
    assert.match(await page.locator('#preflight-summary').textContent(), /^Checks expired/);
    assert.equal(submissions, 1);
    assert.equal(await page.locator('#installer-apply').isVisible(), false);
    await context.setOffline(true);
    await page.getByRole('button', {name: 'Refresh status', exact: true}).click();
    await page.waitForFunction(() => document.querySelector('#installer-message').textContent.startsWith('The server could not be reached.'));
    assert.equal(await page.locator('#installer-preflight').isVisible(), false);
    await context.setOffline(false);
    fs.writeFileSync(cfg.completion, '', {mode: 0o600});
    await page.getByRole('button', {name: 'Refresh status', exact: true}).click();
    await page.waitForFunction(() => document.querySelector('#installer-message').textContent.startsWith('The installer is closed or unavailable.'));
    assert.equal(await page.locator('#installer-apply').isVisible(), false);
    assert.equal(submissions, 1);
    assert.deepEqual(errors, []);
    await context.close();
    // With scripts disabled, the browser must never put the token into a GET URL.
    if (label === 'desktop') {
      fs.unlinkSync(cfg.completion);
      const noJS = await browser.newContext({javaScriptEnabled: false, ignoreHTTPSErrors: true});
      const fallback = await noJS.newPage();
      await fallback.goto(cfg.origin+'/install/');
      await fallback.getByLabel('Installation access token', {exact: true}).fill('non-secret-no-js-fixture-token-0123456789');
      const beforeState = fs.readFileSync(stateFile, 'utf8');
      const beforeRequest = fs.readFileSync(request, 'utf8');
      const response = fallback.waitForResponse(r => r.request().method() === 'POST');
      await fallback.getByRole('button', {name: 'Authenticate', exact: true}).click();
      const rejected = await response;
      const posted = rejected.request();
      // Native form POST under no-referrer sends an opaque Origin. The exact
      // origin guard rejects it before the JSON content-type guard is reached.
      assert.equal(await posted.headerValue('origin'), 'null');
      assert.equal(rejected.status(), 403);
      assert.deepEqual(await rejected.json(), {error: 'Invalid installer origin'});
      assert.equal(posted.url(), cfg.origin+'/install/');
      assert.equal(new URLSearchParams(posted.postData()).get('bootstrap_token'), 'non-secret-no-js-fixture-token-0123456789');
      assert.equal(new URL(fallback.url()).search, '');
      assert.equal(fs.readFileSync(stateFile, 'utf8'), beforeState);
      assert.equal(fs.readFileSync(request, 'utf8'), beforeRequest);
      await noJS.close();
    }
  }
} finally { await browser.close(); }
console.log(JSON.stringify({desktop_mobile_browser: true, real_tls_fpm_browser_requests: true,
  browser_configuration_submission: true, client_validation_no_post: true,
  browser_approval_acknowledgement: true, approval_record_is_fixture: true, approval_does_not_submit_apply: true,
  keyboard_access: true, no_horizontal_overflow: true, authentication_and_apply_submission: true,
  no_secret_browser_storage: true, no_script_post_only_verified: true, offline_recovery: true, post_lock_closed: true,
  snapshot_ready_failed_stale_unavailable: true, snapshot_expiry_clears_checks: true, snapshot_is_fixture: true,
  execution_phases_failed_stale: true, independent_execution_expiry: true, execution_is_fixture: true,
  unexpected_console_errors: 0, approved_plan_is_fixture: true, privileged_apply_verified: false}));
