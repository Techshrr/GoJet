import {chromium} from 'playwright-core';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
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
async function delivered(prefix,previous=0){return until(()=>messages().filter(m=>m.to?.includes(email)).slice(previous).map(m=>m.text.match(new RegExp(prefix+'[A-Za-z0-9_-]+'))?.[0]).find(Boolean));}
function totp(secret){let bits='';for(const c of secret.replace(/=+$/,''))bits+='ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.indexOf(c).toString(2).padStart(5,'0');const key=Buffer.from((bits.match(/.{8}/g)||[]).map(b=>parseInt(b,2)));const counter=Buffer.alloc(8);counter.writeBigUInt64BE(BigInt(Math.floor(Date.now()/30000)));const hash=createHmac('sha1',key).update(counter).digest();const off=hash[19]&15;return ((hash.readUInt32BE(off)&0x7fffffff)%1000000).toString().padStart(6,'0');}
const browser=await chromium.launch({args:['--no-proxy-server','--host-resolver-rules=MAP www.linketic.test 127.0.0.1']});
const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1366,height:1000}});const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
await context.route('https://challenges.cloudflare.com/turnstile/v0/api.js*',route=>route.fulfill({contentType:'application/javascript',body:`const widgets=new Map();window.turnstile={render:(selector,opts)=>{const id=crypto.randomUUID();widgets.set(id,opts);(typeof selector==='string'?document.querySelector(selector):selector).textContent='Security verification fixture';opts.callback('ci-auth-'+crypto.randomUUID());return id;},reset:id=>widgets.get(id)?.callback('ci-auth-'+crypto.randomUUID()),remove:id=>widgets.delete(id)};`}));
async function ready(){await page.getByText('Security verification fixture',{exact:true}).waitFor();}
async function login(pass=password){await page.goto(origin+'/login');await ready();await page.getByLabel('Email',{exact:true}).fill(email);await page.getByLabel('Password',{exact:true}).fill(pass);await page.getByRole('button',{name:'Sign in',exact:true}).click();}
try{
 if(mode!=='after-upgrade'){
  await page.goto(origin+'/register');await ready();await page.getByLabel('Email',{exact:true}).fill(email);await page.getByLabel('Display name',{exact:true}).fill('Customer test');await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Create account',exact:true}).click();await page.getByText('Account created. Check your email for the verification code.',{exact:true}).waitFor();
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
  if(mode==='full'){
   const first=await delivered('gvc_');assert(first);
   await page.getByRole('button',{name:/Resend available in/}).waitFor();
   const users=sql(`SELECT id FROM auth_users WHERE email_normalized='${email}'`);
   sql(`UPDATE auth_one_time_grants SET created_at=UTC_TIMESTAMP()-INTERVAL 20 MINUTE,expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE user_id='${users[0].id}' AND purpose='email_verification'`);
  }else{
   const old=JSON.parse(execFileSync('docker',['exec','linketic-ci','cat','/var/lib/linketic/ci-existing-user.json'],{encoding:'utf8'}));assert.equal(sql(`SELECT id FROM auth_users WHERE email_normalized='${email}'`)[0].id,old.id);
  }
  await login();await page.getByText('Email verification is required.',{exact:true}).waitFor();
  assert.equal(await page.getByText(/unavailable$/).count(),0);
  const count=messages().filter(m=>m.to?.includes(email)).length;
  await page.getByRole('button',{name:'Resend verification email',exact:true}).click();await page.getByText('Verification email requested. Check your inbox and spam folder.',{exact:true}).waitFor();
  const code=await delivered('gvc_',count);
  await page.getByRole('link',{name:'Enter verification code',exact:true}).click();await ready();
  await page.getByLabel('Verification code',{exact:true}).fill(code);await page.getByRole('button',{name:'Verify email',exact:true}).click();await page.getByText('Email verified successfully.',{exact:true}).waitFor();
  await login();await page.getByText('Signed in successfully.',{exact:false}).waitFor();
  assert.equal(sql(`SELECT status FROM auth_users WHERE email_normalized='${email}'`)[0].status,'active');
  await page.screenshot({path:out+'/customer-'+mode+'.png',fullPage:true});
  // Password recovery and email-code login must use the same real worker/SMTP path.
  await page.goto(origin+'/forgot-password');await ready();await page.getByLabel('Email',{exact:true}).fill(email);await page.getByRole('button',{name:'Send reset instructions'}).click();
  const reset=await delivered('grp_');await page.getByRole('link',{name:'Enter password reset code',exact:true}).click();await ready();await page.getByLabel('Password reset code',{exact:true}).fill(reset);
  await page.getByLabel('New password',{exact:true}).fill(password+'updated');await page.getByRole('button',{name:'Reset password',exact:true}).click();await page.getByText('Password reset successfully. You can sign in with the new password.',{exact:true}).waitFor();
  await login(password+'updated');await page.getByText('Signed in successfully.',{exact:false}).waitFor();
  await page.goto(origin+'/login');await ready();await page.getByLabel('Email',{exact:true}).fill(email);await page.getByRole('button',{name:'Send email sign-in code'}).click();const loginCode=await delivered('glc_');await page.getByLabel('Email sign-in code',{exact:true}).fill(loginCode);await page.getByRole('button',{name:'Sign in with email code',exact:true}).click();await page.getByText('Signed in with the one-time email code.',{exact:true}).waitFor();
  if(mode==='full'){
   await page.goto(origin+'/admin/login');await page.getByLabel('Email',{exact:true}).fill('admin@example.com');await page.getByLabel('Password',{exact:true}).fill('CI-Admin-password-2026!');await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.getByText('Authentication complete.',{exact:true}).waitFor();
   await page.goto(origin+'/admin/platform/general');await page.getByRole('link',{name:'Configure customer sign-in providers',exact:true}).click();await page.getByRole('heading',{name:'OAuth provider configuration'}).waitFor();await page.getByLabel('Client ID',{exact:true}).fill('fixture-google-client');await page.getByLabel('Client secret',{exact:true}).fill('fixture-google-secret-not-real');await page.getByLabel('Authorization URL',{exact:true}).fill('https://accounts.google.com/o/oauth2/v2/auth');await page.getByLabel('Token URL',{exact:true}).fill('https://oauth2.googleapis.com/token');await page.getByLabel('User info URL',{exact:true}).fill('https://openidconnect.googleapis.com/v1/userinfo');await page.getByLabel('Scopes',{exact:true}).fill('openid email profile');await page.getByLabel('Reason for change',{exact:true}).fill('CI customer sign-in configuration');await page.getByRole('button',{name:'Save provider',exact:true}).click();await page.getByText('This change requires a recent two-factor verification.',{exact:false}).waitFor();
   await page.getByRole('link',{name:'Set up two-factor authentication',exact:true}).click();await page.getByRole('button',{name:'Enroll TOTP',exact:true}).click();const secret=await page.locator('[data-secret-once] code').innerText();await page.getByLabel('Six-digit code — TOTP confirmation code').fill(totp(secret));await page.getByRole('button',{name:'Confirm TOTP',exact:true}).click();await page.getByText('TOTP enabled',{exact:true}).waitFor();
   await page.goto(origin+'/admin/login');await page.getByLabel('Email',{exact:true}).fill('admin@example.com');await page.getByLabel('Password',{exact:true}).fill('CI-Admin-password-2026!');await page.getByLabel('TOTP code',{exact:true}).fill(totp(secret));await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.getByText('Authentication complete.',{exact:true}).waitFor();
   await page.goto(origin+'/admin/platform/oauth');await page.getByLabel('Client ID',{exact:true}).fill('fixture-google-client');await page.getByLabel('Client secret',{exact:true}).fill('fixture-google-secret-not-real');await page.getByLabel('Authorization URL',{exact:true}).fill('https://accounts.google.com/o/oauth2/v2/auth');await page.getByLabel('Token URL',{exact:true}).fill('https://oauth2.googleapis.com/token');await page.getByLabel('User info URL',{exact:true}).fill('https://openidconnect.googleapis.com/v1/userinfo');await page.getByLabel('Scopes',{exact:true}).fill('openid email profile');await page.getByLabel('Reason for change',{exact:true}).fill('CI customer sign-in configuration');await page.getByRole('button',{name:'Save provider',exact:true}).click();await page.getByText('Provider configuration saved. Client secret remains masked.',{exact:true}).waitFor();assert.equal(await page.getByLabel('Client secret',{exact:true}).inputValue(),'');await page.reload();await page.getByText('Secret status: Configured · masked',{exact:true}).waitFor();assert.equal(await page.getByLabel('Client ID',{exact:true}).inputValue(),'fixture-google-client');await page.screenshot({path:out+'/oauth-configuration.png',fullPage:true});
   await page.goto(origin+'/login');await ready();assert.equal(await page.getByRole('link',{name:'Google',exact:true}).count(),1);assert.equal(await page.getByText(/unavailable$/).count(),0);
   await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:out+'/customer-mobile.png',fullPage:true});
  }
  assert.deepEqual(errors,[]);await writeFile(out+'/customer-'+mode+'.json',JSON.stringify({passed:true,registration_mail:mode==='full',expired_verification_resend:true,existing_account_retained:mode==='after-upgrade',password_login:true,password_reset_mail:true,email_login_code:true,oauth_configuration:mode==='full',live_external_oauth_login:false}));
 }
}catch(error){await page.screenshot({path:out+'/customer-'+mode+'-failure.png',fullPage:true}).catch(()=>{});await writeFile(out+'/customer-'+mode+'-failure.txt',String(error)+'\n'+await page.locator('body').innerText().catch(()=>''));throw error;}finally{await browser.close();}
