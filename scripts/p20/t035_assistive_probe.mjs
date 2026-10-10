/** Supplemental native evidence, not a WCAG conformance declaration. */
import { readFileSync, statSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';

const hash = data => createHash('sha256').update(data).digest('hex');

export function textSpacingClipped(node) {
  const clipped = value => ['hidden','clip','auto','scroll'].includes(value);
  return (clipped(node.overflow_x) && node.scroll_width > node.client_width + 1) ||
    (clipped(node.overflow_y) && node.scroll_height > node.client_height + 1);
}

// Orca logs SPEECH OUTPUT even without a speech server. Require the real
// speech-dispatcher path too; a debug-only utterance cannot pass this sample.
export function speechReceipt(segment, name, role) {
  if (!name || name.length > 80 || /[@\r\n]|https?:/i.test(name)) return null;
  if (!['link', 'button'].includes(role)) return null;
  const output = segment.split('\n').filter(line => line.includes('SPEECH OUTPUT:'));
  const dispatch = segment.split('\n').filter(line => line.includes('SPEECH DISPATCHER:') && line.includes('Speaking'));
  // Orca can use separate voices/utterances for the label and role. Both must
  // come from the fresh focus interval and both must reach its dispatcher path.
  const named = line => line.toLowerCase().includes(name.toLowerCase());
  const typed = line => new RegExp(`\\b${role}\\b`, 'i').test(line);
  const utterance = output.find(named), sent = dispatch.find(named);
  const roleOnly = line => {
    const match = /(?:SPEECH OUTPUT: |Speaking )'([^']+)'/.exec(line);
    return match && /^(?:(?:visited|unvisited|push)\s+)?(?:link|button)[.\s]*$/i.test(match[1]);
  };
  // A prior control's delayed dispatch may land in this byte interval. Prefer
  // this control's combined name+role; a separate role must contain ONLY the
  // role, never another control's name.
  const roleUtterance = output.find(line => named(line) && typed(line)) || output.find(line => typed(line) && roleOnly(line));
  const roleSent = dispatch.find(line => named(line) && typed(line)) || dispatch.find(line => typed(line) && roleOnly(line));
  if (!utterance || !sent || !roleUtterance || !roleSent) return null;
  return {name, role, speech_output: utterance, dispatcher_output: sent,
    role_speech_output: roleUtterance, role_dispatcher_output: roleSent};
}

export async function assistiveProbe(page, surface, out) {
  const savedViewport = page.viewportSize();
  const savedTheme = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
  const savedScroll = await page.evaluate(() => ({x: scrollX, y: scrollY}));
  const focused = await page.evaluateHandle(() => document.activeElement);
  const session = await page.context().newCDPSession(page);
  const result = {zoom: [], text_spacing: [], navigation: [], screen_reader: {engine: 'Orca', engine_version: process.env.P20_ORCA_VERSION,
    scope: 'two distinct native Tab control announcements and dispatcher attempts; not audio playback or complete manual review', steps: [], attempts: []}};
  try {
    // Same 1440x900 physical area at 200%: 720x450 CSS pixels, DPR 2.
    // This is explicitly device-metrics-equivalent evidence, not a claim that
    // the browser toolbar zoom setting was changed.
    for (const theme of ['light', 'dark']) {
      await page.setViewportSize({width:720,height:450});
      await session.send('Emulation.clearDeviceMetricsOverride');
      await session.send('Emulation.setDeviceMetricsOverride', {
        width: 720, height: 450, deviceScaleFactor: 2, mobile: false,
        screenWidth: 1440, screenHeight: 900,
      });
      await page.evaluate(value => {
        document.documentElement.setAttribute('data-theme', value);
        scrollTo(0, 0);
      }, theme);
      await page.waitForTimeout(400);
      const layout = await page.evaluate(() => ({width: innerWidth, height: innerHeight,
        dpr: devicePixelRatio, scroll_width: document.documentElement.scrollWidth,
        scroll_height: Math.max(innerHeight, document.documentElement.scrollHeight),
        main_visible: [...document.querySelectorAll('main')].some(el => el.getClientRects().length > 0)}));
      const capture = `${surface}-zoom200-${theme}.png`;
      // Playwright fullPage restores its own metrics, which competes with this
      // supplemental CDP session. Capture directly on the owning session.
      const shot = await session.send('Page.captureScreenshot', {format:'png',fromSurface:true,
        captureBeyondViewport:true,clip:{x:0,y:0,width:720,height:layout.scroll_height,scale:1}});
      const png = Buffer.from(shot.data, 'base64');
      writeFileSync(`${out}/${capture}`, png);
      result.zoom.push({theme, method: '200%-device-metrics-equivalent', physical_viewport: {width:1440,height:900},
        layout, capture, capture_sha256: hash(png)});
      if (layout.width !== 720 || layout.height !== 450 || layout.dpr !== 2 || png.readUInt32BE(16) !== 1440 ||
          layout.scroll_width > 721 || !layout.main_visible) throw new Error(`200% equivalent reflow failed: ${theme}`);
    }
    await session.send('Emulation.clearDeviceMetricsOverride');
    if (['workspace','admin'].includes(surface)) {
      await page.setViewportSize({width:320,height:844});
      for (const theme of ['light','dark']) {
        await page.evaluate(value=>document.documentElement.setAttribute('data-theme',value),theme);
        const summary=page.locator('[data-mobile-navigation] > summary');
        for(let n=0;n<256 && !await summary.evaluate(el=>el===document.activeElement);n++) await page.keyboard.press('Tab');
        if(!await summary.evaluate(el=>el===document.activeElement)) throw Error('mobile navigation summary unreachable');
        await page.keyboard.press('Enter');
        await page.waitForFunction(()=>document.querySelector('[data-mobile-navigation]')?.open);
        const inventory=await page.locator('[data-mobile-navigation]').evaluate(el=>{
          const links=[...el.querySelectorAll('a[href]')];
          const desktop=[...document.querySelectorAll('aside a[href]')];
          return {count:links.length,expected:desktop.length,
            same_routes:desktop.every(a=>links.some(b=>a.getAttribute('href')===b.getAttribute('href'))),
            controls:[...el.querySelectorAll('button,select,a[href]')].filter(x=>!x.disabled).map(x=>[...document.querySelectorAll('*')].indexOf(x))};
        });
        const seen=new Set();const steps=[];
        for(let n=0;n<256 && seen.size<inventory.controls.length;n++) {
          await page.keyboard.press('Tab');
          const step=await page.locator('[data-mobile-navigation]').evaluate(el=>({
            element_index:[...document.querySelectorAll('*')].indexOf(document.activeElement),
            inside:el.contains(document.activeElement),tag:document.activeElement?.tagName}));
          steps.push(step);if(!step.inside) throw Error('expanded mobile navigation omitted a control');
          if(inventory.controls.includes(step.element_index))seen.add(step.element_index);
        }
        if(!inventory.same_routes || inventory.count!==inventory.expected || seen.size!==inventory.controls.length)
          throw Error('mobile navigation loses desktop routes or keyboard controls');
        const capture=`${surface}-navigation320-${theme}.png`;
        const png=await page.screenshot({path:`${out}/${capture}`,fullPage:true});
        await page.keyboard.press('Escape');
        if(!await summary.evaluate(el=>el===document.activeElement&&!el.parentElement.open))throw Error('mobile navigation Escape/focus return failed');
        result.navigation.push({theme,inventory,steps,escape_focus_return:true,capture,capture_sha256:hash(png)});
      }
    }
    // SC 1.4.12: real user stylesheet overrides, on the real current page.
    // This changes presentation only; no fixture content or API is inserted.
    await page.setViewportSize({width:320,height:844});
    // Inspector user styles are not an untrusted inline <style>. Public Text
    // keeps its immutable CSP; never enable bypassCSP or weaken that policy.
    await session.send('DOM.enable');
    await session.send('CSS.enable');
    const origins=new Map();
    session.on('CSS.styleSheetAdded',({header})=>origins.set(header.styleSheetId,header.origin));
    const {frameTree}=await session.send('Page.getFrameTree');
    const {styleSheetId}=await session.send('CSS.createStyleSheet',{frameId:frameTree.frame.id});
    for(let i=0;i<20 && !origins.has(styleSheetId);i++)await page.waitForTimeout(50);
    if(origins.get(styleSheetId)!=='inspector')throw Error('text-spacing user stylesheet origin unproven');
    await session.send('CSS.setStyleSheetText',{styleSheetId,text:'* { line-height: 1.5 !important; letter-spacing: .12em !important; word-spacing: .16em !important; } p { margin-bottom: 2em !important; }'});
    try {
      for (const theme of ['light','dark']) {
        await page.evaluate(value=>{document.documentElement.setAttribute('data-theme',value);scrollTo(0,0);},theme);
        await page.waitForTimeout(300);
        const layout = await page.evaluate(()=>{const style=getComputedStyle(document.body);return {width:innerWidth,scroll_width:document.documentElement.scrollWidth,
          override:{font_size:parseFloat(style.fontSize),line_height:parseFloat(style.lineHeight),letter_spacing:parseFloat(style.letterSpacing),word_spacing:parseFloat(style.wordSpacing)},
          nodes:[...document.querySelectorAll('main h1,main h2,main h3,main button,main a,main label,main dd,main code,main p')]
            .filter(el=>el instanceof HTMLElement && el.getClientRects().length && el.clientWidth>0)
            .map(el=>{const style=getComputedStyle(el);return {
              element_index:[...document.querySelectorAll('*')].indexOf(el),tag:el.tagName,
              client_width:el.clientWidth,client_height:el.clientHeight,scroll_width:el.scrollWidth,scroll_height:el.scrollHeight,
              overflow_x:style.overflowX,overflow_y:style.overflowY};})};});
        layout.clipped=layout.nodes.filter(textSpacingClipped);
        const capture=`${surface}-textspacing320-${theme}.png`;
        const png=await page.screenshot({path:`${out}/${capture}`,fullPage:true});
        result.text_spacing.push({theme,method:'WCAG-1.4.12-inspector-user-stylesheet',origin:'inspector',layout,capture,capture_sha256:hash(png)});
        if (layout.width!==320 || layout.scroll_width>321 || layout.clipped.length) throw new Error(`text-spacing reflow failed: ${theme}`);
      }
    } finally { await session.send('CSS.setStyleSheetText',{styleSheetId,text:''}); }
    if (savedViewport) await page.setViewportSize(savedViewport);
    const log = process.env.P20_ORCA_LOG;
    if (!log) throw new Error('native Orca session was not started');
    const orcaPid = Number(process.env.P20_ORCA_PID);
    if (!Number.isInteger(orcaPid) || orcaPid <= 0) throw new Error('native Orca process binding missing');
    process.kill(orcaPid, 0);
    await page.bringToFront();
    await page.waitForTimeout(1000);
    const windowId=execFileSync('xdotool',['getwindowfocus'],{encoding:'utf8'}).trim();
    if(!/^\d+$/.test(windowId))throw Error('native screen-reader window binding absent');
    // search --class is supported by Ubuntu's packaged xdotool versions;
    // getwindowclassname is newer and unavailable on some CI images.
    const chromeWindows=execFileSync('xdotool',['search','--onlyvisible','--class','chrome|chromium'],{encoding:'utf8'}).trim().split(/\s+/);
    if(!chromeWindows.includes(windowId) || !await page.evaluate(()=>document.hasFocus()))
      throw Error('native screen-reader Chrome window is not focused');
    result.screen_reader.input_authority='X11 XTEST keyboard; active Chrome window and DOM focus checked';
    const visited = new Set();
    const attempts = new Map();
    for (let i = 0; i < 128 && result.screen_reader.steps.length < 2; i++) {
      const start = statSync(log).size;
      process.kill(orcaPid, 0);
      execFileSync('xdotool',['key','--clearmodifiers','Tab']);
      await page.waitForTimeout(100);
      const target = await page.evaluate(() => {
        const el = document.activeElement;
        if (!el || !['A', 'BUTTON'].includes(el.tagName)) return null;
        const name = (el.getAttribute('aria-label') || el.textContent || '').trim().replace(/\s+/g, ' ');
        return {name, role: el.tagName === 'A' ? 'link' : 'button', element_index: [...document.querySelectorAll('*')].indexOf(el)};
      });
      if (!target || visited.has(target.element_index) || !target.name || target.name.length > 80 || /[@\r\n]|https?:/i.test(target.name)) continue;
      if((attempts.get(target.element_index)||0)>=3)continue;
      attempts.set(target.element_index,(attempts.get(target.element_index)||0)+1);
      let receipt;
      for (let n = 0; n < 12 && !receipt; n++) {
        await page.waitForTimeout(250);
        receipt = speechReceipt(readFileSync(log).subarray(start).toString('utf8'), target.name, target.role);
      }
      if (!receipt) {
        const segment=readFileSync(log).subarray(start).toString('utf8');
        result.screen_reader.attempts.push({...target,input:'native-X11-Tab',log_start:start,log_end:statSync(log).size,
          document_has_focus:await page.evaluate(()=>document.hasFocus()),
          speech_output_lines:segment.split('\n').filter(x=>x.includes('SPEECH OUTPUT:')).length,
          dispatcher_lines:segment.split('\n').filter(x=>x.includes('SPEECH DISPATCHER:')&&x.includes('Speaking')).length});
        continue;
      }
      visited.add(target.element_index);
      result.screen_reader.steps.push({...receipt, element_index: target.element_index, input: 'native-X11-Tab',
        log_start: start, log_end: statSync(log).size});
    }
    if (result.screen_reader.steps.length !== 2) throw new Error('Orca did not produce two fresh named-control speech receipts');
    return result;
  } catch (error) {
    error.assistive = result;
    throw error;
  } finally {
    await session.send('Emulation.clearDeviceMetricsOverride');
    await session.detach();
    if (savedViewport) await page.setViewportSize(savedViewport);
    await page.evaluate(theme => theme === null ? document.documentElement.removeAttribute('data-theme') : document.documentElement.setAttribute('data-theme', theme), savedTheme);
    await focused.evaluate(el => {if (el instanceof HTMLElement && el.isConnected) el.focus({preventScroll:true});});
    await focused.dispose();
    await page.evaluate(({x,y}) => scrollTo(x,y), savedScroll);
  }
}
