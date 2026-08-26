import {
  SITE_BAD_TURNSTILE_URL, SITE_URL, assert, assertCleanDiagnostics, assertNoHorizontalOverflow,
  attachDiagnostics, delayedAPIRoute, diagnostics, expectState, mysqlScalar, screenshot,
  seedPublicFixture, sqlLiteral, tabToAccessibleName, viewports,
} from './browser_common.mjs';

const CASE = 'P16-T027';
const safetyReasons = ['pending', 'review', 'blocked', 'domain-suspended', 'domain-revoked', 'domain-expired', 'operational-unavailable'];

async function assertPrivatePublicHTML(page, response, label) {
  const headers = response ? await response.allHeaders() : {};
  assert((headers['cache-control'] ?? '').toLowerCase().includes('no-store'), `${label} missing no-store`);
  assert((headers['x-robots-tag'] ?? '').toLowerCase().includes('noindex'), `${label} missing noindex header`);
  assert((headers['referrer-policy'] ?? '').toLowerCase() === 'no-referrer', `${label} missing no-referrer`);
  const robots = await page.locator('meta[name="robots"]').getAttribute('content');
  assert((robots ?? '').toLowerCase().includes('noindex'), `${label} meta robots missing noindex`);
  const referrer = await page.locator('meta[name="referrer"]').getAttribute('content');
  assert((referrer ?? '').toLowerCase() === 'no-referrer', `${label} meta referrer mismatch`);
}

async function fillReport(page, fixture, suffix) {
  await page.getByLabel('Resource type').selectOption('short-link-risk');
  await page.getByLabel('Hostname').fill(fixture.hostname);
  await page.getByLabel('Short-link code').fill(fixture.code);
  await page.getByLabel('Category').selectOption('phishing');
  await page.getByLabel(/Details/).fill(`P16 browser abuse report ${suffix}. No private evidence is required.`);
}

export async function run(browser) {
  const fixture = seedPublicFixture();
  const captures = [];
  const states = { linkunavailable: [], abuse_report: [] };
  const responsive = {};

  const site = await browser.newContext({ viewport: viewports.desktop });
  const safetyPage = await site.newPage();
  const safetyDiag = diagnostics();
  attachDiagnostics(safetyPage, safetyDiag);

  let response = await safetyPage.goto(`${SITE_URL}/linkunavailable?reason=pending&code=${encodeURIComponent(fixture.code)}`, { waitUntil: 'networkidle' });
  states.linkunavailable.push(await expectState(safetyPage, 'linkunavailable', 'pending'));
  await assertPrivatePublicHTML(safetyPage, response, 'linkunavailable');
  captures.push(await screenshot(safetyPage, CASE, 'link-pending'));
  const safeReference = await safetyPage.getByText(fixture.code).count();
  assert(safeReference >= 1, 'Allowlisted safe code reference was not rendered');
  const focusReport = await tabToAccessibleName(safetyPage, 'Report abuse');
  assert(focusReport.tag === 'A', 'Report abuse must be keyboard reachable as a link');

  for (const reason of safetyReasons.slice(1)) {
    await safetyPage.goto(`${SITE_URL}/linkunavailable?reason=${reason}&code=${encodeURIComponent(fixture.code)}`, { waitUntil: 'networkidle' });
    states.linkunavailable.push(await expectState(safetyPage, 'linkunavailable', reason));
    captures.push(await screenshot(safetyPage, CASE, `link-${reason}`));
  }

  await safetyPage.goto(`${SITE_URL}/linkunavailable?reason=not-authorized&code=%3Cscript%3Eunsafe%3C%2Fscript%3E`, { waitUntil: 'networkidle' });
  states.linkunavailable.push(await expectState(safetyPage, 'linkunavailable', 'operational-unavailable'));
  const safetyText = await safetyPage.locator('body').innerText();
  assert(!safetyText.includes('customer.example/p16-public-sensitive-target'), 'Safety page leaked destination target');
  assert(!safetyText.includes('<script>unsafe</script>'), 'Safety page echoed an unsafe code query');
  assert(await safetyPage.getByRole('link', { name: /continue anyway/i }).count() === 0, 'Safety page exposes continue-anyway link');
  assert(await safetyPage.getByRole('button', { name: /continue anyway/i }).count() === 0, 'Safety page exposes continue-anyway button');
  captures.push(await screenshot(safetyPage, CASE, 'link-unknown-fails-closed'));

  const reportPage = await site.newPage();
  const reportDiag = diagnostics();
  attachDiagnostics(reportPage, reportDiag, { allowStatuses: [400, 404, 409, 429, 503] });
  response = await reportPage.goto(`${SITE_URL}/abuse/report`, { waitUntil: 'networkidle' });
  states.abuse_report.push(await expectState(reportPage, 'abuse-report', 'input'));
  await assertPrivatePublicHTML(reportPage, response, 'abuse report');
  captures.push(await screenshot(reportPage, CASE, 'abuse-input'));
  const submitFocus = await tabToAccessibleName(reportPage, 'Submit report');
  assert(submitFocus.tag === 'BUTTON', 'Submit report must be keyboard reachable');

  await reportPage.getByRole('button', { name: 'Submit report' }).click();
  states.abuse_report.push(await expectState(reportPage, 'abuse-report', 'validation-error'));
  captures.push(await screenshot(reportPage, CASE, 'abuse-validation-error'));
  await fillReport(reportPage, fixture, 'success');

  let postResponse;
  reportPage.on('response', (candidate) => {
    if (candidate.request().method() === 'POST' && candidate.url().endsWith('/api/public/abuse-reports')) postResponse = candidate;
  });
  const delay = await delayedAPIRoute(reportPage, (request) => request.method() === 'POST' && request.url().endsWith('/api/public/abuse-reports'));
  await reportPage.getByRole('button', { name: 'Submit report' }).click();
  await delay.seen;
  states.abuse_report.push(await expectState(reportPage, 'abuse-report', 'submitting'));
  captures.push(await screenshot(reportPage, CASE, 'abuse-submitting'));
  delay.release(); await delay.dispose();
  states.abuse_report.push(await expectState(reportPage, 'abuse-report', 'success-persistent'));
  captures.push(await screenshot(reportPage, CASE, 'abuse-success-persistent'));
  assert(await reportPage.getByText(/Reference: abr_/).count() === 1, 'Persistent abuse receipt is not visible');
  assert(postResponse && postResponse.status() >= 200 && postResponse.status() < 300, `Public abuse API did not succeed: ${postResponse?.status()}`);
  const postHeaders = postResponse ? await postResponse.allHeaders() : {};
  assert((postHeaders['cache-control'] ?? '').toLowerCase().includes('no-store'), 'Abuse POST response missing no-store');
  assert((postHeaders['x-robots-tag'] ?? '').toLowerCase().includes('noindex'), 'Abuse POST response missing noindex');
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_reports WHERE resource_type='short-link-risk' AND hostname_ascii=${sqlLiteral(fixture.hostname)} AND safe_code=${sqlLiteral(fixture.code)}`) === '1', 'Successful public report was not durably stored exactly once');
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_report_events e JOIN abuse_reports r ON r.id=e.report_id WHERE r.hostname_ascii=${sqlLiteral(fixture.hostname)} AND r.safe_code=${sqlLiteral(fixture.code)} AND e.action='abuse.public-intake' AND e.result='success'`) === '1', 'Public abuse intake audit event missing');

  const badContext = await browser.newContext({ viewport: viewports.desktop });
  const badPage = await badContext.newPage();
  const badDiag = diagnostics();
  attachDiagnostics(badPage, badDiag, { allowStatuses: [400, 429] });
  await badPage.goto(`${SITE_BAD_TURNSTILE_URL}/abuse/report`, { waitUntil: 'networkidle' });
  await fillReport(badPage, fixture, 'bad-verification');
  await badPage.getByRole('button', { name: 'Submit report' }).click();
  states.abuse_report.push(await expectState(badPage, 'abuse-report', 'Turnstile-error'));
  captures.push(await screenshot(badPage, CASE, 'abuse-turnstile-error'));
  for (let attempt = 0; attempt < 4; attempt += 1) {
    if ((await badPage.locator('[data-page="abuse-report"]').getAttribute('data-state')) === 'rate-limited') break;
    await badPage.getByRole('button', { name: 'Submit report' }).click();
    await badPage.waitForTimeout(100);
  }
  states.abuse_report.push(await expectState(badPage, 'abuse-report', 'rate-limited'));
  captures.push(await screenshot(badPage, CASE, 'abuse-rate-limited'));
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_reports WHERE resource_type='short-link-risk' AND hostname_ascii=${sqlLiteral(fixture.hostname)} AND safe_code=${sqlLiteral(fixture.code)}`) === '1', 'Failed verification or rate-limit attempt mutated abuse report state');

  for (const [surface, url] of [
    ['linkunavailable', `${SITE_URL}/linkunavailable?reason=blocked&code=${encodeURIComponent(fixture.code)}`],
    ['abuse-report', `${SITE_URL}/abuse/report`],
  ]) {
    responsive[surface] = {};
    for (const [name, viewport] of Object.entries(viewports)) {
      const page = await site.newPage();
      await page.setViewportSize(viewport);
      await page.goto(url, { waitUntil: 'networkidle' });
      responsive[surface][name] = await assertNoHorizontalOverflow(page, `${surface} ${name}`);
      captures.push(await screenshot(page, CASE, `responsive-${surface}-${name}`));
      await page.close();
    }
  }

  assertCleanDiagnostics(safetyDiag, 'linkunavailable');
  assert(reportDiag.page_errors.length === 0 && reportDiag.request_failures.length === 0, `Abuse report diagnostics failed: ${JSON.stringify(reportDiag)}`);
  assert(badDiag.page_errors.length === 0 && badDiag.request_failures.length === 0, `Bad verification diagnostics failed: ${JSON.stringify(badDiag)}`);
  await badContext.close(); await site.close();

  return {
    states, responsive, captures, screenshot_count: captures.length,
    security_checks: {
      link_reason_allowlist: true,
      unsafe_code_not_echoed: true,
      destination_target_not_disclosed: true,
      no_continue_anyway_control: true,
      safety_no_store_noindex: true,
      abuse_no_store_noindex: true,
      server_turnstile_enforced: true,
      rate_limit_enforced: true,
      persistent_success_receipt: true,
      durable_intake_and_audit: true,
    },
  };
}
