// T024 protected HTTP observer. Root fixture control uses inherited pipes only.
import { chromium } from 'playwright-core';
import { createInterface } from 'node:readline';
import assert from 'node:assert/strict';
import { executablePath, viewports, capturesDir, STORAGE_ROOT, REAL_CLAMD } from './browser_env.mjs';
import { diagnostics, attachDiagnostics, assertDiagnostics, layoutEvidence, assertLayout } from './browser_ui.mjs';

const input = createInterface({ input: process.stdin, crlfDelay: Infinity })[Symbol.asyncIterator]();
async function read() {
  const line = await input.next();
  assert.equal(line.done, false, 'Root fixture control closed');
  return JSON.parse(line.value);
}
const cfg = await read();
async function arrange(op) {
  process.stdout.write(JSON.stringify({op})+'\n');
  return read();
}
const routes = ['/install/environment', '/install/services', '/install/health'];
const browser = await chromium.launch({executablePath, headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage']});
const evidence = {};
try {
  for (const name of ['desktop', 'mobile']) {
    await arrange('healthy');
    // Python independently verifies the generated certificate and both TLS origins.
    const context = await browser.newContext({viewport: viewports[name], ignoreHTTPSErrors: true});
    const page = await context.newPage();
    const report = diagnostics();
    attachDiagnostics(page, report);
    let applyPosts = 0;
    page.on('request', request => {
      if (request.method() === 'POST' && request.url().endsWith('/install/services')) applyPosts++;
    });
    async function get(origin, route, expected, error) {
      const response = await context.request.get(origin+route, {headers: {Accept: 'application/json'}});
      assert.equal(response.status(), expected, `Protected route ${route}`);
      const body = await response.json();
      if (error) assert.deepEqual(body, {error});
      return body;
    }
    async function authenticate(fixture) {
      const response = await page.goto(fixture.origin+'/install/');
      assert.equal(response.status(), 401);
      await page.getByLabel('Installation access token', {exact: true}).fill(fixture.token);
      await page.getByRole('button', {name: 'Authenticate', exact: true}).click();
      await page.waitForFunction(() => document.querySelector('#bootstrap-token').value === ''
        && !document.querySelector('#installer-refresh').disabled);
      const cookie = (await context.cookies(fixture.origin+'/install/')).find(item => item.name === '__Secure-gojet_install');
      assert.ok(cookie?.secure && cookie.httpOnly && cookie.sameSite === 'Strict' && cookie.path === '/install');
      assert.equal(await page.evaluate(() => localStorage.length+sessionStorage.length), 0);
    }
    for (const fixture of Object.values(cfg)) {
      for (const route of routes) await get(fixture.origin, route, 401, 'Installer authentication required');
    }
    await authenticate(cfg.environment);
    assert.deepEqual(await get(cfg.environment.origin, '/install/environment', 200), {step: 'environment', status: 'ready'});
    for (const route of ['/install/services', '/install/health']) {
      await get(cfg.environment.origin, route, 409, 'Installer step unavailable');
    }
    assert.equal((await page.goto(cfg.environment.origin+'/install/environment')).status(), 200);
    assert.equal(await page.locator('body').getAttribute('data-shell'), 'installer');
    // Cookies are host scoped, not port scoped. Clear the other fixture's session.
    await context.clearCookies();
    await authenticate(cfg.services);
    for (const route of ['/install/environment', '/install/health']) {
      await get(cfg.services.origin, route, 409, 'Installer step unavailable');
    }
    for (const route of ['/install/services', '/install/health']) {
      const response = await context.request.get(cfg.services.origin+route, {headers: {Accept: 'text/html'}});
      assert.equal(response.status(), 409, 'Direct navigation must not manufacture a passed step');
      assert.deepEqual(await response.json(), {error: 'Installer step unavailable'});
    }
    const before = await get(cfg.services.origin, '/install/', 200);
    assert.deepEqual(before, {step: 'services', status: 'ready'});
    async function observe(mode, storage, clamav, expectedStatus = 'failed') {
      const published = await arrange(mode);
      await page.getByRole('button', {name: 'Refresh status', exact: true}).click();
      await page.waitForFunction(() => !document.querySelector('#installer-refresh').disabled);
      const result = await get(cfg.services.origin, '/install/services', 200);
      assert.equal(result.preflight.status, expectedStatus);
      assert.equal(result.preflight.ready, false);
      assert.equal(result.preflight.installation_complete, false);
      assert.equal(result.execution, null);
      if (published) assert.deepEqual(result.preflight, published);
      if (expectedStatus === 'failed') {
        assert.equal(result.preflight.checks.storage, storage);
        assert.equal(result.preflight.checks.clamav, clamav);
        for (const key of ['package', 'systemd', 'nginx', 'php', 'mysql', 'redis']) assert.equal(result.preflight.checks[key], false);
        assert.match(await page.locator('#preflight-summary').innerText(), /^Some dependency checks failed/);
        for (const [label, passed] of [['File storage', storage], ['ClamAV', clamav]]) {
          const row = page.locator('#preflight-checks > div').filter({has: page.getByText(label, {exact: true})});
          assert.equal(await row.locator('dd').innerText(), passed ? 'Passed' : 'Failed');
        }
      } else {
        assert.equal(result.preflight.checks, null);
        assert.equal(await page.locator('#preflight-checks').isVisible(), false);
        assert.match(await page.locator('#preflight-summary').innerText(), expectedStatus === 'stale' ? /^Checks expired/ : /^Checks unavailable/);
      }
      const text = await page.locator('body').innerText();
      for (const privateValue of [STORAGE_ROOT, REAL_CLAMD, cfg.services.token, 'GOJET_MYSQL_DSN']) {
        assert.equal(text.includes(privateValue), false, 'Installer disclosed private configuration');
      }
      assertLayout(await layoutEvidence(page), `T024 ${name} ${mode}`);
      return {status: result.preflight.status, storage, clamav};
    }
    const healthy = await observe('healthy', true, true);
    await page.screenshot({path: `${capturesDir}/P09-T024-${name}-installer-files-healthy.png`, fullPage: true});
    const scannerDown = await observe('down', true, false);
    await page.screenshot({path: `${capturesDir}/P09-T024-${name}-installer-scanner-failed.png`, fullPage: true});
    const unsafeStorage = await observe('unsafe', false, true);
    await observe('stale', null, null, 'stale');
    await observe('missing', null, null, 'unavailable');
    await observe('healthy', true, true);
    assert.deepEqual(await get(cfg.services.origin, '/install/', 200), before);
    assert.equal(applyPosts, 0);
    assertDiagnostics(report, `T024 ${name} protected Installer`, {allowedHttp: [
      {includes: cfg.environment.origin+'/install/', status: 401},
      {includes: cfg.services.origin+'/install/', status: 401},
    ]});
    evidence[name] = {unauthenticated_routes: 401, environment_route: 200, premature_services_health: 409,
      services_status: 200, out_of_phase_environment_health: 409, healthy, scanner_down: scannerDown,
      unsafe_storage: unsafeStorage, stale_hidden: true, missing_hidden: true, recovered: true,
      apply_posts: applyPosts, layout_verified: true, unexpected_browser_errors: 0};
    await context.close();
  }
  await arrange('complete');
  const closed = await browser.newContext({ignoreHTTPSErrors: true});
  for (const fixture of Object.values(cfg)) {
    for (const route of ['/install/', ...routes]) {
      const response = await closed.request.get(fixture.origin+route);
      assert.equal(response.status(), 404);
      assert.deepEqual(await response.json(), {error: 'Not found'});
    }
  }
  await closed.close();
  process.stdout.write(JSON.stringify({op: 'done', evidence: {viewports: evidence,
    root_completion_guard: true, completion_marker_is_fixture: true, full_installation_verified: false}})+'\n');
} finally {
  await browser.close();
  process.stdin.destroy();
}
