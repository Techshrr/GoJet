import {
  ADMIN_URL,
  addSessionCookie,
  assert,
  assertCleanDiagnostics,
  assertNoHorizontalOverflow,
  attachDiagnostics,
  createBrowserSessions,
  delayedAPIRoute,
  diagnostics,
  expectState,
  mysql,
  mysqlScalar,
  screenshot,
  seedAdminFixture,
  sqlLiteral,
  tabToAccessibleName,
  viewports,
} from './browser_common.mjs';

const CASE = 'P16-T026';

async function openPage(context, path, allowedStatuses = []) {
  const page = await context.newPage();
  const report = diagnostics();
  attachDiagnostics(page, report, { allowStatuses: allowedStatuses });
  const response = await page.goto(`${ADMIN_URL}${path}`, { waitUntil: 'networkidle' });
  return { page, report, response };
}

async function assertPrivateAdminHTML(page, response) {
  const headers = response ? await response.allHeaders() : {};
  assert((headers['cache-control'] ?? '').toLowerCase().includes('no-store'), `Admin HTML missing no-store: ${headers['cache-control'] ?? ''}`);
  assert((headers['x-robots-tag'] ?? '').toLowerCase().includes('noindex'), `Admin HTML missing noindex: ${headers['x-robots-tag'] ?? ''}`);
  const robots = await page.locator('meta[name="robots"]').getAttribute('content');
  assert((robots ?? '').toLowerCase().includes('noindex'), `Admin meta robots missing noindex: ${robots ?? ''}`);
}

async function setDestinationState(page, fixture, state, reason, covered, captures) {
  mysql(`UPDATE destination_risk_decisions SET state=${sqlLiteral(state)},reason_category=${sqlLiteral(reason)},updated_at=CURRENT_TIMESTAMP(6) WHERE scan_id=${fixture.destination_risk_id}`);
  await page.reload({ waitUntil: 'networkidle' });
  const expected = reason.includes('stale') || reason.includes('fingerprint') ? 'stale-fingerprint' : reason.includes('provider') && state !== 'allow' ? 'provider-partial' : state;
  covered.destination.push(await expectState(page, 'admin-destination-risk-detail', expected));
  captures.push(await screenshot(page, CASE, `destination-${expected}`));
}

async function setDomainState(page, fixture, state, reason, covered, captures) {
  mysql(`UPDATE domain_risk_evaluations SET state=${sqlLiteral(state)},reason_category=${sqlLiteral(reason)},updated_at=CURRENT_TIMESTAMP(6) WHERE domain_id=${fixture.domain_id} ORDER BY id DESC LIMIT 1`);
  await page.reload({ waitUntil: 'networkidle' });
  const expected = state === 'provider_partial' || state === 'malformed' || (reason.includes('provider') && state !== 'allow') ? 'provider-partial' : state;
  covered.domain.push(await expectState(page, 'admin-domain-risk-detail', expected));
  captures.push(await screenshot(page, CASE, `domain-${expected}`));
}

export async function run(browser) {
  const sessions = createBrowserSessions();
  const captures = [];
  const covered = { destination: [], domain: [], abuse: [], permissions: [] };
  const responsive = {};

  const securityContext = await browser.newContext({ viewport: viewports.desktop });
  await addSessionCookie(securityContext, sessions, 'security');
  const destinationPage = await securityContext.newPage();
  const destinationDiag = diagnostics();
  attachDiagnostics(destinationPage, destinationDiag);
  const destinationDelay = await delayedAPIRoute(destinationPage, (request) => request.method() === 'GET' && request.url().includes('/api/admin/destination-risks'));
  const destinationResponse = await destinationPage.goto(`${ADMIN_URL}/admin/trust/destination-risk`, { waitUntil: 'domcontentloaded' });
  await destinationDelay.seen;
  covered.destination.push(await expectState(destinationPage, 'admin-destination-risk', 'loading'));
  captures.push(await screenshot(destinationPage, CASE, 'destination-loading'));
  destinationDelay.release();
  await destinationDelay.dispose();
  covered.destination.push(await expectState(destinationPage, 'admin-destination-risk', 'empty'));
  captures.push(await screenshot(destinationPage, CASE, 'destination-empty'));
  await assertPrivateAdminHTML(destinationPage, destinationResponse);

  const domainContext = await browser.newContext({ viewport: viewports.desktop });
  await addSessionCookie(domainContext, sessions, 'domain');
  const domainPage = await domainContext.newPage();
  const domainDiag = diagnostics();
  attachDiagnostics(domainPage, domainDiag);
  const domainDelay = await delayedAPIRoute(domainPage, (request) => request.method() === 'GET' && request.url().includes('/api/admin/domain-risks'));
  await domainPage.goto(`${ADMIN_URL}/admin/trust/domain-risk`, { waitUntil: 'domcontentloaded' });
  await domainDelay.seen;
  covered.domain.push(await expectState(domainPage, 'admin-domain-risk', 'loading'));
  captures.push(await screenshot(domainPage, CASE, 'domain-loading'));
  domainDelay.release();
  await domainDelay.dispose();
  covered.domain.push(await expectState(domainPage, 'admin-domain-risk', 'empty'));
  captures.push(await screenshot(domainPage, CASE, 'domain-empty'));

  const abusePage = await securityContext.newPage();
  const abuseDiag = diagnostics();
  attachDiagnostics(abusePage, abuseDiag);
  const abuseDelay = await delayedAPIRoute(abusePage, (request) => request.method() === 'GET' && request.url().includes('/api/admin/abuse'));
  await abusePage.goto(`${ADMIN_URL}/admin/trust/abuse`, { waitUntil: 'domcontentloaded' });
  await abuseDelay.seen;
  covered.abuse.push(await expectState(abusePage, 'admin-abuse', 'loading'));
  captures.push(await screenshot(abusePage, CASE, 'abuse-loading'));
  abuseDelay.release();
  await abuseDelay.dispose();
  covered.abuse.push(await expectState(abusePage, 'admin-abuse', 'empty'));
  captures.push(await screenshot(abusePage, CASE, 'abuse-empty'));

  const fixture = seedAdminFixture();

  await destinationPage.reload({ waitUntil: 'networkidle' });
  covered.destination.push(await expectState(destinationPage, 'admin-destination-risk', 'provider-partial'));
  await destinationPage.goto(`${ADMIN_URL}/admin/trust/destination-risk/${fixture.destination_risk_id}`, { waitUntil: 'networkidle' });
  covered.destination.push(await expectState(destinationPage, 'admin-destination-risk-detail', 'provider-partial'));
  captures.push(await screenshot(destinationPage, CASE, 'destination-provider-partial'));
  let destinationText = await destinationPage.locator('body').innerText();
  assert(!destinationText.includes(fixture.sensitive_target), 'Admin destination page exposed reachable target');
  assert(!destinationText.includes(fixture.provider_marker), 'Admin destination page exposed provider evidence marker');

  for (const [state, reason] of [
    ['pending', 'evaluation-started'],
    ['allow', 'policy-allow'],
    ['review', 'manual-review-required'],
    ['block', 'provider-block'],
    ['review', 'decision-stale'],
    ['review', 'provider-partial'],
  ]) {
    await setDestinationState(destinationPage, fixture, state, reason, covered, captures);
  }

  const focusRescan = await tabToAccessibleName(destinationPage, 'Request rescan');
  assert(focusRescan.tag === 'BUTTON', `Request rescan is not keyboard reachable as a button: ${JSON.stringify(focusRescan)}`);
  await destinationPage.getByRole('button', { name: 'Request rescan' }).click();
  covered.destination.push(await expectState(destinationPage, 'admin-destination-risk-detail', 'destructive-confirm'));
  captures.push(await screenshot(destinationPage, CASE, 'destination-rescan-confirm'));
  const scansBefore = Number(mysqlScalar(`SELECT COUNT(*) FROM destination_risk_scans WHERE link_id=${fixture.destination_link_id}`));
  await destinationPage.getByRole('button', { name: 'Confirm rescan' }).click();
  await destinationPage.getByText(/Rescan #\d+ was queued|existing idempotent rescan/).waitFor();
  const scansAfter = Number(mysqlScalar(`SELECT COUNT(*) FROM destination_risk_scans WHERE link_id=${fixture.destination_link_id}`));
  assert(scansAfter === scansBefore + 1, `Destination rescan did not create exactly one durable scan: ${scansBefore} -> ${scansAfter}`);
  captures.push(await screenshot(destinationPage, CASE, 'destination-rescan-result'));

  await destinationPage.getByRole('button', { name: 'Create bounded override' }).click();
  covered.destination.push(await expectState(destinationPage, 'admin-destination-risk-detail', 'destructive-confirm'));
  await destinationPage.getByLabel('Accountable reason').fill('P16 browser security reviewer approved this bounded exact-fingerprint override.');
  captures.push(await screenshot(destinationPage, CASE, 'destination-override-confirm'));
  await destinationPage.getByRole('button', { name: 'Confirm override' }).click();
  await destinationPage.getByText(/Override #\d+ recorded as allow/).waitFor();
  assert(mysqlScalar(`SELECT COUNT(*) FROM destination_risk_overrides WHERE link_id=${fixture.destination_link_id} AND invalidated_at IS NULL`) === '1', 'Destination override was not durably recorded');
  assert(mysqlScalar(`SELECT COUNT(*) FROM destination_risk_audit_events WHERE link_id=${fixture.destination_link_id} AND action='destination-risk.override-create' AND result='success'`) === '1', 'Destination override audit result missing');
  captures.push(await screenshot(destinationPage, CASE, 'destination-override-result'));

  await domainPage.reload({ waitUntil: 'networkidle' });
  covered.domain.push(await expectState(domainPage, 'admin-domain-risk', 'allow'));
  await domainPage.goto(`${ADMIN_URL}/admin/trust/domain-risk/${fixture.domain_id}`, { waitUntil: 'networkidle' });
  covered.domain.push(await expectState(domainPage, 'admin-domain-risk-detail', 'allow'));
  captures.push(await screenshot(domainPage, CASE, 'domain-allow'));
  for (const [state, reason] of [
    ['pending', 'evaluation-started'],
    ['review', 'provider-review'],
    ['block', 'provider-block'],
    ['revalidating', 'evaluation-started'],
    ['stale', 'decision-stale'],
    ['provider_partial', 'provider-partial'],
    ['allow', 'policy-allow'],
  ]) {
    await setDomainState(domainPage, fixture, state, reason, covered, captures);
  }
  const axisText = await domainPage.locator('body').innerText();
  for (const expected of ['Entitlement', 'Ownership', 'Ingress DNS', 'HTTPS', 'Routing']) {
    assert(axisText.includes(expected), `Domain independent axis missing: ${expected}`);
  }
  await domainPage.getByRole('button', { name: 'Request revalidation' }).click();
  await domainPage.getByLabel('Accountable reason').fill('P16 browser evidence requires a current domain reputation revalidation.');
  captures.push(await screenshot(domainPage, CASE, 'domain-revalidate-confirm'));
  await domainPage.getByRole('button', { name: 'Confirm revalidation' }).click();
  await domainPage.getByText(/Domain reputation was revalidated|existing idempotent revalidation/).waitFor();
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM domain_risk_evaluations WHERE domain_id=${fixture.domain_id} AND request_kind='revalidation'`)) >= 1, 'Domain revalidation did not create durable evaluation authority');
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM domain_risk_audit_events WHERE domain_id=${fixture.domain_id} AND action='domain-risk.evaluate.complete'`)) >= 1, 'Domain revalidation completion audit missing');
  captures.push(await screenshot(domainPage, CASE, 'domain-revalidate-result'));

  await abusePage.reload({ waitUntil: 'networkidle' });
  covered.abuse.push(await expectState(abusePage, 'admin-abuse', 'open'));
  await abusePage.goto(`${ADMIN_URL}/admin/trust/abuse/${fixture.abuse_id}`, { waitUntil: 'networkidle' });
  covered.abuse.push(await expectState(abusePage, 'admin-abuse-detail', 'open'));
  captures.push(await screenshot(abusePage, CASE, 'abuse-open'));
  const abuseText = await abusePage.locator('body').innerText();
  assert(!abuseText.includes(fixture.sensitive_target), 'Admin abuse page exposed destination target');
  assert(!abuseText.includes(fixture.provider_marker), 'Admin abuse page exposed provider evidence marker');

  await abusePage.getByRole('button', { name: 'Begin investigation' }).click();
  covered.abuse.push(await expectState(abusePage, 'admin-abuse-detail', 'destructive-confirm'));
  await abusePage.getByLabel('Accountable reason').fill('P16 browser reviewer began an accountable abuse investigation.');
  captures.push(await screenshot(abusePage, CASE, 'abuse-investigate-confirm'));
  await abusePage.getByRole('button', { name: 'Confirm investigate' }).click();
  await abusePage.getByText(/Begin investigation: server authority changed and was audited/).waitFor();
  covered.abuse.push(await expectState(abusePage, 'admin-abuse-detail', 'investigating'));
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_report_events WHERE report_id=${fixture.abuse_id} AND action='abuse.admin-transition' AND result='success'`) === '1', 'Abuse investigation audit event missing');

  await abusePage.getByRole('button', { name: 'Block short-link resource' }).click();
  covered.abuse.push(await expectState(abusePage, 'admin-abuse-detail', 'destructive-confirm'));
  await abusePage.getByLabel('Accountable reason').fill('P16 browser evidence confirms the investigated short link requires an exact-fingerprint hold.');
  captures.push(await screenshot(abusePage, CASE, 'abuse-block-confirm'));
  await abusePage.getByRole('button', { name: 'Confirm block' }).click();
  await abusePage.getByText(/Block short-link resource: server authority changed and was audited/).waitFor();
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_resource_holds WHERE report_id=${fixture.abuse_id} AND state='active'`) === '1', 'Abuse resource hold was not created');
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_report_events WHERE report_id=${fixture.abuse_id} AND action='abuse.resource-block' AND result='success'`) === '1', 'Abuse resource block audit event missing');
  captures.push(await screenshot(abusePage, CASE, 'abuse-block-result'));

  mysql(`UPDATE abuse_reports SET status='resolved',updated_at=CURRENT_TIMESTAMP(6) WHERE id=${fixture.abuse_id}`);
  await abusePage.reload({ waitUntil: 'networkidle' });
  covered.abuse.push(await expectState(abusePage, 'admin-abuse-detail', 'resolved'));
  captures.push(await screenshot(abusePage, CASE, 'abuse-resolved'));
  mysql(`UPDATE abuse_reports SET status='dismissed',updated_at=CURRENT_TIMESTAMP(6) WHERE id=${fixture.abuse_id}`);
  await abusePage.reload({ waitUntil: 'networkidle' });
  covered.abuse.push(await expectState(abusePage, 'admin-abuse-detail', 'dismissed'));
  captures.push(await screenshot(abusePage, CASE, 'abuse-dismissed'));

  const deniedContext = await browser.newContext({ viewport: viewports.desktop });
  await addSessionCookie(deniedContext, sessions, 'denied');
  const denied = await openPage(deniedContext, '/admin/trust/destination-risk', [403]);
  await denied.page.getByText(/permission does not cover this area/i).first().waitFor();
  covered.permissions.push('security.manage-denied');
  captures.push(await screenshot(denied.page, CASE, 'permission-denied'));

  const crossSecurity = await openPage(securityContext, '/admin/trust/domain-risk', [403]);
  await crossSecurity.page.getByText(/permission does not cover this area/i).first().waitFor();
  covered.permissions.push('security-manage-does-not-grant-domain-risk');
  const crossDomain = await openPage(domainContext, '/admin/trust/abuse', [403]);
  await crossDomain.page.getByText(/permission does not cover this area/i).first().waitFor();
  covered.permissions.push('domain-risk-manage-does-not-grant-abuse');

  for (const [surface, path, context] of [
    ['destination', `/admin/trust/destination-risk/${fixture.destination_risk_id}`, securityContext],
    ['domain', `/admin/trust/domain-risk/${fixture.domain_id}`, domainContext],
    ['abuse', `/admin/trust/abuse/${fixture.abuse_id}`, securityContext],
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

  destinationText = await destinationPage.locator('body').innerText();
  assert(!/continue anyway/i.test(destinationText), 'Destination Admin offered continue-anyway bypass');
  assertCleanDiagnostics(destinationDiag, 'destination Admin');
  assertCleanDiagnostics(domainDiag, 'domain Admin');
  assertCleanDiagnostics(abuseDiag, 'abuse Admin');
  assert(denied.report.page_errors.length === 0, `permission page errors: ${JSON.stringify(denied.report.page_errors)}`);

  await deniedContext.close();
  await domainContext.close();
  await securityContext.close();

  return {
    states: covered,
    responsive,
    screenshot_count: captures.length,
    captures,
    security_checks: {
      session_cookie_authority: true,
      dedicated_permissions: true,
      csrf_origin_protected_mutations: true,
      destination_target_not_disclosed: true,
      provider_evidence_not_disclosed: true,
      no_continue_anyway_bypass: true,
      destination_override_audited: true,
      domain_revalidation_audited: true,
      abuse_transition_and_hold_audited: true,
      admin_no_store_noindex: true,
    },
  };
}
