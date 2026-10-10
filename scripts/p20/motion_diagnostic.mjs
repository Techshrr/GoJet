import { chromium } from '/tmp/gojet-motion/node_modules/playwright-core/index.mjs';
import { readFileSync } from 'node:fs';
const css=readFileSync('frontend/packages/tokens/generated/tokens.css','utf8')+readFileSync('frontend/packages/ui/src/styles.css','utf8').replace(/^@import.*$/m,'');
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
for(const open of [false,true]){
 const page=await browser.newPage();await page.emulateMedia({reducedMotion:'reduce'});
 await page.setContent(`<style>${css}</style><button class="gj-button">Visible</button><details ${open?'open':''}><summary>Menu</summary><button class="gj-button">Hidden Create</button><nav><a href="#">Hidden link</a></nav></details>`);
 for(const theme of ['light','dark']){
  await page.evaluate(t=>document.documentElement.setAttribute('data-theme',t),theme);await page.waitForTimeout(200);
  const before=await page.evaluate(()=>document.getAnimations().length);
  const after=await page.evaluate(()=>{
   [...document.querySelectorAll('a')].forEach(a=>a.getClientRects());
   return document.getAnimations().map(a=>({closed:!!a.effect.target.closest('details:not([open])'),tag:a.effect.target.tagName,property:a.transitionProperty,time:a.currentTime,duration:a.effect.getComputedTiming().duration}));
  });
  await page.waitForTimeout(500);
  const later=await page.evaluate(()=>document.getAnimations().map(a=>({property:a.transitionProperty,time:a.currentTime,state:a.playState})));
  console.log(JSON.stringify({open,theme,before,after,later}));
 }
 await page.close();
}
await browser.close();
