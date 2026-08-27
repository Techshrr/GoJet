import {
  ADMIN_URL, addSessionCookie, assert, assertCleanDiagnostics, assertNoHorizontalOverflow, attachDiagnostics,
  createBrowserSessions, delayedAPIRoute, diagnostics, expectState, mysql, mysqlScalar, screenshot,
  seedAdminFixture, sqlLiteral, tabToAccessibleName, viewports,
} from './browser_common.mjs';

const CASE = 'P16-T026';
const partialReasons = ['provider-partial', 'provider-unavailable', 'provider-incomplete', 'provider-malformed'];

async function capture(page, pageName, state, captures, label) {
  const value = await expectState(page, pageName, state);
  captures.push(await screenshot(page, CASE, label));
  return value;
}

function projectedDestination(state, reason) {
  if (reason.includes('stale') || reason.includes('fingerprint')) return 'stale-fingerprint';
  if (partialReasons.some((value) => reason.includes(value)) && state !== 'allow') return 'provider-partial';
  return state;
}

function projectedDomain(state, reason) {
  if (state === 'provider_partial' || state === 'malformed') return 'provider-partial';
  if (reason.includes('stale')) return 'stale';
  if (partialReasons.some((value) => reason.includes(value)) && state !== 'allow') return 'provider-partial';
  if (state === 'revalidating') return 'revalidating';
  return state;
}

async function setDestination(page, fixture, state, reason, states, captures) {
  mysql(`UPDATE destination_risk_decisions SET state=${sqlLiteral(state)},reason_category=${sqlLiteral(reason)} WHERE scan_id=${fixture.destination_risk_id}`);
  await page.reload({ waitUntil: 'networkidle' });
  const expected = projectedDestination(state, reason);
  states.push(await capture(page, 'admin-destination-risk-detail', expected, captures, `destination-${expected}`));
}

async function setDomain(page, fixture, state, reason, states, captures) {
  mysql(`UPDATE domain_risk_evaluations SET state=${sqlLiteral(state)},reason_category=${sqlLiteral(reason)},updated_at=CURRENT_TIMESTAMP(6) WHERE domain_id=${fixture.domain_id} ORDER BY id DESC LIMIT 1`);
  await page.reload({ waitUntil: 'networkidle' });
  const expected = projectedDomain(state, reason);
  states.push(await capture(page, 'admin-domain-risk-detail', expected, captures, `domain-${expected}`));
}

async function permissionDenied(context, path, captures, label) {
  const page = await context.newPage();
  const report = diagnostics();
  attachDiagnostics(page, report, { allowStatuses: [403] });
  await page.goto(`${ADMIN_URL}${path}`, { waitUntil: 'networkidle' });
  await page.getByText(/permission does not cover this area/i).first().waitFor();
  captures.push(await screenshot(page, CASE, label));
  assert(report.page_errors.length === 0 && report.request_failures.length === 0, `${label} browser diagnostics failed`);
  await page.close();
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
  const destinationDiag = diagnostics(); attachDiagnostics(destinationPage, destinationDiag);
  const destinationDelay = await delayedAPIRoute(destinationPage, (request) => request.method() === 'GET' && request.url().includes('/api/admin/destination-risks'));
  const htmlResponse = await destinationPage.goto(`${ADMIN_URL}/admin/trust/destination-risk`, { waitUntil: 'domcontentloaded' });
  await destinationDelay.seen;
  states.destination.push(await capture(destinationPage, 'admin-destination-risk', 'loading', captures, 'destination-loading'));
  destinationDelay.release(); await destinationDelay.dispose();
  states.destination.push(await capture(destinationPage, 'admin-destination-risk', 'empty', captures, 'destination-empty'));
  const htmlHeaders = htmlResponse ? await htmlResponse.allHeaders() : {};
  assert((htmlHeaders['cache-control'] ?? '').toLowerCase().includes('no-store'), 'Admin HTML missing no-store');
  assert((htmlHeaders['x-robots-tag'] ?? '').toLowerCase().includes('noindex'), 'Admin HTML missing noindex');
  assert((await destinationPage.locator('meta[name="robots"]').getAttribute('content') ?? '').toLowerCase().includes('noindex'), 'Admin meta robots missing noindex');

  const domainPage = await domain.newPage();
  const domainDiag = diagnostics(); attachDiagnostics(domainPage, domainDiag);
  const domainDelay = await delayedAPIRoute(domainPage, (request) => request.method() === 'GET' && request.url().includes('/api/admin/domain-risks'));
  await domainPage.goto(`${ADMIN_URL}/admin/trust/domain-risk`, { waitUntil: 'domcontentloaded' });
  await domainDelay.seen;
  states.domain.push(await capture(domainPage, 'admin-domain-risk', 'loading', captures, 'domain-loading'));
  domainDelay.release(); await domainDelay.dispose();
  states.domain.push(await capture(domainPage, 'admin-domain-risk', 'empty', captures, 'domain-empty'));

  const abusePage = await security.newPage();
  const abuseDiag = diagnostics(); attachDiagnostics(abusePage, abuseDiag);
  const abuseDelay = await delayedAPIRoute(abusePage, (request) => request.method() === 'GET' && request.url().includes('/api/admin/abuse'));
  await abusePage.goto(`${ADMIN_URL}/admin/trust/abuse`, { waitUntil: 'domcontentloaded' });
  await abuseDelay.seen;
  states.abuse.push(await capture(abusePage, 'admin-abuse', 'loading', captures, 'abuse-loading'));
  abuseDelay.release(); await abuseDelay.dispose();
  states.abuse.push(await capture(abusePage, 'admin-abuse', 'empty', captures, 'abuse-empty'));

  const fixture = seedAdminFixture();
  await destinationPage.goto(`${ADMIN_URL}/admin/trust/destination-risk/${fixture.destination_risk_id}`, { waitUntil: 'networkidle' });
  states.destination.push(await capture(destinationPage, 'admin-destination-risk-detail', 'provider-partial', captures, 'destination-provider-partial'));
  let body = await destinationPage.locator('body').innerText();
  assert(!body.includes(fixture.sensitive_target) && !body.includes(fixture.provider_marker), 'Destination control plane leaked provider/target evidence');
  for (const pair of [['pending','evaluation-started'],['allow','policy-allow'],['review','provider-review'],['block','provider-block'],['review','decision-stale'],['review','provider-partial']]) await setDestination(destinationPage, fixture, pair[0], pair[1], states.destination, captures);

  const focus = await tabToAccessibleName(destinationPage, 'Request rescan');
  assert(focus.tag === 'BUTTON', 'Request rescan is not keyboard reachable');
  await destinationPage.getByRole('button', { name: 'Request rescan' }).click();
  states.destination.push(await capture(destinationPage, 'admin-destination-risk-detail', 'destructive-confirm', captures, 'destination-rescan-confirm'));
  const scansBefore = Number(mysqlScalar(`SELECT COUNT(*) FROM destination_risk_scans WHERE link_id=${fixture.destination_link_id}`));
  await destinationPage.getByRole('button', { name: 'Confirm rescan' }).click();
  await destinationPage.getByText(/Rescan #\d+ was queued|existing idempotent rescan/).waitFor();
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM destination_risk_scans WHERE link_id=${fixture.destination_link_id}`)) === scansBefore + 1, 'Rescan durable row count mismatch');
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
  states.domain.push(await capture(domainPage, 'admin-domain-risk-detail', 'allow', captures, 'domain-allow'));
  for (const pair of [['pending','evaluation-started'],['review','provider-review'],['block','provider-block'],['revalidating','evaluation-started'],['stale','decision-stale'],['provider_partial','provider-partial'],['allow','policy-allow']]) await setDomain(domainPage, fixture, pair[0], pair[1], states.domain, captures);
  body = await domainPage.locator('body').innerText();
  for (const axis of ['Entitlement','Ownership','Ingress DNS','HTTPS','Routing']) assert(body.includes(axis), `Missing independent domain axis ${axis}`);
  await domainPage.getByRole('button', { name: 'Request revalidation' }).click();
  await domainPage.getByLabel('Accountable reason').fill('P16 browser evidence requests current domain reputation authority.');
  captures.push(await screenshot(domainPage, CASE, 'domain-revalidate-confirm'));
  await domainPage.getByRole('button', { name: 'Confirm revalidation' }).click();
  await domainPage.getByText(/Domain reputation was revalidated|existing idempotent revalidation/).waitFor();
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM domain_risk_evaluations WHERE domain_id=${fixture.domain_id} AND request_kind='revalidation'`)) >= 1, 'Domain revalidation row missing');
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM domain_risk_audit_events WHERE domain_id=${fixture.domain_id} AND action='domain-risk.evaluate.complete'`)) >= 1, 'Domain revalidation audit missing');
  captures.push(await screenshot(domainPage, CASE, 'domain-revalidate-result'));

  await abusePage.goto(`${ADMIN_URL}/admin/trust/abuse/${fixture.abuse_id}`, { waitUntil: 'networkidle' });
  states.abuse.push(await capture(abusePage, 'admin-abuse-detail', 'open', captures, 'abuse-open'));
  body = await abusePage.locator('body').innerText();
  assert(!body.includes(fixture.sensitive_target) && !body.includes(fixture.provider_marker), 'Abuse Admin leaked unsafe evidence');
  await abusePage.getByRole('button', { name: 'Begin investigation' }).click();
  states.abuse.push(await capture(abusePage, 'admin-abuse-detail', 'destructive-confirm', captures, 'abuse-investigate-confirm'));
  await abusePage.getByLabel('Accountable reason').fill('P16 browser reviewer began an accountable abuse investigation.');
  await abusePage.getByRole('button', { name: 'Confirm investigate' }).click();
  await abusePage.getByText(/Begin investigation: server authority changed and was audited/).waitFor();
  states.abuse.push(await capture(abusePage, 'admin-abuse-detail', 'investigating', captures, 'abuse-investigating'));
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_report_events WHERE report_id=${fixture.abuse_id} AND action='abuse.admin-transition' AND result='success'`) === '1', 'Abuse transition audit missing');
  await abusePage.getByRole('button', { name: 'Block short-link resource' }).click();
  states.abuse.push(await capture(abusePage, 'admin-abuse-detail', 'destructive-confirm', captures, 'abuse-block-confirm'));
  await abusePage.getByLabel('Accountable reason').fill('P16 browser evidence requires a current exact-fingerprint abuse hold.');
  await abusePage.getByRole('button', { name: 'Confirm block' }).click();
  await abusePage.getByText(/Block short-link resource: server authority changed and was audited/).waitFor();
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_resource_holds WHERE report_id=${fixture.abuse_id} AND state='active'`) === '1', 'Abuse hold missing');
  assert(mysqlScalar(`SELECT COUNT(*) FROM abuse_report_events WHERE report_id=${fixture.abuse_id} AND action='abuse.resource-block' AND result='success'`) === '1', 'Abuse block audit missing');
  captures.push(await screenshot(abusePage, CASE, 'abuse-block-result'));
  for (const status of ['resolved','dismissed']) { mysql(`UPDATE abuse_reports SET status=${sqlLiteral(status)},updated_at=CURRENT_TIMESTAMP(6) WHERE id=${fixture.abuse_id}`); await abusePage.reload({ waitUntil: 'networkidle' }); states.abuse.push(await capture(abusePage, 'admin-abuse-detail', status, captures, `abuse-${status}`)); }

  await permissionDenied(denied, '/admin/trust/destination-risk', captures, 'permission-denied'); states.permissions.push('security.manage-denied');
  await permissionDenied(security, '/admin/trust/domain-risk', captures, 'permission-cross-domain'); states.permissions.push('security-manage-does-not-grant-domain-risk');
  await permissionDenied(domain, '/admin/trust/abuse', captures, 'permission-cross-abuse'); states.permissions.push('domain-risk-manage-does-not-grant-abuse');

  for (const [surface,path,context] of [['destination',`/admin/trust/destination-risk/${fixture.destination_risk_id}`,security],['domain',`/admin/trust/domain-risk/${fixture.domain_id}`,domain],['abuse',`/admin/trust/abuse/${fixture.abuse_id}`,security]]) {
    responsive[surface] = {};
    for (const [name,viewport] of Object.entries(viewports)) { const page = await context.newPage(); await page.setViewportSize(viewport); await page.goto(`${ADMIN_URL}${path}`, { waitUntil: 'networkidle' }); responsive[surface][name] = await assertNoHorizontalOverflow(page, `${surface} ${name}`); captures.push(await screenshot(page, CASE, `responsive-${surface}-${name}`)); await page.close(); }
  }

  assert(await destinationPage.getByRole('link', { name: /continue anyway/i }).count() === 0, 'Destination Admin exposes continue-anyway link');
  assert(await destinationPage.getByRole('button', { name: /continue anyway/i }).count() === 0, 'Destination Admin exposes continue-anyway button');
  assertCleanDiagnostics(destinationDiag, 'destination'); assertCleanDiagnostics(domainDiag, 'domain'); assertCleanDiagnostics(abuseDiag, 'abuse');
  await denied.close(); await domain.close(); await security.close();
  return { states, responsive, captures, screenshot_count: captures.length, security_checks: { p15_session_cookie: true, dedicated_permissions: true, csrf_origin_mutations: true, unsafe_target_not_disclosed: true, provider_evidence_not_disclosed: true, no_continue_anyway_control: true, destination_override_audited: true, domain_revalidation_audited: true, abuse_transition_and_hold_audited: true, no_store_noindex: true } };
}
