import { WORKSPACE_URL, ADMIN_URL, INSTALL_URL, INSTALL_FAULT_URL, PLATFORM_URL, WORKSPACE, STORAGE_ROOT, REAL_CLAMD, BENIGN, EICAR, assert, sleep, resetFiles, uploadApi, patchPolicy, action, runWorker, workerPopen, stopProcess, startFault, waitUntil, dbState, holdFilesWriteLock, viewports, capturesDir } from './browser_env.mjs';
import { diagnostics, attachDiagnostics, assertDiagnostics, newPage, gotoWorkspace, waitPageState, layoutEvidence, assertLayout, tabUntil, focusEvidence } from './browser_ui.mjs';

async function publicBinary(context, slug) {
  const response = await context.request.get(`${WORKSPACE_URL}/api/public/files/${encodeURIComponent(slug)}`);
  return { status: response.status(), body: await response.body() };
}

export async function caseT021(browser) {
  resetFiles();
  const report = diagnostics();
  const { context, page } = await newPage(browser, viewports.desktop);
  attachDiagnostics(page, report);

  const listLock = holdFilesWriteLock(3);
  await sleep(500);
  await gotoWorkspace(page, '/app/files', 'domcontentloaded');
  await page.locator('[data-page="files-list"]').waitFor();
  assert(await page.locator('[data-page="files-list"]').getAttribute('data-state') === 'loading', 'APP-FILES did not expose loading while authoritative query was pending');
  await listLock.done;
  await waitPageState(page, '[data-page="files-list"]', 'empty');

  const uploadLock = holdFilesWriteLock(3);
  await sleep(500);
  const input = page.getByLabel('File', { exact: true });
  await input.setInputFiles({ name: 't021-quarantine.txt', mimeType: 'text/plain', buffer: BENIGN });
  const responsePromise = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === `/api/workspaces/${WORKSPACE}/files`);
  await page.getByRole('button', { name: 'Upload privately', exact: true }).click();
  await waitPageState(page, '[data-page="files-list"]', 'uploading');
  const uploadResponse = await responsePromise;
  assert(uploadResponse.status() === 201, `UI upload status ${uploadResponse.status()}`);
  await uploadLock.done;
  const quarantined = await uploadResponse.json();
  await waitPageState(page, '[data-page="file-detail"]', 'quarantined');

  runWorker();
  assert(dbState(quarantined.id) === 'safe', 'initial quarantine fixture did not leave the durable queue through the real worker');
  const safe = await uploadApi('t021-safe.txt');
  runWorker();
  assert(dbState(safe.id) === 'safe', 'real clean file did not become safe');
  const blocked = await uploadApi('t021-eicar.txt', EICAR);
  runWorker();
  assert(dbState(blocked.id) === 'blocked', 'real EICAR file did not become blocked');
  const scanError = await uploadApi('t021-error.txt');
  runWorker('127.0.0.1:39999');
  assert(dbState(scanError.id) === 'scan_error', 'unavailable scanner did not fail closed');
  const scanning = await uploadApi('t021-scanning.txt');
  const fault = await startFault('hold', 33992, 20);
  const worker = workerPopen('127.0.0.1:33992', { GOJET_CLAMAV_SCAN_TIMEOUT: '30s' });
  await waitUntil(() => dbState(scanning.id) === 'scanning', 7000, 'scanning state');
  await action(quarantined.id, 'rescan');
  assert(dbState(quarantined.id) === 'quarantined', 'rescan did not restore quarantined authority for five-state evidence');
  await gotoWorkspace(page, '/app/files');
  await page.locator('.files-list').waitFor();
  const visibleStates = await page.locator('.files-card').evaluateAll((cards) => cards.map((card) => card.getAttribute('data-scan-state')).sort());
  assert(JSON.stringify(visibleStates) === JSON.stringify(['blocked', 'quarantined', 'safe', 'scan_error', 'scanning'].sort()), `APP-FILES states mismatch: ${JSON.stringify(visibleStates)}`);

  await input.setInputFiles({ name: 't021-quota.txt', mimeType: 'text/plain', buffer: BENIGN });
  const quotaResponsePromise = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname === `/api/workspaces/${WORKSPACE}/files`);
  await page.getByRole('button', { name: 'Upload privately', exact: true }).click();
  const quotaResponse = await quotaResponsePromise;
  assert(quotaResponse.status() === 429, `quota upload status ${quotaResponse.status()}`);
  await waitPageState(page, '[data-page="files-list"]', 'quota-reached');
  assert(await page.getByText('Workspace file quota reached.', { exact: false }).isVisible(), 'quota warning missing');

  await stopProcess(worker);
  await stopProcess(fault);
  await context.close();
  assertDiagnostics(report, 'P09-T021', { allowedHttp: [{ includes: `/api/workspaces/${WORKSPACE}/files`, status: 429 }] });
  return { loading: true, empty: true, uploading: true, quarantined_id: quarantined.id, authoritative_states: visibleStates, quota_status: quotaResponse.status(), fake_success_before_server_confirmation: false };
}

export async function caseT022(browser) {
  resetFiles();
  const safe = await uploadApi('t022-protected.txt');
  runWorker();
  await patchPolicy(safe.id, { password: 'P09-browser-password' });
  const report = diagnostics();
  const { context, page } = await newPage(browser, viewports.desktop);
  attachDiagnostics(page, report);
  await gotoWorkspace(page, `/app/files/${safe.id}`);
  await waitPageState(page, '[data-page="file-detail"]', 'safe');
  assert(await page.getByRole('button', { name: 'Publish safe file', exact: true }).isEnabled(), 'safe publish action not available');
  const publishResponsePromise = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith(`/${safe.id}/publish`));
  await page.getByRole('button', { name: 'Publish safe file', exact: true }).click();
  const publishResponse = await publishResponsePromise;
  assert(publishResponse.status() === 200, `publish UI status ${publishResponse.status()}`);
  await page.getByText('Public page:', { exact: false }).waitFor();
  const slug = String((await publishResponse.json()).public_slug);
  const preauth = await publicBinary(context, slug);
  assert(preauth.status === 403 && !preauth.body.equals(BENIGN), `preauth binary status ${preauth.status}`);
  await page.goto(`${WORKSPACE_URL}/f/${encodeURIComponent(slug)}`, { waitUntil: 'networkidle' });
  assert(await page.locator('[data-file-state="password-required"]').isVisible(), 'password-required public page missing');
  assert(!page.url().includes('P09-browser-password'), 'password leaked into public URL');
  await page.getByRole('textbox', { name: 'Password', exact: true }).fill('P09-browser-password');
  await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle' }), page.getByRole('button', { name: 'Continue', exact: true }).click()]);
  assert(await page.locator('[data-file-state="available"]').isVisible(), 'authorized public page did not become available');
  const allowed = await publicBinary(context, slug);
  assert(allowed.status === 200 && allowed.body.equals(BENIGN), `authorized public binary failed status=${allowed.status}`);

  await gotoWorkspace(page, `/app/files/${safe.id}`);
  const rescanResponsePromise = page.waitForResponse((response) => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith(`/${safe.id}/rescan`));
  await page.getByRole('button', { name: 'Rescan', exact: true }).click();
  const rescanResponse = await rescanResponsePromise;
  assert(rescanResponse.status() === 202, `rescan UI status ${rescanResponse.status()}`);
  await waitPageState(page, '[data-page="file-detail"]', 'quarantined');
  await page.goto(`${WORKSPACE_URL}/f/${encodeURIComponent(slug)}`, { waitUntil: 'networkidle' });
  assert(await page.locator('[data-file-state="scan-pending"]').isVisible(), 'rescan did not fail public page closed');
  const rescanBinary = await publicBinary(context, slug);
  assert(rescanBinary.status === 403 && !rescanBinary.body.equals(BENIGN), `rescan binary status ${rescanBinary.status}`);
  runWorker();
  assert(dbState(safe.id) === 'safe', 'rescan generation did not complete clean before the blocked fixture');

  const infected = await uploadApi('t022-blocked.txt', EICAR);
  runWorker();
  await page.goto(`${WORKSPACE_URL}/f/${encodeURIComponent(infected.public_slug)}`, { waitUntil: 'networkidle' });
  assert(await page.locator('[data-file-state="blocked"]').isVisible(), 'blocked public state missing');
  const blockedBinary = await publicBinary(context, infected.public_slug);
  assert(blockedBinary.status === 403 && !blockedBinary.body.equals(EICAR), `blocked binary status ${blockedBinary.status}`);
  await context.close();
  assertDiagnostics(report, 'P09-T022', { allowedHttp: [{ includes: '/api/public/files/', status: 403 }] });
  return { detail_route_state: 'safe', publish_status: publishResponse.status(), public_password_state: 'password-required', preauth_binary_status: preauth.status, authorized_binary_status: allowed.status, rescan_public_state: 'scan-pending', rescan_binary_status: rescanBinary.status, blocked_public_state: 'blocked', blocked_binary_status: blockedBinary.status };
}

export async function caseT023(browser) {
  resetFiles();
  const resource = await uploadApi('t023-layout.txt');
  runWorker();
  const published = await action(resource.id, 'publish');
  const perViewport = {};
  for (const [name, viewport] of Object.entries(viewports)) {
    const report = diagnostics();
    const { context, page } = await newPage(browser, viewport);
    attachDiagnostics(page, report);
    await gotoWorkspace(page, `/app/files/${resource.id}`);
    await waitPageState(page, '[data-page="file-detail"]', 'safe');
    const workspaceLayout = await layoutEvidence(page);
    assertLayout(workspaceLayout, `T023 ${name} Workspace detail`);
    const workspaceCapture = `P09-T023-${name}-workspace.png`;
    await page.screenshot({ path: `${capturesDir}/${workspaceCapture}`, fullPage: true });
    await page.goto(`${WORKSPACE_URL}/f/${encodeURIComponent(published.public_slug)}`, { waitUntil: 'networkidle' });
    assert(await page.locator('[data-file-state="available"]').isVisible(), `${name} public page unavailable`);
    const publicLayout = await layoutEvidence(page);
    assertLayout(publicLayout, `T023 ${name} public page`);
    const publicCapture = `P09-T023-${name}-public.png`;
    await page.screenshot({ path: `${capturesDir}/${publicCapture}`, fullPage: true });
    perViewport[name] = { viewport, workspace: workspaceLayout, public: publicLayout, workspace_capture: workspaceCapture, public_capture: publicCapture };
    await context.close();
    assertDiagnostics(report, `P09-T023 ${name}`);
  }
  return { canonical_viewports: perViewport, root_body_overflow_zero: true, clipped_required_content: false };
}

export async function caseT024(browser) {
  const report = diagnostics();
  const { context, page } = await newPage(browser, viewports.desktop);
  attachDiagnostics(page, report);
  await page.goto(`${ADMIN_URL}/admin/platform/storage`, { waitUntil: 'networkidle' });
  await page.locator('[data-page="admin-storage"]').waitFor();
  assert(await page.locator('[data-page="admin-storage"]').getAttribute('data-state') === 'healthy', 'Admin Files health is not healthy');
  const adminText = await page.locator('[data-page="admin-storage"]').innerText();
  assert(adminText.includes('P17 administrator completion') && adminText.includes('P22 installation closure'), 'later-node ownership disclaimer missing');
  assert(!adminText.includes(STORAGE_ROOT) && !adminText.includes(REAL_CLAMD) && !adminText.includes('GOJET_MYSQL_DSN'), 'Admin health leaked private dependency details');
  await page.screenshot({ path: `${capturesDir}/P09-T024-admin-storage.png`, fullPage: true });
  const installer = {};
  for (const route of ['/install/environment', '/install/services', '/install/health']) {
    const response = await page.goto(`${INSTALL_URL}${route}`, { waitUntil: 'networkidle' });
    assert(response?.status() === 200, `${route} installer status ${response?.status()}`);
    assert(await page.locator('body').getAttribute('data-state') === 'step-pass', `${route} did not consume healthy preflight`);
    installer[route] = { status: response.status(), state: 'step-pass' };
  }
  const failed = await page.goto(`${INSTALL_FAULT_URL}/install/health`, { waitUntil: 'networkidle' });
  assert(failed?.status() === 503, `fault Installer status ${failed?.status()}`);
  assert(await page.locator('body').getAttribute('data-state') === 'hard-failure', 'fault Installer did not fail closed');
  const failureText = await page.locator('body').innerText();
  assert(!failureText.includes('39998') && !failureText.includes(STORAGE_ROOT), 'Installer fault UI leaked private details');
  await page.screenshot({ path: `${capturesDir}/P09-T024-installer-hard-failure.png`, fullPage: true });
  await context.close();
  assertDiagnostics(report, 'P09-T024', { allowedHttp: [{ includes: '127.0.0.1:4177/install/health', status: 503 }] });
  return { admin_route: '/admin/platform/storage', admin_state: 'healthy', installer, fault_installer: { status: failed.status(), state: 'hard-failure' }, p17_completion_claimed: false, p22_completion_claimed: false, private_dependency_detail_leaked: false };
}

export async function caseT025(browser) {
  resetFiles();
  const quarantined = await uploadApi('t025-quarantined.txt');
  runWorker();
  assert(dbState(quarantined.id) === 'safe', 'initial T025 quarantine fixture did not leave the durable queue through the real worker');
  const safe = await uploadApi('t025-safe.txt'); runWorker();
  const blocked = await uploadApi('t025-blocked.txt', EICAR); runWorker();
  const scanError = await uploadApi('t025-error.txt'); runWorker('127.0.0.1:39999');
  const scanning = await uploadApi('t025-scanning.txt');
  const fault = await startFault('hold', 33993, 20);
  const worker = workerPopen('127.0.0.1:33993', { GOJET_CLAMAV_SCAN_TIMEOUT: '30s' });
  await waitUntil(() => dbState(scanning.id) === 'scanning', 7000, 'T025 scanning state');
  await action(quarantined.id, 'rescan');
  assert(dbState(quarantined.id) === 'quarantined', 'T025 rescan did not restore quarantined authority');
  const expected = [
    [quarantined.id, 'quarantined', 'PackageLock', 'File quarantined'],
    [safe.id, 'safe', 'ShieldCheck', 'File is safe to publish'],
    [blocked.id, 'blocked', 'ShieldX', 'File blocked'],
    [scanError.id, 'scan_error', 'TriangleAlert', 'Scan unavailable; file remains private'],
    [scanning.id, 'scanning', 'LoaderCircle', 'Security scan in progress'],
  ];
  const report = diagnostics();
  const { context, page } = await newPage(browser, { width: 320, height: 800 }, { reducedMotion: 'reduce' });
  attachDiagnostics(page, report);
  const stateEvidence = {};
  for (const [id, state, icon, headline] of expected) {
    await gotoWorkspace(page, `/app/files/${id}`);
    await waitPageState(page, '[data-page="file-detail"]', state);
    const status = page.locator(`.file-state[data-state="${state}"]`);
    assert(await status.getByText(headline, { exact: true }).isVisible(), `${state} visible headline missing`);
    assert(await status.locator('.file-state-icon').getAttribute('data-icon') === icon, `${state} icon mismatch`);
    const reason = (await status.locator('p').innerText()).trim();
    assert(reason.length > 0, `${state} reason/next step missing`);
    const layout = await layoutEvidence(page); assertLayout(layout, `T025 ${state} 320px`);
    stateEvidence[state] = { icon, headline, reason, layout };
  }
  await gotoWorkspace(page, `/app/files/${safe.id}`);
  await waitPageState(page, '[data-page="file-detail"]', 'safe');
  const publish = page.getByRole('button', { name: 'Publish safe file', exact: true });
  const tabs = await tabUntil(page, publish, 60);
  const focus = await focusEvidence(publish);
  assert(focus.active, 'Publish action not focused after keyboard traversal');
  assert(focus.outline_width !== '0px' || focus.box_shadow !== 'none', `Publish action has no visible focus treatment: ${JSON.stringify(focus)}`);
  const reduced = await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches);
  assert(reduced, 'reduced-motion browser preference was not active');
  await gotoWorkspace(page, '/app/files');
  await page.locator('.files-list').waitFor();
  const listLayout = await layoutEvidence(page); assertLayout(listLayout, 'T025 320px list reflow');
  await page.screenshot({ path: `${capturesDir}/P09-T025-320-reduced-motion.png`, fullPage: true });
  await stopProcess(worker); await stopProcess(fault); await context.close();
  assertDiagnostics(report, 'P09-T025');
  return { viewport: { width: 320, height: 800 }, reduced_motion: reduced, keyboard_tabs_to_publish: tabs, focus, safety_states: stateEvidence, list_layout: listLayout, color_only_safety_meaning: false };
}