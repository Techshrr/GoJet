import {chromium} from '/tmp/gojet-motion/node_modules/playwright-core/index.mjs';
import {readFileSync} from 'node:fs';
import assert from 'node:assert/strict';
import {observeNativeVisual,settleNativeVisualMotion} from './t034_visual_probe.mjs';
const css=readFileSync('frontend/packages/tokens/generated/tokens.css','utf8')+readFileSync('frontend/packages/ui/src/styles.css','utf8').replace(/^@import.*$/m,'');
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
try {
 for(const open of [false,true]){
  const page=await browser.newPage();await page.emulateMedia({reducedMotion:'reduce'});
  await page.setContent(`<style>${css}</style><button class="gj-button">Visible</button><details ${open?'open':''}><summary>Menu</summary><button class="gj-button">Hidden Create</button><nav><a href="#">Hidden link</a></nav></details>`);
  for(const theme of ['light','dark']){
   await page.evaluate(t=>document.documentElement.setAttribute('data-theme',t),theme);await page.waitForTimeout(200);
   const initial=await observeNativeVisual(page,[]);
   const settled=await settleNativeVisualMotion(page,160);
   const final=await observeNativeVisual(page,[]);
   assert.equal(settled.failure,null);assert.equal(final.active_animations,0);
   console.log(JSON.stringify({open,theme,initial:initial.active_animations,settled,final:final.active_animations}));
  }
  await page.close();
 }
 const page=await browser.newPage();
 await page.setContent('<style>@keyframes loop{to{opacity:.5}}button{animation:loop 120ms infinite}</style><button>Repeat</button>');
 const rejected=await settleNativeVisualMotion(page,160);assert.ok(rejected.failure);
 assert.ok((await observeNativeVisual(page,[])).active_animations>0);
 console.log(JSON.stringify({negative:'persistent animation rejected without cancellation',rejected}));
 await page.close();
}finally{await browser.close()}
