import {
  WORKSPACE, WORKSPACE_OWNER_URL, WORKSPACE_VIEWER_URL, viewports, resetBilling, seedPlan, seedSubscription,
  seedOrder, mysqlScalar, viewerApi, diagnostics, attachDiagnostics, assertCleanDiagnostics, assert, screenshot,
} from './browser_common.mjs';

async function waitState(page, state) {
  const root = page.locator('[data-page="workspace-billing"]');
  await root.waitFor();
  await page.waitForFunction((expected) => document.querySelector('[data-page="workspace-billing"]')?.getAttribute('data-state') === expected, state);
  return root;
}

export async function run(browser) {
  const captures = [];
  const observed = {};
  const report = diagnostics();
  const context = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1 });
  const page = await context.newPage();
  attachDiagnostics(page, report);

  resetBilling();
  const loadingPlan = seedPlan({ code: 'p13_browser_loading' });
  seedSubscription(loadingPlan, 'active', 'sub-browser-loading');
  let releaseLoading;
  const loadingGate = new Promise((resolve) => { releaseLoading = resolve; });
  const summaryPattern = `**/api/workspaces/${WORKSPACE}/billing`;
  await page.route(summaryPattern, async (route) => { await loadingGate; await route.continue(); });
  await page.goto(`${WORKSPACE_OWNER_URL}/app/billing`, { waitUntil: 'domcontentloaded' });
  await waitState(page, 'loading');
  await page.locator('[role="status"]').filter({ hasText: 'Loading authoritative billing state' }).waitFor();
  captures.push(await screenshot(page, 'P13-T021', 'loading'));
  releaseLoading();
  await waitState(page, 'active');
  await page.unroute(summaryPattern);
  observed.loading_to_active = true;

  const scenarios = [
    ['active', 'active', null],
    ['payment-pending', 'active', 'pending'],
    ['payment-failed', 'active', 'failed'],
    ['overdue', 'overdue', 'paid'],
    ['canceled', 'canceled', 'paid'],
    ['provider-partial', 'active', 'processing'],
  ];
  for (const [expected, subscriptionStatus, orderStatus] of scenarios) {
    resetBilling();
    const planId = seedPlan({ code: `p13_browser_${expected.replaceAll('-', '_')}` });
    seedSubscription(planId, subscriptionStatus, `sub-${expected}`);
    if (orderStatus) seedOrder(planId, orderStatus, `ord-${expected}`);
    await page.goto(`${WORKSPACE_OWNER_URL}/app/billing`, { waitUntil: 'networkidle' });
    await waitState(page, expected);
    const stateText = await page.locator('[data-page="workspace-billing"]').innerText();
    assert(stateText.includes(expected) || expected === 'provider-partial', `${expected} server state not rendered`);
    observed[expected] = true;
    captures.push(await screenshot(page, 'P13-T021', expected));
  }

  resetBilling();
  const errorPlan = seedPlan({ code: 'p13_browser_error' });
  seedSubscription(errorPlan, 'active', 'sub-browser-error');
  const errorPage = await context.newPage();
  await errorPage.route(summaryPattern, (route) => route.abort('failed'));
  await errorPage.goto(`${WORKSPACE_OWNER_URL}/app/billing`, { waitUntil: 'domcontentloaded' });
  await waitState(errorPage, 'error');
  await errorPage.getByText(/Billing status could not be loaded/i).waitFor();
  captures.push(await screenshot(errorPage, 'P13-T021', 'error'));
  observed.error = true;
  await errorPage.close();

  resetBilling();
  const currentPlan = seedPlan({ code: 'p13_browser_current', name: 'Current Browser Plan' });
  seedSubscription(currentPlan, 'active', 'sub-browser-owner-mutation');
  const targetPlan = seedPlan({ code: 'p13_browser_target', name: 'Target Browser Plan', amount: 2900 });
  await page.goto(`${WORKSPACE_OWNER_URL}/app/billing`, { waitUntil: 'networkidle' });
  await waitState(page, 'active');
  await page.locator('.billing-plan-action').getByRole('combobox').selectOption(String(targetPlan));
  await page.getByRole('button', { name: 'Create order' }).click();
  await page.getByText(/Payment settlement is still pending server authority/i).waitFor();
  const ownerOrders = Number(mysqlScalar(`SELECT COUNT(*) FROM billing_orders WHERE workspace_id='${WORKSPACE}'`));
  assert(ownerOrders === 1, `owner browser mutation did not create exactly one order: ${ownerOrders}`);
  observed.owner_mutation = { created_orders: ownerOrders };

  const viewerPage = await context.newPage();
  await viewerPage.goto(`${WORKSPACE_VIEWER_URL}/app/billing`, { waitUntil: 'networkidle' });
  await viewerPage.getByText(/Billing summary and financial records are restricted/i).waitFor();
  assert(await viewerPage.getByRole('button', { name: 'Create order' }).count() === 0, 'viewer browser exposed Create order');
  const denied = await viewerApi(`/api/workspaces/${WORKSPACE}/orders`, { method: 'POST', body: { plan_id: targetPlan, kind: 'upgrade' }, headers: { 'Idempotency-Key': 'p13-browser-viewer-denied' } });
  assert(denied.status === 403, `viewer order mutation status=${denied.status}`);
  observed.viewer_mutation_denied = denied.status;
  captures.push(await screenshot(viewerPage, 'P13-T021', 'viewer-denied'));
  await viewerPage.close();

  assertCleanDiagnostics(report, 'P13-T021 positive browser states');
  await context.close();
  return { observed_states: observed, owner_only_mutation_authority: true, captures };
}
