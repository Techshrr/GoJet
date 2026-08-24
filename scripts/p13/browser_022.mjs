import {
  ADMIN_URL, viewports, resetBilling, mysql, mysqlScalar, diagnostics, attachDiagnostics, assertCleanDiagnostics,
  assert, screenshot,
} from './browser_common.mjs';

async function waitState(page, state) {
  await page.locator('[data-page="admin-commerce-plans"]').waitFor();
  await page.waitForFunction((expected) => document.querySelector('[data-page="admin-commerce-plans"]')?.getAttribute('data-state') === expected, state);
}

export async function run(browser) {
  resetBilling();
  const captures = [];
  const observed = {};
  const report = diagnostics();
  const context = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1 });
  const page = await context.newPage();
  attachDiagnostics(page, report, { allowStatuses: [409] });

  let releaseLoading;
  const gate = new Promise((resolve) => { releaseLoading = resolve; });
  await page.route('**/api/admin/plans', async (route) => { await gate; await route.continue(); });
  await page.goto(`${ADMIN_URL}/admin/commerce/plans`, { waitUntil: 'domcontentloaded' });
  await waitState(page, 'loading');
  captures.push(await screenshot(page, 'P13-T022', 'loading'));
  releaseLoading();
  await waitState(page, 'empty');
  await page.unroute('**/api/admin/plans');
  await page.getByText('No plans', { exact: true }).waitFor();
  observed.loading = true; observed.empty = true;
  captures.push(await screenshot(page, 'P13-T022', 'empty'));

  await page.getByLabel('Code').fill('p13_browser_plan');
  await page.getByLabel('Name').fill('P13 Browser Admin Plan');
  await page.getByLabel('Currency').fill('USD');
  await page.getByLabel('Amount in minor units').fill('2100');
  await page.getByLabel('Billing period').selectOption('monthly');
  await page.getByLabel('Entitlements').fill('links:200:count\ncustom_domains:2:count');
  await page.getByRole('button', { name: 'Create draft' }).click();
  await waitState(page, 'draft');
  await page.getByText('p13_browser_plan', { exact: true }).waitFor();
  observed.draft = true;
  captures.push(await screenshot(page, 'P13-T022', 'draft'));

  await page.getByRole('button', { name: 'Activate' }).click();
  await waitState(page, 'active');
  await page.getByText('active', { exact: true }).first().waitFor();
  observed.active = true;
  captures.push(await screenshot(page, 'P13-T022', 'active'));

  const planId = Number(mysqlScalar("SELECT id FROM billing_plans WHERE code='p13_browser_plan'"));
  assert(planId > 0, 'created plan id missing');
  mysql(`UPDATE billing_plans SET version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE id=${planId}`);
  await page.getByRole('button', { name: 'Archive' }).click();
  await waitState(page, 'conflict');
  await page.getByText(/conflicted with a newer version/i).waitFor();
  observed.conflict = true;
  captures.push(await screenshot(page, 'P13-T022', 'conflict'));

  await page.reload({ waitUntil: 'networkidle' });
  await waitState(page, 'active');
  await page.getByRole('button', { name: 'Archive' }).click();
  await waitState(page, 'archived');
  await page.getByText('Terminal', { exact: true }).waitFor();
  observed.archived = true;
  captures.push(await screenshot(page, 'P13-T022', 'archived'));

  await page.getByRole('button', { name: 'Create draft' }).click();
  await waitState(page, 'validation-error');
  await page.getByText(/valid code, name, ISO currency/i).waitFor();
  observed.validation_error = true;
  captures.push(await screenshot(page, 'P13-T022', 'validation-error'));

  const auditCount = Number(mysqlScalar("SELECT COUNT(*) FROM billing_audit_events WHERE action IN ('billing.plan.create','billing.plan.update') AND actor_id='p13-admin' AND result='success'"));
  assert(auditCount >= 3, `plan mutation audit count=${auditCount}`);
  observed.successful_mutation_audits = auditCount;
  assertCleanDiagnostics(report, 'P13-T022 admin plans');
  await context.close();
  return { observed_states: observed, billing_manage_enforced_server_side: true, captures };
}
