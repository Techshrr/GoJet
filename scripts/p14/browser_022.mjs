import {
  OWNER_URL, NO_TURNSTILE_URL, BAD_TURNSTILE_URL, FOREIGN_URL, WORKSPACE, OWNER,
  adminApi, assert, assertCleanDiagnostics, assertNoHorizontalOverflow, attachDiagnostics,
  diagnostics, ensureWorkspace, mysql, mysqlScalar, ownerApi, screenshot, sqlLiteral,
  tabToAccessibleName, unique, viewports,
} from './browser_common.mjs';

const CASE = 'P14-T022';

async function expectState(page, pageName, state) {
  const locator = page.locator(`[data-page="${pageName}"]`);
  await locator.waitFor({ state: 'visible' });
  await page.waitForFunction(({ pageName, state }) => document.querySelector(`[data-page="${pageName}"]`)?.getAttribute('data-state') === state, { pageName, state });
  return state;
}

async function delayedRoute(page, matcher) {
  let release;
  let seenResolve;
  const gate = new Promise((resolve) => { release = resolve; });
  const seen = new Promise((resolve) => { seenResolve = resolve; });
  let matched = false;
  const handler = async (route) => {
    if (!matched && matcher(route.request())) {
      matched = true;
      seenResolve();
      await gate;
      await route.continue();
      return;
    }
    await route.continue();
  };
  await page.route('**/api/**', handler);
  return { seen, release: () => release(), dispose: () => page.unroute('**/api/**', handler) };
}

async function fillNewTicket(page, suffix) {
  await page.getByLabel('Support subject').fill(`Browser support ${suffix}`);
  await page.getByLabel('Support message').fill(`Requester browser message ${suffix}`);
}

async function captureResponsive(page, url, captures, responsive) {
  for (const [name, viewport] of Object.entries(viewports)) {
    await page.setViewportSize(viewport);
    await page.goto(url, { waitUntil: 'networkidle' });
    await assertNoHorizontalOverflow(page, `${name} Support`);
    responsive[name] = await page.evaluate(() => ({ innerWidth: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }));
    captures.push(await screenshot(page, CASE, `responsive-${name}`));
  }
}

export async function run(browser) {
  ensureWorkspace();
  assert(mysqlScalar(`SELECT COUNT(*) FROM support_tickets WHERE workspace_id=${sqlLiteral(WORKSPACE)}`) === '0', 'browser DB must start without Workspace support tickets');

  const captures = [];
  const covered = { app_support: [], app_support_new: [], app_support_thread: [] };
  const ownerContext = await browser.newContext({ viewport: viewports.desktop });
  const ownerPage = await ownerContext.newPage();
  const ownerDiag = diagnostics();
  attachDiagnostics(ownerPage, ownerDiag);

  const listDelay = await delayedRoute(ownerPage, (request) => request.method() === 'GET' && request.url().includes('/api/support/tickets?workspace_id='));
  const shellResponse = await ownerPage.goto(`${OWNER_URL}/app/support`, { waitUntil: 'domcontentloaded' });
  await listDelay.seen;
  covered.app_support.push(await expectState(ownerPage, 'support-list', 'loading'));
  captures.push(await screenshot(ownerPage, CASE, 'support-loading'));
  listDelay.release();
  await listDelay.dispose();
  covered.app_support.push(await expectState(ownerPage, 'support-list', 'empty'));
  captures.push(await screenshot(ownerPage, CASE, 'support-empty'));

  const shellHeaders = shellResponse ? await shellResponse.allHeaders() : {};
  const cacheControl = shellHeaders['cache-control'] ?? '';
  const robotsHeader = shellHeaders['x-robots-tag'] ?? '';
  const robotsMeta = await ownerPage.locator('meta[name="robots"]').getAttribute('content');
  assert(cacheControl.toLowerCase().includes('no-store'), `Workspace shell missing no-store: ${cacheControl}`);
  assert(robotsHeader.toLowerCase().includes('noindex'), `Workspace shell missing X-Robots-Tag noindex: ${robotsHeader}`);
  assert((robotsMeta ?? '').toLowerCase().includes('noindex'), `Workspace meta robots missing noindex: ${robotsMeta}`);
  const focused = await tabToAccessibleName(ownerPage, 'New ticket');
  assert(focused.tag === 'A', `New ticket keyboard target is not a link: ${JSON.stringify(focused)}`);

  const noTokenPage = await ownerContext.newPage();
  await noTokenPage.goto(`${NO_TURNSTILE_URL}/app/support/new`, { waitUntil: 'networkidle' });
  covered.app_support_new.push(await expectState(noTokenPage, 'support-new', 'Turnstile-required'));
  await noTokenPage.getByText('Verification is required before this ticket can be submitted.').waitFor();
  captures.push(await screenshot(noTokenPage, CASE, 'new-turnstile-required'));
  await noTokenPage.close();

  const newPage = await ownerContext.newPage();
  const newDiag = diagnostics();
  attachDiagnostics(newPage, newDiag);
  let createRequests = 0;
  newPage.on('request', (request) => { if (request.method() === 'POST' && request.url().endsWith('/api/support/tickets')) createRequests += 1; });
  await newPage.goto(`${OWNER_URL}/app/support/new?category=custom-domain-access`, { waitUntil: 'networkidle' });
  covered.app_support_new.push(await expectState(newPage, 'support-new', 'input'));
  assert(await newPage.getByLabel('Support category').inputValue() === 'custom-domain-access', 'custom-domain-access query did not select request category');
  await newPage.getByText('This creates a support request only.').waitFor();
  captures.push(await screenshot(newPage, CASE, 'new-input'));
  await newPage.getByLabel('Support category').selectOption('general');
  await fillNewTicket(newPage, 'primary');

  await newPage.getByLabel('Support attachment').setInputFiles({ name: 'blocked-browser.txt', mimeType: 'text/plain', buffer: Buffer.from('p14-browser-blocked') });
  covered.app_support_new.push(await expectState(newPage, 'support-new', 'attachment'));
  assert(await newPage.getByRole('button', { name: 'Submit ticket' }).isDisabled(), 'attachment selection did not fail closed');
  assert(createRequests === 0, 'attachment selection emitted a ticket request');
  await newPage.getByText('has not been uploaded').waitFor();
  captures.push(await screenshot(newPage, CASE, 'new-attachment-blocked'));
  await newPage.getByLabel('Support attachment').setInputFiles([]);

  await ownerContext.setOffline(true);
  await newPage.getByRole('button', { name: 'Submit ticket' }).click();
  covered.app_support_new.push(await expectState(newPage, 'support-new', 'error'));
  await newPage.getByText('Support could not complete this request.').waitFor();
  captures.push(await screenshot(newPage, CASE, 'new-offline-error'));
  assert(mysqlScalar(`SELECT COUNT(*) FROM support_tickets WHERE workspace_id=${sqlLiteral(WORKSPACE)}`) === '0', 'offline submit mutated durable ticket state');
  await ownerContext.setOffline(false);

  const createDelay = await delayedRoute(newPage, (request) => request.method() === 'POST' && request.url().endsWith('/api/support/tickets'));
  await newPage.getByRole('button', { name: 'Submit ticket' }).click();
  await createDelay.seen;
  covered.app_support_new.push(await expectState(newPage, 'support-new', 'submitting'));
  captures.push(await screenshot(newPage, CASE, 'new-submitting'));
  createDelay.release();
  await createDelay.dispose();
  covered.app_support_new.push(await expectState(newPage, 'support-new', 'success'));
  captures.push(await screenshot(newPage, CASE, 'new-success'));
  const openTicketHref = await newPage.getByRole('link', { name: 'Open ticket' }).getAttribute('href');
  assert(openTicketHref?.startsWith('/app/support/'), `missing support detail href: ${openTicketHref}`);
  const ticketId = openTicketHref.split('/').at(-1);
  assert(ticketId && !/^\d+$/.test(ticketId), `ticket id is not opaque: ${ticketId}`);
  assert(mysqlScalar(`SELECT COUNT(*) FROM support_tickets WHERE workspace_id=${sqlLiteral(WORKSPACE)}`) === '1', 'successful browser submit did not create exactly one ticket');

  await ownerPage.goto(`${OWNER_URL}/app/support`, { waitUntil: 'networkidle' });
  covered.app_support.push(await expectState(ownerPage, 'support-list', 'awaiting-support'));
  captures.push(await screenshot(ownerPage, CASE, 'support-awaiting-support'));

  const threadPage = await ownerContext.newPage();
  const threadDiag = diagnostics();
  attachDiagnostics(threadPage, threadDiag);
  const detailDelay = await delayedRoute(threadPage, (request) => request.method() === 'GET' && request.url().includes(`/api/support/tickets/${ticketId}`));
  await threadPage.goto(`${OWNER_URL}/app/support/${ticketId}`, { waitUntil: 'domcontentloaded' });
  await detailDelay.seen;
  covered.app_support_thread.push(await expectState(threadPage, 'support-thread', 'loading'));
  captures.push(await screenshot(threadPage, CASE, 'thread-loading'));
  detailDelay.release();
  await detailDelay.dispose();
  covered.app_support_thread.push(await expectState(threadPage, 'support-thread', 'awaiting'));
  captures.push(await screenshot(threadPage, CASE, 'thread-awaiting-support'));

  await threadPage.getByLabel('Reply attachment').setInputFiles({ name: 'blocked-reply.txt', mimeType: 'text/plain', buffer: Buffer.from('blocked-reply') });
  covered.app_support_thread.push(await expectState(threadPage, 'support-thread', 'attachment-blocked'));
  assert(await threadPage.getByRole('button', { name: 'Send reply' }).isDisabled(), 'thread attachment did not block reply');
  await threadPage.getByText('No file bytes were sent or represented as clean.').waitFor();
  captures.push(await screenshot(threadPage, CASE, 'thread-attachment-blocked'));
  await threadPage.getByLabel('Reply attachment').setInputFiles([]);

  mysql(`UPDATE support_tickets SET status='open',closed_at=NULL,updated_at=CURRENT_TIMESTAMP(6) WHERE id=${sqlLiteral(ticketId)}`);
  await threadPage.reload({ waitUntil: 'networkidle' });
  covered.app_support_thread.push(await expectState(threadPage, 'support-thread', 'open'));
  captures.push(await screenshot(threadPage, CASE, 'thread-open'));
  await ownerPage.reload({ waitUntil: 'networkidle' });
  covered.app_support.push(await expectState(ownerPage, 'support-list', 'open'));
  captures.push(await screenshot(ownerPage, CASE, 'support-open'));

  const replyDelay = await delayedRoute(threadPage, (request) => request.method() === 'POST' && request.url().includes(`/api/support/tickets/${ticketId}/replies`));
  await threadPage.getByLabel('Ticket reply').fill('Requester browser reply after open fixture');
  await threadPage.getByRole('button', { name: 'Send reply' }).click();
  await replyDelay.seen;
  covered.app_support_thread.push(await expectState(threadPage, 'support-thread', 'replying'));
  captures.push(await screenshot(threadPage, CASE, 'thread-replying'));
  replyDelay.release();
  await replyDelay.dispose();
  covered.app_support_thread.push(await expectState(threadPage, 'support-thread', 'awaiting'));

  const internalBody = unique('internal-note-hidden');
  const internalResult = await adminApi(`/api/admin/support/tickets/${ticketId}/replies`, { method: 'POST', body: { kind: 'internal_note', message: internalBody } });
  assert(internalResult.status === 201, `internal note admin response=${internalResult.status}`);
  const supportBody = unique('support-reply-visible');
  const supportResult = await adminApi(`/api/admin/support/tickets/${ticketId}/replies`, { method: 'POST', body: { kind: 'support_reply', message: supportBody } });
  assert(supportResult.status === 201, `support reply admin response=${supportResult.status}`);
  const requesterDetail = await ownerApi(`/api/support/tickets/${ticketId}`);
  assert(requesterDetail.status === 200, `requester detail status=${requesterDetail.status}`);
  assert(requesterDetail.data.ticket.status === 'awaiting_user', `expected awaiting_user, got ${requesterDetail.data.ticket.status}`);
  assert(requesterDetail.data.messages.every((message) => message.kind !== 'internal_note'), 'requester API leaked internal_note kind');
  assert(!requesterDetail.text.includes(internalBody), 'requester API leaked internal note body');
  assert(requesterDetail.text.includes(supportBody), 'requester API omitted support reply');

  await threadPage.reload({ waitUntil: 'networkidle' });
  covered.app_support_thread.push(await expectState(threadPage, 'support-thread', 'awaiting'));
  assert(await threadPage.getByText(supportBody).count() === 1, 'support reply not visible exactly once');
  assert(await threadPage.getByText(internalBody).count() === 0, 'internal note leaked into Workspace thread');
  captures.push(await screenshot(threadPage, CASE, 'thread-awaiting-user'));
  await ownerPage.reload({ waitUntil: 'networkidle' });
  covered.app_support.push(await expectState(ownerPage, 'support-list', 'awaiting-user'));
  captures.push(await screenshot(ownerPage, CASE, 'support-awaiting-user'));

  const messageCountBeforeOffline = Number(mysqlScalar(`SELECT COUNT(*) FROM support_ticket_messages WHERE ticket_id=${sqlLiteral(ticketId)}`));
  await ownerContext.setOffline(true);
  await threadPage.getByLabel('Ticket reply').fill('offline mutation must not be represented as success');
  await threadPage.getByRole('button', { name: 'Send reply' }).click();
  await threadPage.getByText('Support could not complete this request.').waitFor();
  assert(await threadPage.getByText(supportBody).count() === 1, 'offline failure discarded safe prior support context');
  captures.push(await screenshot(threadPage, CASE, 'thread-offline-safe-context'));
  await ownerContext.setOffline(false);
  assert(Number(mysqlScalar(`SELECT COUNT(*) FROM support_ticket_messages WHERE ticket_id=${sqlLiteral(ticketId)}`)) === messageCountBeforeOffline, 'offline reply created a durable message');

  await threadPage.reload({ waitUntil: 'networkidle' });
  await threadPage.getByRole('button', { name: 'Close ticket' }).click();
  covered.app_support_thread.push(await expectState(threadPage, 'support-thread', 'closed'));
  captures.push(await screenshot(threadPage, CASE, 'thread-closed'));
  await ownerPage.reload({ waitUntil: 'networkidle' });
  covered.app_support.push(await expectState(ownerPage, 'support-list', 'closed'));
  captures.push(await screenshot(ownerPage, CASE, 'support-closed'));

  const threadErrorPage = await ownerContext.newPage();
  const threadErrorDiag = diagnostics();
  attachDiagnostics(threadErrorPage, threadErrorDiag);
  await threadErrorPage.route(`**/api/support/tickets/${ticketId}`, (route) => route.abort('failed'));
  await threadErrorPage.goto(`${OWNER_URL}/app/support/${ticketId}`, { waitUntil: 'domcontentloaded' });
  covered.app_support_thread.push(await expectState(threadErrorPage, 'support-thread', 'error'));
  captures.push(await screenshot(threadErrorPage, CASE, 'thread-error'));
  assert(threadErrorDiag.console_errors.length === 0 && threadErrorDiag.page_errors.length === 0 && threadErrorDiag.request_failures.length >= 1, 'thread transport error was not isolated to expected request failure');
  await threadErrorPage.close();

  const foreignContext = await browser.newContext({ viewport: viewports.desktop });
  const foreignPage = await foreignContext.newPage();
  const foreignDiag = diagnostics();
  attachDiagnostics(foreignPage, foreignDiag, { allowStatuses: [403, 404] });
  await foreignPage.goto(`${FOREIGN_URL}/app/support`, { waitUntil: 'networkidle' });
  covered.app_support.push(await expectState(foreignPage, 'support-list', 'error'));
  captures.push(await screenshot(foreignPage, CASE, 'support-foreign-error'));
  await foreignPage.goto(`${FOREIGN_URL}/app/support/${ticketId}`, { waitUntil: 'networkidle' });
  covered.app_support_thread.push(await expectState(foreignPage, 'support-thread', 'forbidden'));
  assert(await foreignPage.getByText(/unavailable for the current Workspace identity/i).count() === 1, 'foreign direct URL did not fail closed');
  assert(await foreignPage.getByText(supportBody).count() === 0, 'foreign thread disclosed requester content');
  captures.push(await screenshot(foreignPage, CASE, 'thread-foreign-forbidden'));
  assert(foreignDiag.console_errors.length === 0 && foreignDiag.page_errors.length === 0 && foreignDiag.request_failures.length === 0, 'foreign denial produced client runtime failure');
  await foreignContext.close();

  const replayPage = await ownerContext.newPage();
  await replayPage.goto(`${OWNER_URL}/app/support/new`, { waitUntil: 'networkidle' });
  await fillNewTicket(replayPage, 'replay');
  await replayPage.getByRole('button', { name: 'Submit ticket' }).click();
  await expectState(replayPage, 'support-new', 'Turnstile-error');
  await replayPage.getByText('Verification was rejected. No ticket or mail was created.').waitFor();
  captures.push(await screenshot(replayPage, CASE, 'new-turnstile-replay'));
  await replayPage.close();

  const badPage = await ownerContext.newPage();
  await badPage.goto(`${BAD_TURNSTILE_URL}/app/support/new`, { waitUntil: 'networkidle' });
  await fillNewTicket(badPage, 'bad-token');
  await badPage.getByRole('button', { name: 'Submit ticket' }).click();
  await expectState(badPage, 'support-new', 'Turnstile-error');
  await badPage.getByRole('button', { name: 'Submit ticket' }).click();
  await expectState(badPage, 'support-new', 'Turnstile-error');
  await badPage.getByRole('button', { name: 'Submit ticket' }).click();
  covered.app_support_new.push(await expectState(badPage, 'support-new', 'rate-limited'));
  captures.push(await screenshot(badPage, CASE, 'new-rate-limited'));
  assert(mysqlScalar(`SELECT COUNT(*) FROM support_tickets WHERE workspace_id=${sqlLiteral(WORKSPACE)}`) === '1', 'Turnstile/replay/rate failures created extra tickets');
  await badPage.close();

  await ownerPage.emulateMedia({ reducedMotion: 'reduce' });
  const reducedMotion = await ownerPage.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches);
  assert(reducedMotion, 'reduced-motion emulation not active');
  const responsive = {};
  await captureResponsive(ownerPage, `${OWNER_URL}/app/support`, captures, responsive);

  const expectedStates = {
    app_support: ['loading', 'empty', 'open', 'awaiting-user', 'awaiting-support', 'closed', 'error'],
    app_support_new: ['input', 'attachment', 'Turnstile-required', 'submitting', 'success', 'rate-limited', 'error'],
    app_support_thread: ['loading', 'open', 'replying', 'awaiting', 'closed', 'forbidden', 'attachment-blocked', 'error'],
  };
  for (const [surface, states] of Object.entries(expectedStates)) {
    for (const state of states) assert(covered[surface].includes(state), `${surface} missing frozen browser state ${state}: ${JSON.stringify(covered[surface])}`);
  }

  assertCleanDiagnostics(ownerDiag, 'owner list');
  assert(newDiag.console_errors.length === 0 && newDiag.page_errors.length === 0 && newDiag.http_errors.length === 0 && newDiag.request_failures.length >= 1, `new-ticket offline diagnostics unexpected: ${JSON.stringify(newDiag)}`);
  assert(threadDiag.console_errors.length === 0 && threadDiag.page_errors.length === 0 && threadDiag.http_errors.length === 0, `thread diagnostics contain unexpected errors: ${JSON.stringify(threadDiag)}`);
  await ownerContext.close();

  return {
    routes: ['/app/support', '/app/support/new', `/app/support/${ticketId}`],
    states: Object.fromEntries(Object.entries(covered).map(([key, values]) => [key, [...new Set(values)]])),
    noindex_no_store: { cache_control: cacheControl, x_robots_tag: robotsHeader, meta_robots: robotsMeta },
    requester_internal_note_isolation: true,
    attachment_http_authority_not_invented: true,
    turnstile_replay_failed_closed: true,
    rate_limit_failed_closed: true,
    offline_prior_context_retained: true,
    foreign_direct_url_failed_closed: true,
    keyboard_focus_target: focused,
    reduced_motion: reducedMotion,
    responsive,
    screenshot_count: captures.length,
    captures,
    exact_ticket_count: 1,
  };
}
