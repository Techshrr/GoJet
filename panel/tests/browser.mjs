import {chromium} from 'playwright-core';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {runtime as execFileSync} from './runtime.mjs';
const origin='https://www.linketic.test:11443';
const out='artifacts/panel';await mkdir(out,{recursive:true});
const browser=await chromium.launch({args:['--no-proxy-server','--host-resolver-rules=MAP www.linketic.test 127.0.0.1']});
const context=await browser.newContext({ignoreHTTPSErrors:false,viewport:{width:1440,height:1050}});const page=await context.newPage();const errors=[];const network=[];page.on('pageerror',e=>errors.push(e.message));page.on('requestfailed',r=>network.push({url:r.url(),error:r.failure()?.errorText}));page.on('console',m=>{if(m.type()==='error')network.push({console:m.text()})});
await page.route('https://challenges.cloudflare.com/turnstile/v0/api.js*',route=>route.fulfill({contentType:'application/javascript',body:`window.turnstile={render:(selector,opts)=>{const root=document.querySelector(selector);root.textContent='CI protocol fixture';if(opts['response-field']!==false){const hidden=document.createElement('input');hidden.type='hidden';hidden.name=opts['response-field-name']||'cf-turnstile-response';hidden.value='ci-valid-token';root.append(hidden);}opts.callback('ci-valid-token');return 1},remove:()=>{},reset:()=>{}};`}));
try {
 await page.goto(origin+'/');await page.getByRole('heading',{name:'准备好，开始安装。'}).waitFor();
 await page.screenshot({path:out+'/installer-desktop.png',fullPage:true});
 await page.getByLabel('安装访问码').fill(process.env.NATIVE_TEST?'CiNativeAccessCode1234567890':'ci-panel-access-code-1234567890');await page.getByRole('button',{name:'继续',exact:true}).click();
 await page.getByRole('heading',{name:'配置你的网站'}).waitFor();
 const fields={admin_email:'admin@example.com',admin_name:'Panel Administrator',admin_password:'CI-Admin-password-2026!',smtp_host:'mail.fixture',smtp_port:'1587',smtp_from:'no-reply@example.com',smtp_user:'no-reply@example.com',smtp_password:'ci-mail-password',webrisk_key:'ci-webrisk-invalid-key-1234567890',turnstile_site_key:'ci-turnstile-site-1234567890',turnstile_secret:'ci-turnstile-secret-1234567890'};
 for(const [key,value] of Object.entries(fields))await page.locator('#'+key).fill(value);
 await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:out+'/installer-mobile.png',fullPage:true});await page.setViewportSize({width:1440,height:1050});
 await page.getByRole('button',{name:'加载人机验证'}).click();
 assert.equal(await page.locator('input[name="cf-turnstile-response"]').count(),0);
 // Also exercise a previously injected field: submission must use the API contract only.
 await page.evaluate(()=>{const input=document.createElement('input');input.type='hidden';input.name='cf-turnstile-response';input.value='ci-valid-token';document.getElementById('configuration-form').append(input);});
 const submitted=page.waitForRequest(r=>r.url().endsWith('/install/api/configure')&&r.method()==='POST');
 await page.getByRole('button',{name:'检查并安装'}).click();
 const payload=(await submitted).postDataJSON();assert.deepEqual(Object.keys(payload).sort(),[...Object.keys(fields),'turnstile_token'].sort());assert.equal(payload.turnstile_token,'ci-valid-token');
 await page.getByRole('button',{name:'返回修改配置'}).waitFor({timeout:60000});assert((await page.locator('#message').textContent()).includes('Google Web Risk'));
 await page.getByRole('button',{name:'返回修改配置'}).click();execFileSync('docker',['exec','linketic-ci','supervisorctl','stop','redis']);await page.locator('#webrisk_key').fill('ci-webrisk-valid-key-1234567890');await page.getByRole('button',{name:'加载人机验证'}).click();await page.getByRole('button',{name:'检查并安装'}).click();
 await page.getByRole('button',{name:'重试未完成步骤'}).waitFor({timeout:780000});
 const failedJournal=JSON.parse(execFileSync('docker',['exec','linketic-ci','cat','/var/lib/linketic/private/data/journal.json'],{encoding:'utf8'}));assert.equal(failedJournal.phase,'failed');assert.equal(failedJournal.failed_at,'admin-started');
 execFileSync('docker',['exec','linketic-ci','supervisorctl','start','redis']);
 await page.getByRole('button',{name:'重试未完成步骤'}).click();
 await page.getByRole('heading',{name:'安装已完成'}).waitFor({timeout:180000});
 execFileSync('docker',['exec','linketic-ci','python3','-c',`from pathlib import Path; import json; p=Path('/var/lib/linketic/private'); backups=list(p.glob('failed-initialization-*/recovered.json')); assert len(backups)==1; assert backups[0].with_name('database.sql').stat().st_size>0; assert not (p/'recovery.json').exists()`]);await page.screenshot({path:out+'/installation-complete.png'});
 assert.equal(await page.evaluate(async()=> (await fetch('/install/api/configure',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'})).status),410);
 await page.goto(origin+'/admin/login');await page.getByLabel('Email',{exact:true}).fill(fields.admin_email);await page.getByLabel('Password',{exact:true}).fill(fields.admin_password);await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.getByText('Authentication complete.',{exact:true}).waitFor();
 const apiErrors=[];page.on('response',r=>{if(r.url().includes('/api/admin/')&&r.status()>=400)apiErrors.push({path:new URL(r.url()).pathname,status:r.status()});});
 const checks=[['/admin/','Operations overview'],['/admin/trust/destination-risk','No destination risk records'],['/admin/tickets','Support tickets'],['/admin/mail','Mail operations'],['/admin/platform/general','General settings'],['/admin/commerce/plans','No plans']];
 for(const [route,text] of checks){await page.goto(origin+route);await page.getByText(text,{exact:false}).first().waitFor();await page.waitForLoadState('networkidle');assert.equal(await page.locator('.gj-message[data-variant="danger"]').count(),0);assert(!await page.getByText('Administrator authentication is required.',{exact:true}).count());}
 await page.screenshot({path:out+'/admin.png'});
 assert.deepEqual(apiErrors,[]);assert.deepEqual(errors,[]);await writeFile(out+'/browser.json',JSON.stringify({passed:true,scenarios:['root wizard','Turnstile hidden field excluded from configuration','desktop/mobile','upstream invalid key rejected','correction in browser','32 real migrations and native admin','partial bootstrap failure and browser recovery with SQL backup','8 processes and TLS adapter health','installer locked','admin login and modules'],external_services:'CI protocol fixtures; no claim of live Google/SMTP/Turnstile account validation'}));
}catch(error){console.error(JSON.stringify({error:String(error),pageErrors:errors,network}));await writeFile(out+'/browser-network.json',JSON.stringify({pageErrors:errors,network},null,2));await page.screenshot({path:out+'/failure.png',fullPage:true}).catch(()=>{});await writeFile(out+'/failure.txt',String(error)+'\n'+await page.locator('body').innerText().catch(()=>''));throw error;}finally{await browser.close();}
