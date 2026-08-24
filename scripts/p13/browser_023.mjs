import {
  ADMIN_URL, PLATFORM_URL, viewports, resetBilling, seedPlan, seedPayment, mysqlScalar, api,
  diagnostics, attachDiagnostics, assertCleanDiagnostics, assert, screenshot,
} from './browser_common.mjs';

async function waitListState(page, state) {
  await page.locator('[data-page="admin-commerce-payments"]').waitFor();
  await page.waitForFunction((expected) => document.querySelector('[data-page="admin-commerce-payments"]')?.getAttribute('data-state') === expected, state);
}
async function waitDetailState(page, state) {
  await page.locator('[data-page="admin-commerce-payment-detail"]').waitFor();
  await page.waitForFunction((expected) => document.querySelector('[data-page="admin-commerce-payment-detail"]')?.getAttribute('data-state') === expected, state);
}

export async function run(browser) {
  const captures = [];
  const observed = {};
  const context = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1 });
  const page = await context.newPage();
  const report = diagnostics(); attachDiagnostics(page, report);

  for (const status of ['pending', 'paid', 'failed', 'refunded']) {
    resetBilling();
    const planId = seedPlan({ code: `p13_payment_${status}` });
    const seeded = seedPayment(planId, status);
    await page.goto(`${ADMIN_URL}/admin/commerce/payments`, { waitUntil: 'networkidle' });
    await waitListState(page, status);
    await page.getByText(status, { exact: true }).first().waitFor();
    captures.push(await screenshot(page, 'P13-T023', `list-${status}`));
    await page.goto(`${ADMIN_URL}/admin/commerce/payments/${seeded.id}`, { waitUntil: 'networkidle' });
    await waitDetailState(page, status);
    const bodyText = await page.locator('body').innerText();
    assert(!bodyText.includes(seeded.providerRef), `${status} detail exposed full provider transaction reference`);
    assert(bodyText.includes(seeded.providerRef.slice(-4)), `${status} detail missing masked provider reference suffix`);
    observed[status] = { list: true, detail: true, provider_reference_masked: true };
  }

  resetBilling();
  const txBefore = Number(mysqlScalar('SELECT COUNT(*) FROM billing_transactions'));
  const eventBefore = Number(mysqlScalar('SELECT COUNT(*) FROM payment_callback_events'));
  const invalid = await api('/api/payments/callbacks/stripe', {
    method: 'POST',
    body: {
      event_id: 'p13-browser-invalid-event', transaction_id: 'p13-browser-invalid-txn', order_id: 'missing-order',
      event_type: 'payment.paid', outcome: 'paid', currency: 'USD', amount_minor: 1900,
      received_at: new Date().toISOString(), correlation_id: 'p13-browser-invalid-correlation',
    },
    headers: { 'X-GoJet-Test-Callback-Signature': '00'.repeat(32) },
  });
  const txAfter = Number(mysqlScalar('SELECT COUNT(*) FROM billing_transactions'));
  const eventAfter = Number(mysqlScalar('SELECT COUNT(*) FROM payment_callback_events'));
  assert(invalid.status === 401, `invalid callback status=${invalid.status}`);
  assert(txBefore === txAfter && eventBefore === eventAfter, `invalid callback mutated durable state tx ${txBefore}->${txAfter}, events ${eventBefore}->${eventAfter}`);
  await page.goto(`${ADMIN_URL}/admin/commerce/payments`, { waitUntil: 'networkidle' });
  await page.getByText(/Invalid provider callbacks are rejected before durable payment mutation/i).waitFor();
  captures.push(await screenshot(page, 'P13-T023', 'callback-invalid'));
  observed['callback-invalid'] = { response_status: invalid.status, durable_mutation: false, admin_rejection_semantics_visible: true };

  const partialPage = await context.newPage();
  await partialPage.route('**/api/admin/payments?limit=100', (route) => route.abort('failed'));
  await partialPage.goto(`${ADMIN_URL}/admin/commerce/payments`, { waitUntil: 'domcontentloaded' });
  await waitListState(partialPage, 'partial');
  await partialPage.getByText(/partially unavailable/i).waitFor();
  captures.push(await screenshot(partialPage, 'P13-T023', 'partial'));
  observed.partial = true;
  await partialPage.close();

  assertCleanDiagnostics(report, 'P13-T023 positive payment states');
  await context.close();
  return {
    observed_states: observed,
    callback_invalid_interpretation: 'authenticated UI never fabricates an invalid ledger row; real invalid signature is 401 before durable mutation and Admin renders the fail-closed rejection policy',
    platformapi: PLATFORM_URL,
    captures,
  };
}
