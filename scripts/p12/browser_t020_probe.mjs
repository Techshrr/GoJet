import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const ROOT = process.cwd();
const OWNER_URL = (process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174').replace(/\/$/, '');
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
const WS = 'ws-p12-browser';
const ALT_WS = 'ws-p12-alt';
const OWNER = 'p12-browser-owner';
const OWNER_EMAIL = 'p12-browser-owner@example.test';
const VIEWER = 'p12-browser-viewer';
const VIEWER_EMAIL = 'p12-browser-viewer@example.test';
const runtimeDir = `${ROOT}/artifacts/v10/P12/runtime`;
mkdirSync(runtimeDir, { recursive: true });

const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((path) => existsSync(path));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P12 T020 route probe');

function mysql(sql) {
  return execFileSync('mysql', ['--protocol=tcp', '-h', MYSQL_HOST, '-P', MYSQL_PORT, '-u', MYSQL_USER, '--default-character-set=utf8mb4', '-N', '-B', MYSQL_DATABASE, '-e', sql], {
    encoding: 'utf8', env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
  }).trim();
}

function seed() {
  mysql(`SET FOREIGN_KEY_CHECKS=0;
TRUNCATE TABLE workspace_audit_events;
TRUNCATE TABLE workspace_notifications;
TRUNCATE TABLE workspace_notification_state;
TRUNCATE TABLE workspace_link_tags;
TRUNCATE TABLE workspace_link_organization;
TRUNCATE TABLE workspace_folders;
TRUNCATE TABLE workspace_tags;
TRUNCATE TABLE workspace_campaigns;
TRUNCATE TABLE workspace_organizations;
TRUNCATE TABLE workspace_invitations;
TRUNCATE TABLE workspace_memberships;
TRUNCATE TABLE workspaces;
SET FOREIGN_KEY_CHECKS=1;
INSERT INTO workspaces (id,name,status,version,created_by) VALUES
('${WS}','P12 Browser Primary','active',1,'${OWNER}'),
('${ALT_WS}','P12 Browser Alternate','active',1,'${OWNER}');
INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES
('${WS}','${OWNER}','${OWNER_EMAIL}','P12 Owner','owner'),
('${WS}','${VIEWER}','${VIEWER_EMAIL}','P12 Viewer','viewer'),
('${ALT_WS}','${OWNER}','${OWNER_EMAIL}','P12 Owner','owner');
INSERT INTO workspace_organizations (workspace_id,name,description,version) VALUES
('${WS}','P12 Browser Primary','Primary organization',1),
('${ALT_WS}','P12 Browser Alternate','Alternate organization',1);
INSERT INTO workspace_notification_state (workspace_id,status,data_through_at,state_reason) VALUES
('${WS}','complete',CURRENT_TIMESTAMP(6),'current'),
('${ALT_WS}','complete',CURRENT_TIMESTAMP(6),'current');`);
}

async function main() {
  seed();
  const browser = await chromium.launch({ headless: true, executablePath, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
  const page = await context.newPage();
  const diagnostics = { console_errors: [], page_errors: [], http_errors: [], request_failures: [] };
  page.on('console', (entry) => { if (entry.type() === 'error') diagnostics.console_errors.push(entry.text()); });
  page.on('pageerror', (error) => diagnostics.page_errors.push(String(error)));
  page.on('response', (response) => { if (response.status() >= 400 && !response.url().endsWith('/favicon.ico')) diagnostics.http_errors.push({ status: response.status(), url: response.url() }); });
  page.on('requestfailed', (request) => diagnostics.request_failures.push({ url: request.url(), failure: request.failure() }));

  let navigationStatus = null;
  let success = false;
  let errorText = '';
  try {
    const response = await page.goto(`${OWNER_URL}/app/members`, { waitUntil: 'networkidle' });
    navigationStatus = response?.status() ?? null;
    await page.locator('[data-page="workspace-members"]').waitFor({ state: 'visible', timeout: 7000 });
    success = true;
  } catch (error) {
    errorText = `${error?.name ?? 'Error'}: ${error?.message ?? String(error)}`;
  }

  const bodyText = (await page.locator('body').innerText().catch(() => '')).slice(0, 6000);
  const htmlPrefix = (await page.content().catch(() => '')).slice(0, 12000);
  const evidence = {
    status: success ? 'PASS' : 'FAIL',
    navigation_status: navigationStatus,
    final_url: page.url(),
    title: await page.title().catch(() => ''),
    workspace_page_count: await page.locator('[data-page="workspace-members"]').count().catch(() => -1),
    body_text_prefix: bodyText,
    html_prefix: htmlPrefix,
    diagnostics,
    error: errorText,
  };
  writeFileSync(`${runtimeDir}/P12-T020-route-probe.json`, JSON.stringify(evidence, null, 2) + '\n');
  await context.close();
  await browser.close();
  if (!success) throw new Error(`T020 route probe failed: ${JSON.stringify({ navigation_status: navigationStatus, final_url: evidence.final_url, body_text_prefix: bodyText.slice(0, 1200), diagnostics, error: errorText })}`);
  console.log(JSON.stringify({ case_id: 'P12-T020-route-probe', status: 'PASS', navigation_status: navigationStatus, final_url: evidence.final_url }, null, 2));
}

await main();
