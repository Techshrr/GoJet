import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const ROOT = process.cwd();
const HEAD = process.env.GITHUB_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
const OWNER_URL = (process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174').replace(/\/$/, '');
const VIEWER_URL = (process.env.GOJET_TEST_WORKSPACE_VIEWER_URL ?? 'http://127.0.0.1:4175').replace(/\/$/, '');
const PLATFORM_URL = (process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081').replace(/\/$/, '');
const WORKSPACE = process.env.GOJET_TEST_WORKSPACE_ID ?? 'ws-p10-browser';
const ACTOR = process.env.GOJET_TEST_ACTOR_ID ?? 'p10-browser-owner';
const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
const browserDir = `${ROOT}/artifacts/v10/P10/browser`;
const capturesDir = `${ROOT}/artifacts/v10/P10/captures`;
mkdirSync(browserDir, { recursive: true }); mkdirSync(capturesDir, { recursive: true });

const variables = JSON.parse(readFileSync(`${ROOT}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
function parseViewport(value, name) {
  const match = /^(\d+)×(\d+)$/.exec(String(value));
  if (!match) throw new Error(`Invalid ${name}: ${value}`);
  return { width: Number(match[1]), height: Number(match[2]) };
}
const viewports = {
  desktop: parseViewport(variables['viewport.desktop'].dimensions, 'desktop viewport'),
  tablet: parseViewport(variables['viewport.tablet'].dimensions, 'tablet viewport'),
  mobile: parseViewport(variables['viewport.mobile'].dimensions, 'mobile viewport'),
};
const expected = { desktop:{width:1440,height:900}, tablet:{width:1024,height:768}, mobile:{width:390,height:844} };
for (const name of Object.keys(expected)) if (JSON.stringify(viewports[name]) !== JSON.stringify(expected[name])) throw new Error(`P10 ${name} viewport drift`);
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((path) => existsSync(path));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P10 browser evidence');

function assert(condition, message) { if (!condition) throw new Error(message); }
function mysql(sql) {
  return execFileSync('mysql', ['--protocol=tcp','-h',MYSQL_HOST,'-P',MYSQL_PORT,'-u',MYSQL_USER,'-N','-B',MYSQL_DATABASE,'-e',sql], { encoding:'utf8', env:{...process.env, MYSQL_PWD:MYSQL_PASSWORD} }).trim();
}
function resetText() {
  mysql('SET FOREIGN_KEY_CHECKS=0; TRUNCATE TABLE text_audit_events; TRUNCATE TABLE text_shares; TRUNCATE TABLE text_workspace_counters; SET FOREIGN_KEY_CHECKS=1;');
}
function authHeaders(role='owner') { return {'Accept':'application/json','Content-Type':'application/json','X-GoJet-Test-Actor':ACTOR,'X-GoJet-Test-Workspace':WORKSPACE,'X-GoJet-Test-Workspace-Role':role}; }
async function api(path, init={}) {
  const response = await fetch(`${PLATFORM_URL}${path}`, {...init, headers:{...authHeaders(), ...(init.headers ?? {})}});
  const type = response.headers.get('content-type') ?? '';
  const body = response.status === 204 ? null : type.includes('application/json') ? await response.json() : await response.text();
  return {response, body};
}
async function createText(overrides={}) {
  const payload = {title:'P10 browser Text',content:'P10 browser route-backed content',visibility:'private',one_time:false,change_reason:'P10 browser fixture',...overrides};
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/text-shares`, {method:'POST',body:JSON.stringify(payload)});
  assert(result.response.status === 201, `create failed ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}
async function updateText(item, overrides={}) {
  const payload = {expected_version:item.version,change_reason:'P10 browser external update',title:item.title,...overrides};
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/text-shares/${item.id}`, {method:'PATCH',body:JSON.stringify(payload)});
  assert(result.response.status === 200, `update failed ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}
async function deleteText(item) {
  const result = await api(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/text-shares/${item.id}`, {method:'DELETE',body:JSON.stringify({expected_version:item.version,change_reason:'P10 browser delete fixture'})});
  assert(result.response.status === 204, `delete failed ${result.response.status}`);
}
function diagnostics() { return {console_errors:[],page_errors:[],http_errors:[],request_failures:[]}; }
function attachDiagnostics(page, report) {
  page.on('console', (message) => { if (message.type()==='error') report.console_errors.push(message.text()); });
  page.on('pageerror', (error) => report.page_errors.push(String(error)));
  page.on('response', (response) => { if (response.status()>=400 && !response.url().endsWith('/favicon.ico')) report.http_errors.push({status:response.status(),url:response.url()}); });
  page.on('requestfailed', (request) => report.request_failures.push({url:request.url(),failure:request.failure()}));
}
function assertDiagnostics(report, label, allowedStatuses=[]) {
  const httpErrors = report.http_errors.filter((entry) => !allowedStatuses.includes(entry.status));
  const consoleErrors = report.console_errors.filter((message) => {
    const match = /status of (\d{3})\b/.exec(message);
    return !match || !allowedStatuses.includes(Number(match[1]));
  });
  assert(consoleErrors.length===0, `${label} console errors ${JSON.stringify(consoleErrors)}`);
  assert(report.page_errors.length===0, `${label} page errors ${JSON.stringify(report.page_errors)}`);
  assert(report.request_failures.length===0, `${label} request failures ${JSON.stringify(report.request_failures)}`);
  assert(httpErrors.length===0, `${label} HTTP errors ${JSON.stringify(httpErrors)}`);
}
async function openPage(browser, base, path, viewport=viewports.desktop, options={}) {
  const context = await browser.newContext({viewport,deviceScaleFactor:1,...options});
  const page = await context.newPage(); const report=diagnostics(); attachDiagnostics(page,report);
  await page.goto(`${base}${path}`, {waitUntil:'networkidle'});
  return {context,page,report};
}
async function waitState(page, selector, state) {
  await page.locator(selector).waitFor();
  await page.waitForFunction(([s,v]) => document.querySelector(s)?.getAttribute('data-state')===v, [selector,state]);
}
async function layout(page) {
  return page.evaluate(() => ({
    viewport:{width:innerWidth,height:innerHeight},
    root_overflow_px:Math.max(0,document.documentElement.scrollWidth-document.documentElement.clientWidth),
    body_overflow_px:Math.max(0,document.body.scrollWidth-document.body.clientWidth),
    clipped:[...document.querySelectorAll('main h1,main h2,main button,main a,main label,main dd,main code')].filter((node)=>node instanceof HTMLElement&&node.offsetParent!==null&&node.clientWidth>0&&node.scrollWidth>node.clientWidth+1).map((node)=>({tag:node.tagName,text:node.textContent?.trim().slice(0,80),clientWidth:node.clientWidth,scrollWidth:node.scrollWidth})),
  }));
}
function assertLayout(value,label){assert(value.root_overflow_px===0&&value.body_overflow_px===0,`${label} root/body overflow ${JSON.stringify(value)}`);assert(value.clipped.length===0,`${label} clipped ${JSON.stringify(value.clipped)}`);}
function writeResult(caseId,status,details,errors=[]){writeFileSync(`${browserDir}/${caseId}.json`,JSON.stringify({node:'P10',case_id:caseId,status,implementation_commit:HEAD,generated_at:new Date().toISOString(),environment:{browser:executablePath,workspace_owner:OWNER_URL,workspace_viewer:VIEWER_URL,platformapi:PLATFORM_URL,mysql:`${MYSQL_HOST}:${MYSQL_PORT}/${MYSQL_DATABASE}`,canonical_viewports:viewports,authority:'real built owner/viewer Workspace + native Go platformapi + real MySQL; no request interception or fixture-only browser success'},details,errors},null,2)+'\n');}
async function screenshot(page,name){const path=`${capturesDir}/${name}.png`;await page.screenshot({path,fullPage:true});return path.replace(`${ROOT}/`,'');}

async function caseT016(browser){
  resetText();
  const evidence={};
  let opened=await openPage(browser,OWNER_URL,'/app/text');
  await waitState(opened.page,'[data-page="text-list"]','empty'); evidence.empty=true; assertDiagnostics(opened.report,'T016 empty'); await opened.context.close();

  opened=await openPage(browser,OWNER_URL,'/app/text');
  await opened.page.getByLabel('Title').fill('Created from browser');
  await opened.page.getByLabel('Content').fill('Route-backed Text browser creation');
  await opened.page.getByLabel('Visibility').selectOption('public');
  await opened.page.getByRole('button',{name:'Create Text share'}).click();
  await opened.page.waitForURL(/\/app\/text\/\d+$/); evidence.created_url=opened.page.url(); assertDiagnostics(opened.report,'T016 create'); await opened.context.close();

  opened=await openPage(browser,VIEWER_URL,'/app/text');
  await waitState(opened.page,'[data-page="text-list"]','read-only');
  assert(await opened.page.getByRole('heading',{name:'Create Text share'}).count()===0,'read-only list exposed create form'); evidence.read_only=true; await opened.context.close();

  await createText({title:'Quota second'});
  opened=await openPage(browser,OWNER_URL,'/app/text'); await waitState(opened.page,'[data-page="text-list"]','quota-reached'); evidence.quota=true; await opened.context.close();

  mysql('RENAME TABLE text_shares TO text_shares_p10_fault');
  try { opened=await openPage(browser,OWNER_URL,'/app/text'); await waitState(opened.page,'[data-page="text-list"]','error'); evidence.error=true; assertDiagnostics(opened.report,'T016 controlled error',[500,502]); await opened.context.close(); }
  finally { mysql('RENAME TABLE text_shares_p10_fault TO text_shares'); }
  return evidence;
}

async function caseT017(browser){
  resetText(); const base=await createText({title:'Detail editable',visibility:'public'}); const evidence={};
  let opened=await openPage(browser,OWNER_URL,`/app/text/${base.id}`); await waitState(opened.page,'[data-page="text-detail"]','edit');
  const preview=opened.page.getByRole('link',{name:'Preview public state'}); assert(await preview.count()===1,'preview link missing'); evidence.preview_href=await preview.getAttribute('href'); assertDiagnostics(opened.report,'T017 edit'); await opened.context.close();
  const publicResponse=await fetch(`${PLATFORM_URL}/t/${encodeURIComponent(base.public_slug)}`); evidence.public_preview_status=publicResponse.status; assert(publicResponse.status===200,'public preview not 200');

  opened=await openPage(browser,VIEWER_URL,`/app/text/${base.id}`); await waitState(opened.page,'[data-page="text-detail"]','read-only');
  assert(await opened.page.getByRole('button',{name:'Save current version'}).isDisabled(),'viewer save enabled'); assert(await opened.page.getByRole('button',{name:'Delete Text share'}).isDisabled(),'viewer delete enabled'); evidence.read_only=true; await opened.context.close();

  opened=await openPage(browser,OWNER_URL,`/app/text/${base.id}`); await waitState(opened.page,'[data-page="text-detail"]','edit');
  await updateText(base,{title:'External version'}); await opened.page.getByLabel('Title').fill('Stale browser edit'); await opened.page.getByRole('button',{name:'Save current version'}).click(); await waitState(opened.page,'[data-page="text-detail"]','conflict'); evidence.conflict=true; await opened.context.close();

  const expired=await createText({title:'Expired detail',visibility:'public',expires_at:new Date(Date.now()-60000).toISOString()}); opened=await openPage(browser,OWNER_URL,`/app/text/${expired.id}`); await waitState(opened.page,'[data-page="text-detail"]','expired'); evidence.expired=true; await opened.context.close();
  await deleteText(expired); evidence.expired_fixture_removed=true;
  const deleted=await createText({title:'Deleted detail'}); await deleteText(deleted); opened=await openPage(browser,OWNER_URL,`/app/text/${deleted.id}`); await waitState(opened.page,'[data-page="text-detail"]','deleted'); evidence.deleted=true; assertDiagnostics(opened.report,'T017 deleted',[410]); await opened.context.close();

  mysql('RENAME TABLE text_shares TO text_shares_p10_fault');
  try { opened=await openPage(browser,OWNER_URL,`/app/text/${base.id}`); await waitState(opened.page,'[data-page="text-detail"]','error'); evidence.error=true; assertDiagnostics(opened.report,'T017 controlled error',[500,502]); await opened.context.close(); }
  finally { mysql('RENAME TABLE text_shares_p10_fault TO text_shares'); }
  return evidence;
}

async function caseT018(browser){
  resetText(); const item=await createText({title:'Responsive public Text',content:'Responsive and accessible public Text content.',visibility:'public'}); const captures=[]; const layouts=[];
  for (const [name,viewport] of Object.entries(viewports)) {
    for (const [surface,base,path] of [['list',OWNER_URL,'/app/text'],['detail',OWNER_URL,`/app/text/${item.id}`],['public',PLATFORM_URL,`/t/${item.public_slug}`]]) {
      const opened=await openPage(browser,base,path,viewport); const value=await layout(opened.page); assertLayout(value,`${name} ${surface}`); layouts.push({name,surface,...value}); captures.push(await screenshot(opened.page,`P10-T018-${name}-${surface}`)); await opened.context.close();
    }
  }
  for (const [surface,base,path] of [['detail',OWNER_URL,`/app/text/${item.id}`],['public',PLATFORM_URL,`/t/${item.public_slug}`]]) {
    const opened=await openPage(browser,base,path,{width:320,height:800}); const value=await layout(opened.page); assertLayout(value,`320px ${surface}`); layouts.push({name:'reflow-320',surface,...value}); captures.push(await screenshot(opened.page,`P10-T018-reflow-320-${surface}`)); await opened.context.close();
  }
  const keyboard=await openPage(browser,OWNER_URL,'/app/text',viewports.desktop); const target=keyboard.page.getByLabel('Title');
  for(let count=0;count<40 && !(await target.evaluate((node)=>node===document.activeElement));count+=1) await keyboard.page.keyboard.press('Tab');
  assert(await target.evaluate((node)=>node===document.activeElement),'Text create title is not keyboard reachable');
  const focus=await target.evaluate((node)=>{const style=getComputedStyle(node);return{outline:style.outlineStyle,outlineWidth:style.outlineWidth,boxShadow:style.boxShadow};});
  assert(focus.outline!=='none'||focus.boxShadow!=='none','keyboard focus has no visible indicator'); await keyboard.context.close();
  const statusPage=await openPage(browser,OWNER_URL,'/app/text'); const statusText=await statusPage.page.locator('.text-status').first().textContent(); assert(Boolean(statusText?.trim()),'resource state has no visible text/non-color meaning'); await statusPage.context.close();
  const reduced=await openPage(browser,OWNER_URL,`/app/text/${item.id}`,viewports.mobile,{reducedMotion:'reduce'}); assert(await reduced.page.getByRole('heading',{name:'Responsive public Text'}).count()===1,'reduced-motion detail unusable'); await reduced.context.close();
  return {captures,layouts,keyboard_focus:focus,non_color_state_text:statusText?.trim(),reduced_motion_usable:true};
}

const cases={'P10-T016':caseT016,'P10-T017':caseT017,'P10-T018':caseT018};
async function main(){const index=process.argv.indexOf('--case');const id=index>=0?process.argv[index+1]:'all';if(id!=='all'&&!cases[id])throw new Error(`unsupported P10 browser case ${id}`);const browser=await chromium.launch({executablePath,headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});try{for(const caseId of id==='all'?Object.keys(cases):[id]){let details={};const errors=[];try{details=await cases[caseId](browser);}catch(error){errors.push(error instanceof Error?`${error.name}: ${error.message}`:String(error));}writeResult(caseId,errors.length?'FAIL':'PASS',details,errors);if(errors.length)throw new Error(`${caseId}: ${errors.join('; ')}`);console.log(`${caseId} PASS on ${HEAD}`);}}finally{await browser.close();}}
main().catch((error)=>{console.error(error);process.exitCode=1;});
