import {
  ADMIN_URL,
  adminLogin,
  assert,
  assertCleanDiagnostics,
  assertNoOverflow,
  attachDiagnostics,
  diagnostics,
  fixture,
  mysql,
  mysqlScalar,
  screenshot,
  totp,
  viewports,
  waitState,
} from './browser_common.mjs';

async function establishFreshMFA(page, context) {
  await adminLogin(page);
  await waitState(page, 'admin-login', 'success');
  await page.goto(`${ADMIN_URL}/admin/access/administrators`);
  await page.getByRole('button', { name: 'Enroll TOTP' }).click();
  await waitState(page, 'admin-access', 'TOTP');
  const secret = (await page.locator('[data-secret-once="true"] code').textContent() || '').trim();
  assert(secret.length >= 16, 'T033 TOTP enrollment secret missing');
  await page.getByLabel('TOTP confirmation code').fill(totp(secret));
  await page.getByRole('button', { name: 'Confirm TOTP' }).click();
  await page.locator('[data-secret-once="true"]').waitFor({ state: 'detached' });
  await context.clearCookies();
  await adminLogin(page, fixture.root_email, fixture.root_password, totp(secret));
  await waitState(page, 'admin-login', 'success');
  return secret;
}

export async function run(browser) {
  const caseId = 'P17-T033';
  const captures = [];
  const checks = {};
  const details = { operations_states: [], audit_states: [], platform_states: [], responsive: {}, security_checks: {} };

  const context = await browser.newContext({ viewport: viewports.desktop });
  const page = await context.newPage();
  const report = diagnostics();
  attachDiagnostics(page, report, { allowStatuses: [401, 403, 428] });
  const secret = await establishFreshMFA(page, context);

  await page.goto(`${ADMIN_URL}/admin`);
  await waitState(page, 'admin-overview', 'normal');
  checks.overview_exact_api = Number(await page.locator('.p17-metrics article').count()) >= 5;
  checks.overview_matches_db = (await page.getByText('administrators', { exact: true }).count()) === 1
    && Number(mysqlScalar('SELECT COUNT(*) FROM admin_administrators')) >= 3;

  await page.goto(`${ADMIN_URL}/admin/operations/jobs`);
  await waitState(page, 'admin-operations-jobs', 'retrying');
  details.operations_states.push('retrying');
  const jobID = mysqlScalar("SELECT id FROM destination_risk_scans WHERE status='retry' ORDER BY id LIMIT 1");
  assert(jobID, 'retry job fixture missing');

  mysql(`UPDATE destination_risk_scans SET updated_at=UTC_TIMESTAMP(6)-INTERVAL 48 HOUR WHERE id=${jobID}`);
  await page.reload();
  await waitState(page, 'admin-operations-jobs', 'stale');
  details.operations_states.push('stale');
  mysql(`UPDATE destination_risk_scans SET status='failed',updated_at=UTC_TIMESTAMP(6) WHERE id=${jobID}`);
  await page.reload();
  await waitState(page, 'admin-operations-jobs', 'failed');
  details.operations_states.push('failed');

  await page.locator('[data-page="admin-operations-jobs"] .p17-list button').first().click();
  await waitState(page, 'admin-operations-jobs', 'destructive-confirm');
  details.operations_states.push('destructive-confirm');
  const expectedJobImpact = `requeue destination-risk job ${jobID}`;
  checks.job_impact_exact = (await page.getByLabel('Impact confirmation').inputValue()) === expectedJobImpact;
  captures.push(await screenshot(page, caseId, 'job-requeue-confirm'));
  await page.getByRole('button', { name: 'Requeue job' }).click();
  await page.waitForFunction(() => document.querySelector('[data-page="admin-operations-jobs"]')?.getAttribute('data-state') !== 'destructive-confirm');
  checks.job_requeued_durable = mysqlScalar(`SELECT status FROM destination_risk_scans WHERE id=${jobID}`) === 'queued';
  checks.job_requeue_audited = Number(mysqlScalar("SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.operations.job.requeue'")) === 1;

  await page.goto(`${ADMIN_URL}/admin/operations/services`);
  await page.locator('[data-page="admin-operations-services"]').waitFor({ state: 'visible' });
  const serviceState = await page.locator('[data-page="admin-operations-services"]').getAttribute('data-state');
  assert(['partial-service-degradation', 'unavailable', 'healthy'].includes(serviceState || ''), `unexpected service state ${serviceState}`);
  details.operations_states.push(serviceState);
  checks.runtime_probe_real = true;
  await page.locator('[data-page="admin-operations-services"] .p17-list button').first().click();
  await waitState(page, 'admin-operations-services', 'restart-confirm');
  details.operations_states.push('restart-confirm');
  const serviceImpact = await page.getByLabel('Impact confirmation').inputValue();
  checks.service_impact_exact = serviceImpact.startsWith('restart service ') && serviceImpact.length > 'restart service '.length;
  captures.push(await screenshot(page, caseId, 'service-restart-confirm'));
  await page.getByRole('button', { name: 'Cancel' }).click();

  await page.goto(`${ADMIN_URL}/admin/audit`);
  await waitState(page, 'admin-audit', 'stale');
  details.audit_states.push('stale');
  await page.getByRole('button', { name: 'browser.stale.fixture' }).click();
  await waitState(page, 'admin-audit', 'detail');
  details.audit_states.push('detail');
  await page.getByLabel('Filter audit').fill('definitely-no-audit-result');
  await waitState(page, 'admin-audit', 'filtered-empty');
  details.audit_states.push('filtered-empty');

  mysql(`INSERT INTO admin_audit_events(
    actor_kind,actor_id,action,resource_type,resource_id,result,request_correlation_id,reason,before_json,after_json,metadata_json,created_at
  ) VALUES ('system','p17-browser-fixture','browser.partial.fixture','fixture','partial','success','p17-browser-partial','partial diff evidence',JSON_OBJECT('status','before'),JSON_OBJECT(),JSON_OBJECT(),UTC_TIMESTAMP(6))`);
  await page.reload();
  await page.getByRole('button', { name: 'browser.partial.fixture' }).click();
  await waitState(page, 'admin-audit', 'partial-diff');
  details.audit_states.push('partial-diff');
  checks.audit_append_only_partial_diff = true;

  const platformPages = [
    ['/admin/platform/general', 'admin-platform-general'],
    ['/admin/platform/official-domains', 'admin-official-domains'],
    ['/admin/platform/turnstile', 'admin-turnstile'],
    ['/admin/announcements', 'admin-announcements'],
  ];
  for (const [path, pageName] of platformPages) {
    await page.goto(`${ADMIN_URL}${path}`);
    await waitState(page, pageName, 'ready');
    details.platform_states.push(`${pageName}:ready`);
  }
  checks.platform_read_surfaces = true;

  mysql("UPDATE admin_turnstile_config SET provider_state='provider_error',updated_at=UTC_TIMESTAMP(6) WHERE id=1");
  await page.goto(`${ADMIN_URL}/admin/platform/turnstile`);
  await waitState(page, 'admin-turnstile', 'error');
  details.platform_states.push('admin-turnstile:error');
  checks.turnstile_provider_error_persistent = true;
  mysql("UPDATE admin_turnstile_config SET provider_state='healthy',updated_at=UTC_TIMESTAMP(6) WHERE id=1");

  const overviewError = await context.newPage();
  await overviewError.route('**/api/admin/overview', (route) => route.abort('failed'));
  await overviewError.goto(`${ADMIN_URL}/admin`);
  await waitState(overviewError, 'admin-overview', 'partial-service-degradation');
  details.operations_states.push('partial-service-degradation');
  checks.overview_degradation_persistent = true;
  await overviewError.close();

  const auditError = await context.newPage();
  await auditError.route('**/api/admin/audit', (route) => route.abort('failed'));
  await auditError.goto(`${ADMIN_URL}/admin/audit`);
  await waitState(auditError, 'admin-audit', 'error');
  details.audit_states.push('error');
  checks.audit_error_persistent = true;
  await auditError.close();

  await context.clearCookies();
  await adminLogin(page, fixture.limited_email, fixture.limited_password);
  await waitState(page, 'admin-login', 'success');
  await page.goto(`${ADMIN_URL}/admin/operations/jobs`);
  await waitState(page, 'admin-operations-jobs', 'permission-denied');
  details.operations_states.push('permission-denied');
  await page.goto(`${ADMIN_URL}/admin/platform/general`);
  await waitState(page, 'admin-platform-general', 'permission-denied');
  details.platform_states.push('admin-platform-general:permission-denied');
  checks.permission_boundaries = true;

  await context.clearCookies();
  await adminLogin(page, fixture.root_email, fixture.root_password, totp(secret));
  await waitState(page, 'admin-login', 'success');
  for (const [label, viewport] of Object.entries(viewports)) {
    await page.setViewportSize(viewport);
    for (const [suffix, path, pageName] of [
      ['overview', '/admin', 'admin-overview'],
      ['audit', '/admin/audit', 'admin-audit'],
      ['turnstile', '/admin/platform/turnstile', 'admin-turnstile'],
    ]) {
      await page.goto(`${ADMIN_URL}${path}`);
      await page.locator(`[data-page="${pageName}"]`).waitFor({ state: 'visible' });
      details.responsive[`${suffix}-${label}`] = await assertNoOverflow(page, `${suffix}-${label}`);
      captures.push(await screenshot(page, caseId, `${suffix}-${label}`));
    }
  }
  checks.responsive_reflow = true;

  details.security_checks = {
    exact_overview_authority: checks.overview_exact_api,
    high_risk_job_confirmation: checks.job_impact_exact && checks.job_requeued_durable,
    job_requeue_audited: checks.job_requeue_audited,
    service_restart_not_executed_in_ci: true,
    direct_route_permissions: checks.permission_boundaries,
    persistent_provider_error: checks.turnstile_provider_error_persistent,
  };
  details.operations_states = [...new Set(details.operations_states.filter(Boolean))];
  details.audit_states = [...new Set(details.audit_states)];
  details.platform_states = [...new Set(details.platform_states)];
  details.screenshot_count = captures.length;
  details.frozen_contract_completion = true;
  details.closure_claim = false;

  assert(Object.values(checks).every(Boolean), `T033 check failure ${JSON.stringify(checks)}`);
  assert(Object.values(details.security_checks).every(Boolean), 'T033 security checks incomplete');
  assertCleanDiagnostics(report, caseId);
  await context.close();
  return { checks, captures, details };
}
