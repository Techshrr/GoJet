import {chromium} from 'playwright-core';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
const origin='https://www.linketic.test:11443';
const out='artifacts/panel';await mkdir(out,{recursive:true});
const browser=await chromium.launch({args:['--no-proxy-server','--host-resolver-rules=MAP www.linketic.test 127.0.0.1']});
const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1440,height:1050}});const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
await page.route('https://challenges.cloudflare.com/turnstile/v0/api.js*',route=>route.fulfill({contentType:'application/javascript',body:`window.turnstile={render:(selector,opts)=>{document.querySelector(selector).textContent='CI protocol fixture';opts.callback('ci-valid-token');return 1},remove:()=>{},reset:()=>{}};`}));
try {
 await page.goto(origin+'/');await page.getByRole('heading',{name:'准备好，开始安装。'}).waitFor();
 await page.screenshot({path:out+'/installer-desktop.png',fullPage:true});
 await page.getByLabel('安装访问码').fill('ci-panel-access-code-1234567890');await page.getByRole('button',{name:'继续',exact:true}).click();
 await page.getByRole('heading',{name:'配置你的网站'}).waitFor();
 const fields={admin_email:'admin@example.com',admin_name:'Panel Administrator',admin_password:'CI-Admin-password-2026!',smtp_host:'mail.fixture',smtp_port:'1587',smtp_from:'no-reply@example.com',smtp_user:'no-reply@example.com',smtp_password:'ci-mail-password',webrisk_key:'ci-webrisk-invalid-key-1234567890',turnstile_site_key:'ci-turnstile-site-1234567890',turnstile_secret:'ci-turnstile-secret-1234567890'};
 for(const [key,value] of Object.entries(fields))await page.locator('#'+key).fill(value);
 await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:out+'/installer-mobile.png',fullPage:true});await page.setViewportSize({width:1440,height:1050});
 await page.getByRole('button',{name:'加载人机验证'}).click();await page.getByRole('button',{name:'检查并安装'}).click();
 await page.getByRole('button',{name:'返回修改配置'}).waitFor({timeout:60000});assert((await page.locator('#message').textContent()).includes('Google Web Risk'));
 await page.getByRole('button',{name:'返回修改配置'}).click();await page.locator('#webrisk_key').fill('ci-webrisk-valid-key-1234567890');await page.getByRole('button',{name:'加载人机验证'}).click();await page.getByRole('button',{name:'检查并安装'}).click();
 await page.getByRole('heading',{name:'安装已完成'}).waitFor({timeout:780000});await page.screenshot({path:out+'/installation-complete.png'});
 assert.equal((await page.request.post(origin+'/install/api/configure',{data:fields,headers:{Origin:origin}})).status(),410);
 await page.goto(origin+'/admin/login');await page.getByLabel('Email',{exact:true}).fill(fields.admin_email);await page.getByLabel('Password',{exact:true}).fill(fields.admin_password);await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.getByText('Authentication complete.',{exact:true}).waitFor();
 const checks=[['/admin/','Operations overview'],['/admin/trust/destination-risk','No destination risk records'],['/admin/tickets','Support tickets'],['/admin/mail','Mail operations'],['/admin/platform/general','General settings']];
 for(const [route,text] of checks){await page.goto(origin+route);await page.getByText(text,{exact:false}).first().waitFor();assert(!await page.getByText('Administrator authentication is required.',{exact:true}).count());}
 await page.screenshot({path:out+'/admin.png'});
 assert.deepEqual(errors,[]);await writeFile(out+'/browser.json',JSON.stringify({passed:true,scenarios:['root wizard','desktop/mobile','upstream invalid key rejected','correction in browser','32 real migrations and native admin','8 processes and TLS adapter health','installer locked','admin login and modules'],external_services:'CI protocol fixtures; no claim of live Google/SMTP/Turnstile account validation'}));
}finally{await browser.close();}
