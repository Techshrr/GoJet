import {
  PLATFORM_URL, WORKSPACE, WORKSPACE_OWNER_URL, WORKSPACE_VIEWER_URL, WORKSPACE_UNAUTH_URL,
  ADMIN_URL, ADMIN_DENIED_URL, ADMIN_UNAUTH_URL, ADMIN, ADMIN_EMAIL, DENIED_ADMIN, DENIED_ADMIN_EMAIL,
  viewports, resetBilling, seedPlan, seedSubscription, viewerApi, adminApi, api, diagnostics, attachDiagnostics,
  assertCleanDiagnostics, assert, screenshot, assertNoHorizontalOverflow,
} from './browser_common.mjs';

async function waitWorkspace(page) { await page.locator('[data-page="workspace-billing"]').waitFor(); }
async function waitAdmin(page) { await page.locator('[data-page="admin-commerce-plans"]').waitFor(); }

export async function run(browser) {
  resetBilling();
  const planId = seedPlan({ code: 'p13_browser_responsive', name: 'P13 Responsive Plan' });
  seedSubscription(planId, 'active', 'sub-browser-responsive');
  const captures = [];
  const responsive = {};

  for (const [name, viewport] of Object.entries(viewports)) {
    const context = await browser.newContext({ viewport, deviceScaleFactor: 1 });
    const workspacePage = await context.newPage();
    await workspacePage.goto(`${WORKSPACE_OWNER_URL}/app/billing`, { waitUntil: 'networkidle' });
    await waitWorkspace(workspacePage);
    const wsViewport = await workspacePage.locator('[data-shell="workspace"]').getAttribute('data-viewport');
    const wsOverflow = await assertNoHorizontalOverflow(workspacePage, `workspace ${name}`);
    const adminPage = await context.newPage();
    await adminPage.goto(`${ADMIN_URL}/admin/commerce/plans`, { waitUntil: 'networkidle' });
    await waitAdmin(adminPage);
    const adminViewport = await adminPage.locator('[data-shell="admin"]').getAttribute('data-viewport');
    const adminOverflow = await assertNoHorizontalOverflow(adminPage, `admin ${name}`);
    responsive[name] = { workspace_shell_viewport: wsViewport, admin_shell_viewport: adminViewport, workspace: wsOverflow, admin: adminOverflow };
    if (name === 'narrow' || name === 'desktop') {
      captures.push(await screenshot(workspacePage, 'P13-T025', `workspace-${name}`));
      captures.push(await screenshot(adminPage, 'P13-T025', `admin-${name}`));
    }
    if (name === 'narrow') assert(wsViewport === 'mobile' && adminViewport === 'mobile', `320px did not inherit mobile shell authority: ${wsViewport}/${adminViewport}`);
    await context.close();
  }

  const reducedContext = await browser.newContext({ viewport: viewports.desktop, reducedMotion: 'reduce', deviceScaleFactor: 1 });
  const reducedPage = await reducedContext.newPage();
  await reducedPage.goto(`${WORKSPACE_OWNER_URL}/app/billing`, { waitUntil: 'networkidle' });
  await waitWorkspace(reducedPage);
  const reduced = await reducedPage.evaluate(() => ({
    media: matchMedia('(prefers-reduced-motion: reduce)').matches,
    buttonAnimation: getComputedStyle(document.querySelector('[data-page="workspace-billing"] button') ?? document.body).animationDuration,
    buttonTransition: getComputedStyle(document.querySelector('[data-page="workspace-billing"] button') ?? document.body).transitionDuration,
  }));
  assert(reduced.media, 'reduced-motion emulation not active');
  assert(reduced.buttonAnimation === '0s' || reduced.buttonAnimation === '0.001s', `billing reduced animation=${reduced.buttonAnimation}`);
  await reducedContext.close();

  const focusContext = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1 });
  const focusPage = await focusContext.newPage();
  const positiveReport = diagnostics(); attachDiagnostics(focusPage, positiveReport);
  await focusPage.goto(`${WORKSPACE_OWNER_URL}/app/billing`, { waitUntil: 'networkidle' });
  await waitWorkspace(focusPage);
  const planSelect = focusPage.locator('.billing-plan-action').getByRole('combobox');
  await planSelect.selectOption(String(planId));
  await planSelect.focus();
  await focusPage.keyboard.press('Tab');
  const focus = await focusPage.evaluate(() => {
    const el = document.activeElement;
    const style = el instanceof HTMLElement ? getComputedStyle(el) : null;
    return { tag: el?.tagName ?? '', text: el?.textContent?.trim() ?? '', outlineWidth: style?.outlineWidth ?? '', outlineStyle: style?.outlineStyle ?? '' };
  });
  assert(focus.tag === 'BUTTON' && /Create order/i.test(focus.text), `keyboard focus did not reach Create order: ${JSON.stringify(focus)}`);
  assert(focus.outlineWidth !== '0px' && focus.outlineStyle !== 'none', `keyboard focus indicator missing: ${JSON.stringify(focus)}`);
  assertCleanDiagnostics(positiveReport, 'P13-T025 positive keyboard surface');
  await focusContext.close();

  const viewerContext = await browser.newContext({ viewport: viewports.desktop });
  const viewerPage = await viewerContext.newPage();
  await viewerPage.goto(`${WORKSPACE_VIEWER_URL}/app/billing`, { waitUntil: 'networkidle' });
  await viewerPage.getByText(/Billing summary and financial records are restricted/i).waitFor();
  assert(await viewerPage.getByRole('button', { name: 'Create order' }).count() === 0, 'viewer exposed billing mutation');
  const viewerSummary = await viewerApi(`/api/workspaces/${WORKSPACE}/billing`);
  assert(viewerSummary.status === 403, `viewer billing summary status=${viewerSummary.status}`);
  await viewerContext.close();

  const unauthContext = await browser.newContext({ viewport: viewports.desktop });
  const unauthPage = await unauthContext.newPage();
  await unauthPage.goto(`${WORKSPACE_UNAUTH_URL}/app/billing`, { waitUntil: 'networkidle' });
  await unauthPage.getByText(/Billing authentication context is unavailable/i).waitFor();
  const unauthApi = await fetch(`${PLATFORM_URL}/api/workspaces/${WORKSPACE}/billing`);
  assert(unauthApi.status === 401, `unauthenticated billing API status=${unauthApi.status}`);
  await unauthContext.close();

  const deniedContext = await browser.newContext({ viewport: viewports.desktop });
  const deniedAdminPage = await deniedContext.newPage();
  await deniedAdminPage.goto(`${ADMIN_DENIED_URL}/admin/commerce/plans`, { waitUntil: 'networkidle' });
  await deniedAdminPage.getByText(/Plans could not be loaded/i).waitFor();
  const deniedAdmin = await api('/api/admin/plans', { actor: DENIED_ADMIN, email: DENIED_ADMIN_EMAIL });
  assert(deniedAdmin.status === 403, `forged/non-authoritative admin status=${deniedAdmin.status}`);
  const unauthAdminPage = await deniedContext.newPage();
  await unauthAdminPage.goto(`${ADMIN_UNAUTH_URL}/admin/commerce/plans`, { waitUntil: 'networkidle' });
  await unauthAdminPage.getByText(/billing.manage authority is unavailable/i).waitFor();
  await deniedContext.close();

  const workspaceHtml = await fetch(`${WORKSPACE_OWNER_URL}/app/billing`);
  const workspaceHtmlText = await workspaceHtml.text();
  const adminHtml = await fetch(`${ADMIN_URL}/admin/commerce/plans`);
  const adminHtmlText = await adminHtml.text();
  for (const [label, response, html] of [['workspace', workspaceHtml, workspaceHtmlText], ['admin', adminHtml, adminHtmlText]]) {
    assert((response.headers.get('cache-control') ?? '').includes('no-store'), `${label} HTML missing no-store`);
    assert((response.headers.get('x-robots-tag') ?? '').toLowerCase().includes('noindex'), `${label} HTML missing X-Robots-Tag noindex`);
    assert(/<meta\s+name="robots"\s+content="noindex,nofollow"/i.test(html), `${label} HTML missing robots meta`);
  }
  const workspaceApi = await api(`/api/workspaces/${WORKSPACE}/billing`);
  const adminApiResult = await adminApi('/api/admin/plans');
  for (const [label, result] of [['workspace API', workspaceApi], ['admin API', adminApiResult]]) {
    assert(result.status === 200, `${label} status=${result.status}`);
    assert((result.headers['cache-control'] ?? '').includes('no-store'), `${label} missing no-store`);
    assert((result.headers['x-robots-tag'] ?? '').toLowerCase().includes('noindex'), `${label} missing noindex`);
  }

  const offlineContext = await browser.newContext({ viewport: viewports.desktop });
  const offlinePage = await offlineContext.newPage();
  const pattern = `**/api/workspaces/${WORKSPACE}/billing`;
  await offlinePage.route(pattern, (route) => route.abort('failed'));
  await offlinePage.goto(`${WORKSPACE_OWNER_URL}/app/billing`, { waitUntil: 'domcontentloaded' });
  await offlinePage.waitForFunction(() => document.querySelector('[data-page="workspace-billing"]')?.getAttribute('data-state') === 'error');
  captures.push(await screenshot(offlinePage, 'P13-T025', 'offline'));
  await offlinePage.unroute(pattern);
  await offlinePage.reload({ waitUntil: 'networkidle' });
  await offlinePage.waitForFunction(() => document.querySelector('[data-page="workspace-billing"]')?.getAttribute('data-state') === 'active');
  captures.push(await screenshot(offlinePage, 'P13-T025', 'recovered'));
  await offlineContext.close();

  return {
    responsive,
    reduced_motion: reduced,
    keyboard_focus: focus,
    authorization: { viewer_summary: viewerSummary.status, unauthenticated_api: unauthApi.status, denied_admin: deniedAdmin.status },
    private_surface_headers: { workspace: true, admin: true, workspace_api: true, admin_api: true },
    offline_recovery: true,
    captures,
  };
}
