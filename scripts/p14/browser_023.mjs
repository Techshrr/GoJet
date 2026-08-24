import { execFileSync } from 'node:child_process';
import {
  ADMIN_DENIED_URL, ADMIN_URL, SITE_BAD_TURNSTILE_URL, SITE_URL, WORKSPACE,
  assert, assertCleanDiagnostics, assertNoHorizontalOverflow, attachDiagnostics, diagnostics,
  ensureWorkspace, mysql, mysqlScalar, ownerApi, screenshot, sqlLiteral, tabToAccessibleName,
  unique, viewports,
} from './browser_common.mjs';

const CASE = 'P14-T023';
const TURNSTILE = process.env.GOJET_TEST_SUPPORT_TURNSTILE_TOKEN ?? 'p14-browser-valid-turnstile-token';

async function expectState(page, pageName, state) {
  const locator = page.locator(`[data-page="${pageName}"]`);
  await locator.waitFor({ state: 'visible' });
  await page.waitForFunction(({ pageName, state }) => document.querySelector(`[data-page="${pageName}"]`)?.getAttribute('data-state') === state, { pageName, state });
  return state;
}

async function delayedRoute(page, matcher) {
  let release;
  let seenResolve;
  let handledResolve;
  const gate = new Promise((resolve) => { release = resolve; });
  const seen = new Promise((resolve) => { seenResolve = resolve; });
  const handled = new Promise((resolve) => { handledResolve = resolve; });
  let matched = false;
  const handler = async (route) => {
    if (!matched && matcher(route.request())) {
      matched = true;
      seenResolve();
      await gate;
      await route.continue();
      handledResolve();
      return;
    }
    await route.continue();
  };
  await page.route('**/api/**', handler);
  return { seen, release: () => release(), dispose: async () => { await handled; await page.unroute('**/api/**', handler); } };
}

async function fillContact(page, suffix) {
  await page.getByLabel('Name').fill(`P14 Contact ${suffix}`);
  await page.getByLabel('Email').fill(`p14-${suffix}@example.test`);
  await page.getByLabel('Subject').fill(`Browser contact ${suffix}`);
  await page.getByLabel('Message').fill(`Public contact browser evidence ${suffix}`);
}

async function captureResponsive(page, urls, captures, responsive) {
  for (const [surface, url] of Object.entries(urls)) {
    responsive[surface] = {};
    for (const [name, viewport] of Object.entries(viewports)) {
      await page.setViewportSize(viewport);
      await page.goto(url, { waitUntil: 'networkidle' });
      responsive[surface][name] = await assertNoHorizontalOverflow(page, `${surface} ${name}`);
      captures.push(await screenshot(page, CASE, `responsive-${surface}-${name}`));
    }
  }
}

export async function run(browser) {
  ensureWorkspace();
  assert(mysqlScalar('SELECT COUNT(*) FROM support_tickets') === '0', 'T023 browser DB must start without support tickets');
  assert(mysqlScalar('SELECT COUNT(*) FROM mail_jobs') === '0', 'T023 browser DB must start without mail jobs');

  const captures = [];
  const covered = { web_contact: [], admin_tickets: [], admin_mail: [] };
  const responsive = {};

  const adminContext = await browser.newContext({ viewport: viewports.desktop });
  const ticketsPage = await adminContext.newPage();
  const ticketsDiag = diagnostics();
  attachDiagnostics(ticketsPage, ticketsDiag);
  const ticketListDelay = await delayedRoute(ticketsPage, (request) => request.method() === 'GET' && request.url().endsWith('/api/admin/support/tickets'));
  const adminShellResponse = await ticketsPage.goto(`${ADMIN_URL}/admin/tickets`, { waitUntil: 'domcontentloaded' });
  await ticketListDelay.seen;
  covered.admin_tickets.push(await expectState(ticketsPage, 'admin-tickets', 'loading'));
  captures.push(await screenshot(ticketsPage, CASE, 'admin-tickets-loading'));
  ticketListDelay.release(); await ticketListDelay.dispose();
  covered.admin_tickets.push(await expectState(ticketsPage, 'admin-tickets', 'empty'));
  captures.push(await screenshot(ticketsPage, CASE, 'admin-tickets-empty'));
  const adminHeaders = adminShellResponse ? await adminShellResponse.allHeaders() : {};
  assert((adminHeaders['cache-control'] ?? '').toLowerCase().includes('no-store'), `Admin shell missing no-store: ${adminHeaders['cache-control'] ?? ''}`);
  assert((adminHeaders['x-robots-tag'] ?? '').toLowerCase().includes('noindex'), `Admin shell missing noindex header: ${adminHeaders['x-robots-tag'] ?? ''}`);
  const adminRobots = await ticketsPage.locator('meta[name="robots"]').getAttribute('content');
  assert((adminRobots ?? '').toLowerCase().includes('noindex'), `Admin meta robots missing noindex: ${adminRobots}`);

  const mailPage = await adminContext.newPage();
  const mailDiag = diagnostics();
  attachDiagnostics(mailPage, mailDiag);
  const mailDelay = await delayedRoute(mailPage, (request) => request.method() === 'GET' && request.url().endsWith('/api/admin/mail/queue'));
  await mailPage.goto(`${ADMIN_URL}/admin/mail`, { waitUntil: 'domcontentloaded' });
  await mailDelay.seen;
  covered.admin_mail.push(await expectState(mailPage, 'admin-mail', 'loading'));
  captures.push(await screenshot(mailPage, CASE, 'admin-mail-loading'));
  mailDelay.release(); await mailDelay.dispose();
  covered.admin_mail.push(await expectState(mailPage, 'admin-mail', 'empty'));
  captures.push(await screenshot(mailPage, CASE, 'admin-mail-empty'));

  const siteContext = await browser.newContext({ viewport: viewports.desktop });
  const contactPage = await siteContext.newPage();
  const contactDiag = diagnostics();
  attachDiagnostics(contactPage, contactDiag);
  await contactPage.goto(`${SITE_URL}/contact`, { waitUntil: 'networkidle' });
  covered.web_contact.push(await expectState(contactPage, 'contact', 'input'));
  captures.push(await screenshot(contactPage, CASE, 'contact-input'));
  await contactPage.getByRole('button', { name: 'Send message' }).click();
  covered.web_contact.push(await expectState(contactPage, 'contact', 'validation-error'));
  captures.push(await screenshot(contactPage, CASE, 'contact-validation-error'));
  await fillContact(contactPage, 'success');
  const contactDelay = await delayedRoute(contactPage, (request) => request.method() === 'POST' && request.url().endsWith('/api/public/contact'));
  await contactPage.getByRole('button', { name: 'Send message' }).click();
  await contactDelay.seen;
  covered.web_contact.push(await expectState(contactPage, 'contact', 'submitting'));
  captures.push(await screenshot(contactPage, CASE, 'contact-submitting'));
  contactDelay.release(); await contactDelay.dispose();
  covered.web_contact.push(await expectState(contactPage, 'contact', 'success-persistent'));
  captures.push(await screenshot(contactPage, CASE, 'contact-success-persistent'));
  assert(mysqlScalar(`SELECT COUNT(*) FROM support_tickets WHERE public_contact_id<>''`) === '1', 'successful contact did not create exactly one durable public ticket');
  assert(await contactPage.getByText(/Message received\. Reference:/).count() === 1, 'contact success is not persistently visible in-page');

  const badContactPage = await siteContext.newPage();
  const badContactDiag = diagnostics();
  attachDiagnostics(badContactPage, badContactDiag, { allowStatuses: [400, 429] });
  await badContactPage.goto(`${SITE_BAD_TURNSTILE_URL}/contact`, { waitUntil: 'networkidle' });
  await fillContact(badContactPage, 'bad-verification');
  await badContactPage.getByRole('button', { name: 'Send message' }).click();
  covered.web_contact.push(await expectState(badContactPage, 'contact', 'Turnstile-error'));
  captures.push(await screenshot(badContactPage, CASE, 'contact-turnstile-error'));
  for (let i = 0; i < 2; i += 1) {
    await badContactPage.getByRole('button', { name: 'Send message' }).click();
    await expectState(badContactPage, 'contact', 'Turnstile-error');
  }
  await badContactPage.getByRole('button', { name: 'Send message' }).click();
  covered.web_contact.push(await expectState(badContactPage, 'contact', 'rate-limited'));
  captures.push(await screenshot(badContactPage, CASE, 'contact-rate-limited'));
  assert(mysqlScalar(`SELECT COUNT(*) FROM support_tickets WHERE public_contact_id<>''`) === '1', 'Turnstile/rate failures mutated public contact state');

  execFileSync('redis-cli', ['-h', '127.0.0.1', '-p', '6379', 'FLUSHDB'], { encoding: 'utf8' });
  const ticketResult = await ownerApi('/api/support/tickets', {
    method: 'POST', headers: { 'Idempotency-Key': unique('t023-ticket') },
    body: { workspace_id: WORKSPACE, category: 'general', subject: 'T023 administrator browser ticket', message: 'Requester context for administrator browser evidence.', turnstile_token: TURNSTILE },
  });
  assert(ticketResult.status === 201, `T023 ticket fixture create status=${ticketResult.status}`);
  const ticketId = ticketResult.data?.ticket?.id;
  assert(ticketId && !/^\d+$/.test(ticketId), `T023 ticket fixture id invalid: ${ticketId}`);

  await ticketsPage.reload({ waitUntil: 'networkidle' });
  covered.admin_tickets.push(await expectState(ticketsPage, 'admin-tickets', 'awaiting'));
  captures.push(await screenshot(ticketsPage, CASE, 'admin-tickets-awaiting'));
  mysql(`UPDATE support_tickets SET status='open',closed_at=NULL,updated_at=CURRENT_TIMESTAMP(6) WHERE id=${sqlLiteral(ticketId)}`);
  await ticketsPage.reload({ waitUntil: 'networkidle' });
  covered.admin_tickets.push(await expectState(ticketsPage, 'admin-tickets', 'open'));
  captures.push(await screenshot(ticketsPage, CASE, 'admin-tickets-open'));
  const focusTicket = await tabToAccessibleName(ticketsPage, 'Open ticket');
  assert(focusTicket.tag === 'A', `Admin Open ticket keyboard target is not a link: ${JSON.stringify(focusTicket)}`);

  const detailPage = await adminContext.newPage();
  const detailDiag = diagnostics();
  attachDiagnostics(detailPage, detailDiag);
  await detailPage.goto(`${ADMIN_URL}/admin/tickets/${ticketId}`, { waitUntil: 'networkidle' });
  covered.admin_tickets.push(await expectState(detailPage, 'admin-tickets', 'open'));
  captures.push(await screenshot(detailPage, CASE, 'admin-ticket-detail-open'));
  await detailPage.getByLabel('Admin attachment').setInputFiles({ name: 'blocked-admin.txt', mimeType: 'text/plain', buffer: Buffer.from('p14-admin-blocked') });
  covered.admin_tickets.push(await expectState(detailPage, 'admin-tickets', 'attachment-blocked'));
  assert(await detailPage.getByRole('button', { name: 'Send reply' }).isDisabled(), 'Admin attachment did not fail closed');
  captures.push(await screenshot(detailPage, CASE, 'admin-ticket-attachment-blocked'));
  await detailPage.getByLabel('Admin attachment').setInputFiles([]);
  const replyDelay = await delayedRoute(detailPage, (request) => request.method() === 'POST' && request.url().includes(`/api/admin/support/tickets/${ticketId}/replies`));
  await detailPage.getByLabel('Admin reply').fill('Administrator support reply from T023 browser evidence.');
  await detailPage.getByRole('button', { name: 'Send reply' }).click();
  await replyDelay.seen;
  covered.admin_tickets.push(await expectState(detailPage, 'admin-tickets', 'replying'));
  captures.push(await screenshot(detailPage, CASE, 'admin-ticket-replying'));
  replyDelay.release(); await replyDelay.dispose();
  covered.admin_tickets.push(await expectState(detailPage, 'admin-tickets', 'awaiting'));
  captures.push(await screenshot(detailPage, CASE, 'admin-ticket-awaiting-user'));
  await detailPage.getByRole('button', { name: 'Close ticket' }).click();
  covered.admin_tickets.push(await expectState(detailPage, 'admin-tickets', 'closed'));
  captures.push(await screenshot(detailPage, CASE, 'admin-ticket-closed'));

  const initialSettingsVersion = Number((await mailPage.getByText(/Settings version \d+/).textContent())?.match(/\d+/)?.[0] ?? '0');
  const settingsCheckbox = mailPage.getByLabel('Mail delivery enabled');
  const wasEnabled = await settingsCheckbox.isChecked();
  await settingsCheckbox.setChecked(!wasEnabled);
  await mailPage.getByRole('button', { name: 'Save settings' }).click();
  await mailPage.waitForFunction((version) => {
    const text = [...document.querySelectorAll('p')].map((node) => node.textContent ?? '').find((value) => value.includes('Settings version')) ?? '';
    const current = Number(text.match(/Settings version (\d+)/)?.[1] ?? '0');
    return current > version;
  }, initialSettingsVersion);
  captures.push(await screenshot(mailPage, CASE, 'admin-mail-settings-saved'));

  await mailPage.getByLabel('Test recipient').fill('p14-browser-recipient@example.test');
  await mailPage.getByRole('button', { name: 'Queue test message' }).click();
  covered.admin_mail.push(await expectState(mailPage, 'admin-mail', 'queued'));
  captures.push(await screenshot(mailPage, CASE, 'admin-mail-queued'));
  const mailJobId = mysqlScalar("SELECT id FROM mail_jobs WHERE resource_type='mail_test' ORDER BY created_at DESC,id DESC LIMIT 1");
  assert(mailJobId && !/^\d+$/.test(mailJobId), `mail test job id invalid: ${mailJobId}`);
  assert(await mailPage.getByText('p14-browser-recipient@example.test').count() === 0, 'Admin mail queue exposed recipient value');
  for (const status of ['sending', 'sent', 'failed', 'retrying']) {
    if (status === 'sending') {
      mysql(`UPDATE mail_jobs SET status='sending',attempt_count=CASE WHEN attempt_count=0 THEN 1 ELSE attempt_count END,last_error_code=NULL,next_attempt_at=NULL,claim_token_hash=UNHEX(REPEAT('ab',32)),claim_expires_at=DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 2 MINUTE),updated_at=CURRENT_TIMESTAMP(6) WHERE id=${sqlLiteral(mailJobId)}`);
    } else {
      const errorCode = status === 'failed' ? "'smtp_rejected'" : status === 'retrying' ? "'smtp_transient'" : 'NULL';
      const nextAttempt = status === 'retrying' ? 'DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 1 MINUTE)' : 'NULL';
      mysql(`UPDATE mail_jobs SET status=${sqlLiteral(status)},last_error_code=${errorCode},next_attempt_at=${nextAttempt},claim_token_hash=NULL,claim_expires_at=NULL,updated_at=CURRENT_TIMESTAMP(6) WHERE id=${sqlLiteral(mailJobId)}`);
    }
    await mailPage.reload({ waitUntil: 'networkidle' });
    covered.admin_mail.push(await expectState(mailPage, 'admin-mail', status));
    captures.push(await screenshot(mailPage, CASE, `admin-mail-${status}`));
  }

  const partialPage = await adminContext.newPage();
  const partialDiag = diagnostics();
  attachDiagnostics(partialPage, partialDiag);
  await partialPage.route('**/api/admin/mail/templates', (route) => route.abort('failed'));
  await partialPage.goto(`${ADMIN_URL}/admin/mail`, { waitUntil: 'networkidle' });
  covered.admin_mail.push(await expectState(partialPage, 'admin-mail', 'partial'));
  await partialPage.getByText(/Some mail operational data is unavailable/).waitFor();
  captures.push(await screenshot(partialPage, CASE, 'admin-mail-partial'));
  assert(partialDiag.page_errors.length === 0 && partialDiag.console_errors.length === 0 && partialDiag.request_failures.length >= 1, 'Admin mail partial transport failure was not isolated');

  const deniedContext = await browser.newContext({ viewport: viewports.desktop });
  const deniedTickets = await deniedContext.newPage();
  const deniedTicketsDiag = diagnostics();
  attachDiagnostics(deniedTickets, deniedTicketsDiag, { allowStatuses: [403] });
  await deniedTickets.goto(`${ADMIN_DENIED_URL}/admin/tickets`, { waitUntil: 'networkidle' });
  covered.admin_tickets.push(await expectState(deniedTickets, 'admin-tickets', 'error'));
  await deniedTickets.getByText(/permission does not cover this area/i).first().waitFor();
  captures.push(await screenshot(deniedTickets, CASE, 'admin-tickets-permission-denied'));
  const deniedMail = await deniedContext.newPage();
  const deniedMailDiag = diagnostics();
  attachDiagnostics(deniedMail, deniedMailDiag, { allowStatuses: [403] });
  await deniedMail.goto(`${ADMIN_DENIED_URL}/admin/mail`, { waitUntil: 'networkidle' });
  covered.admin_mail.push(await expectState(deniedMail, 'admin-mail', 'error'));
  await deniedMail.getByText(/permission does not cover this area/i).first().waitFor();
  captures.push(await screenshot(deniedMail, CASE, 'admin-mail-permission-denied'));

  const responsivePage = await adminContext.newPage();
  await captureResponsive(responsivePage, {
    contact: `${SITE_URL}/contact`,
    'admin-tickets': `${ADMIN_URL}/admin/tickets`,
    'admin-mail': `${ADMIN_URL}/admin/mail`,
  }, captures, responsive);
  await responsivePage.emulateMedia({ reducedMotion: 'reduce' });
  await responsivePage.goto(`${ADMIN_URL}/admin/mail`, { waitUntil: 'networkidle' });
  captures.push(await screenshot(responsivePage, CASE, 'admin-mail-reduced-motion'));
  await responsivePage.goto(`${SITE_URL}/contact`, { waitUntil: 'networkidle' });
  const contactFocus = await tabToAccessibleName(responsivePage, 'Send message');
  assert(contactFocus.tag === 'BUTTON', `Contact submit keyboard target is not a button: ${JSON.stringify(contactFocus)}`);
  await responsivePage.goto(`${ADMIN_URL}/admin/mail`, { waitUntil: 'networkidle' });
  await responsivePage.getByLabel('Test recipient').fill('p14-keyboard@example.test');
  const mailFocus = await tabToAccessibleName(responsivePage, 'Queue test message');
  assert(mailFocus.tag === 'BUTTON', `Admin mail test-send keyboard target is not a button: ${JSON.stringify(mailFocus)}`);

  assertCleanDiagnostics(contactDiag, 'public Contact');
  assertCleanDiagnostics(badContactDiag, 'public Contact expected Turnstile/rate responses');
  assertCleanDiagnostics(ticketsDiag, 'Admin tickets');
  assertCleanDiagnostics(detailDiag, 'Admin ticket detail');
  assertCleanDiagnostics(mailDiag, 'Admin mail');
  assertCleanDiagnostics(deniedTicketsDiag, 'Admin tickets denied');
  assertCleanDiagnostics(deniedMailDiag, 'Admin mail denied');

  await Promise.all([contactPage.close(), badContactPage.close(), ticketsPage.close(), detailPage.close(), mailPage.close(), partialPage.close(), deniedTickets.close(), deniedMail.close(), responsivePage.close()]);
  await Promise.all([siteContext.close(), adminContext.close(), deniedContext.close()]);
  return {
    states: covered,
    responsive,
    permission_denial: true,
    public_contact_durable_count: 1,
    mail_recipient_redacted: true,
    p19_final_seo_claim: false,
    screenshot_count: captures.length,
    captures,
  };
}
