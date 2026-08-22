import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const root = process.cwd();
const outDir = `${root}/artifacts/v10/P07/overflow-probe`;
mkdirSync(outDir, { recursive: true });
const PLATFORM = process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081';
const WORKSPACE = 'ws-p07-overflow-probe';
const ACTOR = 'p07-overflow-probe';
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
const WORKSPACE_URL = process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174';

function sqlLiteral(value) { return `'${String(value).replaceAll("'", "''")}'`; }
function mysql(sql) {
  return execFileSync('mysql', ['--protocol=tcp', '-h', MYSQL_HOST, '-P', String(MYSQL_PORT), '-u', MYSQL_USER, '-N', '-B', MYSQL_DATABASE, '-e', sql], {
    cwd: root, encoding: 'utf8', env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
  }).trim();
}
function eventID(linkId, sequence) {
  return createHash('sha256').update(`gojet.analytics.click.v1\n${WORKSPACE}\n${linkId}\n${sequence}`).digest('hex');
}
function mysqlDate(date) { return date.toISOString().replace('T', ' ').replace('Z', ''); }
function authHeaders() {
  return {
    Accept: 'application/json', 'Content-Type': 'application/json',
    'X-GoJet-Test-Actor': ACTOR,
    'X-GoJet-Test-Workspace': WORKSPACE,
    'X-GoJet-Test-Workspace-Role': 'owner',
    'X-GoJet-Test-Analytics-Permission': 'allow',
  };
}
async function api(path, init = {}) {
  const response = await fetch(`${PLATFORM}${path}`, { ...init, headers: { ...authHeaders(), ...(init.headers ?? {}) } });
  const body = await response.json();
  if (!response.ok) throw new Error(`${response.status}: ${JSON.stringify(body)}`);
  return body;
}

const link = await api(`/api/workspaces/${WORKSPACE}/links`, {
  method: 'POST',
  body: JSON.stringify({
    hostname: 'go.p07-overflow.test', domain_kind: 'official', code: 'overflow', title: 'P07 overflow probe',
    primary_destination: 'https://example.com/p07-overflow', redirect_status: 302,
    routing: [], ab: [], utm: {}, access: {}, expires_at: null, click_limit: null, one_time: false,
    change_reason: 'P07 overflow probe',
  }),
});
mysql(`INSERT INTO analytics_workspace_state (workspace_id,status,data_through_at,retention_days,state_reason)
  VALUES (${sqlLiteral(WORKSPACE)},'complete',UTC_TIMESTAMP(6),90,'probe')`);
for (let sequence = 1; sequence <= 3; sequence += 1) {
  const occurred = new Date(Date.now() - sequence * 20 * 60_000);
  const id = eventID(link.id, sequence);
  const bucket = new Date(occurred); bucket.setUTCMinutes(0, 0, 0);
  const payload = JSON.stringify({ schema_version: 1, event_type: 'link.click', event_id: id, workspace_id: WORKSPACE, link_id: link.id, click_sequence: sequence, occurred_at: occurred.toISOString(), dimensions: { country_code: 'sg', device: 'mobile', language: 'en-sg', source_hostname: 'source.example', campaign_id: 'probe' } });
  mysql(`
    INSERT INTO analytics_outbox (event_id,workspace_id,link_id,click_sequence,occurred_at,country_code,device,language,source_hostname,campaign_id,payload_json,published_at,published_stream_id,publish_attempts)
      VALUES (${sqlLiteral(id)},${sqlLiteral(WORKSPACE)},${link.id},${sequence},${sqlLiteral(mysqlDate(occurred))},'sg','mobile','en-sg','source.example','probe',${sqlLiteral(payload)},UTC_TIMESTAMP(6),${sqlLiteral(`probe-${sequence}-0`)},1);
    INSERT INTO analytics_events (event_id,workspace_id,link_id,click_sequence,occurred_at,country_code,device,language,source_hostname,campaign_id,stream_id)
      VALUES (${sqlLiteral(id)},${sqlLiteral(WORKSPACE)},${link.id},${sequence},${sqlLiteral(mysqlDate(occurred))},'sg','mobile','en-sg','source.example','probe',${sqlLiteral(`probe-${sequence}-0`)});
    INSERT INTO analytics_hourly_aggregates (workspace_id,link_id,bucket_start,country_code,device,language,source_hostname,campaign_id,clicks)
      VALUES (${sqlLiteral(WORKSPACE)},${link.id},${sqlLiteral(mysqlDate(bucket))},'sg','mobile','en-sg','source.example','probe',1)
      ON DUPLICATE KEY UPDATE clicks=clicks+1;
  `);
}

const variables = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
const match = /^(\d+)×(\d+)$/.exec(String(variables['viewport.mobile'].dimensions));
if (!match) throw new Error('canonical mobile viewport missing');
const viewport = { width: Number(match[1]), height: Number(match[2]) };
const candidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = candidates.find((item) => existsSync(item));
if (!executablePath) throw new Error('Chrome/Chromium missing');
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
const context = await browser.newContext({ viewport, deviceScaleFactor: 1 });
const page = await context.newPage();
await page.goto(`${WORKSPACE_URL}/app/links/${link.id}`, { waitUntil: 'networkidle' });
await page.getByRole('heading', { name: 'P07 overflow probe' }).waitFor();
await page.getByRole('tab', { name: 'Analytics' }).click();
await page.locator('[data-analytics-state="success"]').waitFor();

const probe = await page.evaluate(() => {
  const describe = (node) => {
    const rect = node.getBoundingClientRect();
    const style = getComputedStyle(node);
    return {
      tag: node.tagName.toLowerCase(), id: node.id || null,
      class: typeof node.className === 'string' ? node.className : null,
      text: (node.textContent ?? '').trim().replace(/\s+/g, ' ').slice(0, 100),
      rect: { left: Math.round(rect.left * 100) / 100, right: Math.round(rect.right * 100) / 100, width: Math.round(rect.width * 100) / 100 },
      clientWidth: node.clientWidth, scrollWidth: node.scrollWidth,
      style: { display: style.display, width: style.width, minWidth: style.minWidth, maxWidth: style.maxWidth, boxSizing: style.boxSizing, overflowX: style.overflowX, paddingLeft: style.paddingLeft, paddingRight: style.paddingRight, borderLeftWidth: style.borderLeftWidth, borderRightWidth: style.borderRightWidth },
    };
  };
  const visible = [...document.querySelectorAll('body *')].filter((node) => node instanceof HTMLElement && node.offsetParent !== null);
  const outsideViewport = visible.filter((node) => {
    const rect = node.getBoundingClientRect();
    return rect.right > innerWidth + 0.5 || rect.left < -0.5;
  }).map(describe);
  const internalOverflow = visible.filter((node) => node.scrollWidth > node.clientWidth + 1).map(describe);
  return {
    viewport: { innerWidth, innerHeight },
    documentElement: describe(document.documentElement),
    body: describe(document.body),
    workspaceContent: document.querySelector('.workspace-content') ? describe(document.querySelector('.workspace-content')) : null,
    linksPage: document.querySelector('.links-page') ? describe(document.querySelector('.links-page')) : null,
    tabs: document.querySelector('.links-page .gj-tabs') ? describe(document.querySelector('.links-page .gj-tabs')) : null,
    linksForm: document.querySelector('.links-form') ? describe(document.querySelector('.links-form')) : null,
    tabPanel: document.querySelector('.links-tab-panel') ? describe(document.querySelector('.links-tab-panel')) : null,
    report: document.querySelector('.analytics-report') ? describe(document.querySelector('.analytics-report')) : null,
    tableRegion: document.querySelector('.analytics-table-region') ? describe(document.querySelector('.analytics-table-region')) : null,
    outsideViewport,
    internalOverflow,
  };
});
writeFileSync(`${outDir}/probe.json`, `${JSON.stringify(probe, null, 2)}\n`);
await page.screenshot({ path: `${outDir}/probe.png`, fullPage: true });
console.log(JSON.stringify(probe, null, 2));
await context.close();
await browser.close();
