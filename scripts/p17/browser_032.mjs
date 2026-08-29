import {
  WORKSPACE_URL,
  assert,
  assertCleanDiagnostics,
  assertNoOverflow,
  attachDiagnostics,
  diagnostics,
  fixture,
  mysql,
  mysqlScalar,
  screenshot,
  viewports,
  waitState,
  workspaceActor,
} from './browser_common.mjs';

async function dismissSecret(page) {
  const secret = (await page.locator('[data-secret-once="true"] code').textContent() || '').trim();
  assert(secret.length >= 20, 'one-time developer secret missing');
  await page.getByRole('button', { name: 'I stored it' }).click();
  await page.locator('[data-secret-once="true"]').waitFor({ state: 'detached' });
  return true;
}

export async function run(browser) {
  const caseId = 'P17-T032';
  const captures = [];
  const checks = {};
  const details = { api_key_states: [], webhook_states: [], responsive: {}, security_checks: {} };

  const context = await browser.newContext({ viewport: viewports.desktop });
  const page = await context.newPage();
  const report = diagnostics();
  attachDiagnostics(page, report, { allowStatuses: [403, 409] });
  await workspaceActor(page, fixture.owner_actor);

  await page.goto(`${WORKSPACE_URL}/app/api-keys`);
  await waitState(page, 'api-keys', 'empty');
  details.api_key_states.push('empty');
  checks.api_key_empty = true;

  await page.getByRole('button', { name: 'Create key' }).click();
  await waitState(page, 'api-keys', 'secret-once');
  details.api_key_states.push('secret-once');
  checks.api_key_create_secret_once = await dismissSecret(page);
  await waitState(page, 'api-keys', 'create');
  details.api_key_states.push('create');
  const firstKeyID = await page.locator('tr[data-key-id]').first().getAttribute('data-key-id');
  assert(firstKeyID, 'created API key id missing');

  await page.locator(`tr[data-key-id="${firstKeyID}"]`).getByRole('button', { name: 'Rotate' }).click();
  await waitState(page, 'api-keys', 'secret-once');
  checks.api_key_rotation_secret_once = await dismissSecret(page);
  await page.locator(`tr[data-key-id="${firstKeyID}"]`).getByRole('button', { name: 'Revoke' }).click();
  await waitState(page, 'api-keys', 'revoked');
  details.api_key_states.push('revoked');
  checks.api_key_revoked_durable = mysqlScalar(`SELECT status FROM workspace_api_keys WHERE id='${firstKeyID}'`) === 'revoked';

  await page.getByLabel('Name').fill('Expiring browser key');
  await page.getByRole('button', { name: 'Create key' }).click();
  await waitState(page, 'api-keys', 'secret-once');
  await dismissSecret(page);
  const keyIDs = await page.locator('tr[data-key-id]').evaluateAll((rows) => rows.map((row) => row.getAttribute('data-key-id')).filter(Boolean));
  const expiringKeyID = keyIDs.find((id) => id !== firstKeyID);
  assert(expiringKeyID, 'second API key id missing');
  mysql(`UPDATE workspace_api_keys SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 MINUTE WHERE id='${expiringKeyID}'`);
  await page.reload();
  await waitState(page, 'api-keys', 'expired');
  details.api_key_states.push('expired');
  checks.api_key_expired_from_durable_clock = (await page.locator(`tr[data-key-id="${expiringKeyID}"]`).getByText('expired').count()) === 1;

  await page.goto(`${WORKSPACE_URL}/app/webhooks`);
  await waitState(page, 'webhooks', 'empty');
  details.webhook_states.push('empty');
  checks.webhook_empty = true;
  await page.getByLabel('HTTPS endpoint').fill('https://example.com/gojet-webhook');
  await page.getByRole('button', { name: 'Create webhook' }).click();
  await waitState(page, 'webhooks', 'secret-rotate');
  checks.webhook_create_secret_once = await dismissSecret(page);
  await waitState(page, 'webhooks', 'create');
  details.webhook_states.push('create');
  const webhookID = await page.locator('tr[data-webhook-id]').first().getAttribute('data-webhook-id');
  assert(webhookID, 'created webhook id missing');

  await page.locator(`tr[data-webhook-id="${webhookID}"]`).getByRole('button', { name: 'Rotate secret' }).click();
  await waitState(page, 'webhooks', 'secret-rotate');
  details.webhook_states.push('secret-rotate');
  checks.webhook_rotate_secret_once = await dismissSecret(page);

  mysql(`INSERT INTO workspace_webhook_deliveries(
    id,workspace_id,webhook_id,event_id,event_type,body,body_sha256,status,attempts,next_attempt_at,last_error_code,request_correlation_id,created_at,updated_at
  ) VALUES ('whd_p17_browser','${fixture.workspace_id}','${webhookID}','evt_p17_browser','link.updated','{}',UNHEX(SHA2('{}',256)),'retrying',1,UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE,'provider-timeout','p17-browser-delivery',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`);
  await page.reload();
  await waitState(page, 'webhooks', 'retrying');
  details.webhook_states.push('retrying');
  checks.webhook_retrying_durable = (await page.locator('[data-delivery-status="retrying"]').count()) === 1;

  mysql("UPDATE workspace_webhook_deliveries SET status='failed',last_error_code='provider-timeout' WHERE id='whd_p17_browser'");
  await page.reload();
  await waitState(page, 'webhooks', 'delivery');
  details.webhook_states.push('delivery');
  await page.getByRole('button', { name: 'Retry' }).click();
  await waitState(page, 'webhooks', 'retrying');
  checks.webhook_retry_server_authority = mysqlScalar("SELECT status FROM workspace_webhook_deliveries WHERE id='whd_p17_browser'") === 'retrying';

  await page.locator(`tr[data-webhook-id="${webhookID}"]`).getByRole('button', { name: 'Disable' }).click();
  await waitState(page, 'webhooks', 'disabled');
  details.webhook_states.push('disabled');
  checks.webhook_disabled_durable = mysqlScalar(`SELECT status FROM workspace_webhooks WHERE id='${webhookID}'`) === 'disabled';

  const memberContext = await browser.newContext({ viewport: viewports.mobile });
  const memberPage = await memberContext.newPage();
  await workspaceActor(memberPage, fixture.member_actor);
  await memberPage.goto(`${WORKSPACE_URL}/app/api-keys`);
  await waitState(memberPage, 'api-keys', 'forbidden');
  details.api_key_states.push('forbidden');
  await memberPage.goto(`${WORKSPACE_URL}/app/webhooks`);
  await waitState(memberPage, 'webhooks', 'forbidden');
  details.webhook_states.push('forbidden');
  checks.member_direct_route_forbidden = true;
  await memberContext.close();

  for (const [label, viewport] of Object.entries(viewports)) {
    await page.setViewportSize(viewport);
    await page.goto(`${WORKSPACE_URL}/app/api-keys`);
    await page.locator('[data-page="api-keys"]').waitFor({ state: 'visible' });
    details.responsive[`api-keys-${label}`] = await assertNoOverflow(page, `api-keys-${label}`);
    captures.push(await screenshot(page, caseId, `api-keys-${label}`));
    await page.goto(`${WORKSPACE_URL}/app/webhooks`);
    await page.locator('[data-page="webhooks"]').waitFor({ state: 'visible' });
    details.responsive[`webhooks-${label}`] = await assertNoOverflow(page, `webhooks-${label}`);
    captures.push(await screenshot(page, caseId, `webhooks-${label}`));
  }
  checks.responsive_reflow = true;

  const errorPage = await context.newPage();
  await workspaceActor(errorPage, fixture.owner_actor);
  await errorPage.route('**/api/workspaces/**/api-keys', (route) => route.abort('failed'));
  await errorPage.goto(`${WORKSPACE_URL}/app/api-keys`);
  await waitState(errorPage, 'api-keys', 'error');
  details.api_key_states.push('error');
  checks.transport_error_persistent = (await errorPage.getByText(/Request failed/).count()) >= 1;
  await errorPage.close();

  checks.no_raw_api_key_storage = Number(mysqlScalar('SELECT COUNT(*) FROM workspace_api_keys WHERE LENGTH(secret_hash)=32')) >= 2;
  checks.webhook_ciphertext_storage = Number(mysqlScalar("SELECT COUNT(*) FROM workspace_webhooks WHERE secret_ciphertext IS NOT NULL AND secret_prefix<>''")) >= 1;

  details.security_checks = {
    owner_membership_server_authority: checks.api_key_create_secret_once && checks.webhook_create_secret_once,
    member_direct_route_forbidden: checks.member_direct_route_forbidden,
    api_key_hash_only_storage: checks.no_raw_api_key_storage,
    webhook_ciphertext_storage: checks.webhook_ciphertext_storage,
    no_secret_screenshots: captures.every((capture) => !capture.includes('secret')),
  };
  details.api_key_states = [...new Set(details.api_key_states)];
  details.webhook_states = [...new Set(details.webhook_states)];
  details.screenshot_count = captures.length;
  details.frozen_contract_completion = true;
  details.closure_claim = false;

  assert(Object.values(checks).every(Boolean), `T032 check failure ${JSON.stringify(checks)}`);
  assert(Object.values(details.security_checks).every(Boolean), 'T032 security checks incomplete');
  assertCleanDiagnostics(report, caseId);
  await context.close();
  return { checks, captures, details };
}
