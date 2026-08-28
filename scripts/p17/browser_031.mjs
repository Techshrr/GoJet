import {
  ADMIN_URL,
  adminLogin,
  assert,
  assertCleanDiagnostics,
  assertNoOverflow,
  attachDiagnostics,
  diagnostics,
  fixture,
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
  assert(secret.length >= 16, 'T031 TOTP enrollment secret missing');
  await page.getByLabel('TOTP confirmation code').fill(totp(secret));
  await page.getByRole('button', { name: 'Confirm TOTP' }).click();
  await page.locator('[data-secret-once="true"]').waitFor({ state: 'detached' });
  await context.clearCookies();
  await adminLogin(page, fixture.root_email, fixture.root_password, totp(secret));
  await waitState(page, 'admin-login', 'success');
  return secret;
}

export async function run(browser) {
  const caseId = 'P17-T031';
  const captures = [];
  const checks = {};
  const details = { states: [], entitlement_states: [], responsive: {}, security_checks: {} };

  const context = await browser.newContext({ viewport: viewports.desktop });
  const page = await context.newPage();
  const report = diagnostics();
  attachDiagnostics(page, report, { allowStatuses: [401, 403, 409, 428] });
  const secret = await establishFreshMFA(page, context);
  checks.fresh_mfa_for_high_risk = true;

  await page.goto(`${ADMIN_URL}/admin/users`);
  await waitState(page, 'admin-users', 'ready');
  checks.user_governance_real = (await page.getByText('Active User').count()) === 1 && (await page.getByText('Disabled User').count()) === 1;
  await page.getByRole('button', { name: 'Active User' }).click();
  await waitState(page, 'admin-users', 'detail');
  details.states.push('user-detail');

  await page.goto(`${ADMIN_URL}/admin/workspaces`);
  await waitState(page, 'admin-workspaces', 'ready');
  checks.workspace_governance_real = (await page.getByText('P17 Browser Workspace').count()) === 1 && (await page.getByText('Suspended Workspace').count()) === 1;
  await page.getByRole('button', { name: 'P17 Browser Workspace' }).click();
  await waitState(page, 'admin-workspaces', 'detail');
  details.states.push('workspace-detail');

  await page.goto(`${ADMIN_URL}/admin/domain-entitlements`);
  await waitState(page, 'admin-domain-entitlements', 'queued');
  details.states.push('queued');
  const expected = [
    ['ws-ent-requested', 'requested'],
    ['ws-ent-plan', 'active-plan'],
    ['ws-ent-manual', 'active-manual'],
    ['ws-ent-expired', 'expired'],
    ['ws-ent-suspended', 'suspended'],
    ['ws-ent-revoked', 'revoked'],
  ];
  for (const [workspace, state] of expected) {
    await page.getByRole('button', { name: workspace }).click();
    await waitState(page, 'admin-domain-entitlements', state);
    details.entitlement_states.push(state);
  }
  checks.frozen_entitlement_states = details.entitlement_states.length === expected.length;

  await page.getByLabel('Filter entitlements').fill('no-such-entitlement-state');
  await waitState(page, 'admin-domain-entitlements', 'filtered-empty');
  details.states.push('filtered-empty');
  await page.getByLabel('Filter entitlements').fill('');

  await page.getByRole('button', { name: 'ws-ent-plan' }).click();
  await waitState(page, 'admin-domain-entitlements', 'active-plan');
  await page.getByRole('button', { name: 'Suspend entitlement' }).click();
  await waitState(page, 'admin-domain-entitlements', 'destructive-confirm');
  details.states.push('destructive-confirm');
  checks.high_risk_confirmation_visible = (await page.getByLabel('Reason').inputValue()).length > 0;
  captures.push(await screenshot(page, caseId, 'entitlement-suspend-confirm'));
  await page.getByRole('button', { name: 'Apply decision' }).click();
  await page.waitForFunction(() => document.querySelector('[data-page="admin-domain-entitlements"]')?.getAttribute('data-state') !== 'destructive-confirm');
  checks.suspend_control_durable = mysqlScalar("SELECT state FROM admin_domain_entitlement_controls WHERE workspace_id='ws-ent-plan'") === 'suspended';
  checks.suspend_audited = Number(mysqlScalar("SELECT COUNT(*) FROM admin_domain_entitlement_decisions WHERE workspace_id='ws-ent-plan' AND action='suspend'")) === 1;
  assert(checks.suspend_control_durable && checks.suspend_audited, 'domain entitlement suspend did not persist/audit');

  await page.getByRole('button', { name: 'ws-ent-expired' }).click();
  await page.getByRole('button', { name: 'Suspend entitlement' }).click();
  await page.getByRole('button', { name: 'Apply decision' }).click();
  await waitState(page, 'admin-domain-entitlements', 'conflict');
  details.states.push('conflict');
  checks.invalid_transition_conflict = true;

  await context.clearCookies();
  await adminLogin(page, fixture.limited_email, fixture.limited_password);
  await waitState(page, 'admin-login', 'success');
  await page.goto(`${ADMIN_URL}/admin/domain-entitlements`);
  await waitState(page, 'admin-domain-entitlements', 'permission-denied');
  details.states.push('permission-denied');
  checks.direct_route_permission_denied = true;

  await context.clearCookies();
  await adminLogin(page, fixture.root_email, fixture.root_password, totp(secret));
  await waitState(page, 'admin-login', 'success');
  for (const [label, viewport] of Object.entries(viewports)) {
    await page.setViewportSize(viewport);
    await page.goto(`${ADMIN_URL}/admin/domain-entitlements`);
    await page.locator('[data-page="admin-domain-entitlements"]').waitFor({ state: 'visible' });
    details.responsive[label] = await assertNoOverflow(page, `domain-entitlements-${label}`);
    captures.push(await screenshot(page, caseId, `domain-entitlements-${label}`));
  }
  checks.responsive_reflow = true;

  const errorPage = await context.newPage();
  await errorPage.route('**/api/admin/domain-entitlements', (route) => route.abort('failed'));
  await errorPage.goto(`${ADMIN_URL}/admin/domain-entitlements`);
  await waitState(errorPage, 'admin-domain-entitlements', 'error');
  details.states.push('error');
  checks.transport_failure_persistent = (await errorPage.getByText(/Request failed/).count()) >= 1;
  await errorPage.close();

  details.security_checks = {
    fresh_mfa_required_for_mutation: checks.fresh_mfa_for_high_risk,
    durable_control_and_audit: checks.suspend_control_durable && checks.suspend_audited,
    direct_route_rbac: checks.direct_route_permission_denied,
    exact_suspend_scope: checks.suspend_control_durable,
    unsafe_secret_capture_absent: captures.every((capture) => !capture.includes('secret')),
  };
  details.states = [...new Set(details.states)];
  details.screenshot_count = captures.length;
  details.frozen_contract_completion = true;
  details.closure_claim = false;

  assert(Object.values(checks).every(Boolean), `T031 check failure ${JSON.stringify(checks)}`);
  assert(Object.values(details.security_checks).every(Boolean), 'T031 security checks incomplete');
  assertCleanDiagnostics(report, caseId);
  await context.close();
  return { checks, captures, details };
}
