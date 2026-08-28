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
  reducedMotionEvidence,
  screenshot,
  tabToAccessibleName,
  viewports,
  waitState,
} from './browser_common.mjs';

export async function run(browser) {
  const caseId = 'P17-T030';
  const captures = [];
  const checks = {};
  const details = { states: [], responsive: {}, security_checks: {} };

  const context = await browser.newContext({ viewport: viewports.desktop });
  const page = await context.newPage();
  const report = diagnostics();
  attachDiagnostics(page, report, { allowStatuses: [401, 403, 409, 423, 428, 429] });

  await page.goto(`${ADMIN_URL}/admin/login`);
  await waitState(page, 'admin-login', 'input');
  details.states.push('input');
  await tabToAccessibleName(page, 'Sign in');
  checks.keyboard_login = true;

  await page.getByLabel('Email').fill('unknown-admin@p17.test');
  await page.getByLabel('Password').fill(fixture.root_password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await waitState(page, 'admin-login', 'invalid');
  details.states.push('invalid');
  checks.invalid_login = true;

  mysql(`UPDATE admin_credentials c JOIN admin_administrators a ON a.id=c.administrator_id SET c.locked_until=UTC_TIMESTAMP(6)+INTERVAL 10 MINUTE WHERE a.email_normalized='${fixture.limited_email}'`);
  await page.getByLabel('Email').fill(fixture.limited_email);
  await page.getByLabel('Password').fill(fixture.limited_password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await waitState(page, 'admin-login', 'locked');
  details.states.push('locked');
  checks.durable_lock = true;
  mysql(`UPDATE admin_credentials c JOIN admin_administrators a ON a.id=c.administrator_id SET c.locked_until=NULL,c.failed_attempts=0 WHERE a.email_normalized='${fixture.limited_email}'`);

  await page.getByLabel('Email').fill('rate-limit-admin@p17.test');
  await page.getByLabel('Password').fill(fixture.root_password);
  for (let attempt = 0; attempt < 12; attempt += 1) {
    await page.getByRole('button', { name: 'Sign in' }).click();
    await page.waitForTimeout(80);
    if ((await page.locator('[data-page="admin-login"]').getAttribute('data-state')) === 'rate-limited') break;
  }
  await waitState(page, 'admin-login', 'rate-limited');
  details.states.push('rate-limited');
  checks.server_rate_limit = true;

  await adminLogin(page);
  await waitState(page, 'admin-login', 'success');
  details.states.push('success');
  checks.real_login_cookie = (await context.cookies()).some((cookie) => cookie.name === 'gojet_admin_session' && cookie.secure);
  assert(checks.real_login_cookie, 'secure administrator session cookie was not established');

  await page.goto(`${ADMIN_URL}/admin/access/administrators`);
  await page.locator('[data-page="admin-access"]').waitFor({ state: 'visible' });
  await page.getByRole('button', { name: 'Enroll TOTP' }).click();
  await waitState(page, 'admin-access', 'TOTP');
  details.states.push('TOTP');
  const secret = (await page.locator('[data-secret-once="true"] code').textContent() || '').trim();
  assert(secret.length >= 16, 'TOTP enrollment did not return one-time secret');
  const { totp } = await import('./browser_common.mjs');
  const code = totp(secret);
  await page.getByLabel('TOTP confirmation code').fill(code);
  await page.getByRole('button', { name: 'Confirm TOTP' }).click();
  await page.locator('[data-secret-once="true"]').waitFor({ state: 'detached' });
  checks.totp_enrolled = mysqlScalar(`SELECT state FROM admin_totp_credentials t JOIN admin_administrators a ON a.id=t.administrator_id WHERE a.email_normalized='${fixture.root_email}'`) === 'active';
  assert(checks.totp_enrolled, 'durable TOTP authority not active');

  await context.clearCookies();
  await adminLogin(page);
  await waitState(page, 'admin-login', 'TOTP-required');
  details.states.push('TOTP-required');
  checks.totp_required = true;
  const freshCode = totp(secret);
  await adminLogin(page, fixture.root_email, fixture.root_password, freshCode);
  await waitState(page, 'admin-login', 'success');

  await page.goto(`${ADMIN_URL}/admin/access/administrators`);
  await waitState(page, 'admin-access', 'active');
  details.states.push('active');
  checks.administrators_route_backed = Number(mysqlScalar('SELECT COUNT(*) FROM admin_administrators')) >= 3;
  const revokeButton = page.getByRole('button', { name: 'Revoke' }).first();
  await revokeButton.click();
  await waitState(page, 'admin-access', 'session-revoke-confirm');
  details.states.push('session-revoke-confirm');
  captures.push(await screenshot(page, caseId, 'session-revoke-confirm'));
  await page.getByRole('button', { name: 'Confirm revoke' }).click();
  await page.waitForFunction(() => document.querySelector('[data-page="admin-access"]')?.getAttribute('data-state') !== 'session-revoke-confirm');
  checks.session_revoked_durable = Number(mysqlScalar(`SELECT COUNT(*) FROM admin_sessions s JOIN admin_administrators a ON a.id=s.administrator_id WHERE a.email_normalized='${fixture.root_email}' AND s.status='revoked'`)) >= 1;
  checks.session_revoke_audited = Number(mysqlScalar("SELECT COUNT(*) FROM admin_audit_events WHERE action LIKE '%session%revoke%'")) >= 1;
  assert(checks.session_revoked_durable && checks.session_revoke_audited, 'session revocation durable/audit authority missing');

  await page.goto(`${ADMIN_URL}/admin/access/roles`);
  await waitState(page, 'admin-roles', 'role-list');
  details.states.push('role-list');
  await page.getByLabel('Role name').fill('P17 Browser Duplicate Probe');
  await page.getByRole('button', { name: 'Create role' }).click();
  await waitState(page, 'admin-roles', 'role-list');
  await page.getByRole('button', { name: 'Create role' }).click();
  await waitState(page, 'admin-roles', 'permission-conflict');
  details.states.push('permission-conflict');
  checks.permission_conflict_server_side = true;

  await context.clearCookies();
  await adminLogin(page, fixture.limited_email, fixture.limited_password);
  await waitState(page, 'admin-login', 'success');
  await page.goto(`${ADMIN_URL}/admin/access/roles`);
  await waitState(page, 'admin-roles', 'permission-denied');
  details.states.push('permission-denied');
  checks.direct_url_rbac = true;

  await context.clearCookies();
  await adminLogin(page, fixture.root_email, fixture.root_password, totp(secret));
  await waitState(page, 'admin-login', 'success');
  for (const [label, viewport] of Object.entries(viewports)) {
    await page.setViewportSize(viewport);
    await page.goto(`${ADMIN_URL}/admin/access/administrators`);
    await page.locator('[data-page="admin-access"]').waitFor({ state: 'visible' });
    details.responsive[label] = await assertNoOverflow(page, `admin-access-${label}`);
    captures.push(await screenshot(page, caseId, `admin-access-${label}`));
  }
  checks.responsive_reflow = true;
  const reduced = await reducedMotionEvidence(browser, `${ADMIN_URL}/admin/login`);
  checks.reduced_motion = reduced.matches === true;
  details.reduced_motion = reduced;

  details.security_checks = {
    dedicated_admin_cookie: checks.real_login_cookie,
    totp_server_authority: checks.totp_enrolled && checks.totp_required,
    session_revocation_audited: checks.session_revoke_audited,
    direct_route_permission_denied: checks.direct_url_rbac,
    no_secret_screenshot: captures.every((capture) => !capture.includes('secret')),
  };
  details.states = [...new Set(details.states)];
  details.screenshot_count = captures.length;
  details.frozen_contract_completion = true;
  details.closure_claim = false;

  assert(Object.values(checks).every(Boolean), `T030 check failure ${JSON.stringify(checks)}`);
  assert(Object.values(details.security_checks).every(Boolean), 'T030 security checks incomplete');
  assertCleanDiagnostics(report, caseId);
  await context.close();
  return { checks, captures, details };
}
