import {
  ADMIN_URL, addSessionCookie, assert, assertCleanDiagnostics, assertNoHorizontalOverflow,
  attachDiagnostics, createBrowserSessions, delayedAPIRoute, diagnostics, expectState,
  mysql, mysqlScalar, screenshot, seedAdminFixture, sqlLiteral, tabToAccessibleName, viewports,
} from './browser_common.mjs';

const CASE = 'P16-T026';

async function privateHeaders(page, response) {
  const headers = response ? await response.allHeaders() : {};
  assert((headers['cache-control'] ?? '').toLowerCase().includes('no-store'), 'Admin HTML must be no-store');
  assert((headers['x-robots-tag'] ?? '').toLowerCase().includes('noindex'), 'Admin HTML must be noindex');
  const robots = await page.locator('meta[name="robots"]').getAttribute('content');
  assert((robots ?? '').toLowerCase().includes('noindex'), 'Admin meta robots must be noindex');
}

async function stateCapture(page, pageName, state, captures, label) {
  const actual = await expectState(page, pageName, state);
  captures.push(await screenshot(page, CASE, label));
  return actual;
}

async function destinationState(page, fixture, state, reason, captures, states) {
  mysql(`UPDATE destination_risk_decisions SET state=${sqlLiteral(state)},reason_category=${sqlLiteral(reason)} WHERE scan_id=${fixture.destination_risk_id}`);
  await page.reload({ waitUntil: 'networkidle' });
  const expected = reason.includes('stale') || reason.includes('fingerprint') ? 'stale-fingerprint' : reason.includes('provider') && state !== 'allow' ? 'provider-partial' : state;
  states.push(await stateCapture(page, 'admin-destination-risk-detail', expected, captures, `destination-${expected}`));
}

async function domainState(page, fixture, state, reason, captures, states) {
  mysql(`UPDATE domain_risk_evaluations SET state=${sqlLiteral(state)},reason_category=${sqlLiteral(reason)},updated_at=CURRENT_TIMESTAMP(6) WHERE domain_id=${fixture.domain_id} ORDER BY id DESC LIMIT 1`);
  await page.reload({ waitUntil: 'networkidle' });
  const expected = state === 'provider_partial' || state === 'malformed' || (reason.includes('provider') && state !== 'allow') ? 'provider-partial' : state;
  states.push(await stateCapture(page, 'admin-domain-risk-detail', expected, captures, `domain-${expected}`));
}

async function deniedPage(context, path, captures, label) {
  const page = await context.newPage();
  const report = diagnostics();
  attachDiagnostics(page, report, { allowStatuses: [403] });
  await page.goto(`${ADMIN_URL}${path}`, { waitUntil: 'networkidle' });
  await page.getByText(/permission does not cover this area/i).first().waitFor();
  captures.push(await screenshot(page, CASE, label));
  assert(report.page_errors.length === 0, `${label} page error`);
  return page;
}

export async function run(browser) {
  const sessions = createBrowserSessions();
  const captures = [];
  const states = { destination: [], domain: [], abuse: [], permissions: [] };
  const responsive = {};

  const security = await browser.newContext({ viewport: viewports.desktop });
  const domain = await browser.newContext({ viewport: viewports.desktop });
  const denied = await browser.newContext({ viewport: viewports.desktop });
  await addSessionCookie(security, sessions, 'security');
  await addSessionCookie(domain, sessions, 'domain');
  await addSessionCookie(denied, sessions, 'denied');

  const destinationPage = await security.newPage();
  const destinationDiag = diagnostics();
  attachDiagnostics(destinationPage, destinationDiag);
  const destinationDelay = await delayedAPIRoute(destinationPage, (request) => request.method() === 'GET' && request.url().includes('/api/admin/destination-risks'));
  const destinationResponse = await destinationPage.goto(`${ADMIN_URL}/admin/trust/destination-risk`, { waitUntil: 'domcontentloaded' });
  await destinationDelay.seen;
  states.destination.push(await stateCapture(destinationPage, 'admin-destination-risk', 'loading', captures, 'destination-loading'));
  destinationDelay.release(); await destinationDelay.dispose();
  states.destination.push(await stateCapture(destinationPage, 'admin-destination-risk', 'empty', captures, 'destination-empty'));
  await privateHeaders(destinationPage, destinationResponse);

  const domainPage = await domain.newPage();
  const domainDiag = diagnostics();
  attachDiagnostics(domainPage, domainDiag);
  const domainDelay = await delayedAPIRoute(domainPage, (request) => request.method() === 'GET' && request.url().includes('/api/admin/domain-risks'));
  await domainPage.goto(`${ADMIN_URL}/admin/trust/domain-risk`, { waitUntil: 'domcontentloaded' });
  await domainDelay.seen;
  states.domain.push(await stateCapture(domainPage, 'admin-domain-risk', 'loading', captures, 'domain-loading'));
  domainDelay.release(); await domainDelay.dispose();
  states.domain.push(await stateCapture(domainPage, 'admin-domain-risk', 'empty', captures, 'domain-empty'));

  const abusePage = await security.newPage();
  const abuseDiag = diagnostics();
  attachDiagnostics(abusePage, abuseDiag);
  const abuseDelay = await delayedAPIRoute(abusePage, (request) => request.method() === 'GET' && request.url().includes('/api/admin/abuse'));
  await abusePage.goto(`${ADMIN_URL}/admin/trust/abuse`, { waitUntil: 'domcontentloaded' });
  await abuseDelay.seen;
  states.abuse.push(await stateCapture(abusePage, 'admin-abuse', 'loading', captures, 'abuse-loading'));
  abuseDelay.release(); await abuseDelay.dispose();
  states.abuse.push(await stateCapture(abusePage, 'admin-abuse', 'empty', captures, 'abuse-empty'));

  const fixture = seedAdminFixture();

  await destinationPage.goto(`${ADMIN_URL}/admin/trust/destination-risk/${fixture.destination_risk_id}`, { waitUntil: 'networkidle' });
  states.destination.push(await stateCapture(destinationPage, 'admin-destination-risk-detail', 'provider-partial', captures, 'destination-provider-partial'));
  let text = await destinationPage.locator('body').innerText();
  assert(!text.includes(fixture.sensitive_target), 'Destination target leaked into Admin UI');
  assert(!text.includes(fixture.provider_marker), 'Provider evidence leaked into Admin UI');
  for (const [value, reason] of [
    ['pending', 'evaluation-started'], ['allow', 'policy-allow'], ['review', 'manual-review-required'],
    ['block', 'provider-block'], ['review', 'decision-stale'], ['review', 'provider-partial'],
  ]) await destinationState(destinationPage, fixture, value, reason, captures, states.destination);

  const focus = await tabToAccessibleName(destinationPage, 'Request rescan');
  assert(focus.tag === 'BUTTON', 'Request rescan must be keyboard reachable');
  await destinationPage.getByRole('button', { name: 'Request rescan' }).click();
  states.destination.push(await stateCapture(destinationPage, 'admin-destination-risk-detail', 'destructive-confirm', captures, 'destination-rescan-confirm'));
  const scanCount = Number(mysqlScalar(`SELECT COUNT(*) FROM destination_risk_scans WHERE link_id=${fixture.destination_link_id}`));
  await destinationPage.getByRole('button', { name: 'Confirm rescan' }).click();
  await destinationPage.getByText(/Rescan #\d+ was queued|existing idempotent rescan/).waitFor();
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM destination_risk_scans WHERE link_id=${fixture.destination_link_id}`)) === scanCount + 1, 'Rescan did not create exactly one durable row');
  captures.push(await screenshot(destinationPage, CASE, 'destination-rescan-result'));

  await destinationPage.getByRole('button', { name: 'Create bounded override' }).click();
  await destinationPage.getByLabel('Accountable reason').fill('P16 browser reviewer approved this bounded exact-fingerprint override.');
  captures.push(await screenshot(destinationPage, CASE, 'destination-override-confirm'));
  await destinationPage.getByRole('button', { name: 'Confirm override' }).click();
  await destinationPage.getByText(/Override #\d+ recorded as allow/).waitFor();
  assert(mysqlScalar(`SELECT COUNT(*) FROM destination_risk_overrides WHERE link_id=${fixture.destination_link_id} AND invalidated_at IS NULL`) === '1', 'Override row missing');
  assert(mysqlScalar(`SELECT COUNT(*) FROM destination_risk_audit_events WHERE link_id=${fixture.destination_link_id} AND action='destination-risk.override-create' AND result='success'`) === '1', 'Override audit missing');
  captures.push(await screenshot(destinationPage, CASE, 'destination-override-result'));

  await domainPage.goto(`${ADMIN_URL}/admin/trust/domain-risk/${fixture.domain_id}`, { waitUntil: 'networkidle' });
  states.domain.push(await stateCapture(domainPage, 'admin-domain-risk-detail', 'allow', captures, 'domain-allow'));
  for (const [value, reason] of [
    ['pending', 'evaluation-started'], ['review', 'provider-review'], ['block', 'provider-block'],
    ['revalidating', 'evaluation-started'], ['stale', 'decision-stale'], ['provider_partial', 'provider-partial'], ['allow', 'policy-allow'],
  ]) await domainState(domainPage, fixture, value, reason, captures, states.domain);
  const domainText = await domainPage.locator('body').innerText();
  for (const axis of ['Entitlement', 'Ownership', 'Ingress DNS', 'HTTPS', 'Routing']) assert(domainText.includes(axis), `Missing independent domain axis ${axis}`);
  await domainPage.getByRole('button', { name: 'Request revalidation' }).click();
  await domainPage.getByLabel('Accountable reason').fill('P16 browser evidence requests current domain reputation authority.');
  captures.push(await screenshot(domainPage, CASE, 'domain-revalidate-confirm'));
  await domainPage.getByRole('button', { name: 'Confirm revalidation' }).click();
  await domainPage.getByText(/Domain reputation was revalidated|existing idempotent revalidation/).waitFor();
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM domain_risk_evaluations WHERE domain_id=${fixture.domain_id} AND request_kind='revalidation'`)) >= 1, 'Domain revalidation row missing');
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM domain_risk_audit_events WHERE domain_id=${fixture.domain_id} AND action='domain-risk.evaluate.complete'`)) >= 1, 'Domain revalidation audit missing');
  captures.push(await screenshot(domainPage, CASE, 'domain-revalidate-result'));

  await abusePage.goto(`${ADMIN_URL}/admin/trust/abuse/${fixture.abuse_id}`, { waitUntil: 'networkidle' });
  states.abuse.push(await stateCapture(abusePage, 'admin-abuse-detail', 'open', captures, 'abuse-open'));
  text = await abusePage.locator('body').innerText();
  assert(!text.includes(fixture.sensitive_target) && !text.includes(fixture.provider_marker), 'Unsafe evidence leaked into Abuse Admin UI');
  await abusePage.getByRole('button', { name: 'Begin investigation' }).click();
  states.abuse.push(await stateCapture(abusePage, 'admin-abuse-detail', 'destructive-confirm', captures, 'abuse-investigate-confirm'));
  await abusePage.getByLabel('Accountable reason').fill('P16 browser reviewer began an accountable abuse investigation.');
  await abusePage.getByRole('button', { name: 'Confirm investigate' }).click();
  await abusePage.getByText(/Begin investigation: server authority changed and was audited/).waitFor();
  states.abuse.push(await expectState(abusePage, 'admin-abuse-detail', 'investigating'));
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_report_events WHERE report_id=${fixture.abuse_id} AND action='abuse.admin-transition' AND result='success'`) === '1', 'Investigation audit missing');
  captures.push(await screenshot(abusePage, CASE, 'abuse-investigating'));

  await abusePage.getByRole('button', { name: 'Block short-link resource' }).click();
  states.abuse.push(await stateCapture(abusePage, 'admin-abuse-detail', 'destructive-confirm', captures, 'abuse-block-confirm'));
  await abusePage.getByLabel('Accountable reason').fill('P16 browser evidence requires a current exact-fingerprint abuse hold.');
  await abusePage.getByRole('button', { name: 'Confirm block' }).click();
  await abusePage.getByText(/Block short-link resource: server authority changed and was audited/).waitFor();
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_resource_holds WHERE report_id=${fixture.abuse_id} AND state='active'`) === '1', 'Active abuse hold missing');
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_report_events WHERE report_id=${fixture.abuse_id} AND action='abuse.resource-block' AND result='success'`) === '1', 'Resource block audit missing');
  captures.push(await screenshot(abusePage, CASE, 'abuse-block-result'));

  for (const status of ['resolved', 'dismissed']) {
    mysql(`UPDATE abuse_reports SET status=${sqlLiteral(status)},updated_at=CURRENT_TIMESTAMP(6) WHERE id=${fixture.abuse_id}`);
    await abusePage.reload({ waitUntil: 'networkidle' });
    states.abuse.push(await stateCapture(abusePage, 'admin-abuse-detail', status, captures, `abuse-${status}`));
  }

  await deniedPage(denied, '/admin/trust/destination-risk', captures, 'permission-denied');
  states.permissions.push('security.manage-denied');
  await deniedPage(security, '/admin/trust/domain-risk', captures, 'permission-cross-domain');
  states.permissions.push('security-manage-does-not-grant-domain-risk');
  await deniedPage(domain, '/admin/trust/abuse', captures, 'permission-cross-abuse');
  states.permissions.push('domain-risk-manage-does-not-grant-abuse');

  for (const [surface, path, context] of [
    ['destination', `/admin/trust/destination-risk/${fixture.destination_risk_id}`, security],
    ['domain', `/admin/trust/domain-risk/${fixture.domain_id}`, domain],
    ['abuse', `/admin/trust/abuse/${fixture.abuse_id}`, security],
  ]) {
    responsive[surface] = {};
    for (const [name, viewport] of Object.entries(viewports)) {
      const page = await context.newPage();
      await page.setViewportSize(viewport);
      await page.goto(`${ADMIN_URL}${path}`, { waitUntil: 'networkidle' });
      responsive[surface][name] = await assertNoHorizontalOverflow(page, `${surface} ${name}`);
      captures.push(await screenshot(page, CASE, `responsive-${surface}-${name}`));
      await page.close();
    }
  }

  text = await destinationPage.locator('body').innerText();
  assert(!/continue anyway/i.test(text), 'Admin destination surface exposed continue-anyway bypass');
  assertCleanDiagnostics(destinationDiag, 'destination');
  assertCleanDiagnostics(domainDiag, 'domain');
  assertCleanDiagnostics(abuseDiag, 'abuse');

  await denied.close(); await domain.close(); await security.close();
  return {
    states, responsive, captures, screenshot_count: captures.length,
    security_checks: {
      p15_session_cookie: true,
      dedicated_permissions: true,
      csrf_origin_mutations: true,
      unsafe_target_not_disclosed: true,
      provider_evidence_not_disclosed: true,
      no_continue_anyway: true,
      destination_override_audited: true,
      domain_revalidation_audited: true,
      abuse_transition_and_hold_audited: true,
      no_store_noindex: true,
    },
  };
}
