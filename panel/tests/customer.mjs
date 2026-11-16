import {chromium} from 'playwright-core';
import assert from 'node:assert/strict';
import {runtime as execFileSync} from './runtime.mjs';
import {createHmac} from 'node:crypto';
import {mkdir,writeFile} from 'node:fs/promises';
const origin='https://www.linketic.test:11443';
const mode=process.env.CUSTOMER_TEST_MODE||'full';
const email=mode==='full'?'customer@example.com':'recovery@example.com';
const password='Customer-test-password-2026!';
const out='artifacts/panel';await mkdir(out,{recursive:true});
function sql(query){const q=Buffer.from(query).toString('base64');return JSON.parse(execFileSync('docker',['exec','-i','linketic-ci','php8.3'],{input:`<?php $d=json_decode(file_get_contents('/var/lib/linketic/private/mysql.json'),true);$db=new PDO('mysql:unix_socket=/run/mysqld/mysqld.sock;dbname=gojet','root',$d['password'],[PDO::ATTR_ERRMODE=>PDO::ERRMODE_EXCEPTION]);$q=$db->query(base64_decode('${q}'));echo json_encode($q->columnCount()?$q->fetchAll(PDO::FETCH_ASSOC):[]);`,encoding:'utf8'}));}
function messages(){return JSON.parse(execFileSync('docker',['exec','linketic-ci','python3','-c',"import json;from pathlib import Path;p=Path('/tmp/panel-mailbox.jsonl');print(json.dumps([json.loads(x) for x in p.read_text().splitlines()] if p.exists() else []))"],{encoding:'utf8'}));}
async function until(check,timeout=30000){const until=Date.now()+timeout;while(Date.now()<until){const result=await check();if(result)return result;await new Promise(r=>setTimeout(r,500));}throw Error('Timed out waiting for customer flow');}
async function delivered(previous=0){return until(()=>messages().filter(m=>m.to?.includes(email)).slice(previous).map(m=>m.text.match(/(?<![A-Za-z0-9])([0-9]{6})(?![A-Za-z0-9])/)?.[1]).find(Boolean));}
function mailCount(){return messages().filter(m=>m.to?.includes(email)).length;}
function totp(secret){let bits='';for(const c of secret.replace(/=+$/,''))bits+='ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.indexOf(c).toString(2).padStart(5,'0');const key=Buffer.from((bits.match(/.{8}/g)||[]).map(b=>parseInt(b,2)));const counter=Buffer.alloc(8);counter.writeBigUInt64BE(BigInt(Math.floor(Date.now()/30000)));const hash=createHmac('sha1',key).update(counter).digest();const off=hash[19]&15;return ((hash.readUInt32BE(off)&0x7fffffff)%1000000).toString().padStart(6,'0');}
const browser=await chromium.launch({args:['--no-proxy-server','--host-resolver-rules=MAP www.linketic.test 127.0.0.1']});
const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1366,height:1000}});const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
await context.route('https://challenges.cloudflare.com/turnstile/v0/api.js*',route=>route.fulfill({contentType:'application/javascript',body:`const widgets=new Map();window.turnstile={render:(selector,opts)=>{const id=crypto.randomUUID();widgets.set(id,opts);(typeof selector==='string'?document.querySelector(selector):selector).textContent='Security verification fixture';opts.callback('ci-auth-'+crypto.randomUUID());return id;},reset:id=>widgets.get(id)?.callback('ci-auth-'+crypto.randomUUID()),remove:id=>widgets.delete(id)};`}));
async function ready(){await page.getByText('Security verification fixture',{exact:true}).waitFor();}
async function login(pass=password){await page.goto(origin+'/login');await ready();await page.getByLabel('Email',{exact:true}).fill(email);await page.getByLabel('Password',{exact:true}).fill(pass);await page.getByRole('button',{name:'Sign in',exact:true}).click();}
try{
 if(mode!=='after-upgrade'){
  await page.goto(origin+'/register');await ready();await page.getByLabel('Email',{exact:true}).fill(email);await page.getByLabel(mode==='before-upgrade'?'Display name':'Your name',{exact:true}).fill('Customer test');await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Create account',exact:true}).click();if(mode==='before-upgrade')await page.getByText('Account created. Check your email for the verification code.',{exact:true}).waitFor();else await page.getByRole('heading',{name:'Check your email',exact:true}).waitFor();
 }
 if(mode==='before-upgrade'){
  await until(()=>sql(`SELECT attempt_count,status FROM mail_jobs WHERE template_key='auth-email-verification' ORDER BY created_at DESC LIMIT 1`)[0]?.attempt_count>=1);
  assert.equal(messages().filter(m=>m.to?.includes(email)).length,0,'legacy worker must reproduce missing registration mail');
  const users=sql(`SELECT id,status FROM auth_users WHERE email_normalized='${email}'`);assert.equal(users[0].status,'pending_verification');
  // Simulate an expired verification email; do not relax production TTL or rate limits.
  sql(`UPDATE auth_one_time_grants SET created_at=UTC_TIMESTAMP()-INTERVAL 20 MINUTE,expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE user_id='${users[0].id}' AND purpose='email_verification'`);
  execFileSync('docker',['exec','-i','linketic-ci','python3','-c',"from pathlib import Path;import sys;Path('/var/lib/linketic/ci-existing-user.json').write_text(sys.stdin.read())"],{input:JSON.stringify(users[0])});
  await writeFile(out+'/upgrade-before.json',JSON.stringify({passed:true,legacy_registration_mail_missing:true,pending_account_preserved:true}));
 }else{
  let first='';
  if(mode==='full'){
   first=await delivered();assert.match(first,/^[0-9]{6}$/);
   await page.getByRole('button',{name:/Resend in/}).waitFor();
   const users=sql(`SELECT id FROM auth_users WHERE email_normalized='${email}'`);
   sql(`UPDATE auth_one_time_grants SET created_at=UTC_TIMESTAMP()-INTERVAL 20 MINUTE,expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE user_id='${users[0].id}' AND purpose='email_verification'`);
   await page.getByLabel('Verification code',{exact:true}).fill(first);
   await page.getByRole('button',{name:'Verify and continue',exact:true}).click();
   await page.getByRole('alert').waitFor();assert.equal(new URL(page.url()).pathname,'/register');
  }else{
   const old=JSON.parse(execFileSync('docker',['exec','linketic-ci','cat','/var/lib/linketic/ci-existing-user.json'],{encoding:'utf8'}));assert.equal(sql(`SELECT id FROM auth_users WHERE email_normalized='${email}'`)[0].id,old.id);
  }
  await login();await page.getByRole('heading',{name:'Verify your email',exact:true}).waitFor();
  assert.equal(await page.getByText(/unavailable$/).count(),0);
  const count=mailCount();await page.getByRole('button',{name:'Resend code',exact:true}).click();
  const code=await delivered(count);assert.match(code,/^[0-9]{6}$/);
  // Negative attempts are bound to the mailbox and purpose. No challenge bypass in the application.
  async function post(path,body){return page.evaluate(async({path,body})=>{const response=await fetch(path,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json','X-Turnstile-Token':'ci-auth-'+crypto.randomUUID()},body:JSON.stringify({...body,correlation_id:'ci-'+crypto.randomUUID()})});return {status:response.status,body:await response.json()};},{path,body});}
  assert.equal((await post('/api/auth/verifyemail',{email:'wrong@example.com',code})).status,401);
  assert.equal((await post('/api/auth/resetpassword',{email,token:code,password:password+'new'})).status,401);
  const wrong=code==='000000'?'000001':'000000';
  assert.equal((await post('/api/auth/verifyemail',{email,code:wrong})).status,401);
  assert.equal(Number(sql(`SELECT attempt_count FROM auth_one_time_grants WHERE email_normalized='${email}' AND purpose='email_verification' ORDER BY created_at DESC LIMIT 1`)[0].attempt_count),1);
  await page.getByLabel('Verification code',{exact:true}).fill(code);await page.getByRole('button',{name:'Verify and continue',exact:true}).click();
  await page.waitForURL(origin+'/app');
  async function workspace(){
    await until(async()=>await page.getByRole('button',{name:'Create workspace',exact:true}).isVisible()||await page.locator('[data-page="workspace-overview"]').isVisible());
    if(await page.getByRole('button',{name:'Create workspace',exact:true}).isVisible()){
      await page.getByLabel('Workspace name',{exact:true}).fill('Customer workspace');await page.getByRole('button',{name:'Create workspace',exact:true}).click();
    }
    await page.locator('[data-page="workspace-overview"] .p12-metric-grid').waitFor();
    assert.equal((await page.request.get(origin+'/api/me')).status(),200);
    assert.equal(await page.getByRole('alert').count(),0);
  }
  await workspace();await page.reload();await workspace();
  assert.equal(sql(`SELECT status FROM auth_users WHERE email_normalized='${email}'`)[0].status,'active');
  assert.equal((await post('/api/auth/verifyemail',{email,code})).status,401,'a used code cannot sign in again');
  await page.screenshot({path:out+'/customer-'+mode+'.png',fullPage:true});
  await page.goto(origin+'/app/links');await page.locator('[data-page="links-list"]').waitFor();
  assert.equal(await page.getByText('Workspace identity authority is unavailable in this build.',{exact:true}).count(),0);
  await page.screenshot({path:out+'/customer-links.png',fullPage:true});
  await page.goto(origin+'/');await page.getByRole('link',{name:'My workspace',exact:true}).first().waitFor();
  await page.goto(origin+'/app');await workspace();await page.getByRole('button',{name:'Sign out',exact:true}).click();await page.waitForURL(origin+'/login');
  assert.equal((await page.request.get(origin+'/api/me')).status(),401);
  await login();await page.waitForURL(origin+'/app');await workspace();
  const oldCookies=await context.cookies();
  const resetCount=mailCount();await page.goto(origin+'/forgot-password');await ready();await page.getByLabel('Email',{exact:true}).fill(email);await page.getByRole('button',{name:'Send reset code',exact:true}).click();
  const reset=await delivered(resetCount);assert.match(reset,/^[0-9]{6}$/);assert.equal(new URL(page.url()).pathname,'/forgot-password');
  await page.getByLabel('Reset code',{exact:true}).fill(reset);await page.getByLabel('New password',{exact:true}).fill(password+'updated');await page.getByRole('button',{name:'Reset password and continue',exact:true}).click();await page.waitForURL(origin+'/app');await workspace();
  const stale=await browser.newContext({ignoreHTTPSErrors:true});await stale.addCookies(oldCookies);assert.equal((await stale.request.get(origin+'/api/me')).status(),401);await stale.close();
  await page.getByRole('button',{name:'Sign out',exact:true}).click();await page.waitForURL(origin+'/login');
  await login(password+'updated');await page.waitForURL(origin+'/app');await workspace();
  const loginCount=mailCount();await page.goto(origin+'/login');await ready();await page.getByRole('button',{name:'Email code',exact:true}).click();await page.getByLabel('Email',{exact:true}).fill(email);await page.getByRole('button',{name:'Send sign-in code',exact:true}).click();
  let loginCode=await delivered(loginCount);assert.match(loginCode,/^[0-9]{6}$/);
  if(mode==='full'){
    const invalid=loginCode==='000000'?'000001':'000000';
    for(let i=0;i<5;i++)assert.equal((await post('/api/public/login-email-code',{email,code:invalid})).status,401);
    assert.equal(Number(sql(`SELECT attempt_count FROM auth_one_time_grants WHERE email_normalized='${email}' AND purpose='login_email_code' ORDER BY created_at DESC LIMIT 1`)[0].attempt_count),5);
    assert.equal((await post('/api/public/login-email-code',{email,code:loginCode})).status,429);
    sql(`UPDATE auth_one_time_grants SET created_at=UTC_TIMESTAMP()-INTERVAL 2 MINUTE WHERE email_normalized='${email}' AND purpose='login_email_code'`);
    const again=mailCount();assert.equal((await post('/api/public/login-email-code',{email})).status,202);loginCode=await delivered(again);
  }
  await page.getByLabel('Email sign-in code',{exact:true}).fill(loginCode);await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.waitForURL(origin+'/app');await workspace();
  if(mode==='full'){
   await page.goto(origin+'/admin/login');await page.getByLabel('Email',{exact:true}).fill('admin@example.com');await page.getByLabel('Password',{exact:true}).fill('CI-Admin-password-2026!');await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.getByText('Authentication complete.',{exact:true}).waitFor();
   await page.goto(origin+'/admin/platform/general');await page.getByRole('link',{name:'Configure customer sign-in providers',exact:true}).click();await page.getByRole('heading',{name:'OAuth provider configuration'}).waitFor();await page.getByLabel('Client ID',{exact:true}).fill('fixture-google-client');await page.getByLabel('Client secret',{exact:true}).fill('fixture-google-secret-not-real');await page.getByLabel('Authorization URL',{exact:true}).fill('https://accounts.google.com/o/oauth2/v2/auth');await page.getByLabel('Token URL',{exact:true}).fill('https://oauth2.googleapis.com/token');await page.getByLabel('User info URL',{exact:true}).fill('https://openidconnect.googleapis.com/v1/userinfo');await page.getByLabel('Scopes',{exact:true}).fill('openid email profile');await page.getByLabel('Reason for change',{exact:true}).fill('CI customer sign-in configuration');await page.getByRole('button',{name:'Save provider',exact:true}).click();await page.getByText('This change requires a recent two-factor verification.',{exact:false}).waitFor();
   await page.getByRole('link',{name:'Set up two-factor authentication',exact:true}).click();await page.getByRole('button',{name:'Enroll TOTP',exact:true}).click();const secret=await page.locator('[data-secret-once] code').innerText();await page.getByLabel('Six-digit code — TOTP confirmation code').fill(totp(secret));await page.getByRole('button',{name:'Confirm TOTP',exact:true}).click();await page.getByText('TOTP enabled',{exact:true}).waitFor();
   await page.goto(origin+'/admin/login');await page.getByLabel('Email',{exact:true}).fill('admin@example.com');await page.getByLabel('Password',{exact:true}).fill('CI-Admin-password-2026!');await page.getByLabel('TOTP code',{exact:true}).fill(totp(secret));await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.getByText('Authentication complete.',{exact:true}).waitFor();
   await page.goto(origin+'/admin/platform/oauth');await page.getByLabel('Client ID',{exact:true}).fill('fixture-google-client');await page.getByLabel('Client secret',{exact:true}).fill('fixture-google-secret-not-real');await page.getByLabel('Authorization URL',{exact:true}).fill('https://accounts.google.com/o/oauth2/v2/auth');await page.getByLabel('Token URL',{exact:true}).fill('https://oauth2.googleapis.com/token');await page.getByLabel('User info URL',{exact:true}).fill('https://openidconnect.googleapis.com/v1/userinfo');await page.getByLabel('Scopes',{exact:true}).fill('openid email profile');await page.getByLabel('Reason for change',{exact:true}).fill('CI customer sign-in configuration');await page.getByRole('button',{name:'Save provider',exact:true}).click();await page.getByText('Provider configuration saved. Client secret remains masked.',{exact:true}).waitFor();assert.equal(await page.getByLabel('Client secret',{exact:true}).inputValue(),'');await page.reload();await page.getByText('Secret status: Configured · masked',{exact:true}).waitFor();assert.equal(await page.getByLabel('Client ID',{exact:true}).inputValue(),'fixture-google-client');await page.screenshot({path:out+'/oauth-configuration.png',fullPage:true});
   await page.goto(origin+'/login');await ready();assert.equal(await page.getByRole('link',{name:'Google',exact:true}).count(),1);assert.equal(await page.getByText(/unavailable$/).count(),0);
   await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:out+'/customer-mobile.png',fullPage:true});
  }
  assert.deepEqual(errors,[]);await writeFile(out+'/customer-'+mode+'.json',JSON.stringify({passed:true,registration_mail:mode==='full',expired_verification_resend:true,existing_account_retained:mode==='after-upgrade',six_digit_codes:true,wrong_mailbox_refused:true,wrong_purpose_refused:true,failed_attempt_charged:true,five_attempt_lockout:mode==='full',replay_refused:true,session_survives_refresh:true,homepage_signed_in:true,workspace_creation:true,logout_revokes_session:true,password_login:true,password_reset_mail:true,password_reset_same_page:true,old_session_revoked:true,email_login_code:true,oauth_configuration:mode==='full',live_external_oauth_login:false}));
 }
}catch(error){await page.screenshot({path:out+'/customer-'+mode+'-failure.png',fullPage:true}).catch(()=>{});await writeFile(out+'/customer-'+mode+'-failure.txt',String(error)+'\n'+await page.locator('body').innerText().catch(()=>''));throw error;}finally{await browser.close();}
