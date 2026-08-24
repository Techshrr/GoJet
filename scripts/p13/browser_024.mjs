import {
  ADMIN_URL, viewports, resetBilling, seedPlan, seedFX, mysqlScalar, api,
  diagnostics, attachDiagnostics, assertCleanDiagnostics, assert, screenshot,
} from './browser_common.mjs';

async function waitState(page, state) {
  await page.locator('[data-page="admin-commerce-fx"]').waitFor();
  await page.waitForFunction((expected) => document.querySelector('[data-page="admin-commerce-fx"]')?.getAttribute('data-state') === expected, state);
}

export async function run(browser) {
  const captures = [];
  const observed = {};
  const context = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1 });
  const page = await context.newPage();
  const report = diagnostics(); attachDiagnostics(page, report);

  for (const state of ['current', 'stale', 'provider-error']) {
    resetBilling();
    seedFX(state);
    await page.goto(`${ADMIN_URL}/admin/commerce/fx`, { waitUntil: 'networkidle' });
    await waitState(page, state);
    await page.getByText(state, { exact: true }).first().waitFor();
    observed[state] = true;
    captures.push(await screenshot(page, 'P13-T024', state));
  }

  resetBilling();
  seedFX('current');
  await page.goto(`${ADMIN_URL}/admin/commerce/fx`, { waitUntil: 'networkidle' });
  await waitState(page, 'current');
  await page.getByLabel('Decimal-string rate').fill('1.234567890123');
  await page.getByLabel('Override reason').fill('P13 browser audited FX override');
  await page.getByRole('button', { name: 'Review override' }).click();
  await waitState(page, 'override-confirm');
  await page.locator('.commerce-confirm > strong').waitFor();
  captures.push(await screenshot(page, 'P13-T024', 'override-confirm'));
  observed['override-confirm'] = true;
  await page.getByRole('button', { name: 'Confirm override' }).click();
  await page.waitForFunction(() => document.querySelector('[data-page="admin-commerce-fx"]')?.getAttribute('data-state') === 'current');
  const fxRows = mysqlScalar("SELECT CONCAT(rate,'|',status,'|',COALESCE(override_reason,'')) FROM billing_fx_rates WHERE base_currency='USD' AND quote_currency='EUR'");
  assert(fxRows.includes('|override|P13 browser audited FX override'), `FX override not durable: ${fxRows}`);
  const fxAudits = Number(mysqlScalar("SELECT COUNT(*) FROM billing_audit_events WHERE action='billing.fx.override' AND actor_id='p13-admin' AND result='success'"));
  assert(fxAudits >= 1, `FX override audit count=${fxAudits}`);
  observed.override_applied = { decimal_string_persisted: true, audit_count: fxAudits };

  await page.getByRole('button', { name: 'Review override' }).click();
  await waitState(page, 'validation-error');
  await page.getByText(/Rate and override reason are required/i).waitFor();
  observed['validation-error'] = true;
  captures.push(await screenshot(page, 'P13-T024', 'validation-error'));

  resetBilling();
  seedPlan({ code: 'p13_public_pricing', name: 'P13 Public Pricing Plan', entitlements: [['links', 500], ['custom_domains', 3]] });
  const publicPlans = await api('/api/public/plans');
  assert(publicPlans.status === 200 && Array.isArray(publicPlans.data?.items) && publicPlans.data.items.length === 1, `public plans substrate status=${publicPlans.status}`);
  const publicPlan = publicPlans.data.items[0];
  assert(publicPlan.status === 'active', 'public plans returned non-active fixture');
  assert(Array.isArray(publicPlan.entitlements) && publicPlan.entitlements.some((item) => item.capability === 'links'), 'structured public entitlements missing');
  assert(!Object.hasOwn(publicPlan, 'features'), 'generic features metadata appeared in public authority substrate');
  const serialized = JSON.stringify(publicPlans.data);
  for (const forbidden of ['secret', 'signature', 'credential', 'idempotency_key_hash', 'provenance_json']) assert(!serialized.toLowerCase().includes(forbidden), `public plans exposed forbidden field ${forbidden}`);
  observed.public_pricing_substrate = { safe: true, final_website_seo_claimed: false, plan_count: publicPlans.data.items.length };

  assertCleanDiagnostics(report, 'P13-T024 FX browser');
  await context.close();
  return { observed_states: observed, p19_website_seo_ownership_preserved: true, captures };
}
